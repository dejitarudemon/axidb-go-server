package runtime_v1

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
)

type memLogger struct {
	mu   sync.Mutex
	info []string
	warn []string
	err  []string
}

func (m *memLogger) Debug(string, ...any) {}

func (m *memLogger) Info(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.info = append(m.info, msg)
}

func (m *memLogger) Warn(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warn = append(m.warn, msg)
}

func (m *memLogger) Error(msg string, _ ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = append(m.err, msg)
}

func TestWithLoggerNilKeepsCurrent(t *testing.T) {
	log := &memLogger{}
	rb := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig()).WithLogger(log)
	if rb.WithLogger(nil) != rb {
		t.Fatal("WithLogger(nil) returned a different builder")
	}
	rt := rb.Build()
	if rt.logger != log {
		t.Fatal("nil WithLogger cleared logger")
	}
}

func TestActivateLogsUnauthorized(t *testing.T) {
	log := &memLogger{}
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).
		WithLogger(log).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return false, nil
		}).
		Build()

	req := frame.Frame{
		RequestID: 0,
		Body:      bodies.Handshake{Login: "user", Hash: [32]byte{1}},
	}
	_, _, _, err := rt.Activate(context.Background(), req, []byte("peer"), false)
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	found := false
	for _, msg := range log.warn {
		if msg == "unauthorized" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warn logs = %v, want unauthorized", log.warn)
	}
}

func TestLoggerAddsTimeField(t *testing.T) {
	var args []any
	log := loggerSpy{onInfo: func(_ string, a ...any) { args = a }}
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig()).WithLogger(log).Build()

	before := time.Now().Add(-time.Second)
	rt.info("hello", "k", 1)
	after := time.Now().Add(time.Second)

	if len(args) < 4 || args[0] != "time" {
		t.Fatalf("args = %#v, want time first", args)
	}
	ts, ok := args[1].(time.Time)
	if !ok || ts.Before(before) || ts.After(after) {
		t.Fatalf("time = %v (%T)", args[1], args[1])
	}
	if args[2] != "k" || args[3] != 1 {
		t.Fatalf("args = %#v", args)
	}
}

func TestInfoWithCtxAddsLoginAndRequestID(t *testing.T) {
	var args []any
	log := loggerSpy{onInfo: func(_ string, a ...any) { args = a }}
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig()).WithLogger(log).Build()

	ctx := NewContext(context.Background(), "alice", 7, true)
	rt.infoWithCtx(ctx, "read", "key", "k")

	if len(args) < 8 {
		t.Fatalf("args = %#v, want time/login/request_id/key", args)
	}
	if args[0] != "time" {
		t.Fatalf("args[0] = %v, want time", args[0])
	}
	if args[2] != "login" || args[3] != "alice" {
		t.Fatalf("login = %#v", args[2:4])
	}
	if args[4] != "request_id" || args[5] != fields.RequestID(7) {
		t.Fatalf("request_id = %#v", args[4:6])
	}
	if args[6] != "key" || args[7] != "k" {
		t.Fatalf("key = %#v", args[6:8])
	}
}

type loggerSpy struct {
	onInfo func(msg string, args ...any)
}

func (l loggerSpy) Debug(string, ...any) {}
func (l loggerSpy) Info(msg string, args ...any) {
	if l.onInfo != nil {
		l.onInfo(msg, args...)
	}
}
func (l loggerSpy) Warn(string, ...any)  {}
func (l loggerSpy) Error(string, ...any) {}
