package runtime_v1

import (
	"context"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// handleRequest runs the handler for body and returns the encoded answer.
//
// Read, write, delete, and ping are dispatched to their handlers. Batch is
// handled by [Runtime.handleBatch], not here. A handler error is answered to
// the client. An encoding failure is answered to the client as well.
func (r Runtime) handleRequest(ctx Context, body body.Body, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), errs.NewErrorRequestInterrupted(ctx.RequestID())))
		return
	}

	switch body.Command() {
	case fields.Read:
		r.handleRead(ctx, body, yield)
	case fields.Write:
		r.handleWrite(ctx, body, yield)
	case fields.Delete:
		r.handleDelete(ctx, body, yield)
	case fields.Ping:
		r.handlePing(ctx, yield)
	case fields.Batch:
		r.handleBatch(ctx, body, yield)
	default:
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorUnsupportedCommand(body.Command())))
	}
}

// handleRead calls the read handler for the key in body.
//
// A nil value from the handler becomes [protocolerrs.ErrorNotFound]. A handler error
// is returned unchanged. A cancelled context is [errs.ErrorRequestInterrupted].
// On success it returns a read answer frame.
func (r Runtime) handleRead(ctx Context, body body.Body, yield YieldFrameIterator) {
	rb, _ := body.(bodies.Read)

	value, err := r.handlerRead(ctx, fields.Key(rb))
	if ctx.Err() != nil {
		err = errs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}
	if value == nil {
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorNotFound(fields.Key(rb))))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewReadAnswer(ctx.RequestID(), value)
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(r.encodeFrame(result, nil))
	return
}

// handleWrite calls the write handler for the key and value in body.
//
// A handler error is returned unchanged. A cancelled context is
// [errs.ErrorRequestInterrupted]. On success it returns a write answer frame.
func (r Runtime) handleWrite(ctx Context, body body.Body, yield YieldFrameIterator) {
	rw, _ := body.(bodies.Write)

	err := r.handlerWrite(ctx, rw.Key, rw.Value)
	if ctx.Err() != nil {
		err = errs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewWriteAnswer(ctx.RequestID())
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(r.encodeFrame(result, nil))
	return
}

// handleDelete calls the delete handler for the key in body.
//
// A handler error is returned unchanged. A cancelled context is
// [errs.ErrorRequestInterrupted]. On success it returns a delete answer frame.
func (r Runtime) handleDelete(ctx Context, body body.Body, yield YieldFrameIterator) {
	rd, _ := body.(bodies.Delete)

	err := r.handlerDelete(ctx, fields.Key(rd))
	if ctx.Err() != nil {
		err = errs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewDeleteAnswer(ctx.RequestID())
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(r.encodeFrame(result, nil))
	return
}

// handlePing returns a ping answer for the request id in ctx.
// A cancelled context is [errs.ErrorRequestInterrupted].
func (r Runtime) handlePing(ctx Context, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), errs.NewErrorRequestInterrupted(ctx.RequestID())))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewPingAnswer(ctx.RequestID())
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(r.encodeFrame(result, nil))
	return
}

func (r Runtime) handleBatch(ctx Context, body body.Body, yield YieldFrameIterator) {
	batch, _ := body.(bodies.Batch)

	if batch.IsSequentialExecution {
		batch.Sort()
		r.executeBatchSequential(ctx, batch.Requests, batch.IsOneAnswer, batch.InterruptAfterError, yield)
		return
	}

	r.executeBatchParrallel(ctx, batch.Requests, batch.IsOneAnswer, batch.InterruptAfterError, yield)
}

func (r Runtime) executeBatchSequential(ctx Context, requests []bodies.Request, isOneAnswer, interruptAfterError bool, yield YieldFrameIterator) {
	rBuilder := builder.NewBatchResultsBuilder()

	for _, request := range requests {
		rBuilder = r.executeRequest(ctx, request, interruptAfterError, rBuilder)

		if !isOneAnswer {
			encoded, err := r.encodeBatchResult(ctx.RequestID(), rBuilder, nil)
			if err != nil {
				yield(nil, err)
				return
			}

			if !yield(encoded, nil) {
				return
			}

			rBuilder = builder.NewBatchResultsBuilder()
		}
	}

	if isOneAnswer {
		encoded, err := r.encodeBatchResult(ctx.RequestID(), rBuilder, nil)
		if err != nil {
			yield(nil, err)
			return
		}

		yield(encoded, nil)
	}
}

func (r Runtime) executeBatchParrallel(parent Context, requests []bodies.Request, isOneAnswer, interruptAfterError bool, yield YieldFrameIterator) {
	rMainBuilder := builder.NewBatchResultsBuilder()
	wg := sync.WaitGroup{}
	defer wg.Wait()

	c, cancel := context.WithCancel(parent)
	defer cancel()

	ctx := NewContext(c, parent.Login(), parent.RequestID(), parent.IsExternal())

	done := make(chan *builder.BatchResultsBuilder, r.maxGoroutinePerBatch)
	queue := make(chan int, r.maxGoroutinePerBatch)

	wg.Go(func() {
		defer close(queue)

		for i := range requests {
			queue <- i
		}
	})

	for range r.maxGoroutinePerBatch {
		wg.Go(
			func() {
				for {
					i, ok := <-queue
					if !ok {
						return
					}

					done <- r.executeRequest(ctx, requests[i], interruptAfterError, builder.NewBatchResultsBuilder())
				}
			},
		)
	}

	for range requests {
		rLocalBuilder := <-done

		if !isOneAnswer {
			encoded, err := r.encodeBatchResult(ctx.RequestID(), rLocalBuilder, nil)
			if err != nil {
				yield(nil, err)
				return
			}

			if !yield(encoded, nil) {
				return
			}
		} else {
			rMainBuilder.Merge(rLocalBuilder, false)
		}
	}

	wg.Wait()

	if isOneAnswer {
		encoded, err := r.encodeBatchResult(ctx.RequestID(), rMainBuilder, nil)
		if err != nil {
			yield(nil, err)
			return
		}

		yield(encoded, nil)
	}
}

func (r Runtime) executeRequest(ctx Context, request bodies.Request, interruptAfterError bool, builder *builder.BatchResultsBuilder) *builder.BatchResultsBuilder {
	if ctx.Err() != nil {
		return r.handleInterruptionInBatch(ctx, request.Number, errs.NewErrorRequestInterrupted(ctx.RequestID()), builder)
	}

	if interruptAfterError && ctx.state.interruptNext {
		return builder.AddError(request.Number, errs.NewErrorRequestInterruptedWithTracebackID(ctx.RequestID(), ctx.state.withTracebackID))
	}

	switch body := request.Body.(type) {
	case bodies.Read:
		result, err := r.handlerRead(ctx, fields.Key(body))
		if ctx.Err() != nil {
			err = errs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		return builder.AddRead(request.Number, result)

	case bodies.Write:
		err := r.handlerWrite(ctx, body.Key, body.Value)
		if ctx.Err() != nil {
			err = errs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		return builder.AddWrite(request.Number)

	case bodies.Delete:
		err := r.handlerDelete(ctx, fields.Key(body))
		if ctx.Err() != nil {
			err = errs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		return builder.AddDelete(request.Number)
	}

	return r.handleInterruptionInBatch(
		ctx,
		request.Number,
		errs.NewErrorUnexpectedCommandInBatch(request.Body.Command(), request.Number),
		builder,
	)
}
