package server

import (
	"context"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
)

// serveConn runs the read loop and writer for one accepted connection.
func (s *Server) serveConn(parent context.Context, conn *Connection) {
	answers := make(chan []byte, s.bufferSize)
	ctx, cancel := newConnContextWithCancel(parent, conn, answers)

	s.active.Store(conn, cancel)
	defer s.active.Delete(conn)

	var wg sync.WaitGroup
	// cancel → wait writer → close socket (LIFO).
	defer func() {
		s.table.Terminate(conn)
		_ = conn.Close()
	}()
	defer wg.Wait()
	defer cancel()

	defer func() {
		if err := recover(); err != nil {
			s.error("recovered from panic", "error", err)
		}
	}()

	wg.Go(func() { s.writeLoop(ctx) })

	for {
		select {
		case <-ctx.Done():
			return

		default:
			deadline := s.nextReadDeadline(ctx)

			if err := conn.SetReadDeadline(deadline); err != nil {
				s.error("failed to set read deadline", "error", err, "deadline", deadline)
				s.closeConn(ctx)
				return
			}

			if s.dispatch(ctx) {
				return
			}
		}
	}
}

// dispatch routes the next frame to registration or a version handler.
// It returns true when the connection must stop (closed or cancelled).
func (s *Server) dispatch(ctx connContext) (stop bool) {
	if !s.table.IsRegistered(ctx.conn) {
		return s.register(ctx)
	}

	return s.dispatchRequest(ctx)
}

// dispatchRequest peeks the protocol version and routes a registered connection.
func (s *Server) dispatchRequest(ctx connContext) (stop bool) {
	version, err := s.peekVersion(ctx)
	if err != nil {
		if isTimeout(err) {
			return s.onReadIdle(ctx)
		}
		if isDisconnect(err) {
			s.info("client disconnected", "source", peer(ctx))
			s.closeConn(ctx)
			return true
		}

		s.error("failed to peek a preamble", "source", peer(ctx), "error", err)
		s.closeConn(ctx)
		return true
	}

	ctx.conn.noteActivity()

	if version == fields.Version(0) {
		s.warn("attempted a second registration", "source", peer(ctx))
		s.closeConn(ctx)
		return true
	}

	if !s.runtimes.supports(version) {
		s.error("got a request from unsupported version", "source", peer(ctx), "version", version)
		s.closeConn(ctx)
		return true
	}

	switch version {
	case fields.Version(1):
		return s.handleV1(ctx)
	}

	return false
}

// peekVersion reads the frame preamble without consuming the full frame.
func (s *Server) peekVersion(ctx connContext) (fields.Version, error) {
	return s.decoder.DecodePreamble(ctx.reader)
}
