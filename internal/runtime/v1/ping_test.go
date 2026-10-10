package runtime_v1

import (
	"bufio"
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

func TestPingEncodesWithAReservedID(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	raw, err := rt.Ping(requestRow, time.Second)
	if err != nil {
		t.Fatalf("Ping() = %v", err)
	}

	got, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if decErr != nil {
		t.Fatalf("decode = %v", decErr)
	}

	if got.RequestID == 0 {
		t.Fatal("request id = 0")
	}
	if _, ok := got.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want Ping", got.Body)
	}

	isExternal, ok := requestRow.IsRegistered(got.RequestID)
	if !ok || !isExternal {
		t.Fatalf("IsRegistered(%d) = (%v, %v), want (true, true)", got.RequestID, isExternal, ok)
	}
}

func TestPingSkipsWhilePreviousActive(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	first, err := rt.Ping(requestRow, time.Second)
	if err != nil || len(first) == 0 {
		t.Fatalf("first Ping() = (%d bytes, %v)", len(first), err)
	}

	second, err := rt.Ping(requestRow, time.Second)
	if err != nil {
		t.Fatalf("second Ping() = %v", err)
	}
	if second != nil {
		t.Fatalf("second Ping() = %d bytes, want nil while first active", len(second))
	}
	if requestRow.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", requestRow.Count())
	}
}

func TestPingReplacesExpiredIdlePing(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	first, err := rt.Ping(requestRow, 0)
	if err != nil || len(first) == 0 {
		t.Fatalf("first Ping() = (%d bytes, %v)", len(first), err)
	}
	a, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(first)))
	if err != nil {
		t.Fatalf("decode first = %v", err)
	}

	// ttl 0 → deadline is now; next Ping treats it as expired.
	time.Sleep(time.Millisecond)
	second, err := rt.Ping(requestRow, time.Second)
	if err != nil || len(second) == 0 {
		t.Fatalf("second Ping() = (%d bytes, %v)", len(second), err)
	}
	b, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(second)))
	if err != nil {
		t.Fatalf("decode second = %v", err)
	}

	if a.RequestID == b.RequestID {
		t.Fatalf("ids = %d and %d, want a new id after expiry", a.RequestID, b.RequestID)
	}
	if _, ok := requestRow.IsRegistered(a.RequestID); ok {
		t.Fatal("expired idle ping id still registered")
	}
	if requestRow.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", requestRow.Count())
	}
}

func TestPingSkipsOccupiedIDs(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	if err := requestRow.Register(1, true); err != nil {
		t.Fatalf("Register(1) = %v", err)
	}

	raw, err := rt.Ping(requestRow, time.Second)
	if err != nil {
		t.Fatalf("Ping() = %v", err)
	}

	got, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if decErr != nil {
		t.Fatalf("decode = %v", decErr)
	}
	if got.RequestID == 1 {
		t.Fatal("Ping reused an occupied id")
	}
	if got.RequestID == 0 {
		t.Fatal("request id = 0")
	}
}

func TestPingNilRowClosesConnection(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()

	_, err := rt.Ping(nil, time.Second)
	if !errors.Is(err, errs.ErrCloseConnection) || !errors.Is(err, errs.ErrNilRequestRow) {
		t.Fatalf("error = %v, want close connection and nil row", err)
	}
}

func TestPingReleasesIDWhenBuildFails(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(0)).Build()
	requestRow := row.NewRequestRow("user", nil)

	_, err := rt.Ping(requestRow, time.Second)
	if !errors.Is(err, errs.ErrLogAndIgnore) {
		t.Fatalf("error = %v, want log and ignore", err)
	}
	if requestRow.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 after failed ping", requestRow.Count())
	}
}
