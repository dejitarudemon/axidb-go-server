package runtime

import "errors"

// ErrNilRequestRow is returned when Handle is called before Handshake has
// created a request row for the connection.
var ErrNilRequestRow = errors.New("expected RequestRow, got nil")
