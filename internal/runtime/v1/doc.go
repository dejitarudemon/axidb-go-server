// Package runtime_v1 is the protocol v1 server runtime.
//
// [RuntimeBuilder] assembles a [Runtime] from a [config.RuntimeBuilderConfig]
// and read, write, delete, and auth handlers. Handlers receive a [Context]
// with the caller login, the request id, and whether the request is external.
// [Runtime.Handshake] authenticates the client and returns a [row.RequestRow].
// [Runtime.Handle] reads one frame and returns the encoded answer.
// Read, write, delete, and ping are handled. Batch is not implemented yet.
//
// A nil error means the caller writes the bytes and keeps the connection.
// [errs.ErrCloseConnection] means the caller closes the connection.
// [errs.ErrLogAndIgnore] means the caller logs the error, writes nothing,
// and keeps the connection. An answer that must only be logged is one of
// [answer.ErrorUnregisteredAnswer], [answer.ErrorNonExternalAnswer], or
// [answer.ErrorUnexpectedAnswer].
package runtime_v1
