package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// RequestRow tracks active independent request IDs for one login.
//
// Count reports how many IDs are currently registered. Methods are safe for
// concurrent use. A RequestRow contains a mutex and must not be copied; share
// the pointer returned by [NewRequestRow].
type RequestRow struct {
	login    string
	requests map[fields.RequestID]struct{}

	mx sync.RWMutex
}

// NewRequestRow returns an empty row for login.
func NewRequestRow(login string) *RequestRow {
	return &RequestRow{
		login:    login,
		requests: make(map[fields.RequestID]struct{}),
		mx:       sync.RWMutex{},
	}
}

// Register marks requestID as active.
//
// If requestID is already registered, Register returns [errs.ErrorRequestsConflict]
// and leaves the row unchanged.
func (r *RequestRow) Register(requestID fields.RequestID) error {
	r.mx.Lock()
	defer r.mx.Unlock()

	if r.isRegistered(requestID) {
		return errs.NewErrorRequestsConflict(requestID)
	}

	r.requests[requestID] = struct{}{}
	return nil
}

// Terminate removes requestID from the active set.
// An ID that is not registered is ignored.
func (r *RequestRow) Terminate(requestID fields.RequestID) {
	r.mx.Lock()
	defer r.mx.Unlock()

	delete(r.requests, requestID)
}

// IsRegistered reports whether requestID is currently active.
func (r *RequestRow) IsRegistered(requestID fields.RequestID) bool {
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
func (r *RequestRow) isRegistered(requestID fields.RequestID) bool {
	_, ok := r.requests[requestID]
	return ok
}

// Login returns the login passed to [NewRequestRow].
func (r *RequestRow) Login() string {
	return r.login
}
