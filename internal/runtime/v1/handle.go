package runtime_v1

import (
	"bufio"
	"context"
	"errors"
	"iter"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err"
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
// Read, write, and delete stay registered until their single answer is yielded.
// A batch stays registered until every answer has been yielded, or until the
// iteration stops with an error. Stopping the range releases the batch too.
func (r Runtime) Handle(ctx context.Context, reader *bufio.Reader, requestRow *row.RequestRow) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
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

		case fields.Batch:
			if err := requestRow.Register(request.RequestID, true); err != nil {
				yield(r.writeErrAnswer(request.RequestID, err))
				return
			}

			defer requestRow.Terminate(request.RequestID)
			r.yieldBatch(NewContext(ctx, requestRow.Login(), request.RequestID, true), request.Body, yield)
			return

		case fields.Read, fields.Write, fields.Delete:
			if err := requestRow.Register(request.RequestID, true); err != nil {
				yield(r.writeErrAnswer(request.RequestID, err))
				return
			}

			defer requestRow.Terminate(request.RequestID)
			yield(r.handleRequest(NewContext(ctx, requestRow.Login(), request.RequestID, true), request.Body))
			return
		}

		yield(r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnsupportedCommand(request.Body.Command())))
	}
}

// yieldBatch yields the answers of one batch, then returns.
//
// The caller removes the batch request id after yieldBatch returns, so the id
// stays registered while answers are still being produced. An error yield ends
// the batch. A false yield means the caller stopped ranging and the batch ends too.
func (r Runtime) yieldBatch(ctx Context, b body.Body, yield func([]byte, error) bool) {
	yield(r.handleRequest(ctx, b))
}

// decodeClosesConnection reports a failure that leaves the stream unusable.
// Body-limit and unsupported-compression errors are returned before the body
// is read, so the next bytes are no longer a frame boundary. A checksum
// mismatch is answered to the client: that body has already been consumed.
func decodeClosesConnection(e error) bool {
	if _, ok := errors.AsType[err.DecodeError](e); ok {
		return true
	}

	if _, ok := errors.AsType[err.BrokenFrameError](e); ok {
		return true
	}

	if _, ok := errors.AsType[protocolerrs.ErrorBodyLimitIsExceeded](e); ok {
		return true
	}

	if _, ok := errors.AsType[protocolerrs.ErrorUnsupportedCompression](e); ok {
		return true
	}

	return false
}

// handleAnswer accepts a ping answer and ignores every other answer.
//
// A ping answer returns nil. Any other answer returns [errs.ErrLogAndIgnore]:
// the caller logs the error, writes nothing, and keeps the connection.
func (r Runtime) handleAnswer(b body.Body) error {
	a, ok := b.(body.Answer)
	if !ok {
		return errs.LogAndIgnore(answer.NewErrorUnexpectedAnswer(b.Command()))
	}

	if a.IsResponseTo() != fields.Ping {
		return errs.LogAndIgnore(answer.NewErrorUnexpectedAnswer(a.IsResponseTo()))
	}

	return nil
}
