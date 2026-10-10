package server

import (
	"fmt"

	fields_v0 "github.com/dejitarudemon/ignicula-wire/v0/fields"
)

func joinAddr(addr string, port uint16) string {
	return fmt.Sprintf("%s:%d", addr, port)
}

// toVersions maps a slice of version-like integers to v0 [fields_v0.Version] values
// for [table.ConnectionsTable.Grant].
func toVersions[T ~uint32](versions []T) []fields_v0.Version {
	result := make([]fields_v0.Version, 0, len(versions))
	for _, version := range versions {
		result = append(result, fields_v0.Version(version))
	}
	return result
}
