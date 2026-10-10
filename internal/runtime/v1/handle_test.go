package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dejitarudemon/ignicula-wire/v1/body"
	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	"github.com/dejitarudemon/ignicula-wire/v1/builder"
	protocolerr "github.com/dejitarudemon/ignicula-wire/v1/err"
	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-wire/v1/value"
	"github.com/dejitarudemon/ignicula-wire/v1/value/values"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/answer"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/config"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/row"
)

func TestHandleNilRowClosesConnection(t *testing.T) {
	rt := handleRuntime(t)

	_, err := runHandle(t, rt.Runtime, context.Background(), nil, frame.Frame{RequestID: 1, Body: bodies.Ping{}})
	if !errors.Is(err, errs.ErrCloseConnection) || !errors.Is(err, errs.ErrNilRequestRow) {
		t.Fatalf("error = %v, want close connection and nil row", err)
	}
}

func TestHandleReadWriteDeleteAndPing(t *testing.T) {
	rt := handleRuntime(t)
	rt.data["k"] = values.String("v")
	requestRow := row.NewRequestRow("user", nil)

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 7,
		Body:      bodies.Read("k"),
	})
	if err != nil {
		t.Fatalf("read error = %v", err)
	}

	if len(frames) != 1 || frames[0].RequestID != 7 || readValue(t, frames[0]) != values.String("v") {
		t.Fatalf("read frames = %#v", frames)
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}

	if len(rt.calls) != 1 || rt.calls[0].op != "read" || rt.calls[0].login != "user" || rt.calls[0].requestID != 7 || !rt.calls[0].external {
		t.Fatalf("read call = %+v", rt.calls)
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 2,
		Body:      bodies.Write{Key: fields.Key("k"), Value: values.String("n")},
	})
	if err != nil {
		t.Fatalf("write error = %v", err)
	}

	if len(frames) != 1 || frames[0].RequestID != 2 || responseTo(t, frames[0]) != fields.Write {
		t.Fatalf("write body = %#v", frames[0].Body)
	}

	if rt.data["k"] != values.String("n") {
		t.Errorf("stored = %q, want n", rt.data["k"])
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 3,
		Body:      bodies.Delete("k"),
	})
	if err != nil {
		t.Fatalf("delete error = %v", err)
	}

	if len(frames) != 1 || responseTo(t, frames[0]) != fields.Delete {
		t.Fatalf("delete body = %#v", frames[0].Body)
	}

	if _, ok := rt.data["k"]; ok {
		t.Errorf("key k is still stored")
	}

	frames, err = runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 4,
		Body:      bodies.Ping{},
	})
	if err != nil {
		t.Fatalf("ping error = %v", err)
	}

	if len(frames) != 1 || frames[0].RequestID != 4 || responseTo(t, frames[0]) != fields.Ping {
		t.Fatalf("ping body = %#v", frames[0].Body)
	}
}

func TestHandleMissingKeyIsNotFound(t *testing.T) {
	rt := handleRuntime(t)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("missing"),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	assertErrorCode(t, frames[0], fields.NotFound)
}

func TestHandleHandlerErrorIsInternal(t *testing.T) {
	rt := handleRuntime(t)
	rt.readErr = errors.New("db")
	rt.writeErr = errors.New("db")
	rt.deleteErr = errors.New("db")
	requestRow := row.NewRequestRow("user", nil)

	for _, body := range []body.Body{
		bodies.Read("k"),
		bodies.Write{Key: fields.Key("k"), Value: values.String("v")},
		bodies.Delete("k"),
	} {
		frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
			RequestID: 1,
			Body:      body,
		})
		if err != nil {
			t.Fatalf("%v error = %v", body.Command(), err)
		}

		assertErrorCode(t, frames[0], fields.InternalError)
	}
}

func TestHandleWriteRejectsNonString(t *testing.T) {
	rt := handleRuntime(t)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body:      bodies.Write{Key: fields.Key("k"), Value: values.Int(1)},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	assertErrorCode(t, frames[0], fields.InternalError)
	if _, ok := rt.data["k"]; ok {
		t.Errorf("key k was stored")
	}
}

func TestHandleDuplicateRequest(t *testing.T) {
	rt := handleRuntime(t)
	requestRow := row.NewRequestRow("user", nil)
	if err := requestRow.Register(1, true); err != nil {
		t.Fatalf("register = %v", err)
	}

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 1,
		Body:      bodies.Ping{},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	assertErrorCode(t, frames[0], fields.RequestsConflict)

	external, ok := requestRow.IsRegistered(1)
	if !ok || !external {
		t.Errorf("registered = (%v, %v), want (true, true)", external, ok)
	}

	if len(rt.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rt.calls)
	}
}

func TestHandleAnswers(t *testing.T) {
	rt := handleRuntime(t)

	t.Run("ping answer", func(t *testing.T) {
		requestRow := row.NewRequestRow("user", nil)
		if err := requestRow.Register(1, true); err != nil {
			t.Fatalf("register = %v", err)
		}

		raw, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		})
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
		requestRow := row.NewRequestRow("user", nil)
		_, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		})
		assertLogAndIgnore[answer.ErrorUnregisteredAnswer](t, err)
		if requestRow.Count() != 0 {
			t.Errorf("registered = %d, want 0", requestRow.Count())
		}
	})

	t.Run("non external", func(t *testing.T) {
		requestRow := row.NewRequestRow("user", nil)
		if err := requestRow.Register(1, false); err != nil {
			t.Fatalf("register = %v", err)
		}

		_, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, frame.Frame{
			RequestID: 1,
			Body:      bodies.PingAnswer{},
		})
		assertLogAndIgnore[answer.ErrorNonExternalAnswer](t, err)

		external, ok := requestRow.IsRegistered(1)
		if !ok || external {
			t.Errorf("registered = (%v, %v), want (false, true)", external, ok)
		}
	})

	unexpected := []body.Body{
		bodies.ReadAnswer{Value: values.String("v")},
		bodies.WriteAnswer{},
		bodies.DeleteAnswer{},
		bodies.ErrorAnswer{Err: protocolerrs.NewErrorNotFound(fields.Key("k"))},
		bodies.BatchAnswer{{Number: 1, Body: bodies.WriteAnswer{}}},
		bodies.HandshakeAnswer{},
	}
	for _, body := range unexpected {
		t.Run(body.Command().String()+" "+responseName(body), func(t *testing.T) {
			requestRow := row.NewRequestRow("user", nil)
			if err := requestRow.Register(1, true); err != nil {
				t.Fatalf("register = %v", err)
			}

			_, err := runHandleRaw(rt.Runtime, context.Background(), requestRow, frame.Frame{
				RequestID: 1,
				Body:      body,
			})
			assertLogAndIgnore[answer.ErrorUnexpectedAnswer](t, err)
			if requestRow.Count() != 0 {
				t.Errorf("registered = %d, want 0", requestRow.Count())
			}
		})
	}
}

func TestHandleHandshakeIsUnexpected(t *testing.T) {
	rt := handleRuntime(t)
	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body:      bodies.NewHandshake("user", [32]byte{}, nil),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	assertErrorCode(t, frames[0], fields.UnexpectedCommand)
}

func TestHandleInvalidBodiesAreInternalErrors(t *testing.T) {
	rt := handleRuntime(t)
	requestRow := row.NewRequestRow("user", nil)

	cases := []body.Body{
		bodies.Read(""),
		bodies.Batch{},
		bodies.Batch{Requests: []bodies.Request{
			{Number: 1, Body: bodies.Read("a")},
			{Number: 1, Body: bodies.Read("b")},
		}},
		bodies.Batch{Requests: []bodies.Request{{Number: 1, Body: bodies.Ping{}}}},
	}
	for _, body := range cases {
		frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
			RequestID: 1,
			Body:      body,
		})
		if err != nil {
			t.Fatalf("%T error = %v", body, err)
		}

		assertErrorCode(t, frames[0], fields.InternalError)
		if requestRow.Count() != 0 {
			t.Fatalf("%T left a registration", body)
		}
	}

	if len(rt.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rt.calls)
	}
}

func TestHandleZeroRequestIDIsInternalError(t *testing.T) {
	rt := handleRuntime(t)
	requestRow := row.NewRequestRow("user", nil)
	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 0,
		Body:      bodies.Ping{},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	assertErrorCode(t, frames[0], fields.InternalError)
	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}
}

func TestHandleCancelledContextIsRequestInterrupted(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	rt := handleRuntime(t)
	rt.data["k"] = values.String("v")
	requestRow := row.NewRequestRow("user", nil)

	commands := []body.Body{
		bodies.Read("k"),
		bodies.Write{Key: fields.Key("k"), Value: values.String("n")},
		bodies.Delete("k"),
		bodies.Ping{},
		bodies.Batch{Requests: []bodies.Request{{Number: 1, Body: bodies.Read("k")}}},
	}
	for i, command := range commands {
		frames, err := runHandle(t, rt.Runtime, parent, requestRow, frame.Frame{
			RequestID: fields.RequestID(i + 1),
			Body:      command,
		})
		if err != nil {
			t.Fatalf("%v error = %v", command.Command(), err)
		}

		if len(frames) != 1 {
			t.Fatalf("%v frames = %d, want 1", command.Command(), len(frames))
		}

		assertErrorCode(t, frames[0], fields.RequestInterrupted)
		if requestRow.Count() != 0 {
			t.Fatalf("%v registered = %d, want 0", command.Command(), requestRow.Count())
		}
	}

	if rt.data["k"] != values.String("v") {
		t.Fatalf("stored = %q, want v", rt.data["k"])
	}

	if len(rt.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rt.calls)
	}
}

func TestHandleCancelDuringHandlerIsRequestInterrupted(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		rt := handleRuntime(t)
		rt.data["k"] = values.String("v")
		rt.onCall = func(handlerCall) { cancel() }

		frames, err := runHandle(t, rt.Runtime, parent, row.NewRequestRow("user", nil), frame.Frame{
			RequestID: 1,
			Body:      bodies.Read("k"),
		})
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		assertErrorCode(t, frames[0], fields.RequestInterrupted)
		if rt.data["k"] != values.String("v") {
			t.Fatalf("stored = %q, want v", rt.data["k"])
		}
	})

	t.Run("write", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		rt := handleRuntime(t)
		rt.onCall = func(handlerCall) { cancel() }

		frames, err := runHandle(t, rt.Runtime, parent, row.NewRequestRow("user", nil), frame.Frame{
			RequestID: 1,
			Body:      bodies.Write{Key: fields.Key("k"), Value: values.String("n")},
		})
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		assertErrorCode(t, frames[0], fields.RequestInterrupted)
		if rt.data["k"] != values.String("n") {
			t.Fatalf("stored = %q, want n", rt.data["k"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		rt := handleRuntime(t)
		rt.data["k"] = values.String("v")
		rt.onCall = func(handlerCall) { cancel() }

		frames, err := runHandle(t, rt.Runtime, parent, row.NewRequestRow("user", nil), frame.Frame{
			RequestID: 1,
			Body:      bodies.Delete("k"),
		})
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		assertErrorCode(t, frames[0], fields.RequestInterrupted)
		if _, ok := rt.data["k"]; ok {
			t.Fatal("key k is still stored")
		}
	})
}

func TestHandleSequentialBatch(t *testing.T) {
	rt := handleRuntime(t)
	rt.data["a"] = values.String("A")
	requestRow := row.NewRequestRow("user", nil)
	request := frame.Frame{
		RequestID: 9,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			Requests: []bodies.Request{
				{Number: 2, Body: bodies.Read("missing")},
				{Number: 1, Body: bodies.Read("a")},
				{Number: 3, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	}

	var frames []frame.Frame
	for encoded, err := range rt.Handle(context.Background(), request, requestRow) {
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		if requestRow.Count() != 1 {
			t.Fatalf("registered during yield = %d, want 1", requestRow.Count())
		}

		frames = append(frames, decodeHandleFrame(t, rt.Runtime, encoded))
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	for _, got := range frames {
		if got.RequestID != 9 || responseTo(t, got) != fields.Batch {
			t.Fatalf("frame = %#v", got)
		}
	}

	results := []bodies.Result{
		onlyResult(t, frames[0]),
		onlyResult(t, frames[1]),
		onlyResult(t, frames[2]),
	}
	if results[0].Number != 1 || readResult(t, results[0]) != values.String("A") {
		t.Fatalf("first = %+v", results[0])
	}

	if results[1].Number != 2 || resultCode(t, results[1]) != fields.NotFound {
		t.Fatalf("second = %+v", results[1])
	}

	if results[2].Number != 3 || resultResponse(t, results[2]) != fields.Write {
		t.Fatalf("third = %+v", results[2])
	}

	if rt.data["b"] != values.String("B") {
		t.Errorf("stored b = %q", rt.data["b"])
	}

	if keys := callKeys(rt, "read"); len(keys) != 2 || keys[0] != "a" || keys[1] != "missing" {
		t.Fatalf("read order = %v, want [a missing]", keys)
	}
}

func TestHandleSequentialBatchOneAnswer(t *testing.T) {
	rt := handleRuntime(t)
	rt.data["a"] = values.String("A")

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 4,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			Requests: []bodies.Request{
				{Number: 2, Body: bodies.Delete("a")},
				{Number: 1, Body: bodies.Read("a")},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(frames) != 1 || frames[0].RequestID != 4 {
		t.Fatalf("frames = %#v", frames)
	}

	results := batchResults(t, frames[0])
	if len(results) != 2 || results[0].Number != 1 || results[1].Number != 2 {
		t.Fatalf("results = %+v", results)
	}

	if readResult(t, results[0]) != values.String("A") || resultResponse(t, results[1]) != fields.Delete {
		t.Fatalf("results = %+v", results)
	}

	if _, ok := rt.data["a"]; ok {
		t.Errorf("key a is still stored")
	}
}

func TestHandleSequentialInterruptAfterError(t *testing.T) {
	rt := handleRuntime(t)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			InterruptAfterError:   true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("missing")},
				{Number: 2, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	results := batchResults(t, frames[0])
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}

	if resultCode(t, results[0]) != fields.NotFound || resultCode(t, results[1]) != fields.RequestInterrupted {
		t.Fatalf("codes = %v %v", resultCode(t, results[0]), resultCode(t, results[1]))
	}

	if resultError(t, results[1]).TracebackID() != resultError(t, results[0]).TracebackID() {
		t.Fatalf("traceback = %v, want %v", resultError(t, results[1]).TracebackID(), resultError(t, results[0]).TracebackID())
	}

	if _, ok := rt.data["b"]; ok {
		t.Errorf("key b was written after the interruption")
	}

	if keys := callKeys(rt, "write"); len(keys) != 0 {
		t.Fatalf("writes = %v, want none", keys)
	}
}

func TestHandleSequentialContinuesAfterError(t *testing.T) {
	rt := handleRuntime(t)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("missing")},
				{Number: 2, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	results := batchResults(t, frames[0])
	if resultCode(t, results[0]) != fields.NotFound || resultResponse(t, results[1]) != fields.Write {
		t.Fatalf("results = %+v", results)
	}

	if rt.data["b"] != values.String("B") {
		t.Errorf("stored b = %q", rt.data["b"])
	}
}

func TestHandleBatchHandlerErrorIsInternal(t *testing.T) {
	rt := handleRuntime(t)
	rt.readErr = errors.New("db")

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			Requests:              []bodies.Request{{Number: 1, Body: bodies.Read("a")}},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if resultCode(t, onlyResult(t, frames[0])) != fields.InternalError {
		t.Fatalf("result = %+v", onlyResult(t, frames[0]))
	}
}

func TestHandleParallelBatch(t *testing.T) {
	rt := handleRuntimeN(t, 1)
	rt.data["a"] = values.String("A")
	rt.data["b"] = values.String("B")
	rt.data["c"] = values.String("C")

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 5,
		Body: bodies.Batch{
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("a")},
				{Number: 2, Body: bodies.Read("b")},
				{Number: 3, Body: bodies.Read("missing")},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	got := map[fields.RequestNumber]fields.Error{}
	valuesByNumber := map[fields.RequestNumber]value.V{}
	for _, gotFrame := range frames {
		if gotFrame.RequestID != 5 {
			t.Fatalf("request id = %d", gotFrame.RequestID)
		}

		result := onlyResult(t, gotFrame)
		if _, ok := result.Body.(bodies.ErrorAnswer); ok {
			got[result.Number] = resultCode(t, result)
			continue
		}

		valuesByNumber[result.Number] = readResult(t, result)
	}

	if valuesByNumber[1] != values.String("A") || valuesByNumber[2] != values.String("B") || got[3] != fields.NotFound {
		t.Fatalf("values = %v, errors = %v", valuesByNumber, got)
	}
}

func TestHandleParallelBatchOneAnswer(t *testing.T) {
	rt := handleRuntimeN(t, 1)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 6,
		Body: bodies.Batch{
			IsOneAnswer: true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Write{Key: fields.Key("a"), Value: values.String("A")}},
				{Number: 2, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
				{Number: 3, Body: bodies.Delete("missing")},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}

	got := map[fields.RequestNumber]fields.Command{}
	for _, result := range batchResults(t, frames[0]) {
		got[result.Number] = resultResponse(t, result)
	}

	if got[1] != fields.Write || got[2] != fields.Write || got[3] != fields.Delete {
		t.Fatalf("results = %v", got)
	}

	if rt.data["a"] != "A" || rt.data["b"] != "B" {
		t.Fatalf("stored = %v", rt.data)
	}
}

func TestHandleStopReleasesTheRequest(t *testing.T) {
	t.Run("sequential", func(t *testing.T) {
		rt := handleRuntime(t)
		rt.data["a"] = values.String("A")
		rt.data["b"] = values.String("B")
		requestRow := row.NewRequestRow("user", nil)
		frames := runHandleStop(t, rt.Runtime, requestRow, frame.Frame{
			RequestID: 1,
			Body: bodies.Batch{
				IsSequentialExecution: true,
				Requests: []bodies.Request{
					{Number: 1, Body: bodies.Read("a")},
					{Number: 2, Body: bodies.Read("b")},
				},
			},
		}, 1)
		if len(frames) != 1 || onlyResult(t, frames[0]).Number != 1 {
			t.Fatalf("frames = %+v", frames)
		}

		if requestRow.Count() != 0 {
			t.Errorf("registered = %d, want 0", requestRow.Count())
		}
	})

	t.Run("parallel", func(t *testing.T) {
		rt := handleRuntimeN(t, 1)
		rt.data["a"] = values.String("A")
		rt.data["b"] = values.String("B")
		rt.data["c"] = values.String("C")
		requestRow := row.NewRequestRow("user", nil)
		frames := runHandleStop(t, rt.Runtime, requestRow, frame.Frame{
			RequestID: 1,
			Body: bodies.Batch{
				Requests: []bodies.Request{
					{Number: 1, Body: bodies.Read("a")},
					{Number: 2, Body: bodies.Read("b")},
					{Number: 3, Body: bodies.Read("c")},
				},
			},
		}, 1)
		if len(frames) != 1 {
			t.Fatalf("frames = %d, want 1", len(frames))
		}

		if requestRow.Count() != 0 {
			t.Errorf("registered = %d, want 0", requestRow.Count())
		}
	})
}

func TestHandleParallelWorkersOverlap(t *testing.T) {
	rt := handleRuntimeN(t, 3)
	rt.data["a"] = values.String("A")
	rt.data["b"] = values.String("B")
	rt.data["c"] = values.String("C")

	started := make(chan struct{}, 3)
	release := make(chan struct{})
	rt.onCall = func(handlerCall) {
		started <- struct{}{}
		<-release
	}

	requestRow := row.NewRequestRow("user", nil)
	request := frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("a")},
				{Number: 2, Body: bodies.Read("b")},
				{Number: 3, Body: bodies.Read("c")},
			},
		},
	}

	type outcome struct {
		frames []frame.Frame
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, request)
		done <- outcome{frames, err}
	}()

	timeout := time.After(2 * time.Second)
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-timeout:
			t.Fatal("workers did not overlap")
		}
	}

	close(release)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("error = %v", got.err)
		}

		if len(got.frames) != 3 {
			t.Fatalf("frames = %d, want 3", len(got.frames))
		}

		if requestRow.Count() != 0 {
			t.Errorf("registered = %d, want 0", requestRow.Count())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("batch did not finish")
	}
}

func TestHandleParallelInterruptAfterError(t *testing.T) {
	rt := handleRuntimeN(t, 2)
	rt.data["slow"] = values.String("S")

	started := make(chan string, 2)
	releaseMissing := make(chan struct{})
	releaseSlow := make(chan struct{})
	rt.onCall = func(call handlerCall) {
		switch call.key {
		case "missing":
			started <- call.key
			<-releaseMissing
		case "slow":
			started <- call.key
			<-releaseSlow
		}
	}

	requestRow := row.NewRequestRow("user", nil)
	request := frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			InterruptAfterError: true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("missing")},
				{Number: 2, Body: bodies.Read("slow")},
				{Number: 3, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	}

	frames := make(chan frame.Frame, 3)
	failed := make(chan error, 1)
	go func() {
		defer close(frames)
		for encoded, err := range rt.Handle(context.Background(), request, requestRow) {
			if err != nil {
				failed <- err
				return
			}

			frames <- decodeHandleFrame(t, rt.Runtime, encoded)
		}
	}()

	seen := map[string]struct{}{}
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case key := <-started:
			seen[key] = struct{}{}
		case <-timeout:
			t.Fatalf("started = %v, want the missing read and the slow read", seen)
		}
	}

	close(releaseMissing)

	early := map[fields.RequestNumber]bodies.Result{}
	for _, got := range readFrames(t, frames, failed, 2) {
		result := onlyResult(t, got)
		early[result.Number] = result
	}

	if len(callKeys(rt, "write")) != 0 {
		t.Fatal("write ran after the interruption")
	}

	if _, ok := early[1]; !ok || resultCode(t, early[1]) != fields.NotFound {
		t.Fatalf("missing = %+v", early[1])
	}

	if _, ok := early[3]; !ok || resultCode(t, early[3]) != fields.RequestInterrupted {
		t.Fatalf("write result = %+v", early[3])
	}

	if resultError(t, early[3]).TracebackID() != resultError(t, early[1]).TracebackID() {
		t.Fatal("interrupted request has a different traceback")
	}

	close(releaseSlow)

	rest := readFrames(t, frames, failed, 1)
	result := onlyResult(t, rest[0])
	if result.Number != 2 || readResult(t, result) != values.String("S") {
		t.Fatalf("slow = %+v", result)
	}

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("extra frame")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("iterator did not finish")
	}

	if _, ok := rt.data["b"]; ok {
		t.Fatal("key b was written")
	}
}

func TestHandleSequentialInterruptYieldsSeparateFrames(t *testing.T) {
	rt := handleRuntime(t)

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			InterruptAfterError:   true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("missing")},
				{Number: 2, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
				{Number: 3, Body: bodies.Delete("c")},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	first := onlyResult(t, frames[0])
	second := onlyResult(t, frames[1])
	third := onlyResult(t, frames[2])
	if first.Number != 1 || resultCode(t, first) != fields.NotFound {
		t.Fatalf("first = %+v", first)
	}

	if second.Number != 2 || third.Number != 3 {
		t.Fatalf("order = %d %d", second.Number, third.Number)
	}

	if resultCode(t, second) != fields.RequestInterrupted || resultCode(t, third) != fields.RequestInterrupted {
		t.Fatalf("codes = %v %v", resultCode(t, second), resultCode(t, third))
	}

	if resultError(t, second).TracebackID() != resultError(t, first).TracebackID() || resultError(t, third).TracebackID() != resultError(t, first).TracebackID() {
		t.Fatal("followers do not reuse the first traceback")
	}

	if len(callKeys(rt, "write")) != 0 || len(callKeys(rt, "delete")) != 0 {
		t.Fatalf("calls = %+v", rt.calls)
	}
}

func TestHandleCancelDuringBatchInterruptsTheRest(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	rt := handleRuntime(t)
	rt.data["a"] = values.String("A")
	rt.onCall = func(call handlerCall) {
		if call.op == "read" {
			cancel()
		}
	}

	frames, err := runHandle(t, rt.Runtime, parent, row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("a")},
				{Number: 2, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	results := batchResults(t, frames[0])
	if resultCode(t, results[0]) != fields.RequestInterrupted || resultCode(t, results[1]) != fields.RequestInterrupted {
		t.Fatalf("results = %+v", results)
	}

	if len(callKeys(rt, "write")) != 0 {
		t.Fatalf("writes = %v", callKeys(rt, "write"))
	}

	if _, ok := rt.data["b"]; ok {
		t.Fatal("key b was written")
	}
}

func TestHandleParallelCancelDuringBatchInterruptsTheRest(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	rt := handleRuntimeN(t, 2)
	rt.data["a"] = values.String("A")
	rt.data["slow"] = values.String("S")

	started := make(chan string, 2)
	releaseRead := make(chan struct{})
	releaseSlow := make(chan struct{})
	rt.onCall = func(call handlerCall) {
		switch call.key {
		case "a":
			started <- call.key
			<-releaseRead
		case "slow":
			started <- call.key
			<-releaseSlow
		}
	}

	requestRow := row.NewRequestRow("user", nil)
	request := frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Read("a")},
				{Number: 2, Body: bodies.Read("slow")},
				{Number: 3, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("B")}},
			},
		},
	}

	frames := make(chan frame.Frame, 3)
	failed := make(chan error, 1)
	go func() {
		defer close(frames)
		for encoded, err := range rt.Handle(parent, request, requestRow) {
			if err != nil {
				failed <- err
				return
			}

			frames <- decodeHandleFrame(t, rt.Runtime, encoded)
		}
	}()

	seen := map[string]struct{}{}
	timeout := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case key := <-started:
			seen[key] = struct{}{}
		case <-timeout:
			t.Fatalf("started = %v, want the read and the slow read", seen)
		}
	}

	cancel()
	close(releaseRead)

	early := readFrames(t, frames, failed, 2)
	if len(callKeys(rt, "write")) != 0 {
		t.Fatal("write ran after cancellation")
	}

	earlyNumbers := map[fields.RequestNumber]struct{}{}
	for _, got := range early {
		result := onlyResult(t, got)
		earlyNumbers[result.Number] = struct{}{}
		if resultCode(t, result) != fields.RequestInterrupted {
			t.Fatalf("result %d = %+v", result.Number, result)
		}
	}

	if _, ok := earlyNumbers[1]; !ok {
		t.Fatal("cancelling read was not answered while the other read was blocked")
	}

	if _, ok := earlyNumbers[3]; !ok {
		t.Fatal("write was not answered while the other read was blocked")
	}

	close(releaseSlow)

	rest := readFrames(t, frames, failed, 1)
	result := onlyResult(t, rest[0])
	if result.Number != 2 || resultCode(t, result) != fields.RequestInterrupted {
		t.Fatalf("slow result = %+v", result)
	}

	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("extra frame")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("iterator did not finish")
	}

	if len(callKeys(rt, "write")) != 0 {
		t.Fatalf("writes = %v", callKeys(rt, "write"))
	}

	if _, ok := rt.data["b"]; ok {
		t.Fatal("key b was written")
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}
}

func TestHandleErrorAnswerTooLargeClosesConnection(t *testing.T) {
	rt := handleRuntime(t)
	rt.limit = 1
	requestRow := row.NewRequestRow("user", nil)

	_, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 1,
		Body:      bodies.Read("missing"),
	})
	assertClose(t, err)
	if _, ok := errors.AsType[protocolerr.BuildError](err); !ok {
		t.Fatalf("error = %v, want a build error", err)
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}
}

func TestHandleBatchWriteAndDeleteErrorsAreInternal(t *testing.T) {
	rt := handleRuntime(t)
	rt.writeErr = errors.New("db")
	rt.deleteErr = errors.New("db")
	rt.data["b"] = values.String("B")

	frames, err := runHandle(t, rt.Runtime, context.Background(), row.NewRequestRow("user", nil), frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			IsOneAnswer:           true,
			Requests: []bodies.Request{
				{Number: 1, Body: bodies.Write{Key: fields.Key("a"), Value: values.String("A")}},
				{Number: 2, Body: bodies.Delete("b")},
			},
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	results := batchResults(t, frames[0])
	if resultCode(t, results[0]) != fields.InternalError || resultCode(t, results[1]) != fields.InternalError {
		t.Fatalf("results = %+v", results)
	}

	if _, ok := rt.data["a"]; ok {
		t.Fatal("key a was written")
	}

	if rt.data["b"] != values.String("B") {
		t.Fatalf("stored b = %q, want B", rt.data["b"])
	}
}

func TestHandleBatchEncodeFailureIsReturned(t *testing.T) {
	rt := handleRuntime(t)
	rt.limit = 32
	rt.data["a"] = values.String(strings.Repeat("x", 200))
	requestRow := row.NewRequestRow("user", nil)

	frames, err := runHandle(t, rt.Runtime, context.Background(), requestRow, frame.Frame{
		RequestID: 1,
		Body: bodies.Batch{
			IsSequentialExecution: true,
			Requests:              []bodies.Request{{Number: 1, Body: bodies.Read("a")}},
		},
	})
	if len(frames) != 0 {
		t.Fatalf("frames = %d, want 0", len(frames))
	}

	if err == nil || errors.Is(err, errs.ErrCloseConnection) || errors.Is(err, errs.ErrLogAndIgnore) {
		t.Fatalf("error = %v, want the encode failure", err)
	}

	if _, ok := errors.AsType[protocolerr.BuildError](err); !ok {
		t.Fatalf("error = %v, want a build error", err)
	}

	if requestRow.Count() != 0 {
		t.Errorf("registered = %d, want 0", requestRow.Count())
	}
}

func TestHandleUnexpectedCommandInBatch(t *testing.T) {
	rt := handleRuntime(t)
	ctx := NewContext(context.Background(), "user", 1, true)
	got := rt.executeRequest(ctx, bodies.Request{Number: 7, Body: bodies.Ping{}}, false, builder.NewBatchResultsBuilder())

	encoded, err := rt.encodeBatchResult(row.NewRequestRow("user", nil), 1, got)
	if err != nil {
		t.Fatalf("encode = %v", err)
	}

	result := onlyResult(t, decodeHandleFrame(t, rt.Runtime, encoded))
	if result.Number != 7 || resultCode(t, result) != fields.UnexpectedCommandInBatch {
		t.Fatalf("result = %+v", result)
	}

	if len(rt.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rt.calls)
	}
}

type handlerCall struct {
	op        string
	key       string
	login     string
	requestID fields.RequestID
	external  bool
}

type storedRuntime struct {
	Runtime
	mu        sync.Mutex
	data      map[string]values.String
	calls     []handlerCall
	readErr   error
	writeErr  error
	deleteErr error
	onCall    func(handlerCall)
}

func handleRuntime(t *testing.T) *storedRuntime {
	t.Helper()

	return handleRuntimeN(t, 4)
}

func handleRuntimeN(t *testing.T, goroutines int) *storedRuntime {
	t.Helper()

	stored := &storedRuntime{data: map[string]values.String{}}
	cfg := config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20).WithMaxGoroutinesPerBatch(goroutines)
	stored.Runtime = NewRuntimeBuilder(*cfg).
		WithHandlerRead(func(ctx Context, key fields.Key) (value.V, error) {
			stored.note("read", ctx, key)
			if stored.readErr != nil {
				return nil, stored.readErr
			}

			stored.mu.Lock()
			defer stored.mu.Unlock()
			v, ok := stored.data[string(key)]
			if !ok {
				return nil, nil
			}

			return v, nil
		}).
		WithHandlerWrite(func(ctx Context, key fields.Key, v value.V) error {
			stored.note("write", ctx, key)
			if stored.writeErr != nil {
				return stored.writeErr
			}

			s, ok := v.(values.String)
			if !ok {
				return errors.New("value is not a string")
			}

			stored.mu.Lock()
			defer stored.mu.Unlock()
			stored.data[string(key)] = s
			return nil
		}).
		WithHandlerDelete(func(ctx Context, key fields.Key) error {
			stored.note("delete", ctx, key)
			if stored.deleteErr != nil {
				return stored.deleteErr
			}

			stored.mu.Lock()
			defer stored.mu.Unlock()
			delete(stored.data, string(key))
			return nil
		}).
		Build()

	return stored
}

func (s *storedRuntime) note(op string, ctx Context, key fields.Key) handlerCall {
	call := handlerCall{
		op:        op,
		key:       string(key),
		login:     ctx.Login(),
		requestID: ctx.RequestID(),
		external:  ctx.IsExternal(),
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	onCall := s.onCall
	s.mu.Unlock()
	if onCall != nil {
		onCall(call)
	}

	return call
}

func callKeys(rt *storedRuntime, op string) []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var keys []string
	for _, call := range rt.calls {
		if call.op == op {
			keys = append(keys, call.key)
		}
	}

	return keys
}

func runHandle(t *testing.T, rt Runtime, ctx context.Context, requestRow *row.RequestRow, request frame.Frame) ([]frame.Frame, error) {
	t.Helper()

	encoded, err := runHandleRaw(rt, ctx, requestRow, request)
	if err != nil {
		return nil, err
	}

	frames := make([]frame.Frame, 0, len(encoded))
	for _, item := range encoded {
		frames = append(frames, decodeHandleFrame(t, rt, item))
	}

	return frames, nil
}

func readFrames(t *testing.T, frames <-chan frame.Frame, failed <-chan error, n int) []frame.Frame {
	t.Helper()

	got := make([]frame.Frame, 0, n)
	timeout := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatalf("iterator ended after %d frames, want %d", len(got), n)
			}

			got = append(got, frame)
		case err := <-failed:
			t.Fatalf("error = %v", err)
		case <-timeout:
			t.Fatalf("timed out after %d frames, want %d", len(got), n)
		}
	}

	return got
}

func runHandleStop(t *testing.T, rt Runtime, requestRow *row.RequestRow, request frame.Frame, stopAfter int) []frame.Frame {
	t.Helper()

	var frames []frame.Frame
	for encoded, err := range rt.Handle(context.Background(), request, requestRow) {
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		frames = append(frames, decodeHandleFrame(t, rt, encoded))
		if len(frames) == stopAfter {
			break
		}
	}

	return frames
}

func runHandleRaw(rt Runtime, ctx context.Context, requestRow *row.RequestRow, request frame.Frame) ([][]byte, error) {
	var frames [][]byte
	for encoded, err := range rt.Handle(ctx, request, requestRow) {
		if err != nil {
			return frames, err
		}

		frames = append(frames, encoded)
	}

	return frames, nil
}

func decodeHandleFrame(t *testing.T, rt Runtime, raw []byte) frame.Frame {
	t.Helper()

	got, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("decode = %v", err)
	}

	return got
}

func responseTo(t *testing.T, f frame.Frame) fields.Command {
	t.Helper()

	got, ok := f.Body.(body.Answer)
	if !ok {
		t.Fatalf("body = %T, want an answer", f.Body)
	}

	return got.IsResponseTo()
}

func errorCode(t *testing.T, f frame.Frame) protocolerr.ProtocolError {
	t.Helper()

	got, ok := f.Body.(bodies.ErrorAnswer)
	if !ok || got.Err == nil {
		t.Fatalf("body = %#v, want an error answer", f.Body)
	}

	return got.Err
}

func assertErrorCode(t *testing.T, f frame.Frame, want fields.Error) {
	t.Helper()

	if got := errorCode(t, f).Code(); got != want {
		t.Fatalf("code = %v, want %v", got, want)
	}
}

func assertClose(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, errs.ErrCloseConnection) {
		t.Fatalf("error = %v, want close connection", err)
	}
}

func readValue(t *testing.T, f frame.Frame) value.V {
	t.Helper()

	got, ok := f.Body.(bodies.ReadAnswer)
	if !ok {
		t.Fatalf("body = %T, want a read answer", f.Body)
	}

	return got.Value
}

func batchResults(t *testing.T, f frame.Frame) bodies.BatchAnswer {
	t.Helper()

	got, ok := f.Body.(bodies.BatchAnswer)
	if !ok {
		t.Fatalf("body = %T, want a batch answer", f.Body)
	}

	return got
}

func onlyResult(t *testing.T, f frame.Frame) bodies.Result {
	t.Helper()

	results := batchResults(t, f)
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}

	return results[0]
}

func readResult(t *testing.T, result bodies.Result) value.V {
	t.Helper()

	got, ok := result.Body.(bodies.ReadAnswer)
	if !ok {
		t.Fatalf("result %d body = %T, want a read answer", result.Number, result.Body)
	}

	return got.Value
}

func resultResponse(t *testing.T, result bodies.Result) fields.Command {
	t.Helper()

	return result.IsResponseTo()
}

func resultError(t *testing.T, result bodies.Result) protocolerr.ProtocolError {
	t.Helper()

	got, ok := result.Body.(bodies.ErrorAnswer)
	if !ok || got.Err == nil {
		t.Fatalf("result %d body = %#v, want an error answer", result.Number, result.Body)
	}

	return got.Err
}

func resultCode(t *testing.T, result bodies.Result) fields.Error {
	t.Helper()

	return resultError(t, result).Code()
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

func responseName(b body.Body) string {
	got, ok := b.(body.Answer)
	if !ok {
		return ""
	}

	return got.IsResponseTo().String()
}
