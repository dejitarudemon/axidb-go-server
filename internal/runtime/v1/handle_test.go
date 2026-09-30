package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value/values"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/answer"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

func TestHandleNilRowClosesConnection(t *testing.T) {
	rt := handleRuntime(t)

	_, err := runHandle(t, rt.Runtime, context.Background(), nil, mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}}, nil))
	if !errors.Is(err, errs.ErrCloseConnection) || !errors.Is(err, errs.ErrNilRequestRow) {
		t.Fatalf("error = %v, want close connection and nil row", err)
	}
}

func TestHandleReadWriteDeleteAndPing(t *testing.T) {
	rt := handleRuntime(t)
	rt.data["k"] = values.String("v")
	requestRow := row.NewRequestRow("user", nil)

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("k"),
	}, nil))
	if err != nil {
		t.Fatalf("read error = %v", err)
	}

	if len(frames) != 1 || readValue(t, frames[0]) != values.String("v") {
		t.Fatalf("read frames = %#v", frames)
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 2,
		Body:      bodies.Write{Key: fields.Key("k"), Value: values.String("n")},
	}, nil))
	if err != nil {
		t.Fatalf("write error = %v", err)
	}

	if len(frames) != 1 || responseTo(t, frames[0]) != fields.Write {
		t.Fatalf("write body = %#v", frames[0].Body)
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 3,
		Body:      bodies.Delete("k"),
	}, nil))
	if err != nil {
		t.Fatalf("delete error = %v", err)
	}

	if len(frames) != 1 || responseTo(t, frames[0]) != fields.Delete {
		t.Fatalf("delete body = %#v", frames[0].Body)
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 4,
		Body:      bodies.Ping{},
	}, nil))
	if err != nil {
		t.Fatalf("ping error = %v", err)
	}

	if len(frames) != 1 || responseTo(t, frames[0]) != fields.Ping {
		t.Fatalf("ping body = %#v", frames[0].Body)
	}
}

func TestHandleMissingKeyIsNotFound(t *testing.T) {
	rt := handleRuntime(t)
	requestRow := row.NewRequestRow("user", nil)

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("missing"),
	}, nil))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if errorCode(t, frames[0]).Code() != fields.NotFound {
		t.Errorf("code = %v, want not found", errorCode(t, frames[0]).Code())
	}
}

func TestHandleDuplicateRequest(t *testing.T) {
	rt := handleRuntime(t)
	requestRow := row.NewRequestRow("user", nil)
	if err := requestRow.Register(1, true); err != nil {
		t.Fatalf("register = %v", err)
	}

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.Ping{},
	}, nil))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if errorCode(t, frames[0]).Code() != fields.RequestsConflict {
		t.Errorf("code = %v, want requests conflict", errorCode(t, frames[0]).Code())
	}

	external, ok := requestRow.IsRegistered(1)
	if !ok || !external {
		t.Errorf("registered = (%v, %v), want (true, true)", external, ok)
	}
}

func TestHandleAnswers(t *testing.T) {
	rt := handleRuntime(t)

	t.Run("ping answer", func(t *testing.T) {
		requestRow := row.NewRequestRow("user", nil)
		if err := requestRow.Register(1, true); err != nil {
			t.Fatalf("register = %v", err)
		}

		raw, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		}, nil))
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if len(raw) != 1 || raw[0] != nil {
			t.Fatalf("yields = %#v, want one empty yield", raw)
		}

		if requestRow.Count() != 0 {
			t.Errorf("registered = %d, want 0", requestRow.Count())
		}
	})

	t.Run("unregistered", func(t *testing.T) {
		_, err := runHandleRaw(rt.Runtime, context.Background(), row.NewRequestRow("user", nil), mustEncodeFrame(t, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		}, nil))
		assertLogAndIgnore[answer.ErrorUnregisteredAnswer](t, err)
	})

	t.Run("non external", func(t *testing.T) {
		requestRow := row.NewRequestRow("user", nil)
		if err := requestRow.Register(1, false); err != nil {
			t.Fatalf("register = %v", err)
		}

		_, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		}, nil))
		assertLogAndIgnore[answer.ErrorNonExternalAnswer](t, err)
	})

	t.Run("unexpected", func(t *testing.T) {
		requestRow := row.NewRequestRow("user", nil)
		if err := requestRow.Register(1, true); err != nil {
			t.Fatalf("register = %v", err)
		}

		_, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, mustEncodeFrame(t, frame.Frame{
			RequestID: 1,
			Body:      bodies.ReadAnswer{Value: values.String("v")},
		}, nil))
		assertLogAndIgnore[answer.ErrorUnexpectedAnswer](t, err)
	})
}

func TestHandleHandshakeIsUnexpected(t *testing.T) {
	rt := handleRuntime(t)
	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.NewHandshake("user", [32]byte{}, nil),
	}, nil))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if errorCode(t, frames[0]).Code() != fields.UnexpectedCommand {
		t.Errorf("code = %v, want unexpected command", errorCode(t, frames[0]).Code())
	}
}

func TestHandleBodyLimitClosesConnection(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(0)).Build()
	_, err := runHandle(t, rt, context.Background(), row.NewRequestRow("user", nil), mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("x"),
	}, nil))

	if !errors.Is(err, errs.ErrCloseConnection) {
		t.Fatalf("error = %v, want close connection", err)
	}

	if _, ok := errors.AsType[protocolerrs.ErrorBodyLimitIsExceeded](err); !ok {
		t.Fatalf("error = %v, want body limit exceeded", err)
	}
}

func TestHandleCancelledContextIsRequestInterrupted(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	rt := handleRuntime(t)
	rt.data["k"] = values.String("v")
	frames, err := runHandle(t, rt.Runtime, parent, row.NewRequestRow("user", nil), mustEncodeFrame(t, frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("k"),
	}, nil))
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(frames) != 1 || errorCode(t, frames[0]).Code() != fields.RequestInterrupted {
		t.Fatalf("frames = %#v, want request interrupted", frames)
	}
}

type storedRuntime struct {
	Runtime
	data map[string]values.String
}

func handleRuntime(t *testing.T) *storedRuntime {
	t.Helper()

	stored := &storedRuntime{data: map[string]values.String{}}
	cfg := config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)
	stored.Runtime = NewRuntimerBuilder(*cfg).
		WithHandlerRead(func(_ Context, key fields.Key) (value.V, error) {
			v, ok := stored.data[string(key)]
			if !ok {
				return nil, nil
			}

			return v, nil
		}).
		WithHandlerWrite(func(_ Context, key fields.Key, v value.V) error {
			s, ok := v.(values.String)
			if !ok {
				return errors.New("value is not a string")
			}

			stored.data[string(key)] = s
			return nil
		}).
		WithHandlerDelete(func(_ Context, key fields.Key) error {
			delete(stored.data, string(key))
			return nil
		}).
		Build()

	return stored
}

func runHandle(t *testing.T, rt Runtime, ctx context.Context, requestRow *row.RequestRow, raw []byte) ([]frame.Frame, error) {
	t.Helper()

	encoded, err := runHandleRaw(rt, ctx, requestRow, raw)
	if err != nil {
		return nil, err
	}

	frames := make([]frame.Frame, 0, len(encoded))
	for _, item := range encoded {
		got, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(item)))
		if decErr != nil {
			t.Fatalf("decode = %v", decErr)
		}

		frames = append(frames, got)
	}

	return frames, nil
}

func runHandleRaw(rt Runtime, ctx context.Context, requestRow *row.RequestRow, raw []byte) ([][]byte, error) {
	var frames [][]byte
	for encoded, err := range rt.Handle(ctx, bufio.NewReader(bytes.NewReader(raw)), requestRow) {
		if err != nil {
			return frames, err
		}

		frames = append(frames, encoded)
	}

	return frames, nil
}

func responseTo(t *testing.T, f frame.Frame) fields.Command {
	t.Helper()

	answer, ok := f.Body.(body.Answer)
	if !ok {
		t.Fatalf("body = %T, want an answer", f.Body)
	}

	return answer.IsResponseTo()
}

func assertLogAndIgnore[T error](t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, errs.ErrLogAndIgnore) {
		t.Fatalf("error = %v, want log and ignore", err)
	}

	if _, ok := errors.AsType[T](err); !ok {
		t.Fatalf("error = %v, want %T", err, *new(T))
	}
}
