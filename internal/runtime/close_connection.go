package runtime

import (
	"errors"
	"fmt"
)

// ErrCloseConnection is returned by a runtime Handle when the connection must be
// closed. The stream is no longer on a frame boundary, or the answer could not
// be encoded. Unwrap the error for the cause.
var ErrCloseConnection = errors.New("close connection")

// CloseConnection wraps cause so errors.Is(err, ErrCloseConnection) reports that
// the caller must close the connection.
func CloseConnection(cause error) error {
	return fmt.Errorf("%w: %w", ErrCloseConnection, cause)
}
