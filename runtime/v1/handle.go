package runtime_v1

import (
	"context"

	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-framework/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/answer"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/row"
)

// Handle returns an iterator of encoded answers for an already decoded request.
//
// The caller must decode the frame first, typically with [Runtime.Decode].
// Each yield is one answer. A nil error means the caller writes the bytes and
// keeps the connection.
// An error for which errors.Is(err, [errs.ErrCloseConnection]) is true means
// the connection with this client must be closed. The caller does not keep
// reading frames. An error answer that cannot be encoded is yielded this way.
// An error for which errors.Is(err, [errs.ErrLogAndIgnore]) is true means
// the frame was consumed: the caller logs the error, writes nothing, and
// keeps the connection.
// Any other yielded error is a batch answer that could not be encoded.
//
// Read, write, delete, and ping stay registered until their single answer is yielded.
// A batch stays registered until every answer has been yielded, or until the
// iteration stops. Stopping the range releases the batch after parallel workers
// have finished.
//
// A cancelled ctx is answered with [protocolerrs.ErrorRequestInterrupted].
// When the context is already cancelled, the handler is skipped. A handler
// cancelled while it runs still runs to completion, and its result is replaced
// with that error. Inside a batch the same rule applies to each nested command.
// With interrupt-after-error, a nested command that has not started is answered
// with RequestInterrupted and the traceback of the first error, and its handler
// is skipped.
//
// A batch that asks for one answer yields a single combined frame after every
// nested command has finished. Otherwise it yields one frame per nested command.
// Sequential execution runs those commands in request-number order. Otherwise
// they run concurrently, up to [config.RuntimeBuilderConfig.MaxGoroutinesPerBatch],
// and frames are yielded as they finish.
func (r Runtime) Handle(ctx context.Context, request frame.Frame, requestRow *row.RequestRow) FrameIterator {
	return func(yield YieldFrameIterator) {
		if requestRow == nil {
			r.error("handle with nil request row", withLogin("", request.RequestID)...)
			yield(nil, errs.CloseConnection(errs.ErrNilRequestRow))
			return
		}

		reqCtx := NewContext(ctx, requestRow.Login(), request.RequestID, true)

		if err := request.IsValid(); err != nil {
			r.warnWithCtx(reqCtx, "handle rejected invalid frame", "error", err)
			yield(r.writeErrAnswer(request.RequestID, err))
			return
		}

		switch request.Body.Command() {
		case fields.Handshake:
			r.warnWithCtx(reqCtx, "unexpected handshake after activate")
			yield(r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Read)))
			return

		case fields.Answer:
			isExternal, ok := requestRow.IsRegistered(request.RequestID)
			if !ok {
				err := answer.NewErrorUnregisteredAnswer(request.RequestID)
				r.warnWithCtx(reqCtx, "ignoring unregistered answer", "error", err)
				yield(nil, errs.LogAndIgnore(err))
				return
			}

			if !isExternal {
				err := answer.NewErrorNonExternalAnswer(request.RequestID)
				r.warnWithCtx(reqCtx, "ignoring non-external answer", "error", err)
				yield(nil, errs.LogAndIgnore(err))
				return
			}

			defer requestRow.Terminate(request.RequestID)
			if err := r.handleAnswer(request.Body); err != nil {
				r.warnWithCtx(reqCtx, "ignoring answer", "error", err)
				yield(nil, err)
				return
			}
			r.debugWithCtx(reqCtx, "accepted ping answer")
			yield(nil, nil)
			return

		case fields.Read, fields.Write, fields.Delete, fields.Ping, fields.Batch:
			if err := requestRow.Register(request.RequestID, true); err != nil {
				r.warnWithCtx(reqCtx, "request id conflict", "command", request.Body.Command(), "error", err)
				yield(r.writeErrAnswer(request.RequestID, err))
				return
			}

			defer requestRow.Terminate(request.RequestID)

			r.debugWithCtx(reqCtx, "handling request", "command", request.Body.Command())
			r.handleRequest(reqCtx, requestRow, request.Body, yield)
			return
		}

		r.warnWithCtx(reqCtx, "unsupported command", "command", request.Body.Command())
		yield(r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnsupportedCommand(request.Body.Command())))
	}
}
