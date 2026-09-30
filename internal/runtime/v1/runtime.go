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
// the read, write, delete, and auth handlers, and the limits copied from
// [config.RuntimeBuilderConfig]. Read, write, delete, ping, and batch are
// handled. [Runtime.Handle] yields protocol error answers when the frame was
// fully read. A yielded [errs.ErrCloseConnection] means the connection with
// this client must be closed, and [errs.ErrLogAndIgnore] means the caller must
// log the failure, write nothing, and keep the connection.
type Runtime struct {
	decoder decoder.Decoder

	allowedVersions     []fields.Version
	allowedCompressions map[fields.Compression]compressor.Compressor

	handlerRead   func(ctx Context, key fields.Key) (value.V, error)
	handlerWrite  func(ctx Context, key fields.Key, value value.V) error
	handlerDelete func(ctx Context, key fields.Key) error

	handlerAuth func(ctx Context, login string, hash [32]byte) (bool, error)

	// maxGoroutinePerBatch is how many nested commands of one parallel batch
	// may run at once. A sequential batch does not use it.
	maxGoroutinePerBatch int

	// limit is the maximum encoded frame body size in bytes.
	limit int
}
