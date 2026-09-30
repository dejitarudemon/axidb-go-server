package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
)

func TestHandshakeAcceptsTheClient(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.NewHandshake("user", [32]byte{1}, nil),
	}, nil)

	requestRow, versions, answer, err := rt.Handshake(context.Background(), bufio.NewReader(bytes.NewReader(raw)), []byte("peer"))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if requestRow == nil || requestRow.Login() != "user" {
		t.Fatalf("row = %#v, want login user", requestRow)
	}

	if len(versions) != 1 || versions[0] != 1 {
		t.Errorf("versions = %v, want [1]", versions)
	}

	got, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(answer)))
	if decErr != nil {
		t.Fatalf("decode = %v", decErr)
	}

	if _, ok := got.Body.(bodies.HandshakeAnswer); !ok {
		t.Fatalf("body = %T, want HandshakeAnswer", got.Body)
	}
}

func TestHandshakeRejectsUnauthorized(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return false, nil
		}).
		Build()

	requestRow, _, answer, err := rt.Handshake(context.Background(), bufio.NewReader(bytes.NewReader(handshakeFrame(t))), []byte("peer"))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if requestRow != nil {
		t.Fatal("row is set, want nil")
	}

	if code := decodedErrorCode(t, rt, answer); code != fields.Unauthorized {
		t.Errorf("code = %v, want unauthorized", code)
	}
}

func TestHandshakeAuthErrorBecomesAnAnswer(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return false, errors.New("db")
		}).
		Build()

	requestRow, _, answer, err := rt.Handshake(context.Background(), bufio.NewReader(bytes.NewReader(handshakeFrame(t))), nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if requestRow != nil {
		t.Fatal("row is set, want nil")
	}

	if code := decodedErrorCode(t, rt, answer); code != fields.InternalError {
		t.Errorf("code = %v, want internal", code)
	}
}

func TestHandshakeUnexpectedCommand(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}}, nil)

	requestRow, _, answer, err := rt.Handshake(context.Background(), bufio.NewReader(bytes.NewReader(raw)), nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if requestRow != nil {
		t.Fatal("row is set, want nil")
	}

	if code := decodedErrorCode(t, rt, answer); code != fields.UnexpectedCommand {
		t.Errorf("code = %v, want unexpected command", code)
	}
}

func TestHandshakeBodyLimitClosesConnection(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(0)).Build()
	_, _, answer, err := rt.Handshake(context.Background(), bufio.NewReader(bytes.NewReader(handshakeFrame(t))), nil)

	if answer != nil {
		t.Fatalf("answer = %v, want nil", answer)
	}

	if !errors.Is(err, errs.ErrCloseConnection) {
		t.Fatalf("error = %v, want close connection", err)
	}

	if _, ok := errors.AsType[protocolerrs.ErrorBodyLimitIsExceeded](err); !ok {
		t.Fatalf("error = %v, want body limit exceeded", err)
	}
}

func handshakeFrame(t *testing.T) []byte {
	t.Helper()

	return mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.NewHandshake("user", [32]byte{1}, nil),
	}, nil)
}

func decodedErrorCode(t *testing.T, rt Runtime, raw []byte) fields.Error {
	t.Helper()

	got, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("decode = %v", err)
	}

	return errorCode(t, got).Code()
}
