package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/config"
)

func TestActivateAcceptsTheClient(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		Build()

	requestRow, versions, answer, err := rt.Activate(context.Background(), handshakeRequest(), []byte("peer"), false)
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

func TestActivateRejectsUnauthorized(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return false, nil
		}).
		Build()

	requestRow, _, answer, err := rt.Activate(context.Background(), handshakeRequest(), []byte("peer"), false)
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

func TestActivateAuthErrorBecomesAnAnswer(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return false, errors.New("db")
		}).
		Build()

	requestRow, _, answer, err := rt.Activate(context.Background(), handshakeRequest(), nil, false)
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

func TestActivateUnexpectedCommand(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()

	requestRow, _, answer, err := rt.Activate(context.Background(), frame.Frame{
		RequestID: 1,
		Body:      bodies.Ping{},
	}, nil, false)
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

func TestActivateSkipAuthDoesNotCallTheHandler(t *testing.T) {
	called := false
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			called = true
			return false, errors.New("db")
		}).
		Build()

	requestRow, versions, answer, err := rt.Activate(context.Background(), handshakeRequest(), []byte("peer"), true)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if called {
		t.Fatal("auth handler was called")
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

func TestActivateSkipAuthStillRequiresHandshake(t *testing.T) {
	called := false
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			called = true
			return true, nil
		}).
		Build()

	requestRow, _, answer, err := rt.Activate(context.Background(), frame.Frame{
		RequestID: 1,
		Body:      bodies.Ping{},
	}, nil, true)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if called {
		t.Fatal("auth handler was called")
	}

	if requestRow != nil {
		t.Fatal("row is set, want nil")
	}

	if code := decodedErrorCode(t, rt, answer); code != fields.UnexpectedCommand {
		t.Errorf("code = %v, want unexpected command", code)
	}
}

func TestActivateInvalidHandshakeIsAnAnswer(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()

	requestRow, _, answer, err := rt.Activate(context.Background(), frame.Frame{
		RequestID: 1,
		Body: bodies.Handshake{
			Login:        "user",
			Hash:         [32]byte{1},
			Compressions: make([]fields.Compression, bodies.MaxCompressionsPerOneHandshake+1),
		},
	}, nil, false)
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

func handshakeRequest() frame.Frame {
	return frame.Frame{
		RequestID: 1,
		Body:      bodies.NewHandshake("user", [32]byte{1}, nil),
	}
}

func decodedErrorCode(t *testing.T, rt Runtime, raw []byte) fields.Error {
	t.Helper()

	got, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("decode = %v", err)
	}

	return errorCode(t, got).Code()
}
