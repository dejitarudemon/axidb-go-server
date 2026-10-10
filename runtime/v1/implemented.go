package runtime_v1

import (
	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/value"
)

// defaultHandlerRead is the stub read handler wired by [NewRuntimeBuilder] so the
// runtime never panics on a nil handler. Callers must register a real read
// handler via [RuntimeBuilder.WithHandlerRead].
//
// It returns [protocolerrs.ErrorCommandNotImplemented].
func defaultHandlerRead(ctx Context, key fields.Key) (value.V, error) {
	return nil, protocolerrs.NewErrorCommandNotImplemented()
}

// defaultHandlerWrite is the stub write handler wired by [NewRuntimeBuilder] so
// the runtime never panics on a nil handler. Callers must register a real write
// handler via [RuntimeBuilder.WithHandlerWrite].
//
// It returns [protocolerrs.ErrorCommandNotImplemented].
func defaultHandlerWrite(ctx Context, key fields.Key, value value.V) error {
	return protocolerrs.NewErrorCommandNotImplemented()
}

// defaultHandlerDelete is the stub delete handler wired by [NewRuntimeBuilder]
// so the runtime never panics on a nil handler. Callers must register a real
// delete handler via [RuntimeBuilder.WithHandlerDelete].
//
// It returns [protocolerrs.ErrorCommandNotImplemented].
func defaultHandlerDelete(ctx Context, key fields.Key) error {
	return protocolerrs.NewErrorCommandNotImplemented()
}

// defaultHandlerAuth is the stub auth handler wired by [NewRuntimeBuilder] so
// the runtime never panics on a nil handler. Callers must register a real auth
// handler via [RuntimeBuilder.WithHandlerAuth].
//
// It returns (false, [protocolerrs.ErrorCommandNotImplemented]).
func defaultHandlerAuth(ctx Context, login string, hash [32]byte) (bool, error) {
	return false, protocolerrs.NewErrorCommandNotImplemented()
}
