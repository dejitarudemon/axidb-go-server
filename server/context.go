package server

import (
	"bufio"
	"context"
)

// connContext is the per-connection read/write state shared by the read loop
// and the writer goroutine.
type connContext struct {
	context.Context

	cancel context.CancelFunc

	reader *bufio.Reader
	conn   *Connection

	answers chan []byte
}

func newConnContext(parent context.Context, conn *Connection, answers chan []byte) connContext {
	return connContext{
		Context: parent,
		reader:  bufio.NewReader(conn),
		conn:    conn,
		answers: answers,
	}
}

func newConnContextWithCancel(parent context.Context, conn *Connection, answers chan []byte) (connContext, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	connCtx := newConnContext(ctx, conn, answers)
	connCtx.cancel = cancel
	return connCtx, cancel
}

// peer returns a stable log label for the remote address.
func peer(ctx connContext) string {
	if ctx.conn == nil || ctx.conn.RemoteAddr() == nil {
		return ""
	}
	return ctx.conn.RemoteAddr().String()
}
