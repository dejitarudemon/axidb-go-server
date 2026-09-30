package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
)

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
// an internal error. An encoding failure is returned to the caller and is not
// wrapped in another answer.
func (r Runtime) writeErrAnswer(requestID fields.RequestID, cause error) ([]byte, error) {
	pe := r.toProtocolError(cause)

	result, err := builder.NewFrameBuilder(r.limit).NewErrAnswer(requestID, pe)
	if err != nil {
		return nil, errs.CloseConnection(err)
	}

	encoded, err := encodeFrame(result)
	if err != nil {
		return nil, errs.CloseConnection(err)
	}

	return encoded, nil
}

// frameBufferPool reuses encode buffers. [buffer.Slice.Bytes] copies the
// encoded bytes, so the buffer can return to the pool after encodeFrame.
var frameBufferPool = sync.Pool{
	New: func() any {
		return &buffer.Slice{}
	},
}

// encodeFrame writes f into a pooled buffer and returns a copy of the encoded bytes.
// The buffer is cleaned and returned to the pool before encodeFrame returns.
func encodeFrame(f frame.Frame) ([]byte, error) {
	buf := frameBufferPool.Get().(*buffer.Slice)
	defer func() {
		buf.Clean()
		frameBufferPool.Put(buf)
	}()

	buf.Clean()
	buf.Preallocate(f.Size())

	if err := f.Encode(buf, nil); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
