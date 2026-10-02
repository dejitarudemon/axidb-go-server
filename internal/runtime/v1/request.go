package runtime_v1

import (
	"context"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// handleRequest yields the encoded answers for body.
//
// Read, write, delete, ping, and batch are dispatched to their handlers.
// A context that is already cancelled is answered with [protocolerrs.ErrorRequestInterrupted]
// and the handler is skipped. A handler error is answered to the client. An encoding
// failure of that error answer closes the connection. A batch answer that
// cannot be encoded is yielded as that error.
func (r Runtime) handleRequest(ctx Context, body body.Body, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())))
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

// handleRead yields the read answer for the key in body.
//
// A nil value from the handler becomes [protocolerrs.ErrorNotFound]. A handler
// error is answered to the client. When the context is cancelled during the
// handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a read answer frame.
func (r Runtime) handleRead(ctx Context, body body.Body, yield YieldFrameIterator) {
	rb, _ := body.(bodies.Read)

	value, err := r.handlerRead(ctx, fields.Key(rb))
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
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
}

// handleWrite yields the write answer for the key and value in body.
//
// A handler error is answered to the client. When the context is cancelled
// during the handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a write answer frame.
func (r Runtime) handleWrite(ctx Context, body body.Body, yield YieldFrameIterator) {
	rw, _ := body.(bodies.Write)

	err := r.handlerWrite(ctx, rw.Key, rw.Value)
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
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
}

// handleDelete yields the delete answer for the key in body.
//
// A handler error is answered to the client. When the context is cancelled
// during the handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a delete answer frame.
func (r Runtime) handleDelete(ctx Context, body body.Body, yield YieldFrameIterator) {
	rd, _ := body.(bodies.Delete)

	err := r.handlerDelete(ctx, fields.Key(rd))
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
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
}

// handlePing yields a ping answer for the request id in ctx.
// A context that is already cancelled is answered with [protocolerrs.ErrorRequestInterrupted].
func (r Runtime) handlePing(ctx Context, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewPingAnswer(ctx.RequestID())
	if err != nil {
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	yield(r.encodeFrame(result, nil))
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

	done := make(chan *builder.BatchResultsBuilder, len(requests))
	queue := make(chan int, len(requests))

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
	if interrupted, tracebackID := ctx.state.current(); interruptAfterError && interrupted {
		return builder.AddError(request.Number, protocolerrs.NewErrorRequestInterruptedWithTracebackID(ctx.RequestID(), tracebackID))
	}

	if ctx.Err() != nil {
		return r.handleInterruptionInBatch(ctx, request.Number, protocolerrs.NewErrorRequestInterrupted(ctx.RequestID()), builder)
	}

	switch body := request.Body.(type) {
	case bodies.Read:
		result, err := r.handlerRead(ctx, fields.Key(body))
		if ctx.Err() != nil {
			err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		if result == nil {
			return r.handleInterruptionInBatch(ctx, request.Number, protocolerrs.NewErrorNotFound(fields.Key(body)), builder)
		}

		return builder.AddRead(request.Number, result)

	case bodies.Write:
		err := r.handlerWrite(ctx, body.Key, body.Value)
		if ctx.Err() != nil {
			err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		return builder.AddWrite(request.Number)

	case bodies.Delete:
		err := r.handlerDelete(ctx, fields.Key(body))
		if ctx.Err() != nil {
			err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
		}
		if err != nil {
			return r.handleInterruptionInBatch(ctx, request.Number, err, builder)
		}

		return builder.AddDelete(request.Number)
	}

	return r.handleInterruptionInBatch(
		ctx,
		request.Number,
		protocolerrs.NewErrorUnexpectedCommandInBatch(request.Body.Command(), request.Number),
		builder,
	)
}
