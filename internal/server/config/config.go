package config

import (
	"crypto/tls"
	"time"
)

const (
	defaultNetwork      = "tcp4"
	defaultBufferSize   = 16
	defaultReadTimeout  = 30 * time.Second
	defaultPingInterval = 30 * time.Second
	defaultPingTimeout  = 100 * time.Second
)

// ServerConfig holds construction options for the TCP server.
//
// Use [NewServerConfig] for defaults, then chain With* methods. Fields are
// unexported; read them through the getter methods.
type ServerConfig struct {
	network      string
	bufferSize   int
	readTimeout  time.Duration
	pingInterval time.Duration
	pingTimeout  time.Duration

	// tls is an optional listener TLS config. When nil and no cert/key files
	// are set, the server listens in plain TCP.
	tls *tls.Config

	// tlsCertFile and tlsKeyFile are PEM paths loaded on Start when tls is nil.
	tlsCertFile string
	tlsKeyFile  string
}

// NewServerConfig returns a config with protocol-recommended keepalive defaults
// (30s idle Ping interval, 100s Ping reply/activity timeout), a 30s read
// timeout, buffer size 16, and network "tcp4". TLS is off until
// [ServerConfig.WithTLSConfig] or [ServerConfig.WithTLSFiles] is used.
func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		network:      defaultNetwork,
		bufferSize:   defaultBufferSize,
		readTimeout:  defaultReadTimeout,
		pingInterval: defaultPingInterval,
		pingTimeout:  defaultPingTimeout,
	}
}

// WithNetwork sets the address family passed to [net.Listen] (for example "tcp4").
// An empty network is ignored.
func (c *ServerConfig) WithNetwork(network string) *ServerConfig {
	if network != "" {
		c.network = network
	}
	return c
}

// WithBufferSize sets the per-connection answer channel capacity.
// A value below 1 is raised to 1.
func (c *ServerConfig) WithBufferSize(size int) *ServerConfig {
	c.bufferSize = max(1, size)
	return c
}

// WithReadTimeout sets the maximum wait for the next readable frame.
// Idle keepalive may wake sooner using [ServerConfig.PingInterval].
// A non-positive duration is ignored.
func (c *ServerConfig) WithReadTimeout(d time.Duration) *ServerConfig {
	if d > 0 {
		c.readTimeout = d
	}
	return c
}

// WithPingInterval sets how long a registered connection may stay idle before
// the server sends a Ping on the minimum active protocol version.
// A non-positive duration is ignored.
func (c *ServerConfig) WithPingInterval(d time.Duration) *ServerConfig {
	if d > 0 {
		c.pingInterval = d
	}
	return c
}

// WithPingTimeout sets how long the server waits for any client frame after the
// first idle Ping before closing the connection. A Ping answer is not required:
// any request resets the wait. A non-positive duration is ignored.
func (c *ServerConfig) WithPingTimeout(d time.Duration) *ServerConfig {
	if d > 0 {
		c.pingTimeout = d
	}
	return c
}

// WithTLSConfig sets the TLS configuration used when the server listens.
//
// A non-nil cfg enables TLS and takes precedence over
// [ServerConfig.WithTLSFiles]. The server raises MinVersion to TLS 1.3 if it
// is lower. A nil cfg clears a previously set TLS config but leaves cert/key
// file paths unchanged.
func (c *ServerConfig) WithTLSConfig(cfg *tls.Config) *ServerConfig {
	c.tls = cfg
	return c
}

// WithTLSFiles sets PEM certificate and private-key paths used when no
// [tls.Config] is set via [ServerConfig.WithTLSConfig].
//
// Both paths must be non-empty to enable file-based TLS. If either is empty,
// the file credentials are cleared. Files are loaded when the server starts.
func (c *ServerConfig) WithTLSFiles(certFile, keyFile string) *ServerConfig {
	if certFile == "" || keyFile == "" {
		c.tlsCertFile = ""
		c.tlsKeyFile = ""
		return c
	}
	c.tlsCertFile = certFile
	c.tlsKeyFile = keyFile
	return c
}

// Network returns the listen network name.
func (c *ServerConfig) Network() string { return c.network }

// BufferSize returns the per-connection answer channel capacity.
func (c *ServerConfig) BufferSize() int { return c.bufferSize }

// ReadTimeout returns the maximum wait for the next readable frame.
func (c *ServerConfig) ReadTimeout() time.Duration { return c.readTimeout }

// PingInterval returns the idle duration before a server Ping.
func (c *ServerConfig) PingInterval() time.Duration { return c.pingInterval }

// PingTimeout returns the wait for activity after the first idle Ping.
func (c *ServerConfig) PingTimeout() time.Duration { return c.pingTimeout }

// TLSConfig returns the TLS config set by [ServerConfig.WithTLSConfig], or nil.
func (c *ServerConfig) TLSConfig() *tls.Config { return c.tls }

// TLSCertFile returns the PEM certificate path set by [ServerConfig.WithTLSFiles].
func (c *ServerConfig) TLSCertFile() string { return c.tlsCertFile }

// TLSKeyFile returns the PEM private-key path set by [ServerConfig.WithTLSFiles].
func (c *ServerConfig) TLSKeyFile() string { return c.tlsKeyFile }

// TLSConfigured reports whether TLS credentials are present (a non-nil
// [tls.Config] or both cert and key file paths).
func (c *ServerConfig) TLSConfigured() bool {
	if c.tls != nil {
		return true
	}
	return c.tlsCertFile != "" && c.tlsKeyFile != ""
}
