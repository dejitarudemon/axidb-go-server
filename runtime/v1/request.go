package runtime_v1

import (
	"context"
	"sync"

	"github.com/dejitarudemon/ignicula-wire/v1/body"
	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	"github.com/dejitarudemon/ignicula-wire/v1/builder"
	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/row"
)

// handleRequest yields the encoded answers for body.
//
// Read, write, delete, ping, and batch are dispatched to their handlers.
// A context that is already cancelled is answered with [protocolerrs.ErrorRequestInterrupted]
// and the handler is skipped. A handler error is answered to the client. An encoding
// failure of that error answer closes the connection. A batch answer that
// cannot be encoded is yielded as that error.
func (r Runtime) handleRequest(ctx Context, row *row.RequestRow, body body.Body, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		r.warnWithCtx(ctx, "request interrupted before handler", "command", body.Command())
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())))
		return
	}

	switch body.Command() {
	case fields.Read:
		r.handleRead(ctx, row, body, yield)
	case fields.Write:
		r.handleWrite(ctx, body, yield)
	case fields.Delete:
		r.handleDelete(ctx, body, yield)
	case fields.Ping:
		r.handlePing(ctx, yield)
	case fields.Batch:
		r.infoWithCtx(ctx, "batch", "sequential", body.(bodies.Batch).IsSequentialExecution)
		r.handleBatch(ctx, row, body, yield)
	default:
		r.warnWithCtx(ctx, "unsupported command in handler", "command", body.Command())
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorUnsupportedCommand(body.Command())))
	}
}

// handleRead yields the read answer for the key in body.
//
// A nil value from the handler becomes [protocolerrs.ErrorNotFound]. A handler
// error is answered to the client. When the context is cancelled during the
// handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a read answer frame.
func (r Runtime) handleRead(ctx Context, row *row.RequestRow, body body.Body, yield YieldFrameIterator) {
	rb, _ := body.(bodies.Read)
	key := fields.Key(rb)

	r.infoWithCtx(ctx, "read", "key", string(key))

	value, err := r.handlerRead(ctx, key)
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		r.warnWithCtx(ctx, "read failed", "key", string(key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}
	if value == nil {
		r.infoWithCtx(ctx, "read not found", "key", string(key))
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorNotFound(key)))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewReadAnswer(ctx.RequestID(), value)
	if err != nil {
		r.errorWithCtx(ctx, "failed to build read answer", "key", string(key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	r.infoWithCtx(ctx, "read ok", "key", string(key), "type", value.Type())

	yield(r.encodeFrame(result, r.selectCompression(row, result.Body)))
}

// handleWrite yields the write answer for the key and value in body.
//
// A handler error is answered to the client. When the context is cancelled
// during the handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a write answer frame.
func (r Runtime) handleWrite(ctx Context, body body.Body, yield YieldFrameIterator) {
	rw, _ := body.(bodies.Write)

	valueType := any(nil)
	if rw.Value != nil {
		valueType = rw.Value.Type()
	}
	r.infoWithCtx(ctx, "write", "key", string(rw.Key), "type", valueType)

	err := r.handlerWrite(ctx, rw.Key, rw.Value)
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		r.warnWithCtx(ctx, "write failed", "key", string(rw.Key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewWriteAnswer(ctx.RequestID())
	if err != nil {
		r.errorWithCtx(ctx, "failed to build write answer", "key", string(rw.Key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	r.infoWithCtx(ctx, "write ok", "key", string(rw.Key))
	yield(r.encodeFrame(result, nil))
}

// handleDelete yields the delete answer for the key in body.
//
// A handler error is answered to the client. When the context is cancelled
// during the handler, the handler runs to completion and the answer is
// [protocolerrs.ErrorRequestInterrupted]. On success it yields a delete answer frame.
func (r Runtime) handleDelete(ctx Context, body body.Body, yield YieldFrameIterator) {
	rd, _ := body.(bodies.Delete)
	key := fields.Key(rd)

	r.infoWithCtx(ctx, "delete", "key", string(key))

	err := r.handlerDelete(ctx, key)
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
	}
	if err != nil {
		r.warnWithCtx(ctx, "delete failed", "key", string(key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewDeleteAnswer(ctx.RequestID())
	if err != nil {
		r.errorWithCtx(ctx, "failed to build delete answer", "key", string(key), "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	r.infoWithCtx(ctx, "delete ok", "key", string(key))
	yield(r.encodeFrame(result, nil))
}

// handlePing yields a ping answer for the request id in ctx.
// A context that is already cancelled is answered with [protocolerrs.ErrorRequestInterrupted].
func (r Runtime) handlePing(ctx Context, yield YieldFrameIterator) {
	if ctx.Err() != nil {
		r.warnWithCtx(ctx, "ping interrupted")
		yield(r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())))
		return
	}

	result, err := builder.NewFrameBuilder(r.limit).NewPingAnswer(ctx.RequestID())
	if err != nil {
		r.errorWithCtx(ctx, "failed to build ping answer", "error", err)
		yield(r.writeErrAnswer(ctx.RequestID(), err))
		return
	}

	r.debugWithCtx(ctx, "ping ok")
	yield(r.encodeFrame(result, nil))
}

// handleBatch yields the answers for a batch body.
//
// Sequential execution sorts the nested commands by request number and runs
// them in that order. Otherwise they run concurrently, up to the runtime
// parallel-batch limit. A batch that asks for one answer yields a single
// combined frame after every nested command has finished. Otherwise it yields
// one frame per nested command.
func (r Runtime) handleBatch(ctx Context, row *row.RequestRow, body body.Body, yield YieldFrameIterator) {
	batch, _ := body.(bodies.Batch)

	if batch.IsSequentialExecution {
		batch.Sort()
		r.executeBatchSequential(ctx, row, batch.Requests, batch.IsOneAnswer, batch.InterruptAfterError, yield)
		return
	}

	r.executeBatchParallel(ctx, row, batch.Requests, batch.IsOneAnswer, batch.InterruptAfterError, yield)
}

// executeBatchSequential runs requests in the order they are passed.
// [handleBatch] has already sorted them by request number.
//
// When isOneAnswer is set, one combined frame is yielded after every command
// has finished. Otherwise each command is yielded as its own frame before the
// next command starts. An encoding failure is yielded as that error, and the
// remaining commands are not run. A yield that returns false stops the batch
// the same way. interruptAfterError is applied by [executeRequest].
func (r Runtime) executeBatchSequential(ctx Context, row *row.RequestRow, requests []bodies.Request, isOneAnswer, interruptAfterError bool, yield YieldFrameIterator) {
	rBuilder := builder.NewBatchResultsBuilder()

	for _, request := range requests {
		rBuilder = r.executeRequest(ctx, request, interruptAfterError, rBuilder)

		if !isOneAnswer {
			encoded, err := r.encodeBatchResult(row, ctx.RequestID(), rBuilder)
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
		encoded, err := r.encodeBatchResult(row, ctx.RequestID(), rBuilder)
		if err != nil {
			yield(nil, err)
			return
		}

		yield(encoded, nil)
	}
}

// executeBatchParallel runs requests concurrently, up to the runtime
// parallel-batch limit. The workers share a child of parent, so cancellation
// and the first recorded interruption are visible to every nested command.
//
// When isOneAnswer is set, one combined frame is yielded after every command
// has finished. Otherwise each result is yielded as its own frame as it
// finishes. An encoding failure is yielded as that error. The call waits for
// the workers before it returns, including when the iteration stops.
func (r Runtime) executeBatchParallel(parent Context, row *row.RequestRow, requests []bodies.Request, isOneAnswer, interruptAfterError bool, yield YieldFrameIterator) {
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
			encoded, err := r.encodeBatchResult(row, ctx.RequestID(), rLocalBuilder)
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
		encoded, err := r.encodeBatchResult(row, ctx.RequestID(), rMainBuilder)
		if err != nil {
			yield(nil, err)
			return
		}

		yield(encoded, nil)
	}
}

// executeRequest runs one nested command and records its result on builder.
//
// When interruptAfterError is set and an interruption is already recorded,
// the handler is skipped and the result is [protocolerrs.ErrorRequestInterrupted]
// with that traceback. A context that is already cancelled does the same.
// A handler cancelled while it runs still runs to completion, and its result
// is replaced with that error. A nil read value becomes [protocolerrs.ErrorNotFound].
// Each of these errors is added to builder and registers an interruption; the
// first traceback is kept. A command other than read, write, or delete is
// recorded as [protocolerrs.ErrorUnexpectedCommandInBatch].
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
