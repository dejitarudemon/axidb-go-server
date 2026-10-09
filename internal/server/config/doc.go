// Package config holds construction options for [server.Server].
//
// [ServerConfig] sets read and keepalive timeouts, the listen network, and the
// per-connection answer buffer size. [NewServerConfig] starts from the
// protocol heartbeat recommendations (Ping every 30s of idle time; close after
// 100s without activity following the first Ping).
package config
