package config

import (
	"slices"

	"github.com/dejitarudemon/ignicula-wire/v1/compressor"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
)

const (
	// defaultBodyLimit is the default max request body size in bytes (10 KiB).
	defaultBodyLimit = 10 << 10
	// defaultBatchLimit is the default max number of operations in one batch (64).
	defaultBatchLimit = 1 << 6
	// defaultGoroutinesPerParrallelBatch is how many nested commands of one
	// parallel batch may run at once when the config does not set another limit.
	defaultGoroutinesPerParrallelBatch = 4

	// defaultStartUseCompression is the default body size (bytes) at which
	// answers may start using compression (5 KiB).
	defaultStartUseCompression = 10 << 9
)

var (
	// defaultVersions is the protocol versions allowed when none are registered yet.
	defaultVersions = []fields.Version{fields.Version(1)}
)

// limits holds decoder/runtime size constraints from [RuntimeBuilderConfig].
type limits struct {
	body  fields.BodyLimit
	batch fields.BatchLimit
	// goroutines is how many nested commands of one parallel batch may run at once.
	goroutines int

	// compression is the body size (bytes) at which answer compression may start.
	compression int
}

// RuntimeBuilderConfig holds construction options for the v1 runtime builder.
//
// Use NewRuntimeBuilderConfig for defaults, then chain With* methods.
type RuntimeBuilderConfig struct {
	versions    []fields.Version
	compressors []compressor.Compressor
	limits      limits
}

// NewRuntimeBuilderConfig returns a config with default limits (10 KiB body,
// 64 operations per batch, 4 goroutines per parallel batch, compression from
// 5 KiB body size), protocol version 1 allowed, and no compressors registered
// (requests are accepted only uncompressed).
func NewRuntimeBuilderConfig() *RuntimeBuilderConfig {
	return &RuntimeBuilderConfig{
		limits: limits{
			body:        defaultBodyLimit,
			batch:       defaultBatchLimit,
			goroutines:  defaultGoroutinesPerParrallelBatch,
			compression: defaultStartUseCompression,
		},
		versions:    append([]fields.Version(nil), defaultVersions...),
		compressors: make([]compressor.Compressor, 0),
	}
}

// WithBodyLimit sets the maximum request body size in bytes.
func (rbc *RuntimeBuilderConfig) WithBodyLimit(limit fields.BodyLimit) *RuntimeBuilderConfig {
	rbc.limits.body = limit
	return rbc
}

// WithBatchLimit sets the maximum number of operations allowed in one batch.
func (rbc *RuntimeBuilderConfig) WithBatchLimit(limit fields.BatchLimit) *RuntimeBuilderConfig {
	rbc.limits.batch = limit
	return rbc
}

// WithMaxGoroutinesPerBatch sets how many nested commands of one parallel batch
// may run at once. A value below 1 is raised to 1. A sequential batch runs one
// command at a time and does not use this limit.
func (rbc *RuntimeBuilderConfig) WithMaxGoroutinesPerBatch(goroutines int) *RuntimeBuilderConfig {
	rbc.limits.goroutines = max(goroutines, 1)
	return rbc
}

// WithCompressors appends compressors to the allowed set, skipping nil values
// and duplicates (same [compressor.Compressor.Code]). An empty set means no
// compression is available; requests are sent/accepted without compression.
func (rbc *RuntimeBuilderConfig) WithCompressors(compressors ...compressor.Compressor) *RuntimeBuilderConfig {
	for _, c := range compressors {
		if c != nil && !rbc.hasCompressor(c.Code()) {
			rbc.compressors = append(rbc.compressors, c)
		}

	}
	return rbc
}

// WithAllowedVersions appends protocol versions to the allowed set, skipping
// duplicates.
func (rbc *RuntimeBuilderConfig) WithAllowedVersions(versions ...fields.Version) *RuntimeBuilderConfig {
	for _, v := range versions {
		if !rbc.hasVersion(v) {
			rbc.versions = append(rbc.versions, v)
		}
	}
	return rbc
}

// BodyLimit returns the maximum request body size in bytes.
func (rbc *RuntimeBuilderConfig) BodyLimit() fields.BodyLimit {
	return rbc.limits.body
}

// BatchLimit returns the maximum number of operations allowed in one batch.
func (rbc *RuntimeBuilderConfig) BatchLimit() fields.BatchLimit {
	return rbc.limits.batch
}

// Versions returns the protocol versions this config allows.
func (rbc *RuntimeBuilderConfig) Versions() []fields.Version {
	return rbc.versions
}

// Compressors returns the compressors this config allows.
func (rbc *RuntimeBuilderConfig) Compressors() []compressor.Compressor {
	return rbc.compressors
}

// MaxGoroutinesPerBatch returns how many nested commands of one parallel batch may run at once.
func (rbc *RuntimeBuilderConfig) MaxGoroutinesPerBatch() int {
	return rbc.limits.goroutines
}

func (rbc *RuntimeBuilderConfig) hasCompressor(code fields.Compression) bool {
	return slices.ContainsFunc(rbc.compressors, func(c compressor.Compressor) bool {
		return c.Code() == code
	})
}

func (rbc *RuntimeBuilderConfig) hasVersion(version fields.Version) bool {
	return slices.Contains(rbc.versions, version)
}

// WithStartUseCompressionAt sets the minimum encoded body size in bytes at
// which the runtime may compress answers. A negative value is raised to 0
// (compress whenever a compressor is otherwise selected). Compression still
// requires registered compressors and client handshake support.
func (rbc *RuntimeBuilderConfig) WithStartUseCompressionAt(start int) *RuntimeBuilderConfig {
	rbc.limits.compression = max(0, start)
	return rbc
}

// StartUseCompressionAt returns the body-size threshold (bytes) for answer
// compression. Bodies smaller than this are left uncompressed.
func (rbc *RuntimeBuilderConfig) StartUseCompressionAt() int {
	return rbc.limits.compression
}
