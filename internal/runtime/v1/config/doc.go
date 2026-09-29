// Package config holds construction options for the v1 runtime builder.
//
// [RuntimeBuilderConfig] sets the body and batch limits, the allowed protocol
// versions, and the allowed compressors. [NewRuntimeBuilderConfig] starts from
// the defaults: a 10 KiB body, 64 operations per batch, protocol version 1,
// and no compressors.
package config
