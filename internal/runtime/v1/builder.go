package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/decoder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
)

// RuntimeBuilder assembles a [Runtime] from a [config.RuntimeBuilderConfig] and handlers.
//
// The builder is finished after [RuntimeBuilder.Build]. Create a new builder to
// assemble another runtime.
type RuntimeBuilder struct {
	runtime Runtime
}

// NewRuntimerBuilder returns a builder filled from config.
//
// Read, write, delete, and auth handlers start as the package stubs. Replace
// them with WithHandler* before Build. The runtime frame-size limit is the
// config body limit, in bytes. The parallel-batch goroutine limit is
// [config.RuntimeBuilderConfig.MaxGoroutinesPerBatch].
func NewRuntimerBuilder(cfg config.RuntimeBuilderConfig) *RuntimeBuilder {
	return &RuntimeBuilder{
		runtime: Runtime{
			decoder:             decoder.NewDecoder(cfg.BodyLimit(), cfg.Compressors()),
			allowedVersions:     cfg.Versions(),
			allowedCompressions: compressorsToRuntimeMap(cfg.Compressors()),

			handlerRead:   defaultHandlerRead,
			handlerWrite:  defaultHandlerWrite,
			handlerDelete: defaultHandlerDelete,

			handlerAuth: defaultHandlerAuth,

			limit:                int(cfg.BodyLimit()),
			maxGoroutinePerBatch: cfg.MaxGoroutinesPerBatch(),

			pool: &sync.Pool{
				New: func() any {
					return &buffer.Slice{}
				},
			},
		},
	}
}

// compressorsToRuntimeMap indexes compressors by [compressor.Compressor.Code].
func compressorsToRuntimeMap(compressors []compressor.Compressor) map[fields.Compression]compressor.Compressor {
	compressions := make(map[fields.Compression]compressor.Compressor, len(compressors))

	for _, compressor := range compressors {
		compressions[compressor.Code()] = compressor
	}

	return compressions
}

// WithHandlerRead sets the handler called for a read of key.
// A nil handler is ignored and the current handler is kept.
//
// The handler returns the stored value. (nil, nil) means the key was not found,
// the same as (nil, ErrorNotFound).
func (rb *RuntimeBuilder) WithHandlerRead(handler func(ctx Context, key fields.Key) (value.V, error)) *RuntimeBuilder {
	if handler != nil {
		rb.runtime.handlerRead = handler
	}

	return rb
}

// WithHandlerWrite sets the handler called for a write of key to value.
// A nil handler is ignored and the current handler is kept.
func (rb *RuntimeBuilder) WithHandlerWrite(handler func(ctx Context, key fields.Key, value value.V) error) *RuntimeBuilder {
	if handler != nil {
		rb.runtime.handlerWrite = handler
	}

	return rb
}

// WithHandlerDelete sets the handler called for a delete of key.
// A nil handler is ignored and the current handler is kept.
func (rb *RuntimeBuilder) WithHandlerDelete(handler func(ctx Context, key fields.Key) error) *RuntimeBuilder {
	if handler != nil {
		rb.runtime.handlerDelete = handler
	}

	return rb
}

// WithHandlerAuth sets the handler called to authenticate login with the
// 32-byte handshake hash. A nil handler is ignored and the current handler is kept.
//
// A nil error means the check finished: true means the user is authenticated,
// false means the user is not. A non-nil error means business logic failed and
// the request ends with that error; the user is not authenticated, whatever the
// bool is.
func (rb *RuntimeBuilder) WithHandlerAuth(handler func(ctx Context, login string, hash [32]byte) (bool, error)) *RuntimeBuilder {
	if handler != nil {
		rb.runtime.handlerAuth = handler
	}

	return rb
}

// Build returns the assembled [Runtime].
//
// The builder is finished after Build. Create a new builder to assemble another runtime.
func (rb *RuntimeBuilder) Build() Runtime {
	return rb.runtime
}
