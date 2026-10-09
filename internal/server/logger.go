package server

import "time"

// info records msg at info level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (s *Server) info(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Info(msg, withTime(args)...)
	}
}

// warn records msg at warn level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (s *Server) warn(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Warn(msg, withTime(args)...)
	}
}

// error records msg at error level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (s *Server) error(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Error(msg, withTime(args)...)
	}
}

func withTime(args []any) []any {
	return append([]any{"time", time.Now()}, args...)
}
