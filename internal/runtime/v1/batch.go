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

// firstTraceback stores the traceback ID of the first failed command.
// Later RequestInterrupted errors reuse it.
type firstTraceback struct {
	mu sync.Mutex
	id fields.TracebackID
	ok bool
}

// remember stores id when it is the first failure.
func (t *firstTraceback) remember(id fields.TracebackID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ok {
		return
	}

	t.id = id
	t.ok = true
}

// get returns the traceback ID stored by remember.
func (t *firstTraceback) get() fields.TracebackID {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.id
}

// interrupted returns RequestInterrupted for requestID using the stored traceback ID.
func (t *firstTraceback) interrupted(requestID fields.RequestID) error {
	return protocolerrs.NewErrorRequestInterruptedWithTracebackID(requestID, t.get())
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
// yield, or an encoding failure, stops the batch.
func (r Runtime) handleBatch(ctx Context, b body.Body) FrameIterator {
	return func(yield yieldFrameIterator) {
		bb, _ := b.(bodies.Batch)

		if bb.IsSequentialExecution {
			bb.Sort()
			if bb.IsOneAnswer {
				r.yieldSequentialOneBatchAnswer(ctx, bb.InterruptAfterError, bb.Requests, yield)
				return
			}

			r.yieldSequentialBatchFrames(ctx, bb.InterruptAfterError, bb.Requests, yield)
			return
		}

		if bb.IsOneAnswer {
			r.yieldParallelOneBatchAnswer(ctx, bb.InterruptAfterError, bb.Requests, yield)
			return
		}

		r.yieldParallelBatchFrames(ctx, bb.InterruptAfterError, bb.Requests, yield)
	}
}

// yieldSequentialBatchFrames yields one encoded answer per nested command, in order.
// After the first failure, later commands are answered with RequestInterrupted
// and are not executed when interrupt is set. Those answers reuse the first
// error's traceback ID.
func (r Runtime) yieldSequentialBatchFrames(ctx Context, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	interrupted := false
	tracebackID := fields.TracebackID{}

	for _, req := range requests {
		if interrupted {
			if !r.yieldCommandAnswer(ctx, frame.Frame{}, protocolerrs.NewErrorRequestInterruptedWithTracebackID(ctx.RequestID(), tracebackID), yield) {
				return
			}

			continue
		}

		result, err := r.runBatchRequest(ctx, req)
		if err != nil {
			cause := r.toProtocolError(err)
			if interrupt {
				tracebackID = cause.TracebackID()
			}

			if !r.yieldCommandAnswer(ctx, frame.Frame{}, cause, yield) {
				return
			}

			interrupted = interrupt
			continue
		}

		if !r.yieldCommandAnswer(ctx, result, nil, yield) {
			return
		}
	}
}

// yieldSequentialOneBatchAnswer runs the nested commands in order and yields one batch answer.
// Commands after the first failure are included as RequestInterrupted and are
// not executed when interrupt is set. Those results reuse the first error's traceback ID.
func (r Runtime) yieldSequentialOneBatchAnswer(ctx Context, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	results := builder.NewBatchResultsBuilder()
	interrupted := false
	tracebackID := fields.TracebackID{}

	for _, req := range requests {
		if interrupted {
			results.AddError(req.Number, protocolerrs.NewErrorRequestInterruptedWithTracebackID(ctx.RequestID(), tracebackID))
			continue
		}

		result, err := r.runBatchRequest(ctx, req)
		if err != nil {
			cause := r.toProtocolError(err)
			results.AddError(req.Number, cause)
			if interrupt {
				tracebackID = cause.TracebackID()
			}

			interrupted = interrupt
			continue
		}

		addBatchSuccess(results, req.Number, req.Body.Command(), result)
	}

	r.yieldBuiltBatchAnswer(ctx, results, yield)
}

// yieldParallelBatchFrames runs the nested commands concurrently and yields one
// encoded answer as each command finishes. Commands that have not started after
// the first failure are answered with RequestInterrupted when interrupt is set.
func (r Runtime) yieldParallelBatchFrames(ctx Context, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	results, cancel := r.runParallelBatch(ctx, interrupt, requests)
	defer cancel()

	for got := range results {
		if !r.yieldCommandAnswer(ctx, got.result, got.err, yield) {
			return
		}
	}
}

// yieldParallelOneBatchAnswer runs the nested commands concurrently and yields one batch answer.
// Results are ordered by request number. Commands that have not started after
// the first failure are included as RequestInterrupted when interrupt is set.
func (r Runtime) yieldParallelOneBatchAnswer(ctx Context, interrupt bool, requests []bodies.Request, yield yieldFrameIterator) {
	incoming, cancel := r.runParallelBatch(ctx, interrupt, requests)
	defer cancel()

	done := make([]batchResult, 0, len(requests))
	for got := range incoming {
		done = append(done, got)
	}

	slices.SortFunc(done, func(a, b batchResult) int {
		return cmp.Compare(a.req.Number, b.req.Number)
	})

	results := builder.NewBatchResultsBuilder()
	for _, got := range done {
		if got.err != nil {
			results.AddError(got.req.Number, r.toProtocolError(got.err))
			continue
		}

		addBatchSuccess(results, got.req.Number, got.req.Body.Command(), got.result)
	}

	r.yieldBuiltBatchAnswer(ctx, results, yield)
}

// runParallelBatch runs requests on at most [Runtime.maxGoroutinePerBatch] goroutines.
// Fewer workers are started when the batch is smaller than the limit.
// The caller must call cancel when it no longer reads results. Each request
// produces one result. The results channel buffers every request, so workers
// can finish after the caller has stopped reading.
func (r Runtime) runParallelBatch(parent Context, interrupt bool, requests []bodies.Request) (<-chan batchResult, context.CancelFunc) {
	runCtx, cancel := context.WithCancel(parent.Context)
	run := NewContext(runCtx, parent.Login(), parent.RequestID(), parent.IsExternal())
	results := make(chan batchResult, len(requests))

	if len(requests) == 0 {
		close(results)
		return results, cancel
	}

	jobs := make(chan bodies.Request)
	traceback := &firstTraceback{}

	var wg sync.WaitGroup
	for range r.batchWorkers(len(requests)) {
		wg.Go(func() {
			for req := range jobs {
				results <- r.runParallelRequest(parent, run, interrupt, cancel, traceback, req)
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
			if interrupt && run.Err() != nil && parent.Err() == nil {
				results <- batchResult{req: req, err: traceback.interrupted(parent.RequestID())}
				continue
			}

			select {
			case jobs <- req:
			case <-run.Done():
				if interrupt && parent.Err() == nil {
					results <- batchResult{req: req, err: traceback.interrupted(parent.RequestID())}
					continue
				}

				return
			}
		}
	}()

	return results, cancel
}

// batchWorkers returns how many goroutines run this batch.
// The count is [Runtime.maxGoroutinePerBatch], and never more than requests.
// A non-positive limit still runs one worker, so the job queue cannot block forever.
func (r Runtime) batchWorkers(requests int) int {
	return min(requests, max(r.maxGoroutinePerBatch, 1))
}

// runParallelRequest runs req unless this batch was already interrupted.
// A handler error cancels the shared context when interrupt is set. A command
// that observes that cancellation is answered with RequestInterrupted and the
// first failure's traceback ID. A command that finishes keeps its own result.
func (r Runtime) runParallelRequest(parent Context, run Context, interrupt bool, cancel context.CancelFunc, traceback *firstTraceback, req bodies.Request) batchResult {
	if interrupt && run.Err() != nil && parent.Err() == nil {
		return batchResult{req: req, err: traceback.interrupted(parent.RequestID())}
	}

	result, err := r.runBatchRequest(run, req)
	if err == nil {
		return batchResult{req: req, result: result}
	}

	if !interrupt {
		return batchResult{req: req, err: r.toProtocolError(err)}
	}

	if parent.Err() == nil && errors.Is(err, context.Canceled) {
		return batchResult{req: req, err: traceback.interrupted(parent.RequestID())}
	}

	cause := r.toProtocolError(err)
	traceback.remember(cause.TracebackID())
	cancel()

	return batchResult{req: req, err: cause}
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

// yieldBuiltBatchAnswer encodes results as one batch answer and yields it.
func (r Runtime) yieldBuiltBatchAnswer(ctx Context, results *builder.BatchResultsBuilder, yield yieldFrameIterator) {
	frame, err := builder.NewFrameBuilder(r.limit).NewBatchAnswer(ctx.RequestID(), *results)
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
