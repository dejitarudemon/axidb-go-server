package row

import (
	"errors"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// ErrNoFreeRequestID means every non-zero request ID is already registered.
var ErrNoFreeRequestID = errors.New("no free request ID")

// RequestRow tracks active independent request IDs for one login and the
// compressions negotiated for that connection.
//
// Each ID is stored with the isExternal flag passed to [RequestRow.Register]
// or [RequestRow.Reserve]. The compression set is fixed by [NewRequestRow].
// Count reports how many IDs are currently registered. Methods are safe for
// concurrent use. A RequestRow contains a mutex and must not be copied; share
// the pointer returned by [NewRequestRow].
type RequestRow struct {
	login        string
	requests     map[fields.RequestID]bool
	compressions map[fields.Compression]struct{}

	// next is the next candidate for [RequestRow.Reserve]. Zero means start at 1.
	next fields.RequestID

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

// Terminate removes requestID from the active set.
// An ID that is not registered is ignored.
func (r *RequestRow) Terminate(requestID fields.RequestID) {
	r.mx.Lock()
	defer r.mx.Unlock()

	delete(r.requests, requestID)
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

// Login returns the login passed to [NewRequestRow].
func (r *RequestRow) Login() string {
	return r.login
}

// IsSupportCompression reports whether compression was passed to [NewRequestRow].
func (r *RequestRow) IsSupportCompression(compression fields.Compression) bool {
	_, ok := r.compressions[compression]
	return ok
}
