package server

import (
	"github.com/dejitarudemon/ignicula-wire/v0/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/buffer"
)

// register handles the first v0 Hello on a new connection.
// It returns true when the connection must stop.
func (s *Server) register(ctx connContext) (stop bool) {
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

	if version != fields.Version(0) {
		s.warn("attempt to skip connection registration", "source", peer(ctx), "version", version)
		s.closeConn(ctx)
		return true
	}

	if _, err := s.decoder.DecodeFrame(ctx.reader); err != nil {
		if isTimeout(err) {
			return s.onReadIdle(ctx)
		}
		if isDisconnect(err) {
			s.info("client disconnected", "source", peer(ctx))
			s.closeConn(ctx)
			return true
		}

		s.error("failed to read a hello frame", "source", peer(ctx), "error", err)
		s.closeConn(ctx)
		return true
	}

	ctx.conn.noteActivity()
	s.info("got a registration request", "source", peer(ctx))

	if err := s.table.Register(ctx.conn); err != nil {
		s.error("failed to register a connection", "source", peer(ctx), "error", err)
		s.closeConn(ctx)
		return true
	}

	s.info("connection is registered", "source", peer(ctx))

	if err := s.sendHelloAnswer(ctx); err != nil {
		s.table.Terminate(ctx.conn)
		s.error("failed to answer client", "source", peer(ctx), "error", err, "stage", "Registration")
		s.closeConn(ctx)
		return true
	}

	return false
}

// sendHelloAnswer encodes the v0 Hello reply listing supported protocol versions.
func (s *Server) sendHelloAnswer(ctx connContext) error {
	frame, err := s.builder.NewHello(s.runtimes.supportedVersions())
	if err != nil {
		return err
	}

	buf := buffer.Slice{}
	buf.Preallocate(frame.Size())

	if err := frame.Encode(&buf); err != nil {
		return err
	}

	s.sendAnswer(ctx, buf.Bytes())
	return nil
}
