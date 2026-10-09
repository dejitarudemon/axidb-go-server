// Package runtime_v1 is the protocol v1 server runtime.
//
// [RuntimeBuilder] assembles a [Runtime] from a [config.RuntimeBuilderConfig]
// and read, write, delete, and auth handlers. An optional [logger.Logger] may be
// set with [RuntimeBuilder.WithLogger]; a nil logger makes log helpers no-ops.
// Handlers receive a [Context] with the caller login, the request id, and
// whether the request is external.
//
// [Runtime.Decode] reads one frame from a stream. A decode failure that leaves
// the stream unusable closes the connection. Other decode failures return an
// encoded error answer. [Runtime.Activate] and [Runtime.Handle] take an already
// decoded [frame.Frame]. Activate authenticates a handshake and returns a
// [row.RequestRow]; the auth handler runs unless Activate is asked to skip it.
// Handle yields the encoded answers for read, write, delete, ping, and batch.
//
// A batch yields one frame per nested command as it finishes, or one combined
// answer when the batch asks for a single answer. Sequential execution runs
// nested commands in number order. Otherwise they run concurrently, up to
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
