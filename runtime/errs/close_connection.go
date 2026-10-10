package errs

import (
	"errors"
	"fmt"
)

// ErrCloseConnection means the connection with this client must be closed.
// Unwrap the error for the cause.
var ErrCloseConnection = errors.New("connection with this client must be closed")

// CloseConnection wraps cause so errors.Is(err, ErrCloseConnection) reports that
// the connection with this client must be closed.
func CloseConnection(cause error) error {
	return fmt.Errorf("%w: %w", ErrCloseConnection, cause)
}
