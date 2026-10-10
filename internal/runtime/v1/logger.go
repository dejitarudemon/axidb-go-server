package runtime_v1

import (
	"time"

	"github.com/dejitarudemon/ignicula-wire/v1/fields"
)

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

// infoWithCtx is [Runtime.info] with login and request_id from ctx prepended.
func (r Runtime) infoWithCtx(ctx Context, msg string, args ...any) {
	r.info(msg, withCtx(ctx, args)...)
}

// warnWithCtx is [Runtime.warn] with login and request_id from ctx prepended.
func (r Runtime) warnWithCtx(ctx Context, msg string, args ...any) {
	r.warn(msg, withCtx(ctx, args)...)
}

// errorWithCtx is [Runtime.error] with login and request_id from ctx prepended.
func (r Runtime) errorWithCtx(ctx Context, msg string, args ...any) {
	r.error(msg, withCtx(ctx, args)...)
}

// debugWithCtx is [Runtime.debug] with login and request_id from ctx prepended.
func (r Runtime) debugWithCtx(ctx Context, msg string, args ...any) {
	r.debug(msg, withCtx(ctx, args)...)
}

func withTime(args []any) []any {
	return append([]any{"time", time.Now()}, args...)
}

// withCtx prepends login and request_id from ctx before args.
func withCtx(ctx Context, args []any) []any {
	return withLogin(ctx.Login(), ctx.RequestID(), args...)
}

// withLogin prepends login and request_id before args.
func withLogin(login string, requestID fields.RequestID, args ...any) []any {
	base := []any{"login", login, "request_id", requestID}
	return append(base, args...)
}
