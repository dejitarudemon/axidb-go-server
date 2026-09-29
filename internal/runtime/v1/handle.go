package runtime_v1

import (
	"bufio"
	"context"
	"errors"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime"
)

// Handle reads one frame from reader and returns the encoded answer.
//
// A nil error means the caller writes the bytes and keeps the connection.
// An error for which errors.Is(err, [runtime.ErrCloseConnection]) is true means
// the caller closes the connection and does not keep reading frames.
// An error for which errors.Is(err, [runtime.ErrLogAndIgnore]) is true means
// the frame was consumed: the caller logs the error, writes nothing, and
// keeps the connection.
func (r Runtime) Handle(ctx context.Context, reader *bufio.Reader, row *RequestRow) ([]byte, error) {
	if row == nil {
		return nil, runtime.CloseConnection(runtime.ErrNilRequestRow)
	}

	request, err := r.decoder.DecodeFrame(reader)
	if err != nil {
		if decodeClosesConnection(err) {
			return nil, runtime.CloseConnection(err)
		}

		return r.writeErrAnswer(request.RequestID, err)
	}

	if err := request.IsValid(); err != nil {
		return r.writeErrAnswer(request.RequestID, err)
	}

	switch request.Body.Command() {
	case fields.Handshake:
		return r.writeErrAnswer(request.RequestID, errs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Read))

	case fields.Answer:
		isExternal, ok := row.IsRegistered(request.RequestID)
		if !ok {
			return nil, runtime.LogAndIgnore(NewErrorUnregisteredAnswer(request.RequestID))
		}

		if !isExternal {
			return nil, runtime.LogAndIgnore(NewErrorNonExternalAnswer(request.RequestID))
		}

		defer row.Terminate(request.RequestID)
		return nil, r.handleAnswer(request.Body)

	case fields.Read, fields.Write, fields.Delete, fields.Batch:
		if err := row.Register(request.RequestID, true); err != nil {
			return r.writeErrAnswer(request.RequestID, err)
		}
		defer row.Terminate(request.RequestID)
		return r.handleRequest(NewContext(ctx, row.Login(), request.RequestID, true), request.Body)
	}

	return r.writeErrAnswer(request.RequestID, errs.NewErrorUnsupportedCommand(request.Body.Command()))
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

	if _, ok := errors.AsType[errs.ErrorBodyLimitIsExceeded](e); ok {
		return true
	}

	if _, ok := errors.AsType[errs.ErrorUnsupportedCompression](e); ok {
		return true
	}

	return false
}

// handleAnswer accepts a ping answer and ignores every other answer.
//
// A ping answer returns nil. Any other answer returns [runtime.ErrLogAndIgnore]:
// the caller logs the error, writes nothing, and keeps the connection.
func (r Runtime) handleAnswer(b body.Body) error {
	a, ok := b.(body.Answer)
	if !ok {
		return runtime.LogAndIgnore(NewErrorUnexpectedAnswer(b.Command()))
	}

	if a.IsResponseTo() != fields.Ping {
		return runtime.LogAndIgnore(NewErrorUnexpectedAnswer(a.IsResponseTo()))
	}

	return nil
}
