package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/decoder"
	protocolerr "github.com/dejitarudemon/axidb-go-protocol/v1/err"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value/values"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
)

func TestBatchWorkers(t *testing.T) {
	rt := Runtime{maxGoroutinePerBatch: 4}

	if got := rt.batchWorkers(10); got != 4 {
		t.Errorf("workers = %d, want 4", got)
	}

	if got := rt.batchWorkers(2); got != 2 {
		t.Errorf("workers = %d, want 2", got)
	}

	rt.maxGoroutinePerBatch = 0
	if got := rt.batchWorkers(5); got != 1 {
		t.Errorf("workers = %d, want 1", got)
	}
}

func TestSequentialBatchFramesRunInNumberOrder(t *testing.T) {
	var mu sync.Mutex
	var got []fields.Key

	rt := batchRuntime(t, 1, func(_ Context, key fields.Key) (value.V, error) {
		mu.Lock()
		got = append(got, key)
		mu.Unlock()
		return values.String(string(key)), nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		IsSequentialExecution: true,
		Requests: []bodies.Request{
			{Number: 2, Body: bodies.Read("c")},
			{Number: 0, Body: bodies.Read("a")},
			{Number: 1, Body: bodies.Read("b")},
		},
	})

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	wantKeys := []string{"a", "b", "c"}
	if len(got) != len(wantKeys) {
		t.Fatalf("calls = %d, want %d", len(got), len(wantKeys))
	}

	for i, key := range wantKeys {
		if string(got[i]) != key {
			t.Errorf("call %d = %q, want %q", i, got[i], key)
		}

		if readValue(t, frames[i]) != values.String(key) {
			t.Errorf("frame %d = %v, want %q", i, readValue(t, frames[i]), key)
		}
	}
}

func TestSequentialBatchInterruptSkipsTheRest(t *testing.T) {
	calls := map[string]int{}
	var mu sync.Mutex

	rt := batchRuntime(t, 1, func(_ Context, key fields.Key) (value.V, error) {
		mu.Lock()
		calls[string(key)]++
		mu.Unlock()

		if string(key) == "bad" {
			return nil, errors.New("boom")
		}

		return values.String(string(key)), nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		IsSequentialExecution: true,
		InterruptAfterError:   true,
		Requests: []bodies.Request{
			{Number: 0, Body: bodies.Read("ok")},
			{Number: 1, Body: bodies.Read("bad")},
			{Number: 2, Body: bodies.Read("later")},
		},
	})

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	if readValue(t, frames[0]) != values.String("ok") {
		t.Errorf("first = %v, want ok", readValue(t, frames[0]))
	}

	failed := errorCode(t, frames[1])
	interrupted := errorCode(t, frames[2])

	if failed.Code() != fields.InternalError {
		t.Errorf("failed code = %v, want internal", failed.Code())
	}

	if interrupted.Code() != fields.RequestInterrupted {
		t.Errorf("interrupted code = %v, want request interrupted", interrupted.Code())
	}

	if failed.TracebackID() != interrupted.TracebackID() {
		t.Errorf("traceback = %v, want %v", interrupted.TracebackID(), failed.TracebackID())
	}

	if calls["later"] != 0 {
		t.Errorf("later calls = %d, want 0", calls["later"])
	}
}

func TestSequentialBatchWithoutInterruptRunsTheRest(t *testing.T) {
	rt := batchRuntime(t, 1, func(_ Context, key fields.Key) (value.V, error) {
		if string(key) == "bad" {
			return nil, errors.New("boom")
		}

		return values.String(string(key)), nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		IsSequentialExecution: true,
		Requests: []bodies.Request{
			{Number: 0, Body: bodies.Read("bad")},
			{Number: 1, Body: bodies.Read("next")},
		},
	})

	if len(frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(frames))
	}

	if errorCode(t, frames[0]).Code() != fields.InternalError {
		t.Errorf("first code = %v, want internal", errorCode(t, frames[0]).Code())
	}

	if readValue(t, frames[1]) != values.String("next") {
		t.Errorf("second = %v, want next", readValue(t, frames[1]))
	}
}

func TestSequentialOneAnswerInterrupt(t *testing.T) {
	rt := batchRuntime(t, 1, func(_ Context, key fields.Key) (value.V, error) {
		if string(key) == "bad" {
			return nil, protocolerrs.NewErrorNotFound(key)
		}

		return values.String(string(key)), nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		IsSequentialExecution: true,
		InterruptAfterError:   true,
		IsOneAnswer:           true,
		Requests: []bodies.Request{
			{Number: 0, Body: bodies.Read("ok")},
			{Number: 1, Body: bodies.Read("bad")},
			{Number: 2, Body: bodies.Read("later")},
		},
	})

	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}

	results := batchAnswer(t, frames[0])
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	if results[0].Number != 0 || results[0].Body.IsResponseTo() != fields.Read {
		t.Errorf("result 0 = %+v, want read 0", results[0])
	}

	notFound := resultError(t, results[1])
	interrupted := resultError(t, results[2])

	if notFound.Code() != fields.NotFound {
		t.Errorf("result 1 code = %v, want not found", notFound.Code())
	}

	if interrupted.Code() != fields.RequestInterrupted {
		t.Errorf("result 2 code = %v, want request interrupted", interrupted.Code())
	}

	if notFound.TracebackID() != interrupted.TracebackID() {
		t.Errorf("traceback = %v, want %v", interrupted.TracebackID(), notFound.TracebackID())
	}
}

func TestParallelBatchStartsTogether(t *testing.T) {
	const n = 3

	started := make(chan struct{}, n)
	release := make(chan struct{})

	rt := batchRuntime(t, n, func(Context, fields.Key) (value.V, error) {
		started <- struct{}{}
		<-release
		return values.String("ok"), nil
	}, nil, nil)

	requests := make([]bodies.Request, n)
	for i := range requests {
		requests[i] = bodies.Request{Number: fields.RequestNumber(i), Body: bodies.Read("k")}
	}

	done := make(chan []frame.Frame, 1)
	errs := make(chan error, 1)
	go func() {
		frames, err := collectBatch(rt, batchCtx(), bodies.Batch{Requests: requests})
		if err != nil {
			errs <- err
			return
		}

		done <- frames
	}()

	timeout := time.After(time.Second)
	for range n {
		select {
		case <-started:
		case <-timeout:
			close(release)
			t.Fatal("commands did not start together")
		}
	}

	close(release)

	select {
	case err := <-errs:
		t.Fatalf("batch error = %v", err)
	case frames := <-done:
		if len(frames) != n {
			t.Fatalf("frames = %d, want %d", len(frames), n)
		}
	case <-timeout:
		t.Fatal("batch did not finish")
	}
}

func TestParallelBatchRespectsGoroutineLimit(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var peak atomic.Int32
	var inflight atomic.Int32

	rt := batchRuntime(t, 2, func(Context, fields.Key) (value.V, error) {
		n := inflight.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}

		started <- struct{}{}
		<-release
		inflight.Add(-1)
		return values.String("ok"), nil
	}, nil, nil)

	requests := make([]bodies.Request, 4)
	for i := range requests {
		requests[i] = bodies.Request{Number: fields.RequestNumber(i), Body: bodies.Read("k")}
	}

	done := make(chan error, 1)
	go func() {
		_, err := collectBatch(rt, batchCtx(), bodies.Batch{Requests: requests})
		done <- err
	}()

	<-started
	<-started

	third := false
	select {
	case <-started:
		third = true
	case <-time.After(80 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("batch error = %v", err)
	}

	if third {
		t.Fatal("a third command started while two were still running")
	}

	if peak.Load() > 2 {
		t.Fatalf("peak = %d, want at most 2", peak.Load())
	}
}

func TestParallelInterruptDoesNotRunTheRest(t *testing.T) {
	var calls atomic.Int32

	rt := batchRuntime(t, 1, func(_ Context, key fields.Key) (value.V, error) {
		calls.Add(1)
		if string(key) == "bad" {
			return nil, errors.New("boom")
		}

		return values.String(string(key)), nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		InterruptAfterError: true,
		Requests: []bodies.Request{
			{Number: 0, Body: bodies.Read("bad")},
			{Number: 1, Body: bodies.Read("later")},
			{Number: 2, Body: bodies.Read("after")},
		},
	})

	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}

	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(frames))
	}

	var failed, interrupted protocolerr.ProtocolError
	for _, f := range frames {
		pe := errorCode(t, f)
		switch pe.Code() {
		case fields.InternalError:
			failed = pe
		case fields.RequestInterrupted:
			if interrupted == nil {
				interrupted = pe
			} else if interrupted.TracebackID() != pe.TracebackID() {
				t.Errorf("interrupted traceback = %v, want %v", pe.TracebackID(), interrupted.TracebackID())
			}
		default:
			t.Errorf("code = %v, want internal or request interrupted", pe.Code())
		}
	}

	if failed == nil || interrupted == nil {
		t.Fatal("missing failed or interrupted answer")
	}

	if failed.TracebackID() != interrupted.TracebackID() {
		t.Errorf("traceback = %v, want %v", interrupted.TracebackID(), failed.TracebackID())
	}
}

func TestParallelOneAnswerIsNumbered(t *testing.T) {
	rt := batchRuntime(t, 3, func(_ Context, key fields.Key) (value.V, error) {
		return values.String(string(key)), nil
	}, func(Context, fields.Key, value.V) error {
		return nil
	}, func(Context, fields.Key) error {
		return nil
	})

	frames := runBatch(t, rt, bodies.Batch{
		IsOneAnswer: true,
		Requests: []bodies.Request{
			{Number: 2, Body: bodies.Delete("c")},
			{Number: 0, Body: bodies.Read("a")},
			{Number: 1, Body: bodies.Write{Key: fields.Key("b"), Value: values.String("v")}},
		},
	})

	if len(frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(frames))
	}

	results := batchAnswer(t, frames[0])
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	want := []fields.Command{fields.Read, fields.Write, fields.Delete}
	for i, command := range want {
		if results[i].Number != fields.RequestNumber(i) {
			t.Errorf("result %d number = %d", i, results[i].Number)
		}

		if results[i].Body.IsResponseTo() != command {
			t.Errorf("result %d command = %v, want %v", i, results[i].Body.IsResponseTo(), command)
		}
	}
}

func TestBatchRejectsNilAndUnexpectedCommands(t *testing.T) {
	rt := batchRuntime(t, 1, func(Context, fields.Key) (value.V, error) {
		t.Fatal("handler should not run")
		return nil, nil
	}, nil, nil)

	frames := runBatch(t, rt, bodies.Batch{
		IsSequentialExecution: true,
		IsOneAnswer:           true,
		Requests: []bodies.Request{
			{Number: 0, Body: nil},
			{Number: 1, Body: bodies.Ping{}},
		},
	})

	results := batchAnswer(t, frames[0])
	if resultError(t, results[0]).Code() != fields.UnsupportedCommand {
		t.Errorf("nil body code = %v, want unsupported", resultError(t, results[0]).Code())
	}

	if resultError(t, results[1]).Code() != fields.UnexpectedCommandInBatch {
		t.Errorf("ping code = %v, want unexpected command in batch", resultError(t, results[1]).Code())
	}
}

func TestBatchStopsWhenContextIsCancelled(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32

	rt := batchRuntime(t, 1, func(ctx Context, key fields.Key) (value.V, error) {
		calls.Add(1)
		if string(key) == "first" {
			once.Do(func() { close(entered) })
			<-ctx.Done()
			return nil, ctx.Err()
		}

		return values.String(string(key)), nil
	}, nil, nil)

	done := make(chan []frame.Frame, 1)
	failed := make(chan error, 1)
	go func() {
		frames, err := collectBatch(rt, NewContext(parent, "user", 7, true), bodies.Batch{
			IsSequentialExecution: true,
			Requests: []bodies.Request{
				{Number: 0, Body: bodies.Read("first")},
				{Number: 1, Body: bodies.Read("second")},
			},
		})
		if err != nil {
			failed <- err
			return
		}

		done <- frames
	}()

	<-entered
	cancel()

	select {
	case err := <-failed:
		t.Fatalf("batch error = %v", err)
	case frames := <-done:
		if len(frames) != 2 {
			t.Fatalf("frames = %d, want 2", len(frames))
		}

		first := errorCode(t, frames[0])
		second := errorCode(t, frames[1])
		if first.Code() != fields.RequestInterrupted || second.Code() != fields.RequestInterrupted {
			t.Fatalf("codes = %v, %v, want request interrupted", first.Code(), second.Code())
		}

		if first.TracebackID() != second.TracebackID() {
			t.Errorf("traceback = %v, want %v", second.TracebackID(), first.TracebackID())
		}
	case <-time.After(time.Second):
		t.Fatal("batch did not stop")
	}

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestBatchStopsWhenYieldReturnsFalse(t *testing.T) {
	var calls atomic.Int32

	rt := batchRuntime(t, 1, func(Context, fields.Key) (value.V, error) {
		calls.Add(1)
		return values.String("ok"), nil
	}, nil, nil)

	n := 0
	for _, err := range rt.handleBatch(batchCtx(), bodies.Batch{
		IsSequentialExecution: true,
		Requests: []bodies.Request{
			{Number: 0, Body: bodies.Read("a")},
			{Number: 1, Body: bodies.Read("b")},
		},
	}) {
		if err != nil {
			t.Fatalf("yield error = %v", err)
		}

		n++
		break
	}

	if n != 1 {
		t.Errorf("yields = %d, want 1", n)
	}

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestToProtocolError(t *testing.T) {
	rt := Runtime{}
	original := protocolerrs.NewErrorNotFound(fields.Key("k"))
	gotSame := rt.toProtocolError(original)

	if gotSame.Code() != original.Code() || gotSame.TracebackID() != original.TracebackID() {
		t.Errorf("protocol error = %v %v, want %v %v", gotSame.Code(), gotSame.TracebackID(), original.Code(), original.TracebackID())
	}

	got := rt.toProtocolError(errors.New("boom"))
	if got.Code() != fields.InternalError {
		t.Errorf("code = %v, want internal", got.Code())
	}
}

func batchRuntime(
	t *testing.T,
	workers int,
	read func(Context, fields.Key) (value.V, error),
	write func(Context, fields.Key, value.V) error,
	del func(Context, fields.Key) error,
) Runtime {
	t.Helper()

	cfg := config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20).WithMaxGoroutinesPerBatch(workers)
	rb := NewRuntimerBuilder(*cfg)
	if read != nil {
		rb.WithHandlerRead(read)
	}
	if write != nil {
		rb.WithHandlerWrite(write)
	}
	if del != nil {
		rb.WithHandlerDelete(del)
	}

	return rb.Build()
}

func batchCtx() Context {
	return NewContext(context.Background(), "user", 7, true)
}

func runBatch(t *testing.T, rt Runtime, b bodies.Batch) []frame.Frame {
	t.Helper()

	frames, err := collectBatch(rt, batchCtx(), b)
	if err != nil {
		t.Fatalf("batch = %v", err)
	}

	for _, f := range frames {
		if f.RequestID != 7 {
			t.Errorf("request id = %d, want 7", f.RequestID)
		}
	}

	return frames
}

func collectBatch(rt Runtime, ctx Context, b bodies.Batch) ([]frame.Frame, error) {
	var frames []frame.Frame
	for raw, err := range rt.handleBatch(ctx, b) {
		if err != nil {
			return nil, err
		}

		got, decErr := decoder.NewDecoder(1<<20, nil).DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
		if decErr != nil {
			return nil, decErr
		}

		frames = append(frames, got)
	}

	return frames, nil
}

func readValue(t *testing.T, f frame.Frame) values.String {
	t.Helper()

	body, ok := f.Body.(bodies.ReadAnswer)
	if !ok {
		t.Fatalf("body = %T, want ReadAnswer", f.Body)
	}

	s, ok := body.Value.(values.String)
	if !ok {
		t.Fatalf("value = %T, want string", body.Value)
	}

	return s
}

func errorCode(t *testing.T, f frame.Frame) protocolerr.ProtocolError {
	t.Helper()

	body, ok := f.Body.(bodies.ErrorAnswer)
	if !ok {
		t.Fatalf("body = %T, want ErrorAnswer", f.Body)
	}

	return body.Err
}

func batchAnswer(t *testing.T, f frame.Frame) bodies.BatchAnswer {
	t.Helper()

	body, ok := f.Body.(bodies.BatchAnswer)
	if !ok {
		t.Fatalf("body = %T, want BatchAnswer", f.Body)
	}

	return body
}

func resultError(t *testing.T, result bodies.Result) protocolerr.ProtocolError {
	t.Helper()

	body, ok := result.Body.(bodies.ErrorAnswer)
	if !ok {
		t.Fatalf("result body = %T, want ErrorAnswer", result.Body)
	}

	return body.Err
}
