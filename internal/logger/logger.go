// Package logger defines the logging interface used by the server.
//
// Implementations may forward calls to [log/slog] or any other sink. A nil
// [Logger] means the caller skips logging.
package logger

// Logger records a message at one of four levels.
//
// msg is the message text. args are alternating keys and values, as in
// [log/slog]: for example Logger.Info("server is started", "addr", addr).
type Logger interface {
	// Debug records a message at debug level.
	Debug(msg string, args ...any)

	// Info records a message at info level.
	Info(msg string, args ...any)

	// Warn records a message at warn level.
	Warn(msg string, args ...any)

	// Error records a message at error level.
	Error(msg string, args ...any)
}
