package runtime_v1

import (
	"bufio"
	"context"
	"errors"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/answer"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

// Handle reads one frame from reader and returns the encoded answer.
//
// A nil error means the caller writes the bytes and keeps the connection.
// An error for which errors.Is(err, [errs.ErrCloseConnection]) is true means
// the caller closes the connection and does not keep reading frames.
// An error for which errors.Is(err, [errs.ErrLogAndIgnore]) is true means
// the frame was consumed: the caller logs the error, writes nothing, and
// keeps the connection.
func (r Runtime) Handle(ctx context.Context, reader *bufio.Reader, requestRow *row.RequestRow) ([]byte, error) {
	if requestRow == nil {
		return nil, errs.CloseConnection(errs.ErrNilRequestRow)
	}

	request, err := r.decoder.DecodeFrame(reader)
	if err != nil {
		if decodeClosesConnection(err) {
			return nil, errs.CloseConnection(err)
		}

		return r.writeErrAnswer(request.RequestID, err)
	}

	if err := request.IsValid(); err != nil {
		return r.writeErrAnswer(request.RequestID, err)
	}

	switch request.Body.Command() {
	case fields.Handshake:
		return r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Read))

	case fields.Answer:
		isExternal, ok := requestRow.IsRegistered(request.RequestID)
		if !ok {
			return nil, errs.LogAndIgnore(answer.NewErrorUnregisteredAnswer(request.RequestID))
		}

		if !isExternal {
			return nil, errs.LogAndIgnore(answer.NewErrorNonExternalAnswer(request.RequestID))
		}

		defer requestRow.Terminate(request.RequestID)
		return nil, r.handleAnswer(request.Body)

	case fields.Read, fields.Write, fields.Delete, fields.Batch:
		if err := requestRow.Register(request.RequestID, true); err != nil {
			return r.writeErrAnswer(request.RequestID, err)
		}
		defer requestRow.Terminate(request.RequestID)
		return r.handleRequest(NewContext(ctx, requestRow.Login(), request.RequestID, true), request.Body)
	}

	return r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnsupportedCommand(request.Body.Command()))
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
