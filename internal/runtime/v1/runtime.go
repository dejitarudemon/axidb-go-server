package runtime_v1

import (
	"bufio"
	"context"
	"errors"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/decoder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime"
)

// Runtime is the v1 server runtime assembled by [RuntimeBuilder].
//
// It holds the request decoder, the allowed protocol versions and compressors,
// and the read, write, delete, and auth handlers. Command handling is not
// implemented yet. [Runtime.Handle] answers protocol errors when the frame was
// fully read. It returns [runtime.ErrCloseConnection] when the caller must drop
// the connection.
type Runtime struct {
	decoder decoder.Decoder

	allowedVersions     []fields.Version
	allowedCompressions map[fields.Compression]compressor.Compressor

	handlerRead   func(ctx Context, key fields.Key) (value.V, error)
	handlerWrite  func(ctx Context, key fields.Key, value value.V) error
	handlerDelete func(ctx Context, key fields.Key) error

	handlerAuth func(ctx Context, login string, hash [32]byte) (bool, error)

	limit int
}

func (r Runtime) Handshake(ctx context.Context, reader *bufio.Reader, source []byte) (*RequestRow, []fields.Version, []byte, error) {
	request, err := r.decoder.DecodeFrame(reader)
	if err != nil {
		if decodeClosesConnection(err) {
			return nil, nil, nil, runtime.CloseConnection(err)
		}

		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	if err := request.IsValid(); err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	if request.Body.Command() != fields.Handshake {
		answer, err := r.writeErrAnswer(request.RequestID, errs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Handshake))
		return nil, nil, answer, err
	}

	hb, _ := request.Body.(bodies.Handshake)

	requestCtx := NewContext(ctx, hb.Login, request.RequestID, true)

	if ok, err := r.handlerAuth(requestCtx, hb.Login, hb.Hash); err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	} else if !ok {
		answer, err := r.writeErrAnswer(request.RequestID, errs.NewErrorUnauthorized(source))
		return nil, nil, answer, err
	}

	allowedCompressions := make([]fields.Compression, 0, len(r.allowedCompressions))
	for compression := range r.allowedCompressions {
		allowedCompressions = append(allowedCompressions, compression)
	}

	answer, err := builder.NewFrameBuilder(r.limit).NewHandshakeAnswer(allowedCompressions)
	if err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	buf := buffer.Slice{}
	buf.Preallocate(answer.Size())

	if err := answer.Encode(&buf, nil); err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	return NewRequestRow(hb.Login, hb.Compressions), r.allowedVersions, buf.Bytes(), nil

}

// Handle reads one frame from reader and returns the encoded answer.
//
// A nil error means the caller writes the bytes and keeps the connection.
// An error for which errors.Is(err, [runtime.ErrCloseConnection]) is true means
// the caller closes the connection and does not keep reading frames.
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
			// return any spessific error. just to log
		}

		if !isExternal {
			// return any spessific error. just to log
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

func (r Runtime) toProtocolError(e error) err.ProtocolError {
	if pe, ok := e.(err.ProtocolError); ok {
		return pe
	}

	return errs.NewErrorInternalError(e)
}

// don't answer to answer
func (r Runtime) handleAnswer(b body.Body) error {
	a, _ := b.(body.Answer)
	if a.IsResponseTo() != fields.Ping {
		return nil // return some speciffic internal error, just to log
	}

	return nil
}

// writeErrAnswer encodes an error answer for requestID.
// cause is sent as-is when it is already a protocol error; otherwise it becomes
// an internal error. An encoding failure is returned to the caller and is not
// wrapped in another answer.
func (r Runtime) writeErrAnswer(requestID fields.RequestID, cause error) ([]byte, error) {
	pe := r.toProtocolError(cause)

	result, err := builder.NewFrameBuilder(r.limit).NewErrAnswer(requestID, pe)
	if err != nil {
		return nil, runtime.CloseConnection(err)
	}

	buf := buffer.Slice{}
	buf.Preallocate(result.Size())

	if err := result.Encode(&buf, nil); err != nil {
		return nil, runtime.CloseConnection(err)
	}

	return buf.Bytes(), nil
}

func (r Runtime) handlePing(ctx Context) (frame.Frame, error) {
	return builder.NewFrameBuilder(r.limit).NewPingAnswer(ctx.RequestID())
}

func (r Runtime) handleRead(ctx Context, body body.Body) (frame.Frame, error) {
	rb, _ := body.(bodies.Read)

	value, err := r.handlerRead(ctx, fields.Key(rb))
	if err != nil {
		return frame.Frame{}, err
	}
	if value == nil {
		return frame.Frame{}, errs.NewErrorNotFound(fields.Key(rb))
	}

	return builder.NewFrameBuilder(r.limit).NewReadAnswer(ctx.RequestID(), value)
}

func (r Runtime) handleWrite(ctx Context, body body.Body) (frame.Frame, error) {
	rw, _ := body.(bodies.Write)

	if err := r.handlerWrite(ctx, rw.Key, rw.Value); err != nil {
		return frame.Frame{}, err
	}

	return builder.NewFrameBuilder(r.limit).NewWriteAnswer(ctx.RequestID())
}

func (r Runtime) handleDelete(ctx Context, body body.Body) (frame.Frame, error) {
	rd, _ := body.(bodies.Delete)

	if err := r.handlerDelete(ctx, fields.Key(rd)); err != nil {
		return frame.Frame{}, err
	}

	return builder.NewFrameBuilder(r.limit).NewDeleteAnswer(ctx.RequestID())
}

func (r Runtime) handleRequest(ctx Context, body body.Body) ([]byte, error) {
	result := frame.Frame{}
	handlerErr := error(nil)

	switch body.Command() {
	case fields.Read:
		result, handlerErr = r.handleRead(ctx, body)
	case fields.Write:
		result, handlerErr = r.handleWrite(ctx, body)
	case fields.Delete:
		result, handlerErr = r.handleDelete(ctx, body)
	case fields.Ping:
		result, handlerErr = r.handlePing(ctx)
	case fields.Batch:
		// handle Batch
	default:
		return r.writeErrAnswer(ctx.RequestID(), errs.NewErrorUnsupportedCommand(body.Command()))
	}

	if handlerErr != nil {
		return r.writeErrAnswer(ctx.RequestID(), handlerErr)
	}

	buf := buffer.Slice{}
	buf.Preallocate(result.Size())

	if err := result.Encode(&buf, nil); err != nil {
		return r.writeErrAnswer(ctx.RequestID(), err)
	}

	return buf.Bytes(), nil
}
