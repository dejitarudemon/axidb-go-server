package runtime_v1

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
)

// batchResult is the outcome of one nested command.
// result is set only when err is nil.
type batchResult struct {
	req    bodies.Request
	result frame.Frame
	err    error
}

// batchTrace is the shared interrupt state of one batch.
//
// halt means later commands must not run. traceback is the id reused by their
// RequestInterrupted answers: the first command failure when there is one,
// otherwise the id created for a cancelled context.
type batchTrace struct {
	mu        sync.Mutex
	traceback fields.TracebackID
	hasTrace  bool
	halt      bool
}

// remember stores id when this batch has no traceback yet.
func (t *batchTrace) remember(id fields.TracebackID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.hasTrace {
		return
	}

	t.traceback = id
	t.hasTrace = true
}

// interrupted returns RequestInterrupted for requestID, reusing the stored traceback.
// The first call stores a new traceback ID.
func (t *batchTrace) interrupted(requestID fields.RequestID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.hasTrace {
		err := protocolerrs.NewErrorRequestInterrupted(requestID)
		t.traceback = err.TracebackID()
		t.hasTrace = true
		return err
	}

	return protocolerrs.NewErrorRequestInterruptedWithTracebackID(requestID, t.traceback)
}

// handleBatch yields the answers for one batch.
//
// IsSequentialExecution runs the nested commands in request-number order.
// Otherwise they run concurrently, at most [Runtime.maxGoroutinePerBatch] at
// once. InterruptAfterError does not run commands that have not started after
// the first failure. Each of them is answered with
// [protocolerrs.ErrorRequestInterrupted] and the traceback ID of that failure.
// The failed command keeps its own error. A command already running finishes
// with its own result. IsOneAnswer yields one batch answer. Otherwise each
// command yields its own frame. yield is called from this goroutine. A false
// yield, or an encoding failure, stops the batch. A cancelled ctx is answered
// as [protocolerrs.ErrorRequestInterrupted]: the command that observed it and
// every command that has not started share one traceback ID and are not executed.
func (r Runtime) handleBatch(ctx Context, b body.Body) FrameIterator {
	return func(yield yieldFrameIterator) {
		bb, _ := b.(bodies.Batch)

		if bb.IsSequentialExecution {
			bb.Sort()
			r.yieldSequential(ctx, bb.IsOneAnswer, bb.InterruptAfterError, bb.Requests, yield)
			return
		}

		r.yieldParallel(ctx, bb.IsOneAnswer, bb.InterruptAfterError, bb.Requests, yield)
	}
}

// yieldSequential runs requests one by one, in their current order.
// oneAnswer yields a single batch answer after every command has a result.
// Otherwise each result is yielded before the next command starts, so a false
// yield does not run the rest.
func (r Runtime) yieldSequential(ctx Context, oneAnswer bool, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	trace := &batchTrace{}

	if oneAnswer {
		done := make([]batchResult, 0, len(requests))
		for _, req := range requests {
			done = append(done, r.sequentialResult(ctx, interrupt, trace, req))
		}

		r.yieldCombinedAnswer(ctx, done, yield)
		return
	}

	for _, req := range requests {
		got := r.sequentialResult(ctx, interrupt, trace, req)
		if !r.yieldCommandAnswer(ctx, got.result, got.err, yield) {
			return
		}
	}
}

// sequentialResult runs req, or answers RequestInterrupted when the batch has halted.
// A cancelled ctx halts the batch. InterruptAfterError halts it after the first
// command error and keeps that error's traceback ID.
func (r Runtime) sequentialResult(ctx Context, interrupt bool, trace *batchTrace, req bodies.Request) batchResult {
	if trace.halt || ctx.Err() != nil {
		trace.halt = true
		return batchResult{req: req, err: trace.interrupted(ctx.RequestID())}
	}

	result, err := r.runBatchRequest(ctx, req)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		trace.halt = true
		return batchResult{req: req, err: trace.interrupted(ctx.RequestID())}
	}
	if err != nil {
		cause := r.toProtocolError(err)
		if interrupt {
			trace.remember(cause.TracebackID())
			trace.halt = true
		}

		return batchResult{req: req, err: cause}
	}

	return batchResult{req: req, result: result}
}

// yieldParallel runs requests on a limited number of goroutines.
// Frames are yielded as commands finish. One answer waits for every result
// and then orders them by request number.
func (r Runtime) yieldParallel(ctx Context, oneAnswer bool, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	incoming, cancel := r.runParallel(ctx, interrupt, requests)
	defer cancel()

	if !oneAnswer {
		for got := range incoming {
			if !r.yieldCommandAnswer(ctx, got.result, got.err, yield) {
				return
			}
		}

		return
	}

	done := make([]batchResult, 0, len(requests))
	for got := range incoming {
		done = append(done, got)
	}

	slices.SortFunc(done, func(a, b batchResult) int {
		return cmp.Compare(a.req.Number, b.req.Number)
	})
	r.yieldCombinedAnswer(ctx, done, yield)
}

// parallelRun is one parallel batch: the shared context, the interrupt flag,
// and the traceback reused by RequestInterrupted answers.
type parallelRun struct {
	runtime   Runtime
	parent    Context
	run       Context
	cancel    context.CancelFunc
	interrupt bool
	trace     *batchTrace
}

// runParallel starts workers and returns their results.
// The caller must call cancel when it no longer reads results. Each request
// produces one result. The results channel buffers every request, so workers
// can finish after the caller has stopped reading.
func (r Runtime) runParallel(parent Context, interrupt bool, requests []bodies.Request) (<-chan batchResult, context.CancelFunc) {
	runCtx, cancel := context.WithCancel(parent.Context)
	results := make(chan batchResult, len(requests))
	if len(requests) == 0 {
		close(results)
		return results, cancel
	}

	run := &parallelRun{
		runtime:   r,
		parent:    parent,
		run:       NewContext(runCtx, parent.Login(), parent.RequestID(), parent.IsExternal()),
		cancel:    cancel,
		interrupt: interrupt,
		trace:     &batchTrace{},
	}

	jobs := make(chan bodies.Request)
	var wg sync.WaitGroup
	for range r.batchWorkers(len(requests)) {
		wg.Go(func() {
			for req := range jobs {
				results <- run.result(req)
			}
		})
	}

	go func() {
		defer func() {
			close(jobs)
			wg.Wait()
			close(results)
		}()

		for _, req := range requests {
			if run.run.Err() != nil {
				results <- batchResult{req: req, err: run.trace.interrupted(parent.RequestID())}
				continue
			}

			select {
			case jobs <- req:
			case <-run.run.Done():
				results <- batchResult{req: req, err: run.trace.interrupted(parent.RequestID())}
			}
		}
	}()

	return results, cancel
}

// result runs req unless this batch was already cancelled or interrupted.
// A handler error cancels the shared context when interrupt is set. A command
// that observes that cancellation is answered with RequestInterrupted.
func (p *parallelRun) result(req bodies.Request) batchResult {
	if p.parent.Err() != nil || p.run.Err() != nil {
		return batchResult{req: req, err: p.trace.interrupted(p.parent.RequestID())}
	}

	result, err := p.runtime.runBatchRequest(p.run, req)
	if p.parent.Err() != nil || errors.Is(err, context.Canceled) {
		return batchResult{req: req, err: p.trace.interrupted(p.parent.RequestID())}
	}
	if err == nil {
		return batchResult{req: req, result: result}
	}
	if !p.interrupt {
		return batchResult{req: req, err: p.runtime.toProtocolError(err)}
	}

	cause := p.runtime.toProtocolError(err)
	p.trace.remember(cause.TracebackID())
	p.cancel()

	return batchResult{req: req, err: cause}
}

// batchWorkers returns how many goroutines run this batch.
// The count is [Runtime.maxGoroutinePerBatch], and never more than requests.
// A non-positive limit still runs one worker, so the job queue cannot block forever.
func (r Runtime) batchWorkers(requests int) int {
	return min(requests, max(r.maxGoroutinePerBatch, 1))
}

// yieldCommandAnswer yields one encoded answer for a nested command.
// false means the caller stopped reading, or the answer could not be encoded.
func (r Runtime) yieldCommandAnswer(ctx Context, result frame.Frame, cause error, yield yieldFrameIterator) bool {
	if cause != nil {
		encoded, err := r.writeErrAnswer(ctx.RequestID(), cause)
		return yield(encoded, err) && err == nil
	}

	encoded, err := encodeFrame(result)
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return false
	}

	return yield(encoded, nil)
}

// yieldCombinedAnswer encodes results as one batch answer and yields it.
// results stay in the order they were passed.
func (r Runtime) yieldCombinedAnswer(ctx Context, results []batchResult, yield yieldFrameIterator) {
	built := builder.NewBatchResultsBuilder()
	for _, got := range results {
		if got.err != nil {
			built.AddError(got.req.Number, r.toProtocolError(got.err))
			continue
		}

		addBatchSuccess(built, got.req.Number, got.req.Body.Command(), got.result)
	}

	frame, err := builder.NewFrameBuilder(r.limit).NewBatchAnswer(ctx.RequestID(), *built)
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	encoded, err := encodeFrame(frame)
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(encoded, nil)
}

// runBatchRequest runs one nested read, write, or delete.
// The returned frame is set only when err is nil.
func (r Runtime) runBatchRequest(ctx Context, req bodies.Request) (frame.Frame, error) {
	if req.Body == nil {
		return frame.Frame{}, protocolerrs.NewErrorUnsupportedCommand(fields.Command(0))
	}

	switch req.Body.Command() {
	case fields.Read:
		return r.handleRead(ctx, req.Body)
	case fields.Write:
		return r.handleWrite(ctx, req.Body)
	case fields.Delete:
		return r.handleDelete(ctx, req.Body)
	default:
		return frame.Frame{}, protocolerrs.NewErrorUnexpectedCommandInBatch(req.Body.Command(), req.Number)
	}
}

// addBatchSuccess records a successful nested answer under number.
func addBatchSuccess(results *builder.BatchResultsBuilder, number fields.RequestNumber, command fields.Command, result frame.Frame) {
	switch command {
	case fields.Read:
		answer, _ := result.Body.(bodies.ReadAnswer)
		results.AddRead(number, answer.Value)
	case fields.Write:
		results.AddWrite(number)
	case fields.Delete:
		results.AddDelete(number)
	default:
		results.AddError(number, protocolerrs.NewErrorUnexpectedCommand(command, fields.Read))
	}
}
