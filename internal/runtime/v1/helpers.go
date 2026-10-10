package runtime_v1

import (
	"errors"

	"github.com/dejitarudemon/ignicula-wire/v1/buffer"
	"github.com/dejitarudemon/ignicula-wire/v1/builder"
	"github.com/dejitarudemon/ignicula-wire/v1/compressor"
	"github.com/dejitarudemon/ignicula-wire/v1/err"
	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/row"
)

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

// toProtocolError returns e when it is already a protocol error.
// Any other error becomes an internal error.
func (r Runtime) toProtocolError(e error) err.ProtocolError {
	if pe, ok := e.(err.ProtocolError); ok {
		return pe
	}

	return protocolerrs.NewErrorInternalError(e)
}

// writeErrAnswer encodes an error answer for requestID.
// cause is sent as-is when it is already a protocol error; otherwise it becomes
// an internal error. An encoding failure is returned as [errs.ErrCloseConnection]
// and is not wrapped in another answer.
func (r Runtime) writeErrAnswer(requestID fields.RequestID, cause error) ([]byte, error) {
	pe := r.toProtocolError(cause)

	result, err := builder.NewFrameBuilder(r.limit).NewErrAnswer(requestID, pe)
	if err != nil {
		return nil, errs.CloseConnection(err)
	}

	encoded, err := r.encodeFrame(result, nil)
	if err != nil {
		return nil, errs.CloseConnection(err)
	}

	return encoded, nil
}

// encodeFrame encodes frame with compressor, using a buffer from the runtime
// pool, and returns a copy of the bytes. The buffer goes back to the pool.
// An encode failure is returned as that error.
func (r Runtime) encodeFrame(frame frame.Frame, compressor compressor.Compressor) ([]byte, error) {
	buf := r.pool.Get().(*buffer.Slice)
	buf.Preallocate(frame.Size())
	buf.Clean()

	defer r.pool.Put(buf)

	if err := frame.Encode(buf, compressor); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// encodeBatchResult builds a batch answer for requestID from rBuilder and
// encodes it with [Runtime.selectCompression]. A build or encode failure is
// returned as that error.
func (r Runtime) encodeBatchResult(row *row.RequestRow, requestID fields.RequestID, rBuilder *builder.BatchResultsBuilder) ([]byte, error) {
	frame, err := builder.NewFrameBuilder(r.limit).NewBatchAnswer(requestID, *rBuilder)
	if err != nil {
		return nil, err
	}

	return r.encodeFrame(frame, r.selectCompression(row, frame.Body))
}

// handleInterruptionInBatch records err as the result of nested command number.
//
// A cancelled context replaces err with [protocolerrs.ErrorRequestInterrupted].
// The error is converted with [Runtime.toProtocolError], its traceback is
// registered when none is recorded yet, and the protocol error is added to builder.
func (r Runtime) handleInterruptionInBatch(ctx Context, number fields.RequestNumber, err error, builder *builder.BatchResultsBuilder) *builder.BatchResultsBuilder {
	if ctx.Err() != nil {
		err = protocolerrs.NewErrorRequestInterrupted(ctx.RequestID())
	}

	pe := r.toProtocolError(err)
	ctx.state.registerInterruption(pe.TracebackID())

	return builder.AddError(number, pe)
}
