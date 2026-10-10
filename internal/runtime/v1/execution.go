package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/ignicula-wire/v1/fields"
)

// executionState records whether a batch has already failed and the traceback
// of the first failure. It is safe for concurrent use.
type executionState struct {
	interruptNext   bool
	withTracebackID fields.TracebackID

	mx sync.RWMutex
}

// registerInterruption records tracebackID when no interruption is recorded yet.
// A later call keeps the first traceback.
func (e *executionState) registerInterruption(tracebackID fields.TracebackID) {
	e.mx.Lock()
	defer e.mx.Unlock()

	if !e.interruptNext {
		e.interruptNext = true
		e.withTracebackID = tracebackID
	}
}

// current reports whether an interruption is recorded and returns its traceback.
func (e *executionState) current() (bool, fields.TracebackID) {
	e.mx.RLock()
	defer e.mx.RUnlock()

	return e.interruptNext, e.withTracebackID
}
