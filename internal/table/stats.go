package table

import "github.com/dejitarudemon/axidb-go-protocol/v0/fields"

type ConnectionsTableStats struct {
	ConnectionsTotal      int
	ConnectionsPerVersion map[fields.Version]int

	RequestsTotal         int
	RequestsPerConnection map[fields.Version]int
}
