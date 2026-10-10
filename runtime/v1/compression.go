package runtime_v1

import (
	"github.com/dejitarudemon/ignicula-wire/v1/body"
	"github.com/dejitarudemon/ignicula-wire/v1/compressor"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/row"
)

const (
	// useHardestCompressionIfBodySizeAtLeast is the minimum body size (bytes)
	// before Zstd is preferred (~160 KiB). Smaller eligible bodies use any
	// compressor shared with the connection.
	useHardestCompressionIfBodySizeAtLeast = 10 << 14
	hardestCompression                     = fields.Zstd
)

// selectCompression chooses a compressor for an outgoing answer body.
//
// It returns nil (no compression) when:
//   - row is nil,
//   - no compressors are registered on the runtime,
//   - [Runtime.useCompressionAt] is greater than [Runtime.limit],
//   - the body is smaller than useCompressionAt,
//   - the body is Handshake, Ping, or an Answer to Handshake, Ping, Write,
//     Delete, or Answer (error answers).
//
// Otherwise Zstd is used when the body is larger than ~160 KiB and both the
// runtime and the connection support it. If not, any registered compressor
// the connection supports is used. If none apply, the result is nil.
func (r Runtime) selectCompression(row *row.RequestRow, target body.Body) compressor.Compressor {
	if row == nil || target == nil {
		return nil
	}

	size := target.Size()
	if r.useCompressionAt > r.limit || len(r.allowedCompressions) == 0 || size < r.useCompressionAt {
		return nil
	}

	switch target.Command() {
	case fields.Ping, fields.Handshake:
		return nil
	case fields.Answer:
		a, ok := target.(body.Answer)
		if !ok {
			return nil
		}
		switch a.IsResponseTo() {
		case fields.Answer, fields.Delete, fields.Write, fields.Handshake, fields.Ping:
			return nil
		}
	}

	if c, ok := r.allowedCompressions[hardestCompression]; ok && size > useHardestCompressionIfBodySizeAtLeast {
		if row.IsSupportCompression(hardestCompression) {
			return c
		}
	}

	for code, c := range r.allowedCompressions {
		if code == hardestCompression {
			continue
		}
		if row.IsSupportCompression(code) {
			return c
		}
	}

	if c, ok := r.allowedCompressions[hardestCompression]; ok && row.IsSupportCompression(hardestCompression) {
		return c
	}

	return nil
}
