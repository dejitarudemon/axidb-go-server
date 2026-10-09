package server

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Connection wraps [net.Conn] with a once-only Close and idle-ping clocks.
type Connection struct {
	net.Conn

	closeOnce sync.Once

	// lastActivity is Unix nanoseconds of the last client frame.
	lastActivity atomic.Int64
	// firstPingAt is Unix nanoseconds of the first unanswered idle Ping; 0 if none.
	firstPingAt atomic.Int64
}

func newConnection(conn net.Conn) *Connection {
	c := &Connection{Conn: conn}
	c.noteActivity()
	return c
}

// Close closes the underlying connection at most once.
func (c *Connection) Close() error {
	var err error

	c.closeOnce.Do(func() {
		err = c.Conn.Close()
	})

	return err
}

// noteActivity records that the client sent a frame and clears the Ping wait.
func (c *Connection) noteActivity() {
	c.lastActivity.Store(time.Now().UnixNano())
	c.firstPingAt.Store(0)
}

func (c *Connection) lastActivityTime() time.Time {
	return time.Unix(0, c.lastActivity.Load())
}

func (c *Connection) firstPingTime() time.Time {
	ns := c.firstPingAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// markFirstPing records the first idle Ping if one is not already outstanding.
func (c *Connection) markFirstPing(at time.Time) {
	c.firstPingAt.CompareAndSwap(0, at.UnixNano())
}

// closeConn removes the peer from the table, cancels its context, and closes
// the socket. It is safe to call more than once for the same connection.
func (s *Server) closeConn(ctx connContext) {
	s.table.Terminate(ctx.conn)

	if ctx.cancel != nil {
		ctx.cancel()
	}

	if err := ctx.conn.Close(); err != nil {
		s.error("failed to close connection", "source", peer(ctx), "error", err)
	}
}
