package table

import "github.com/dejitarudemon/axidb-go-protocol/v0/fields"

// ConnectionsTableStats is a snapshot returned by [ConnectionsTable.Stats].
//
// The maps are non-nil and belong to the snapshot. Changing them does not
// change the table.
type ConnectionsTableStats struct {
	// ConnectionsActive is the number of registered connections, including
	// connections with no recorded versions.
	ConnectionsActive int

	// ConnectionsPerVersion counts, for each version, how many connections have
	// that version recorded.
	ConnectionsPerVersion map[fields.Version]int

	// RequestsActive is the sum of [RegistrationRow.Count] over rows stored in
	// the table.
	RequestsActive int

	// RequestsPerVersion is the sum of [RegistrationRow.Count] for each version.
	// A stored row whose Count is zero is included as zero.
	RequestsPerVersion map[fields.Version]int

	// VersionsGranted is the number of recorded versions across all connections.
	// One connection with two versions counts as two.
	VersionsGranted int

	// VersionsActive is the number of recorded versions that have a row.
	// One connection with two active versions counts as two.
	VersionsActive int
}

// NewConnectionsTableStats returns a snapshot with empty version maps.
func NewConnectionsTableStats() ConnectionsTableStats {
	return ConnectionsTableStats{
		ConnectionsPerVersion: make(map[fields.Version]int),
		RequestsPerVersion:    make(map[fields.Version]int),
	}
}
