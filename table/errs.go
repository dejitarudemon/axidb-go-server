package table

import (
	"fmt"
	"net"

	"github.com/dejitarudemon/ignicula-wire/v0/fields"
)

// ErrorNilConnection is returned when a connection argument is nil.
type ErrorNilConnection struct{}

// NewErrorNilConnection returns an ErrorNilConnection.
func NewErrorNilConnection() ErrorNilConnection {
	return ErrorNilConnection{}
}

// Error returns a human-readable summary of the error.
func (e ErrorNilConnection) Error() string {
	return "expected net.Conn, got nil"
}

// ErrorConnectionAlreadyRegistered is returned when Register is called for a
// connection that is already in the table.
type ErrorConnectionAlreadyRegistered struct {
	connection net.Conn
}

// NewErrorConnectionAlreadyRegistered returns an ErrorConnectionAlreadyRegistered
// for connection.
func NewErrorConnectionAlreadyRegistered(connection net.Conn) ErrorConnectionAlreadyRegistered {
	return ErrorConnectionAlreadyRegistered{connection: connection}
}

// Connection returns the connection that was already registered.
func (e ErrorConnectionAlreadyRegistered) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorConnectionAlreadyRegistered) Error() string {
	return fmt.Sprintf("connection %v is already registered", e.connection)
}

// ErrorConnectionNotRegistered is returned when an operation names a connection
// that is not in the table.
type ErrorConnectionNotRegistered struct {
	connection net.Conn
}

// NewErrorConnectionNotRegistered returns an ErrorConnectionNotRegistered for connection.
func NewErrorConnectionNotRegistered(connection net.Conn) ErrorConnectionNotRegistered {
	return ErrorConnectionNotRegistered{connection: connection}
}

// Connection returns the connection that is not registered.
func (e ErrorConnectionNotRegistered) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorConnectionNotRegistered) Error() string {
	return fmt.Sprintf("connection %v is not registered", e.connection)
}

// ErrorNilRegistrationRow is returned when Activate is called with a nil row.
type ErrorNilRegistrationRow struct{}

// NewErrorNilRegistrationRow returns an ErrorNilRegistrationRow.
func NewErrorNilRegistrationRow() ErrorNilRegistrationRow {
	return ErrorNilRegistrationRow{}
}

// Error returns a human-readable summary of the error.
func (e ErrorNilRegistrationRow) Error() string {
	return "expected RegistrationRow, got nil"
}

// ErrorVersionNotGranted is returned when a version has not been recorded for
// the connection.
type ErrorVersionNotGranted struct {
	version    fields.Version
	connection net.Conn
}

// NewErrorVersionNotGranted returns an ErrorVersionNotGranted for version on connection.
func NewErrorVersionNotGranted(version fields.Version, connection net.Conn) ErrorVersionNotGranted {
	return ErrorVersionNotGranted{version: version, connection: connection}
}

// Version returns the version that is not granted.
func (e ErrorVersionNotGranted) Version() fields.Version {
	return e.version
}

// Connection returns the connection the version was requested for.
func (e ErrorVersionNotGranted) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorVersionNotGranted) Error() string {
	return fmt.Sprintf("version %v for connection %v is not granted", e.version, e.connection)
}

// ErrorVersionAlreadyActive is returned when Activate is called for a version
// that already has a row.
type ErrorVersionAlreadyActive struct {
	version    fields.Version
	connection net.Conn
}

// NewErrorVersionAlreadyActive returns an ErrorVersionAlreadyActive for version on connection.
func NewErrorVersionAlreadyActive(version fields.Version, connection net.Conn) ErrorVersionAlreadyActive {
	return ErrorVersionAlreadyActive{version: version, connection: connection}
}

// Version returns the version that already has a row.
func (e ErrorVersionAlreadyActive) Version() fields.Version {
	return e.version
}

// Connection returns the connection the version belongs to.
func (e ErrorVersionAlreadyActive) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorVersionAlreadyActive) Error() string {
	return fmt.Sprintf("version %v for connection %v is already active", e.version, e.connection)
}

// ErrorVersionNotActivated is returned when Get is called for a granted version
// that has no row.
type ErrorVersionNotActivated struct {
	version    fields.Version
	connection net.Conn
}

// NewErrorVersionNotActivated returns an ErrorVersionNotActivated for version on connection.
func NewErrorVersionNotActivated(version fields.Version, connection net.Conn) ErrorVersionNotActivated {
	return ErrorVersionNotActivated{version: version, connection: connection}
}

// Version returns the version that has no row.
func (e ErrorVersionNotActivated) Version() fields.Version {
	return e.version
}

// Connection returns the connection the version belongs to.
func (e ErrorVersionNotActivated) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorVersionNotActivated) Error() string {
	return fmt.Sprintf("version %v for connection %v is not activated", e.version, e.connection)
}

// ErrorNoActiveVersion is returned when a registered connection has no
// activated protocol version.
type ErrorNoActiveVersion struct {
	connection net.Conn
}

// NewErrorNoActiveVersion returns an ErrorNoActiveVersion for connection.
func NewErrorNoActiveVersion(connection net.Conn) ErrorNoActiveVersion {
	return ErrorNoActiveVersion{connection: connection}
}

// Connection returns the connection with no active version.
func (e ErrorNoActiveVersion) Connection() net.Conn {
	return e.connection
}

// Error returns a human-readable summary of the error.
func (e ErrorNoActiveVersion) Error() string {
	return fmt.Sprintf("connection %v has no active version", e.connection)
}
