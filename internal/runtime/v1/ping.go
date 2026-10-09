package runtime_v1

import (
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

// Ping encodes a server-initiated ping for requestRow.
//
// It reserves a free non-zero request id with [row.RequestRow.Reserve] marked
// external so a client ping answer is accepted by [Runtime.Handle], builds a
// ping frame, and returns the encoded bytes. The id stays registered until
// [row.RequestRow.Terminate] runs, typically when the ping answer is handled.
// A nil requestRow returns [errs.ErrCloseConnection]. A reserve, build, or
// encode failure returns [errs.ErrLogAndIgnore] after releasing the id when it
// was reserved.
func (r Runtime) Ping(requestRow *row.RequestRow) ([]byte, error) {
	if requestRow == nil {
		r.error("ping with nil request row")
		return nil, errs.CloseConnection(errs.ErrNilRequestRow)
	}

	requestID, err := requestRow.Reserve(true)
	if err != nil {
		r.warn("failed to reserve idle ping id", "login", requestRow.Login(), "error", err)
		return nil, errs.LogAndIgnore(err)
	}

	frame, err := builder.NewFrameBuilder(r.limit).NewPing(requestID)
	if err != nil {
		requestRow.Terminate(requestID)
		r.warn("failed to build idle ping", "login", requestRow.Login(), "request_id", requestID, "error", err)
		return nil, errs.LogAndIgnore(err)
	}

	encoded, err := r.encodeFrame(frame, nil)
	if err != nil {
		requestRow.Terminate(requestID)
		r.warn("failed to encode idle ping", "login", requestRow.Login(), "request_id", requestID, "error", err)
		return nil, errs.LogAndIgnore(err)
	}

	r.debug("built idle ping", "login", requestRow.Login(), "request_id", requestID)
	return encoded, nil
}
