package runtime_v1

import (
	"context"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

func TestNewContext(t *testing.T) {
	type key struct{}

	parent := context.WithValue(context.Background(), key{}, "meta")

	tests := []struct {
		name      string
		parent    context.Context
		login     string
		requestID fields.RequestID
	}{
		{"background", context.Background(), "user", 1},
		{"empty login", context.Background(), "", 1},
		{"zero request id", context.Background(), "user", 0},
		{"max request id", context.Background(), "user", fields.RequestID(^uint32(0))},
		{"unicode login", parent, "пользователь", 7},
		{"nil parent", nil, "user", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewContext(tt.parent, tt.login, tt.requestID)

			if got.Login() != tt.login {
				t.Errorf("Login() = %q, want %q", got.Login(), tt.login)
			}

			if got.RequestID() != tt.requestID {
				t.Errorf("RequestID() = %d, want %d", got.RequestID(), tt.requestID)
			}

			if tt.parent == nil {
				return
			}

			if got.Value(key{}) != tt.parent.Value(key{}) {
				t.Errorf("Value() = %v, want %v", got.Value(key{}), tt.parent.Value(key{}))
			}
		})
	}
}

func TestContextParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	got := NewContext(parent, "user", 42)

	cancel()

	if err := got.Err(); err == nil {
		t.Fatal("Err() = nil, want context.Canceled")
	}

	if got.Login() != "user" {
		t.Errorf("Login() = %q, want %q", got.Login(), "user")
	}

	if got.RequestID() != 42 {
		t.Errorf("RequestID() = %d, want 42", got.RequestID())
	}
}

func TestContextDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	got := NewContext(parent, "user", 1)

	deadline, ok := got.Deadline()
	if !ok {
		t.Fatal("Deadline() ok = false, want true")
	}

	parentDeadline, _ := parent.Deadline()
	if !deadline.Equal(parentDeadline) {
		t.Errorf("Deadline() = %v, want %v", deadline, parentDeadline)
	}
}
