// Package runtime_v1 is the protocol v1 server runtime.
//
// [RuntimeBuilder] assembles a [Runtime] from a [config.RuntimeBuilderConfig]
// and read, write, delete, and auth handlers. Handlers receive a [Context]
// with the caller login, the request id, and whether the request is external.
// [Runtime.Handshake] authenticates the client and returns a [row.RequestRow].
// [Runtime.Handle] reads one frame and yields its encoded answers.
// Read, write, delete, ping, and batch are handled. A batch yields one frame
// per nested command as it finishes, or one combined answer when the batch asks
// for a single answer. Sequential execution runs nested commands in number order.
// Otherwise they run concurrently, up to
// [config.RuntimeBuilderConfig.MaxGoroutinesPerBatch].
//
// A nil error means the caller writes the bytes and keeps the connection.
// [errs.ErrCloseConnection] means the connection with this client must be closed.
// [errs.ErrLogAndIgnore] means the caller logs the error, writes nothing,
// and keeps the connection. An answer that must only be logged is one of
// [answer.ErrorUnregisteredAnswer], [answer.ErrorNonExternalAnswer], or
// [answer.ErrorUnexpectedAnswer]. A cancelled context is answered with
// [protocolerrs.ErrorRequestInterrupted].
package runtime_v1
