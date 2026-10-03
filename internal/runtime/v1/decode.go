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
			return frame.Frame{}, nil, errs.CloseConnection(err)
		}

		answer, err := r.writeErrAnswer(request.RequestID, err)
		return frame.Frame{}, answer, err
	}

	return request, nil, nil
}
