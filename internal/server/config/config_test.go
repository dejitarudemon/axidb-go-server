package config

import (
	"testing"
	"time"
)

func TestNewServerConfigDefaults(t *testing.T) {
	cfg := NewServerConfig()

	if cfg.Network() != "tcp4" {
		t.Fatalf("Network() = %q, want tcp4", cfg.Network())
	}
	if cfg.BufferSize() != 16 {
		t.Fatalf("BufferSize() = %d, want 16", cfg.BufferSize())
	}
	if cfg.ReadTimeout() != 30*time.Second {
		t.Fatalf("ReadTimeout() = %v, want 30s", cfg.ReadTimeout())
	}
	if cfg.PingInterval() != 30*time.Second {
		t.Fatalf("PingInterval() = %v, want 30s", cfg.PingInterval())
	}
	if cfg.PingTimeout() != 100*time.Second {
		t.Fatalf("PingTimeout() = %v, want 100s", cfg.PingTimeout())
	}
}

func TestServerConfigWithers(t *testing.T) {
	cfg := NewServerConfig().
		WithNetwork("tcp").
		WithBufferSize(0).
		WithReadTimeout(5 * time.Second).
		WithPingInterval(10 * time.Second).
		WithPingTimeout(20 * time.Second)

	if cfg.Network() != "tcp" {
		t.Fatalf("Network() = %q, want tcp", cfg.Network())
	}
	if cfg.BufferSize() != 1 {
		t.Fatalf("BufferSize() = %d, want 1", cfg.BufferSize())
	}
	if cfg.ReadTimeout() != 5*time.Second {
		t.Fatalf("ReadTimeout() = %v", cfg.ReadTimeout())
	}
	if cfg.PingInterval() != 10*time.Second {
		t.Fatalf("PingInterval() = %v", cfg.PingInterval())
	}
	if cfg.PingTimeout() != 20*time.Second {
		t.Fatalf("PingTimeout() = %v", cfg.PingTimeout())
	}
}

func TestServerConfigIgnoresInvalid(t *testing.T) {
	cfg := NewServerConfig().
		WithNetwork("").
		WithReadTimeout(0).
		WithPingInterval(-1).
		WithPingTimeout(0)

	if cfg.Network() != defaultNetwork {
		t.Fatalf("Network() = %q, want default", cfg.Network())
	}
	if cfg.ReadTimeout() != defaultReadTimeout {
		t.Fatalf("ReadTimeout() changed")
	}
	if cfg.PingInterval() != defaultPingInterval {
		t.Fatalf("PingInterval() changed")
	}
	if cfg.PingTimeout() != defaultPingTimeout {
		t.Fatalf("PingTimeout() changed")
	}
}
