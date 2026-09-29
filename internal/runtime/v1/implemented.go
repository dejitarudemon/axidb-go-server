package runtime_v1

import (
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
)

// defaultHandlerRead is the stub read handler wired by [NewRuntimerBuilder] so the
// runtime never panics on a nil handler. Callers must register a real read
// handler via [RuntimeBuilder.WithHandlerRead].
//
// It returns (nil, nil), which the runtime treats the same as (nil, ErrorNotFound).
func defaultHandlerRead(ctx Context, key fields.Key) (value.V, error) {
	return nil, nil
}

// defaultHandlerWrite is the stub write handler wired by [NewRuntimerBuilder] so
// the runtime never panics on a nil handler. It accepts the write and performs
// no storage. Callers must register a real write handler via
// [RuntimeBuilder.WithHandlerWrite].
func defaultHandlerWrite(ctx Context, key fields.Key, value value.V) error {
	return nil
}

// defaultHandlerDelete is the stub delete handler wired by [NewRuntimerBuilder]
// so the runtime never panics on a nil handler. It accepts the delete and
// performs no storage. Callers must register a real delete handler via
// [RuntimeBuilder.WithHandlerDelete].
func defaultHandlerDelete(ctx Context, key fields.Key) error {
	return nil
}

// defaultHandlerAuth is the stub auth handler wired by [NewRuntimerBuilder] so
// the runtime never panics on a nil handler. It always reports success; callers
// are responsible for registering a real auth handler via
// [RuntimeBuilder.WithHandlerAuth].
func defaultHandlerAuth(ctx Context, login string, hash [32]byte) (bool, error) {
	return true, nil
}
