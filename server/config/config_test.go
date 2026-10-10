package config

import (
	"crypto/tls"
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

func TestServerConfigTLSDefaultsOff(t *testing.T) {
	cfg := NewServerConfig()
	if cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = true, want false")
	}
	if cfg.TLSConfig() != nil || cfg.TLSCertFile() != "" || cfg.TLSKeyFile() != "" {
		t.Fatalf("unexpected TLS credentials: %#v %#v %#v", cfg.TLSConfig(), cfg.TLSCertFile(), cfg.TLSKeyFile())
	}
}

func TestServerConfigWithTLSConfig(t *testing.T) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	cfg := NewServerConfig().WithTLSConfig(tlsCfg)

	if !cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = false, want true")
	}
	if cfg.TLSConfig() != tlsCfg {
		t.Fatal("TLSConfig() pointer mismatch")
	}

	cfg.WithTLSConfig(nil)
	if cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() after clear = true, want false")
	}
}

func TestServerConfigWithTLSFiles(t *testing.T) {
	cfg := NewServerConfig().WithTLSFiles("cert.pem", "key.pem")
	if !cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = false, want true")
	}
	if cfg.TLSCertFile() != "cert.pem" || cfg.TLSKeyFile() != "key.pem" {
		t.Fatalf("files = %q %q", cfg.TLSCertFile(), cfg.TLSKeyFile())
	}

	cfg.WithTLSFiles("", "key.pem")
	if cfg.TLSConfigured() || cfg.TLSCertFile() != "" || cfg.TLSKeyFile() != "" {
		t.Fatal("partial WithTLSFiles should clear file credentials")
	}
}

func TestServerConfigTLSConfigPrefersOverFiles(t *testing.T) {
	tlsCfg := &tls.Config{}
	cfg := NewServerConfig().
		WithTLSFiles("cert.pem", "key.pem").
		WithTLSConfig(tlsCfg)

	if !cfg.TLSConfigured() {
		t.Fatal("TLSConfigured() = false, want true")
	}
	if cfg.TLSConfig() != tlsCfg {
		t.Fatal("TLSConfig() not set")
	}
	if cfg.TLSCertFile() != "cert.pem" {
		t.Fatal("files should remain set alongside TLSConfig")
	}
}
