package table

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
)

// if grant is true, it means the conn doesn't need authorized
// to use this version. just use handshake or something to
// give information about supported compressions and etc
// to server.
// if row is not nil, it means the conn is handshaked already
type versionTable struct {
	grant bool
	row   RegistrationRow
}

type versionsTable map[fields.Version]versionTable

func newVersionsTable() versionsTable {
	return make(versionsTable)
}

// if net.Conn is in the table it means the net.Conn is registered.
type ConnectionsTable struct {
	table map[net.Conn]versionsTable

	mx sync.RWMutex
}

func NewConnectionsTable() *ConnectionsTable {
	return &ConnectionsTable{
		table: make(map[net.Conn]versionsTable),
	}
}

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

func (c *ConnectionsTable) Terminate(connection net.Conn) {
	c.mx.Lock()
	defer c.mx.Unlock()

	delete(c.table, connection)
}

func (c *ConnectionsTable) Stats() ConnectionsTableStats {
	c.mx.RLock()
	defer c.mx.RUnlock()

	stats := NewConnectionsTableStats()

	for _, versions := range c.table {
		stats.ConnectionsTotal++

		for version, table := range versions {
			stats.ConnectionsPerVersion[version]++

			if table.grant {
				stats.VersionsGranted++
			}

			if table.row != nil {
				stats.VersionsActive++
				stats.RequestsTotal += table.row.Count()
				stats.RequestsPerVersion[version] += table.row.Count()
			}
		}
	}

	return stats
}
