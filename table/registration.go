package table

// RegistrationRow is the per-version state stored by [ConnectionsTable.Activate].
//
// Count reports how many requests are active. [ConnectionsTable.Stats] adds
// those values into its request totals.
type RegistrationRow interface {
	// Count returns the number of active requests on this row.
	Count() int
}
