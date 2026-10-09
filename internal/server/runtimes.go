package server

import (
	"fmt"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
	runtime_v1 "github.com/dejitarudemon/axidb-go-server/internal/runtime/v1"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
	"github.com/dejitarudemon/axidb-go-server/internal/table"
)

// runtimes holds the protocol runtimes installed on the server.
type runtimes struct {
	v1 *runtime_v1.Runtime
}

// supports reports whether a runtime is registered for version.
func (r runtimes) supports(version fields.Version) bool {
	switch version {
	case fields.Version(1):
		return r.v1 != nil
	default:
		return false
	}
}

// supportedVersions lists versions advertised in the v0 Hello answer.
func (r runtimes) supportedVersions() []fields.Version {
	if r.supports(fields.Version(1)) {
		return []fields.Version{1}
	}
	return nil
}

// ping encodes a server-initiated Ping for the registration row of version.
func (r runtimes) ping(version fields.Version, reg table.RegistrationRow) ([]byte, error) {
	switch version {
	case fields.Version(1):
		if r.v1 == nil {
			return nil, fmt.Errorf("v1 runtime is nil")
		}
		requestRow, ok := reg.(*row.RequestRow)
		if !ok {
			return nil, fmt.Errorf("unexpected registration row type %T", reg)
		}
		return r.v1.Ping(requestRow)
	default:
		return nil, fmt.Errorf("unsupported version %v", version)
	}
}
