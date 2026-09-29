package runtime_v1

import (
	"context"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
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
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
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
			if err := defaultHandlerWrite(Context{}, tt.key, nil); err != nil {
				t.Fatalf("error = %v, want nil", err)
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
			if err := defaultHandlerDelete(Context{}, tt.key); err != nil {
				t.Fatalf("error = %v, want nil", err)
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
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}

			if !ok {
				t.Fatal("authenticated = false, want true")
			}
		})
	}
}
