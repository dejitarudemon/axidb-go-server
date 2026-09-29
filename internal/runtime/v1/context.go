package runtime_v1

import (
	"context"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// Context is the per-request context passed to runtime handlers.
//
// It embeds [context.Context] for cancellation and deadlines, and carries the
// caller login and the request ID of the current protocol request.
//
// A Context is created when an incoming request has been received, decoded, and
// registered. Login is always set: for a handshake request it is taken from the
// request body; for any other request it is taken from the connection metadata
// on the server.
type Context struct {
	context.Context

	login     string
	requestID fields.RequestID
}

// NewContext returns a Context that wraps parent and stores login and requestID.
// Login must be non-empty; see [Context] for how it is sourced.
func NewContext(parent context.Context, login string, requestID fields.RequestID) Context {
	return Context{parent, login, requestID}
}

// Login returns the caller login associated with this request.
func (c Context) Login() string {
	return c.login
}

// RequestID returns the protocol request ID of this request.
func (c Context) RequestID() fields.RequestID {
	return c.requestID
}
