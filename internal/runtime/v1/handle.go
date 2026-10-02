package runtime_v1

import (
	"bufio"
	"context"

	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/answer"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

// Handle reads one frame from reader and returns an iterator of encoded answers.
//
// The frame is read when the caller ranges over the iterator. Each yield is one
// answer. A nil error means the caller writes the bytes and keeps the connection.
// An error for which errors.Is(err, [errs.ErrCloseConnection]) is true means
// the connection with this client must be closed. The caller does not keep
// reading frames.
// An error for which errors.Is(err, [errs.ErrLogAndIgnore]) is true means
// the frame was consumed: the caller logs the error, writes nothing, and
// keeps the connection.
//
// Read, write, delete, and ping stay registered until their single answer is yielded.
// A batch stays registered until every answer has been yielded, or until the
// iteration stops with an error. Stopping the range releases the batch too.
// A cancelled ctx is answered with [protocolerrs.ErrorRequestInterrupted].
func (r Runtime) Handle(ctx context.Context, reader *bufio.Reader, requestRow *row.RequestRow) FrameIterator {
	return func(yield YieldFrameIterator) {
		if requestRow == nil {
			yield(nil, errs.CloseConnection(errs.ErrNilRequestRow))
			return
		}

		request, err := r.decoder.DecodeFrame(reader)
		if err != nil {
			if decodeClosesConnection(err) {
				yield(nil, errs.CloseConnection(err))
				return
			}

			yield(r.writeErrAnswer(request.RequestID, err))
			return
		}

		if err := request.IsValid(); err != nil {
			yield(r.writeErrAnswer(request.RequestID, err))
			return
		}

		switch request.Body.Command() {
		case fields.Handshake:
			yield(r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Read)))
			return

		case fields.Answer:
			isExternal, ok := requestRow.IsRegistered(request.RequestID)
			if !ok {
				yield(nil, errs.LogAndIgnore(answer.NewErrorUnregisteredAnswer(request.RequestID)))
				return
			}

			if !isExternal {
				yield(nil, errs.LogAndIgnore(answer.NewErrorNonExternalAnswer(request.RequestID)))
				return
			}

			defer requestRow.Terminate(request.RequestID)
			yield(nil, r.handleAnswer(request.Body))
			return

		case fields.Read, fields.Write, fields.Delete, fields.Ping, fields.Batch:
			if err := requestRow.Register(request.RequestID, true); err != nil {
				yield(r.writeErrAnswer(request.RequestID, err))
				return
			}

			defer requestRow.Terminate(request.RequestID)

			r.handleRequest(NewContext(ctx, requestRow.Login(), request.RequestID, true), request.Body, yield)
			return
		}

		yield(r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnsupportedCommand(request.Body.Command())))
	}
}
