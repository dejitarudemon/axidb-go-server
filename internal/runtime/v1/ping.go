package runtime_v1

import (
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

// Ping encodes a server-initiated idle ping for requestRow.
//
// ttl is how long the reserved request id stays outstanding if the client does
// not answer. When a non-expired idle ping is already reserved,
// Ping returns (nil, nil) and sends nothing. When the previous idle ping has
// expired, its id is dropped and a new ping is built. The id is marked external
// so a client ping answer is accepted by [Runtime.Handle]. A nil requestRow
// returns [errs.ErrCloseConnection]. A reserve, build, or encode failure returns
// [errs.ErrLogAndIgnore] after releasing the id when it was reserved.
func (r Runtime) Ping(requestRow *row.RequestRow, ttl time.Duration) ([]byte, error) {
	if requestRow == nil {
		r.error("ping with nil request row")
		return nil, errs.CloseConnection(errs.ErrNilRequestRow)
	}

	login := requestRow.Login()
	if ttl < 0 {
		ttl = 0
	}

	requestID, ok, err := requestRow.BeginIdlePing(time.Now().Add(ttl))
	if err != nil {
		r.warn("failed to reserve idle ping id", withLogin(login, 0, "error", err)...)
		return nil, errs.LogAndIgnore(err)
	}
	if !ok {
		r.debug("skip idle ping; previous still active", withLogin(login, 0)...)
		return nil, nil
	}

	frame, err := builder.NewFrameBuilder(r.limit).NewPing(requestID)
	if err != nil {
		requestRow.Terminate(requestID)
		r.warn("failed to build idle ping", withLogin(login, requestID, "error", err)...)
		return nil, errs.LogAndIgnore(err)
	}

	encoded, err := r.encodeFrame(frame, nil)
	if err != nil {
		requestRow.Terminate(requestID)
		r.warn("failed to encode idle ping", withLogin(login, requestID, "error", err)...)
		return nil, errs.LogAndIgnore(err)
	}

	r.debug("built idle ping", withLogin(login, requestID, "ttl", ttl)...)
	return encoded, nil
}
