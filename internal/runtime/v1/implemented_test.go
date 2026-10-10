package runtime_v1

import (
	"context"
	"errors"
	"testing"

	protocolerrs "github.com/dejitarudemon/ignicula-wire/v1/err/errs"
	"github.com/dejitarudemon/ignicula-wire/v1/fields"
)

func TestDefaultHandlerRead(t *testing.T) {
	tests := []struct {
		name string
		ctx  Context
		key  fields.Key
	}{
		{"nil key", NewContext(context.Background(), "user", 1, false), nil},
		{"empty key", NewContext(context.Background(), "user", 1, false), fields.Key{}},
		{"key", NewContext(context.Background(), "user", 1, false), fields.Key("record")},
		{"zero context", Context{}, fields.Key("record")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultHandlerRead(tt.ctx, tt.key)
			if _, ok := errors.AsType[protocolerrs.ErrorCommandNotImplemented](err); !ok {
				t.Fatalf("error = %T(%v), want ErrorCommandNotImplemented", err, err)
			}
			if got != nil {
				t.Fatalf("value = %v, want nil", got)
			}
		})
	}
}

func TestDefaultHandlerWrite(t *testing.T) {
	tests := []struct {
		name string
		key  fields.Key
	}{
		{"nil key", nil},
		{"empty key", fields.Key{}},
		{"key", fields.Key("record")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := defaultHandlerWrite(Context{}, tt.key, nil)
			if _, ok := errors.AsType[protocolerrs.ErrorCommandNotImplemented](err); !ok {
				t.Fatalf("error = %T(%v), want ErrorCommandNotImplemented", err, err)
			}
		})
	}
}

func TestDefaultHandlerDelete(t *testing.T) {
	tests := []struct {
		name string
		key  fields.Key
	}{
		{"nil key", nil},
		{"empty key", fields.Key{}},
		{"key", fields.Key("record")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := defaultHandlerDelete(Context{}, tt.key)
			if _, ok := errors.AsType[protocolerrs.ErrorCommandNotImplemented](err); !ok {
				t.Fatalf("error = %T(%v), want ErrorCommandNotImplemented", err, err)
			}
		})
	}
}

func TestDefaultHandlerAuth(t *testing.T) {
	tests := []struct {
		name  string
		login string
		hash  [32]byte
	}{
		{"empty login and zero hash", "", [32]byte{}},
		{"login", "user", [32]byte{1, 2, 3}},
		{"full hash", "user", [32]byte{31: 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := defaultHandlerAuth(Context{}, tt.login, tt.hash)
			if _, asOK := errors.AsType[protocolerrs.ErrorCommandNotImplemented](err); !asOK {
				t.Fatalf("error = %T(%v), want ErrorCommandNotImplemented", err, err)
			}
			if ok {
				t.Fatal("authenticated = true, want false")
			}
		})
	}
}
