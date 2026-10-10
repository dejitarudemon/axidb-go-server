package runtime_v1

import (
	"bufio"
	"bytes"
	"errors"
	"testing"

	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	protocolerr "github.com/dejitarudemon/ignicula-wire/v1/err"
	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/config"
)

func TestDecodeAcceptsAFrame(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := encodeFrameBytes(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}})

	got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if answer != nil {
		t.Fatalf("answer = %v, want nil", answer)
	}
	if got.RequestID != 1 || got.Body.Command() != fields.Ping {
		t.Fatalf("frame = %#v, want ping with id 1", got)
	}
}

func TestDecodeBodyLimitClosesConnection(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(0)).Build()
	raw := encodeFrameBytes(t, frame.Frame{RequestID: 1, Body: bodies.Read("x")})

	got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if answer != nil {
		t.Fatalf("answer = %v, want nil", answer)
	}
	if got.Body != nil {
		t.Fatalf("frame = %#v, want empty", got)
	}
	assertClose(t, err)
	if _, ok := errors.AsType[protocolerrs.ErrorBodyLimitIsExceeded](err); !ok {
		t.Fatalf("error = %v, want body limit exceeded", err)
	}
}

func TestDecodeShortReadClosesConnection(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()

	got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader([]byte{0x01, 0x02})))
	if answer != nil {
		t.Fatalf("answer = %v, want nil", answer)
	}
	if got.Body != nil {
		t.Fatalf("frame = %#v, want empty", got)
	}
	assertClose(t, err)
	if _, ok := errors.AsType[protocolerr.DecodeError](err); !ok {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func TestDecodeUnsupportedCompressionClosesConnection(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := encodeFrameBytes(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}})
	raw[8] = 9

	got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if answer != nil {
		t.Fatalf("answer = %v, want nil", answer)
	}
	if got.Body != nil {
		t.Fatalf("frame = %#v, want empty", got)
	}
	assertClose(t, err)
	if _, ok := errors.AsType[protocolerrs.ErrorUnsupportedCompression](err); !ok {
		t.Fatalf("error = %v, want unsupported compression", err)
	}
}

func TestDecodeChecksumMismatchIsAnAnswer(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := encodeFrameBytes(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}})
	raw[len(raw)-1] ^= 0xff

	got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got.Body != nil {
		t.Fatalf("frame = %#v, want empty", got)
	}
	if answer == nil {
		t.Fatal("answer is nil")
	}

	decoded, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(answer)))
	if decErr != nil {
		t.Fatalf("decode answer = %v", decErr)
	}
	assertErrorCode(t, decoded, fields.MismatchedChecksum)
}

func TestDecodeDoesNotCloseOnChecksumMismatch(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	raw := encodeFrameBytes(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}})
	raw[len(raw)-1] ^= 0xff

	_, _, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if errors.Is(err, errs.ErrCloseConnection) {
		t.Fatalf("error = %v, want a kept connection", err)
	}
}

func encodeFrameBytes(t *testing.T, f frame.Frame) []byte {
	t.Helper()
	return mustEncodeFrame(t, f, nil)
}
