package server

import (
	"sync"
	"testing"
	"time"

	serverconfig "github.com/dejitarudemon/ignicula-framework/server/config"
)

type memLogger struct {
	mu   sync.Mutex
	info []logRecord
	warn []logRecord
	err  []logRecord
}

type logRecord struct {
	msg  string
	args []any
}

func (m *memLogger) Debug(string, ...any) {}

func (m *memLogger) Info(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.info = append(m.info, logRecord{msg, append([]any(nil), args...)})
}

func (m *memLogger) Warn(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warn = append(m.warn, logRecord{msg, append([]any(nil), args...)})
}

func (m *memLogger) Error(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = append(m.err, logRecord{msg, append([]any(nil), args...)})
}

func TestLoggerAddsTimeField(t *testing.T) {
	log := &memLogger{}
	s := NewServer(serverconfig.NewServerConfig().WithNetwork("tcp"), log)

	before := time.Now().Add(-time.Second)
	s.info("hello", "k", "v")
	after := time.Now().Add(time.Second)

	if len(log.info) != 1 {
		t.Fatalf("info records = %d, want 1", len(log.info))
	}
	rec := log.info[0]
	if rec.msg != "hello" {
		t.Fatalf("msg = %q, want hello", rec.msg)
	}
	if len(rec.args) < 4 || rec.args[0] != "time" {
		t.Fatalf("args = %#v, want time as first key", rec.args)
	}
	ts, ok := rec.args[1].(time.Time)
	if !ok {
		t.Fatalf("time value = %T, want time.Time", rec.args[1])
	}
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("time = %v, out of range", ts)
	}
	if rec.args[2] != "k" || rec.args[3] != "v" {
		t.Fatalf("args = %#v, want original kv after time", rec.args)
	}
}
