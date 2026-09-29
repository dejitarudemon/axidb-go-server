// Package answer defines errors reported for a protocol v1 answer.
//
// [ErrorUnregisteredAnswer] is an answer for a request id that is not active.
// [ErrorNonExternalAnswer] is an answer for a request that was not registered
// as external. [ErrorUnexpectedAnswer] is an answer that does not reply to ping.
package answer
