package server

import (
	"errors"
	"io"
	"net"
	"time"

	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/table"
)

// nextReadDeadline returns when the next read should wake for keepalive or
// the configured read timeout.
//
// While an idle Ping is outstanding, the deadline is only the ping-timeout
// instant so the loop does not spin. Otherwise it is the earlier of
// [Server.readTimeout] and the next idle-Ping due time.
func (s *Server) nextReadDeadline(ctx connContext) time.Time {
	now := time.Now()

	if !s.table.IsRegistered(ctx.conn) {
		return now.Add(max(s.readTimeout, time.Millisecond))
	}

	if first := ctx.conn.firstPingTime(); !first.IsZero() {
		untilClose := first.Add(s.pingTimeout).Sub(now)
		return now.Add(max(untilClose, time.Millisecond))
	}

	wait := s.readTimeout
	untilPing := s.pingInterval - now.Sub(ctx.conn.lastActivityTime())
	if untilPing < wait {
		wait = untilPing
	}
	return now.Add(max(wait, time.Millisecond))
}

// onReadIdle runs when a read deadline expires with no bytes from the client.
//
// Before registration the connection is closed (Hello did not arrive in time).
// After registration, a connection with no active version is closed. Otherwise
// at most one idle Ping is sent per idle cycle on the minimum active version.
// Further Pings wait until client activity clears the outstanding Ping via
// [Connection.noteActivity]. The runtime refuses a second Ping while the
// previous idle-ping request id is still within [Server.pingTimeout].
// If [Server.pingTimeout] elapses since that Ping without any client frame,
// the connection is closed.
func (s *Server) onReadIdle(ctx connContext) (closed bool) {
	if !s.table.IsRegistered(ctx.conn) {
		s.warn("registration timed out", "source", peer(ctx))
		s.closeConn(ctx)
		return true
	}

	now := time.Now()

	if first := ctx.conn.firstPingTime(); !first.IsZero() {
		if now.Sub(first) >= s.pingTimeout {
			s.warn("ping timeout", "source", peer(ctx), "ping_timeout", s.pingTimeout)
			s.closeConn(ctx)
			return true
		}
		// Outstanding Ping: wait for activity or pingTimeout; do not flood.
		return false
	}

	if now.Sub(ctx.conn.lastActivityTime()) < s.pingInterval {
		return false
	}

	version, reg, err := s.table.MinActiveVersion(ctx.conn)
	if err != nil {
		if _, ok := errors.AsType[table.ErrorNoActiveVersion](err); ok {
			s.warn("no active version on idle connection", "source", peer(ctx))
			s.closeConn(ctx)
			return true
		}
		s.error("failed to resolve active version for ping", "source", peer(ctx), "error", err)
		s.closeConn(ctx)
		return true
	}

	raw, err := s.runtimes.ping(version, reg, s.pingTimeout)
	if err != nil {
		s.error("failed to build idle ping", "source", peer(ctx), "version", version, "error", err)
		if errors.Is(err, errs.ErrCloseConnection) {
			s.closeConn(ctx)
			return true
		}
		return false
	}
	if len(raw) == 0 {
		// Runtime still holds a non-expired idle-ping reservation.
		return false
	}

	ctx.conn.markFirstPing(now)
	s.sendAnswer(ctx, raw)
	s.info("sent idle ping", "source", peer(ctx), "version", version)
	return false
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// isDisconnect reports a clean or mid-frame peer close (EOF / unexpected EOF),
// including when wrapped by the protocol decoder.
func isDisconnect(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
