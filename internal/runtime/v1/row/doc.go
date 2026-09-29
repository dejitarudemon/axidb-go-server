// Package row tracks in-flight request ids for one protocol v1 connection.
//
// [RequestRow] stores each request id with the external flag passed to
// [RequestRow.Register] and the compressions negotiated for that login.
// Methods are safe for concurrent use. A RequestRow contains a mutex and must
// not be copied; share the pointer returned by [NewRequestRow].
package row
