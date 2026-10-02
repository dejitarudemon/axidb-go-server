package table

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
)

// versionTable is the state of one protocol version on one connection.
//
// grant is set when [ConnectionsTable.Grant] records the version. A non-nil row
// is the [RegistrationRow] stored by [ConnectionsTable.Activate]. Both stay until
// [ConnectionsTable.Terminate] removes the connection.
type versionTable struct {
	grant bool
	row   RegistrationRow
}

// versionsTable maps a protocol version to its state on one connection.
type versionsTable map[fields.Version]versionTable

// newVersionsTable returns an empty version map.
func newVersionsTable() versionsTable {
	return make(versionsTable)
}

// ConnectionsTable records registered connections and the protocol versions
// granted to each one.
//
// A connection present in the table is registered. Each granted version may
// hold one [RegistrationRow]. Every version recorded for a connection stays
// until [ConnectionsTable.Terminate] removes that connection.
//
// The methods are safe for concurrent use. A ConnectionsTable contains a mutex
// and must not be copied. Use the pointer from [NewConnectionsTable]. The zero
// ConnectionsTable must not be used.
type ConnectionsTable struct {
	table map[net.Conn]versionsTable

	mx sync.RWMutex
}

// NewConnectionsTable returns an empty table.
func NewConnectionsTable() *ConnectionsTable {
	return &ConnectionsTable{
		table: make(map[net.Conn]versionsTable),
	}
}

// Register records connection.
//
// A nil connection returns an error. A connection that is already registered
// returns an error and stays as it is.
func (c *ConnectionsTable) Register(connection net.Conn) error {
	if connection == nil {
		return errors.New("expected net.Conn, got nil")
	}

	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.table[connection]; ok {
		return fmt.Errorf("connection %v is already registered", connection)
	}

	c.table[connection] = newVersionsTable()

	return nil
}

// Grant records versions for a registered connection.
//
// A nil connection returns an error. A connection that is not registered
// returns an error. A version that is already recorded is left unchanged,
// including a version that already has a row. Grant with no versions returns
// nil and changes nothing. Recorded versions stay until [ConnectionsTable.Terminate].
func (c *ConnectionsTable) Grant(connection net.Conn, versions ...fields.Version) error {
	if connection == nil {
		return errors.New("expected net.Conn, got nil")
	}

	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.table[connection]; !ok {
		return fmt.Errorf("connection %v is not registered", connection)
	}

	for _, version := range versions {
		if _, ok := c.table[connection][version]; !ok {
			c.table[connection][version] = versionTable{grant: true}
		}
	}

	return nil
}

// Activate stores row for a granted version of connection.
//
// A nil connection or a nil row returns an error. A connection that is not
// registered returns an error. A version that is not granted returns an error.
// A version that already has a row returns an error and keeps the stored row.
// One connection may have a row for more than one version.
func (c *ConnectionsTable) Activate(connection net.Conn, version fields.Version, row RegistrationRow) error {
	if connection == nil {
		return errors.New("expected net.Conn, got nil")
	}

	if row == nil {
		return errors.New("expected RegistrationRow, got nil")
	}

	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.table[connection]; !ok {
		return fmt.Errorf("connection %v is not registered", connection)
	}

	if table, ok := c.table[connection][version]; !ok || !table.grant {
		return fmt.Errorf("version %v for connection %v is not granted", version, connection)
	} else if table.row != nil {
		return fmt.Errorf("version %v for connection %v is already active", version, connection)
	} else {
		table.row = row
		c.table[connection][version] = table
	}

	return nil
}

// Get returns the row stored for version on connection.
//
// A nil connection returns an error. A connection that is not registered
// returns an error. A version that is not granted returns an error. A granted
// version with no row returns an error.
func (c *ConnectionsTable) Get(connection net.Conn, version fields.Version) (RegistrationRow, error) {
	if connection == nil {
		return nil, errors.New("expected net.Conn, got nil")
	}

	c.mx.RLock()
	defer c.mx.RUnlock()

	if _, ok := c.table[connection]; !ok {
		return nil, fmt.Errorf("connection %v is not registered", connection)
	}

	if table, ok := c.table[connection][version]; !ok || !table.grant {
		return nil, fmt.Errorf("version %v for connection %v is not granted", version, connection)
	} else if table.row == nil {
		return nil, fmt.Errorf("version %v for connection %v is not activated", version, connection)
	} else {
		return table.row, nil
	}
}

// Terminate removes connection and every version recorded for it.
//
// A nil connection or a connection that is not registered is ignored.
func (c *ConnectionsTable) Terminate(connection net.Conn) {
	c.mx.Lock()
	defer c.mx.Unlock()

	delete(c.table, connection)
}

// Stats returns one snapshot of the table.
//
// The read lock is held while the snapshot is taken. See [ConnectionsTableStats]
// for what each field counts.
func (c *ConnectionsTable) Stats() ConnectionsTableStats {
	c.mx.RLock()
	defer c.mx.RUnlock()

	stats := NewConnectionsTableStats()

	for _, versions := range c.table {
		stats.ConnectionsActive++

		for version, table := range versions {
			stats.ConnectionsPerVersion[version]++

			if table.grant {
				stats.VersionsGranted++
			}

			if table.row != nil {
				stats.VersionsActive++
				stats.RequestsActive += table.row.Count()
				stats.RequestsPerVersion[version] += table.row.Count()
			}
		}
	}

	return stats
}
