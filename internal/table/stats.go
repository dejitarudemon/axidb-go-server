package table

import "github.com/dejitarudemon/axidb-go-protocol/v0/fields"

type ConnectionsTableStats struct {
	ConnectionsTotal      int
	ConnectionsPerVersion map[fields.Version]int

	RequestsTotal      int
	RequestsPerVersion map[fields.Version]int

	VersionsGranted int
	VersionsActive  int
}

func NewConnectionsTableStats() ConnectionsTableStats {
	return ConnectionsTableStats{
		ConnectionsPerVersion: make(map[fields.Version]int),
		RequestsPerVersion:    make(map[fields.Version]int),
	}
}
