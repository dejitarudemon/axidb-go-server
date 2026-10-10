package runtime_v1

import (
	"sync"

	"github.com/dejitarudemon/ignicula-wire/v1/compressor"
	"github.com/dejitarudemon/ignicula-wire/v1/decoder"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/value"
	"github.com/dejitarudemon/ignicula-framework/internal/logger"
)

// Runtime is the v1 server runtime assembled by [RuntimeBuilder].
//
// It holds the request decoder, the allowed protocol versions and compressors,
// the read, write, delete, and auth handlers, and the limits copied from
// [config.RuntimeBuilderConfig]. [Runtime.Decode] reads frames from the stream.
// [Runtime.Activate] and [Runtime.Handle] take an already decoded frame.
// Read, write, delete, ping, and batch are handled by Handle.
// [Runtime.selectCompression] picks Zstd for bodies larger than ~160 KiB when
// available, otherwise any compressor shared with the connection.
// A cancelled context is answered with [protocolerrs.ErrorRequestInterrupted].
// A yielded [errs.ErrCloseConnection] means the connection with this client must
// be closed, including when an error answer cannot be encoded.
// [errs.ErrLogAndIgnore] means the caller must log the failure, write nothing,
// and keep the connection. A batch answer that cannot be encoded is yielded as
// that error.
type Runtime struct {
	decoder decoder.Decoder

	allowedVersions     []fields.Version
	allowedCompressions map[fields.Compression]compressor.Compressor

	handlerRead   func(ctx Context, key fields.Key) (value.V, error)
	handlerWrite  func(ctx Context, key fields.Key, value value.V) error
	handlerDelete func(ctx Context, key fields.Key) error

	handlerAuth func(ctx Context, login string, hash [32]byte) (bool, error)

	// logger is optional; a nil logger makes log helpers no-ops.
	logger logger.Logger

	// maxGoroutinePerBatch is how many nested commands of one parallel batch
	// may run at once. A sequential batch does not use it.
	maxGoroutinePerBatch int

	// limit is the maximum encoded frame body size in bytes.
	limit int

	// useCompressionAt is the body size (bytes) at which answers may start
	// using compression. See [Runtime.selectCompression].
	useCompressionAt int

	pool *sync.Pool
}
