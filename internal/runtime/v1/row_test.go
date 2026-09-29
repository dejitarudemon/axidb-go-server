package runtime_v1

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

func TestRequestRow(t *testing.T) {
	tests := []struct {
		name  string
		login string
	}{
		{"login", "user"},
		{"empty login", ""},
		{"unicode login", "пользователь"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := NewRequestRow(tt.login, nil)

			if row.Login() != tt.login {
				t.Errorf("Login() = %q, want %q", row.Login(), tt.login)
			}

			if row.Count() != 0 {
				t.Errorf("Count() = %d, want 0", row.Count())
			}

			if _, ok := row.IsRegistered(1); ok {
				t.Fatal("IsRegistered(1) = true, want false")
			}
		})
	}
}

func TestRequestRowRegisterAndTerminate(t *testing.T) {
	row := NewRequestRow("user", nil)
	ids := []fields.RequestID{0, 1, fields.RequestID(^uint32(0))}

	for _, id := range ids {
		if err := row.Register(id, false); err != nil {
			t.Fatalf("Register(%d) = %v", id, err)
		}
	}

	if row.Count() != len(ids) {
		t.Fatalf("Count() = %d, want %d", row.Count(), len(ids))
	}

	for _, id := range ids {
		isExternal, ok := row.IsRegistered(id)
		if !ok || isExternal {
			t.Errorf("IsRegistered(%d) = (%v, %v), want (false, true)", id, isExternal, ok)
		}
	}

	row.Terminate(1)
	if _, ok := row.IsRegistered(1); ok {
		t.Fatal("IsRegistered(1) = true after Terminate")
	}

	if row.Count() != len(ids)-1 {
		t.Fatalf("Count() = %d, want %d", row.Count(), len(ids)-1)
	}

	if err := row.Register(1, true); err != nil {
		t.Fatalf("Register(1) after Terminate = %v", err)
	}

	isExternal, ok := row.IsRegistered(1)
	if !ok || !isExternal {
		t.Fatalf("IsRegistered(1) = (%v, %v), want (true, true)", isExternal, ok)
	}
}

func TestRequestRowDuplicateRegister(t *testing.T) {
	row := NewRequestRow("user", nil)
	id := fields.RequestID(7)

	if err := row.Register(id, false); err != nil {
		t.Fatalf("Register = %v", err)
	}

	err := row.Register(id, true)
	assertRequestsConflict(t, err, id)

	isExternal, ok := row.IsRegistered(id)
	if !ok || isExternal {
		t.Fatalf("IsRegistered = (%v, %v), want (false, true)", isExternal, ok)
	}

	if row.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", row.Count())
	}

	if row.requests[id] {
		t.Fatal("isExternal = true, want false")
	}
}

func TestRequestRowTerminateUnknown(t *testing.T) {
	row := NewRequestRow("user", nil)

	row.Terminate(1)
	if err := row.Register(1, false); err != nil {
		t.Fatalf("Register = %v", err)
	}

	row.Terminate(2)
	row.Terminate(1)
	row.Terminate(1)

	if _, ok := row.IsRegistered(1); ok {
		t.Fatal("IsRegistered(1) = true, want false")
	}

	if row.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", row.Count())
	}
}

func TestRequestRowConcurrentRegister(t *testing.T) {
	const n = 64

	row := NewRequestRow("user", nil)

	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(id fields.RequestID) {
			defer wg.Done()

			if err := row.Register(id, false); err != nil {
				t.Errorf("Register(%d) = %v", id, err)
			}
		}(fields.RequestID(i))
	}

	wg.Wait()

	if row.Count() != n {
		t.Fatalf("Count() = %d, want %d", row.Count(), n)
	}
}

func TestRequestRowConcurrentDuplicate(t *testing.T) {
	const n = 32

	row := NewRequestRow("user", nil)

	var wg sync.WaitGroup
	var success, conflict atomic.Int32
	wg.Add(n)

	for range n {
		go func() {
			defer wg.Done()

			err := row.Register(1, true)
			if err == nil {
				success.Add(1)
				return
			}

			if _, ok := errors.AsType[errs.ErrorRequestsConflict](err); !ok {
				t.Errorf("error = %v, want ErrorRequestsConflict", err)
				return
			}

			conflict.Add(1)
		}()
	}

	wg.Wait()

	if success.Load() != 1 {
		t.Errorf("success = %d, want 1", success.Load())
	}

	if conflict.Load() != n-1 {
		t.Errorf("conflict = %d, want %d", conflict.Load(), n-1)
	}

	if row.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", row.Count())
	}
}

func TestRequestRowCompressions(t *testing.T) {
	tests := []struct {
		name         string
		compressions []fields.Compression
		query        fields.Compression
		want         bool
	}{
		{"nil list", nil, fields.Zstd, false},
		{"empty list", []fields.Compression{}, fields.S2, false},
		{"none", []fields.Compression{fields.None}, fields.None, true},
		{"present", []fields.Compression{fields.Zstd, fields.S2}, fields.S2, true},
		{"absent", []fields.Compression{fields.Zstd}, fields.S2, false},
		{"duplicate", []fields.Compression{fields.Zstd, fields.Zstd}, fields.Zstd, true},
		{"unknown", []fields.Compression{fields.Compression(255)}, fields.Compression(255), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := NewRequestRow("user", tt.compressions)
			if got := row.IsSupportCompression(tt.query); got != tt.want {
				t.Fatalf("IsSupportCompression(%v) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestRequestRowNilPanics(t *testing.T) {
	var row *RequestRow

	tests := []struct {
		name string
		call func()
	}{
		{"register", func() { _ = row.Register(1, false) }},
		{"terminate", func() { row.Terminate(1) }},
		{"is registered", func() { _, _ = row.IsRegistered(1) }},
		{"count", func() { _ = row.Count() }},
		{"login", func() { _ = row.Login() }},
		{"compression", func() { _ = row.IsSupportCompression(fields.Zstd) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()

			tt.call()
		})
	}
}

func assertRequestsConflict(t *testing.T, err error, id fields.RequestID) {
	t.Helper()

	conflict, ok := errors.AsType[errs.ErrorRequestsConflict](err)
	if !ok {
		t.Fatalf("error = %v, want ErrorRequestsConflict", err)
	}

	if conflict.Code() != fields.RequestsConflict {
		t.Fatalf("code = %v, want %v", conflict.Code(), fields.RequestsConflict)
	}

	suffix := fmt.Sprintf(": %d,", id)
	if !strings.HasSuffix(err.Error(), suffix) {
		t.Fatalf("error = %q, want suffix %q", err.Error(), suffix)
	}
}
