package server

import (
	"errors"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
	frame_v1 "github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	runtimeerrs "github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
	"github.com/dejitarudemon/axidb-go-server/internal/table"
)

const versionV1 = fields.Version(1)

// handleV1 decodes one v1 frame and either activates the version or handles it.
// It returns true when the connection must stop.
func (s *Server) handleV1(ctx connContext) (stop bool) {
	if s.runtimes.v1 == nil {
		s.error("v1 runtime is nil", "source", peer(ctx))
		s.closeConn(ctx)
		return true
	}

	request, answer, err := s.runtimes.v1.Decode(ctx.reader)
	if err != nil {
		if isTimeout(err) {
			return s.onReadIdle(ctx)
		}
		if isDisconnect(err) {
			s.info("client disconnected", "source", peer(ctx))
			s.closeConn(ctx)
			return true
		}

		s.respondRuntimeError(ctx, answer, err, "failed to decode frame", "version", versionV1)
		return errors.Is(err, runtimeerrs.ErrCloseConnection)
	}

	// Decode may recover a protocol error into an answer with a nil error.
	if answer != nil {
		s.sendAnswer(ctx, answer)
		return false
	}

	go s.serveV1Frame(ctx, request)
	return false
}

// serveV1Frame activates v1 when needed, otherwise runs Handle for request.
func (s *Server) serveV1Frame(ctx connContext, request frame_v1.Frame) {
	reg, err := s.table.Get(ctx.conn, versionV1)
	if err != nil {
		if _, ok := errors.AsType[table.ErrorVersionNotGranted](err); ok {
			s.activateV1(ctx, request, false)
			return
		}
		if _, ok := errors.AsType[table.ErrorVersionNotActivated](err); ok {
			s.activateV1(ctx, request, true)
			return
		}

		s.error("failed to get a connection row", "source", peer(ctx), "error", err)
		s.closeConn(ctx)
		return
	}

	requestRow, ok := reg.(*row.RequestRow)
	if !ok {
		s.error("unexpected registration row type", "source", peer(ctx), "type", reg)
		s.closeConn(ctx)
		return
	}

	for answer, err := range s.runtimes.v1.Handle(ctx.Context, request, requestRow) {
		if stop := s.deliverV1Answer(ctx, answer, err); stop {
			return
		}
	}
}

// activateV1 runs Runtime.Activate, then Grant(1) → Activate(1), and finally
// grants any other versions returned by the handshake without activating them.
//
// skipAuth is true when v1 was already granted and only the request row is missing.
func (s *Server) activateV1(ctx connContext, request frame_v1.Frame, skipAuth bool) {
	source := []byte(peer(ctx))
	requestRow, grantedVersions, answer, err := s.runtimes.v1.Activate(ctx.Context, request, source, skipAuth)

	if err != nil {
		s.respondRuntimeError(ctx, answer, err, "failed to activate version", "version", versionV1)
		return
	}

	// Activate returns a nil row when the bytes are an error answer.
	if requestRow == nil {
		s.sendAnswer(ctx, answer)
		return
	}

	if err := s.table.Grant(ctx.conn, versionV1); err != nil {
		s.error("failed to grant connection version", "source", peer(ctx), "version", versionV1, "error", err)
		s.closeConn(ctx)
		return
	}

	if err := s.table.Activate(ctx.conn, versionV1, requestRow); err != nil {
		if _, ok := errors.AsType[table.ErrorVersionAlreadyActive](err); !ok {
			s.error("failed to store connection row", "source", peer(ctx), "version", versionV1, "error", err)
			s.closeConn(ctx)
			return
		}
	}

	if !skipAuth {
		if err := s.table.Grant(ctx.conn, toVersions(grantedVersions)...); err != nil {
			s.warn("failed to grant remaining versions after activating", "source", peer(ctx), "version", versionV1, "error", err)
		}
	}

	s.sendAnswer(ctx, answer)
	s.info("version is activated", "source", peer(ctx), "version", versionV1, "skip_auth", skipAuth, "granted_to", grantedVersions)
}

// deliverV1Answer applies one Handle yield. It returns true when the caller
// must stop iterating (connection closed).
func (s *Server) deliverV1Answer(ctx connContext, answer []byte, err error) (stop bool) {
	if err != nil {
		s.error("got an error while handling request", "source", peer(ctx), "error", err, "version", versionV1)

		if errors.Is(err, runtimeerrs.ErrCloseConnection) {
			s.closeConn(ctx)
			return true
		}
		if errors.Is(err, runtimeerrs.ErrLogAndIgnore) {
			return false
		}
	}

	s.sendAnswer(ctx, answer)
	return false
}
