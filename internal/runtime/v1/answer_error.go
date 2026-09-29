package runtime_v1

import (
	"fmt"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

// ErrorUnregisteredAnswer is returned when an answer arrives for a request id
// that is not active on this connection.
type ErrorUnregisteredAnswer struct {
	requestID fields.RequestID
}

// NewErrorUnregisteredAnswer returns an ErrorUnregisteredAnswer for requestID.
func NewErrorUnregisteredAnswer(requestID fields.RequestID) ErrorUnregisteredAnswer {
	return ErrorUnregisteredAnswer{requestID: requestID}
}

// RequestID returns the request id the answer named.
func (e ErrorUnregisteredAnswer) RequestID() fields.RequestID {
	return e.requestID
}

// Error returns a human-readable summary of the error.
func (e ErrorUnregisteredAnswer) Error() string {
	return fmt.Sprintf("answer for an unregistered request %d", e.requestID)
}

// ErrorNonExternalAnswer is returned when an answer arrives for a request that
// was not registered as external.
type ErrorNonExternalAnswer struct {
	requestID fields.RequestID
}

// NewErrorNonExternalAnswer returns an ErrorNonExternalAnswer for requestID.
func NewErrorNonExternalAnswer(requestID fields.RequestID) ErrorNonExternalAnswer {
	return ErrorNonExternalAnswer{requestID: requestID}
}

// RequestID returns the request id the answer named.
func (e ErrorNonExternalAnswer) RequestID() fields.RequestID {
	return e.requestID
}

// Error returns a human-readable summary of the error.
func (e ErrorNonExternalAnswer) Error() string {
	return fmt.Sprintf("answer for a non-external request %d", e.requestID)
}

// ErrorUnexpectedAnswer is returned when an answer replies to a command other
// than ping.
type ErrorUnexpectedAnswer struct {
	responseTo fields.Command
}

// NewErrorUnexpectedAnswer returns an ErrorUnexpectedAnswer for responseTo.
func NewErrorUnexpectedAnswer(responseTo fields.Command) ErrorUnexpectedAnswer {
	return ErrorUnexpectedAnswer{responseTo: responseTo}
}

// ResponseTo returns the command the answer replies to.
func (e ErrorUnexpectedAnswer) ResponseTo() fields.Command {
	return e.responseTo
}

// Error returns a human-readable summary of the error.
func (e ErrorUnexpectedAnswer) Error() string {
	return fmt.Sprintf("answer responds to %v, expected %v", e.responseTo, fields.Ping)
}
