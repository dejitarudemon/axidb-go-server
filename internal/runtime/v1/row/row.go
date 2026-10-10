package row

import (
	"errors"
	"sync"
	"time"

	"github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
)

// ErrNoFreeRequestID means every non-zero request ID is already registered.
var ErrNoFreeRequestID = errors.New("no free request ID")

// RequestRow tracks active independent request IDs for one login and the
// compressions negotiated for that connection.
//
// Each ID is stored with the isExternal flag passed to [RequestRow.Register]
// or [RequestRow.Reserve]. At most one server idle-ping id may be active; it
// carries a deadline and is dropped by [RequestRow.BeginIdlePing] when expired,
// or by [RequestRow.Terminate] when the client answers. The compression set is
// fixed by [NewRequestRow]. Count reports how many IDs are currently registered.
// Methods are safe for concurrent use. A RequestRow contains a mutex and must
// not be copied; share the pointer returned by [NewRequestRow].
type RequestRow struct {
	login        string
	requests     map[fields.RequestID]bool
	compressions map[fields.Compression]struct{}

	// next is the next candidate for [RequestRow.Reserve]. Zero means start at 1.
	next fields.RequestID

	// idlePingID is the outstanding server idle-ping reservation, or 0.
	idlePingID fields.RequestID
	// idlePingDeadline is when idlePingID becomes reusable if still unanswered.
	idlePingDeadline time.Time

	mx sync.RWMutex
}

// NewRequestRow returns an empty request set for login.
// compressions are the algorithms this connection may use; duplicate codes are
// kept once. A nil or empty list means no compression is supported.
func NewRequestRow(login string, compressions []fields.Compression) *RequestRow {
	c := make(map[fields.Compression]struct{}, len(compressions))
	for _, compression := range compressions {
		if _, ok := c[compression]; !ok {
			c[compression] = struct{}{}
		}
	}

	return &RequestRow{
		login:        login,
		requests:     make(map[fields.RequestID]bool),
		compressions: c,
		mx:           sync.RWMutex{},
	}
}

// Register marks requestID as active and stores isExternal with it.
//
// If requestID is already registered, Register returns [errs.ErrorRequestsConflict]
// and leaves the stored flag unchanged.
func (r *RequestRow) Register(requestID fields.RequestID, isExternal bool) error {
	r.mx.Lock()
	defer r.mx.Unlock()

	if _, ok := r.isRegistered(requestID); ok {
		return errs.NewErrorRequestsConflict(requestID)
	}

	r.requests[requestID] = isExternal

	return nil
}

// Reserve picks a free non-zero request ID, marks it active with isExternal,
// and returns it.
//
// Zero is never returned: protocol builders reject it for ping and similar
// frames. Candidates advance from the last reserved id; freed ids are reused
// after the counter wraps. If every non-zero ID is already registered, Reserve
// returns [ErrNoFreeRequestID].
func (r *RequestRow) Reserve(isExternal bool) (fields.RequestID, error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	return r.reserveLocked(isExternal)
}

// BeginIdlePing reserves a server idle-ping id that expires at deadline.
//
// If a previous idle ping is still before deadline, ok is false and id is 0 —
// callers must not send another ping. If that previous id is past deadline, it
// is dropped first. The new id is marked external so a client ping answer is
// accepted. A non-positive wait until deadline still reserves once; the next
// BeginIdlePing then treats it as expired.
func (r *RequestRow) BeginIdlePing(deadline time.Time) (id fields.RequestID, ok bool, err error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	now := time.Now()
	if r.idlePingID != 0 {
		if now.Before(r.idlePingDeadline) {
			return 0, false, nil
		}
		delete(r.requests, r.idlePingID)
		r.idlePingID = 0
		r.idlePingDeadline = time.Time{}
	}

	id, err = r.reserveLocked(true)
	if err != nil {
		return 0, false, err
	}

	r.idlePingID = id
	r.idlePingDeadline = deadline
	return id, true, nil
}

// Terminate removes requestID from the active set.
// An ID that is not registered is ignored. If it is the idle-ping id, the idle
// slot is cleared so [RequestRow.BeginIdlePing] may reserve again.
func (r *RequestRow) Terminate(requestID fields.RequestID) {
	r.mx.Lock()
	defer r.mx.Unlock()

	delete(r.requests, requestID)
	if r.idlePingID == requestID {
		r.idlePingID = 0
		r.idlePingDeadline = time.Time{}
	}
}

// IsRegistered reports the isExternal flag stored for requestID and whether
// that ID is active. The flag is false when the ID is not registered.
func (r *RequestRow) IsRegistered(requestID fields.RequestID) (bool, bool) {
	r.mx.RLock()
	defer r.mx.RUnlock()

	return r.isRegistered(requestID)
}

// Count returns the number of active request IDs.
func (r *RequestRow) Count() int {
	r.mx.RLock()
	defer r.mx.RUnlock()

	return len(r.requests)
}

// isRegistered reports whether requestID is in the set.
// The caller must hold r.mx.
func (r *RequestRow) isRegistered(requestID fields.RequestID) (bool, bool) {
	isExternal, ok := r.requests[requestID]
	return isExternal, ok
}

// reserveLocked implements Reserve while r.mx is held.
func (r *RequestRow) reserveLocked(isExternal bool) (fields.RequestID, error) {
	start := r.next
	if start == 0 {
		start = 1
	}

	id := start
	for {
		if _, ok := r.requests[id]; !ok {
			r.requests[id] = isExternal
			r.next = id + 1
			return id, nil
		}

		id++
		if id == 0 {
			id = 1
		}
		if id == start {
			return 0, ErrNoFreeRequestID
		}
	}
}

// Login returns the login passed to [NewRequestRow].
func (r *RequestRow) Login() string {
	return r.login
}

// IsSupportCompression reports whether compression was passed to [NewRequestRow].
func (r *RequestRow) IsSupportCompression(compression fields.Compression) bool {
	_, ok := r.compressions[compression]
	return ok
}
