package runtime_v1

import (
	"bufio"

	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
)

// Decode reads one frame from reader.
//
// On success it returns the decoded frame, a nil answer, and a nil error.
// A failure that leaves the stream unusable returns [errs.ErrCloseConnection]
// and a nil answer. Other decode failures return an encoded error answer for
// the request id recovered from the stream, and that encoding error when the
// answer cannot be built.
func (r *Runtime) Decode(reader *bufio.Reader) (frame.Frame, []byte, error) {
	request, err := r.decoder.DecodeFrame(reader)
	if err != nil {
		if decodeClosesConnection(err) {
			r.error("failed to decode frame", "error", err, "close_connection", true)
			return frame.Frame{}, nil, errs.CloseConnection(err)
		}

		decodeErr := err
		answer, err := r.writeErrAnswer(request.RequestID, decodeErr)
		if err != nil {
			r.error("failed to encode decode error answer", "request_id", request.RequestID, "error", err, "cause", decodeErr)
		} else {
			r.warn("decode error answered to client", "request_id", request.RequestID, "error", decodeErr)
		}
		return frame.Frame{}, answer, err
	}

	r.debug("decoded frame", "request_id", request.RequestID, "command", request.Body.Command())
	return request, nil, nil
}
