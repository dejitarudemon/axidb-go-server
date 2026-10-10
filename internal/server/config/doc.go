// Package config holds construction options for [server.Server].
//
// [ServerConfig] sets read and keepalive timeouts, the listen network, the
// per-connection answer buffer size, and optional TLS credentials
// ([ServerConfig.WithTLSConfig] or [ServerConfig.WithTLSFiles]).
// [NewServerConfig] starts from the protocol heartbeat recommendations
// (Ping every 30s of idle time; close after 100s without activity following
// the first Ping). TLS is off by default.
package config
