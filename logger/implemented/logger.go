package implemented

import (
	"log/slog"

	"github.com/dejitarudemon/ignicula-framework/logger"
)

// Logger forwards [logger.Logger] calls to a [slog.Logger].
type Logger struct {
	s *slog.Logger
}

// New wraps s as a [logger.Logger]. A nil s uses [slog.Default].
func New(s *slog.Logger) *Logger {
	if s == nil {
		s = slog.Default()
	}
	return &Logger{s: s}
}

var _ logger.Logger = (*Logger)(nil)

func (l *Logger) Debug(msg string, args ...any) { l.s.Debug(msg, args...) }
func (l *Logger) Info(msg string, args ...any)  { l.s.Info(msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.s.Warn(msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.s.Error(msg, args...) }
