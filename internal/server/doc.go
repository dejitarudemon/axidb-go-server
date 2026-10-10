// Package server accepts TCP clients and drives the protocol handshake.
//
// [Server] listens for connections, registers each peer with a v0 Hello, then
// routes later frames to a registered protocol runtime (currently v1). Answers
// are written on a per-connection writer goroutine. A nil [logger.Logger] is
// allowed: log helpers become no-ops.
//
// Optional TLS is configured on [config.ServerConfig] via WithTLSConfig or
// WithTLSFiles; the listener requires TLS 1.3 or newer. Without credentials
// [Server.Start] still listens on plain TCP and warns (logger, or stdout when
// the logger is nil).
//
// Idle connections are checked with a server Ping on the minimum active
// protocol version from [table.ConnectionsTable.MinActiveVersion], using the
// intervals in [config.ServerConfig] (defaults follow the protocol heartbeat
// recommendations). A registered connection with no active version is closed.
//
// Lifecycle: [NewServer], optional [Server.RegisterV1Runtime], [Server.Start],
// then [Server.Close]. Close cancels active connections, closes the listener,
// and waits for the accept loop to finish.
package server
