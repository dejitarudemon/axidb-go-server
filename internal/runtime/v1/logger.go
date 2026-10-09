package runtime_v1

import "time"

// info records msg at info level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (r Runtime) info(msg string, args ...any) {
	if r.logger != nil {
		r.logger.Info(msg, withTime(args)...)
	}
}

// warn records msg at warn level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (r Runtime) warn(msg string, args ...any) {
	if r.logger != nil {
		r.logger.Warn(msg, withTime(args)...)
	}
}

// error records msg at error level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (r Runtime) error(msg string, args ...any) {
	if r.logger != nil {
		r.logger.Error(msg, withTime(args)...)
	}
}

// debug records msg at debug level when a logger is set.
// A nil logger is ignored. args are alternating keys and values.
// Every call adds a "time" field with the current local time.
func (r Runtime) debug(msg string, args ...any) {
	if r.logger != nil {
		r.logger.Debug(msg, withTime(args)...)
	}
}

func withTime(args []any) []any {
	return append([]any{"time", time.Now()}, args...)
}
