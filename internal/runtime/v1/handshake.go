package runtime_v1

import (
	"bufio"
	"context"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

// Handshake reads the first frame from reader and authenticates the client.
//
// On success it returns the [row.RequestRow] for the login, the protocol versions
// this runtime accepts, and the encoded handshake answer. source is reported
// to the client when authentication is rejected.
// A nil error means the caller writes the bytes and keeps the connection.
// The request row is nil when those bytes are an error answer.
// An error for which errors.Is(err, [errs.ErrCloseConnection]) is true means
// the caller closes the connection and does not keep reading frames.
func (r Runtime) Handshake(ctx context.Context, reader *bufio.Reader, source []byte) (*row.RequestRow, []fields.Version, []byte, error) {
	request, err := r.decoder.DecodeFrame(reader)
	if err != nil {
		if decodeClosesConnection(err) {
			return nil, nil, nil, errs.CloseConnection(err)
		}

		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	if err := request.IsValid(); err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	if request.Body.Command() != fields.Handshake {
		answer, err := r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnexpectedCommand(request.Body.Command(), fields.Handshake))
		return nil, nil, answer, err
	}

	hb, _ := request.Body.(bodies.Handshake)

	requestCtx := NewContext(ctx, hb.Login, request.RequestID, true)

	if ok, err := r.handlerAuth(requestCtx, hb.Login, hb.Hash); err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	} else if !ok {
		answer, err := r.writeErrAnswer(request.RequestID, protocolerrs.NewErrorUnauthorized(source))
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

	encoded, err := encodeFrame(answer)
	if err != nil {
		answer, err := r.writeErrAnswer(request.RequestID, err)
		return nil, nil, answer, err
	}

	return row.NewRequestRow(hb.Login, hb.Compressions), r.allowedVersions, encoded, nil
}
