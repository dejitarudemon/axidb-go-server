package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

type executionState struct {
	interruptNext   bool
	withTracebackID fields.TracebackID

	mx sync.RWMutex
}

func (e *executionState) registerInterruption(tracebackID fields.TracebackID) {
	e.mx.Lock()
	defer e.mx.Unlock()

	if !e.interruptNext {
		e.interruptNext = true
		e.withTracebackID = tracebackID
	}
}

func (e *executionState) current() (bool, fields.TracebackID) {
	e.mx.RLock()
	defer e.mx.RUnlock()

	return e.interruptNext, e.withTracebackID
}
