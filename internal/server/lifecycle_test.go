package server

import (
	"testing"
	"time"

	serverconfig "github.com/dejitarudemon/ignicula-framework/internal/server/config"
)

func TestNewServerNilConfigUsesDefaults(t *testing.T) {
	s := NewServer(nil, nil)

	if s.readTimeout != serverconfig.NewServerConfig().ReadTimeout() {
		t.Fatalf("readTimeout = %v, want default", s.readTimeout)
	}
	if s.pingInterval != serverconfig.NewServerConfig().PingInterval() {
		t.Fatalf("pingInterval = %v, want default", s.pingInterval)
	}
	if s.bufferSize != serverconfig.NewServerConfig().BufferSize() {
		t.Fatalf("bufferSize = %d, want default", s.bufferSize)
	}
}

func TestRegisterV1RuntimeNil(t *testing.T) {
	s := NewServer(testConfig(t), nil)

	if err := s.RegisterV1Runtime(nil); err == nil {
		t.Fatal("RegisterV1Runtime(nil) = nil, want error")
	}
}

func TestRegisterV1RuntimeAfterStart(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))

	if err := s.RegisterV1Runtime(testRuntime(t)); err == nil {
		t.Fatal("RegisterV1Runtime after Start = nil, want error")
	}
}

func TestRegisterV1RuntimeReplaceBeforeStart(t *testing.T) {
	s := NewServer(testConfig(t), nil)

	first := testRuntime(t)
	second := testRuntime(t)

	if err := s.RegisterV1Runtime(first); err != nil {
		t.Fatalf("RegisterV1Runtime(first) = %v", err)
	}
	if err := s.RegisterV1Runtime(second); err != nil {
		t.Fatalf("RegisterV1Runtime(second) = %v", err)
	}
	if s.runtimes.v1 != second {
		t.Fatal("runtime was not replaced")
	}
}

func TestStartTwice(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))

	if err := s.Start("127.0.0.1", 0); err == nil {
		t.Fatal("Start() twice = nil, want error")
	}
}

func TestCloseWhenNotStarted(t *testing.T) {
	s := NewServer(testConfig(t), nil)

	if err := s.Close(); err == nil {
		t.Fatal("Close() before Start = nil, want error")
	}
}

func TestCloseTwice(t *testing.T) {
	cfg := testConfig(t)
	s := NewServer(cfg, nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}
	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() again = %v", err)
	}
}

func TestStartAfterClose(t *testing.T) {
	cfg := testConfig(t)
	s := NewServer(cfg, nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}

	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() after Close = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() after restart = %v", err)
	}
}

func TestCloseWaitsForAcceptLoop(t *testing.T) {
	cfg := testConfig(t)
	s := NewServer(cfg, nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}
	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- s.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close() blocked longer than 2s")
	}
}

func TestStartListenFailureDoesNotMarkStarted(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}

	err := s.Start("256.256.256.256", 1)
	if err == nil {
		_ = s.Close()
		t.Fatal("Start() with bad addr = nil, want error")
	}

	if s.started.Load() {
		t.Fatal("started = true after failed Start")
	}

	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() after failed Start = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
}
