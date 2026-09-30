// Package config holds construction options for the v1 runtime builder.
//
// [RuntimeBuilderConfig] sets the body and batch limits, how many nested
// commands of a parallel batch may run at once, the allowed protocol versions,
// and the allowed compressors. [NewRuntimeBuilderConfig] starts from the
// defaults: a 10 KiB body, 64 operations per batch, 4 goroutines per parallel
// batch, protocol version 1, and no compressors.
package config
