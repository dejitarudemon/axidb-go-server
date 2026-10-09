package runtime_v1

import (
	"bufio"
	"bytes"
	"errors"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/errs"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

func TestPingEncodesWithAReservedID(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	raw, err := rt.Ping(requestRow)
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

func TestPingUsesDistinctIDs(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	first, err := rt.Ping(requestRow)
	if err != nil {
		t.Fatalf("Ping() = %v", err)
	}
	second, err := rt.Ping(requestRow)
	if err != nil {
		t.Fatalf("Ping() again = %v", err)
	}

	a, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(first)))
	if err != nil {
		t.Fatalf("decode first = %v", err)
	}
	b, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(second)))
	if err != nil {
		t.Fatalf("decode second = %v", err)
	}

	if a.RequestID == 0 || b.RequestID == 0 || a.RequestID == b.RequestID {
		t.Fatalf("ids = %d and %d, want two different non-zero ids", a.RequestID, b.RequestID)
	}
	if requestRow.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", requestRow.Count())
	}
}

func TestPingSkipsOccupiedIDs(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(1 << 20)).Build()
	requestRow := row.NewRequestRow("user", nil)

	if err := requestRow.Register(1, true); err != nil {
		t.Fatalf("Register(1) = %v", err)
	}

	raw, err := rt.Ping(requestRow)
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

	_, err := rt.Ping(nil)
	if !errors.Is(err, errs.ErrCloseConnection) || !errors.Is(err, errs.ErrNilRequestRow) {
		t.Fatalf("error = %v, want close connection and nil row", err)
	}
}

func TestPingReleasesIDWhenBuildFails(t *testing.T) {
	rt := NewRuntimerBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(0)).Build()
	requestRow := row.NewRequestRow("user", nil)

	_, err := rt.Ping(requestRow)
	if !errors.Is(err, errs.ErrLogAndIgnore) {
		t.Fatalf("error = %v, want log and ignore", err)
	}
	if requestRow.Count() != 0 {
		t.Fatalf("Count() = %d, want 0 after failed ping", requestRow.Count())
	}
}
