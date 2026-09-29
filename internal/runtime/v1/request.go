package runtime_v1

import (
	"github.com/dejitarudemon/axidb-go-protocol/v1/body"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/builder"
	protocolerrs "github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
)

// handleRequest runs the handler for body and returns the encoded answer.
//
// Read, write, delete, and ping are dispatched to their handlers. Batch is not
// implemented. A handler error is answered to the client. An encoding failure
// is answered to the client as well.
func (r Runtime) handleRequest(ctx Context, body body.Body) ([]byte, error) {
	result := frame.Frame{}
	handlerErr := error(nil)

	switch body.Command() {
	case fields.Read:
		result, handlerErr = r.handleRead(ctx, body)
	case fields.Write:
		result, handlerErr = r.handleWrite(ctx, body)
	case fields.Delete:
		result, handlerErr = r.handleDelete(ctx, body)
	case fields.Ping:
		result, handlerErr = r.handlePing(ctx)
	case fields.Batch:
		// handle Batch
	default:
		return r.writeErrAnswer(ctx.RequestID(), protocolerrs.NewErrorUnsupportedCommand(body.Command()))
	}

	if handlerErr != nil {
		return r.writeErrAnswer(ctx.RequestID(), handlerErr)
	}

	encoded, err := encodeFrame(result)
	if err != nil {
		return r.writeErrAnswer(ctx.RequestID(), err)
	}

	return encoded, nil
}

// handleRead calls the read handler for the key in body.
//
// A nil value from the handler becomes [protocolerrs.ErrorNotFound]. A handler error
// is returned unchanged. On success it returns a read answer frame.
func (r Runtime) handleRead(ctx Context, body body.Body) (frame.Frame, error) {
	rb, _ := body.(bodies.Read)

	value, err := r.handlerRead(ctx, fields.Key(rb))
	if err != nil {
		return frame.Frame{}, err
	}
	if value == nil {
		return frame.Frame{}, protocolerrs.NewErrorNotFound(fields.Key(rb))
	}

	return builder.NewFrameBuilder(r.limit).NewReadAnswer(ctx.RequestID(), value)
}

// handleWrite calls the write handler for the key and value in body.
//
// A handler error is returned unchanged. On success it returns a write answer frame.
func (r Runtime) handleWrite(ctx Context, body body.Body) (frame.Frame, error) {
	rw, _ := body.(bodies.Write)

	if err := r.handlerWrite(ctx, rw.Key, rw.Value); err != nil {
		return frame.Frame{}, err
	}

	return builder.NewFrameBuilder(r.limit).NewWriteAnswer(ctx.RequestID())
}

// handleDelete calls the delete handler for the key in body.
//
// A handler error is returned unchanged. On success it returns a delete answer frame.
func (r Runtime) handleDelete(ctx Context, body body.Body) (frame.Frame, error) {
	rd, _ := body.(bodies.Delete)

	if err := r.handlerDelete(ctx, fields.Key(rd)); err != nil {
		return frame.Frame{}, err
	}

	return builder.NewFrameBuilder(r.limit).NewDeleteAnswer(ctx.RequestID())
}

// handlePing returns a ping answer for the request id in ctx.
func (r Runtime) handlePing(ctx Context) (frame.Frame, error) {
	return builder.NewFrameBuilder(r.limit).NewPingAnswer(ctx.RequestID())
}
