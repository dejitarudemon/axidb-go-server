// Package errs defines connection-control errors for the server runtime.
//
// [ErrCloseConnection] means the connection with this client must be closed.
// [ErrLogAndIgnore] means the caller logs the error, writes nothing, and keeps
// the connection. [ErrNilRequestRow] is the cause when Handle runs before
// Handshake has created a request row.
package errs
