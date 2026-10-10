package server

import (
	"errors"

	runtimeerrs "github.com/dejitarudemon/ignicula-framework/internal/runtime/errs"
)

// respondRuntimeError logs err, enqueues answer when present, and closes the
// connection when err is [runtimeerrs.ErrCloseConnection].
//
// Used for Decode and Activate outcomes where an error answer may still be sent
// before the connection is dropped.
func (s *Server) respondRuntimeError(ctx connContext, answer []byte, err error, msg string, kv ...any) {
	args := append([]any{"source", peer(ctx), "error", err}, kv...)
	s.error(msg, args...)
	s.sendAnswer(ctx, answer)

	if errors.Is(err, runtimeerrs.ErrCloseConnection) {
		s.closeConn(ctx)
	}
}
