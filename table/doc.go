// Package table records network connections and the protocol versions granted
// to each connection.
//
// [ConnectionsTable.Register] adds a connection. [ConnectionsTable.Grant] records
// versions for it. [ConnectionsTable.Activate] stores a [RegistrationRow] for one
// granted version. [ConnectionsTable.Get] returns that row.
// [ConnectionsTable.MinActiveVersion] returns the smallest activated version.
// [ConnectionsTable.IsRegistered] reports whether a connection is in the table.
// Recorded versions stay until [ConnectionsTable.Terminate] removes that
// connection, or until [ConnectionsTable.Close] clears the whole table.
package table
