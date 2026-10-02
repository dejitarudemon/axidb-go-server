// Package table records network connections and the protocol versions granted
// to each connection.
//
// [ConnectionsTable.Register] adds a connection. [ConnectionsTable.Grant] records
// versions for it. [ConnectionsTable.Activate] stores a [RegistrationRow] for one
// granted version. [ConnectionsTable.Get] returns that row. Recorded versions stay
// until [ConnectionsTable.Terminate] removes the connection.
package table
