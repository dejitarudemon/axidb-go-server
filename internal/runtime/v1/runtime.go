package runtime_v1

import (
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/decoder"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
)

// Runtime is the v1 server runtime assembled by [RuntimeBuilder].
//
// It holds the request decoder, the allowed protocol versions and compressors,
// and the read, write, delete, and auth handlers. Request handling is not
// implemented yet.
type Runtime struct {
	decoder decoder.Decoder

	allowedVersions     []fields.Version
	allowedCompressions map[fields.Compression]compressor.Compressor

	handlerRead   func(ctx Context, key fields.Key) (value.V, error)
	handlerWrite  func(ctx Context, key fields.Key, value value.V) error
	handlerDelete func(ctx Context, key fields.Key) error

	handlerAuth func(ctx Context, login string, hash [32]byte) (bool, error)
}
