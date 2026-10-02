package table

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
)

type stubRow struct {
	n int
}

func (s *stubRow) Count() int { return s.n }

func TestRegister(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)

	if err := table.Register(nil); err == nil || !strings.Contains(err.Error(), "expected net.Conn, got nil") {
		t.Fatalf("Register(nil) = %v", err)
	}

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	if err := table.Register(conn); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("Register() again = %v", err)
	}
}

func TestGrant(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)

	if err := table.Grant(nil, 1); err == nil || !strings.Contains(err.Error(), "expected net.Conn, got nil") {
		t.Fatalf("Grant(nil) = %v", err)
	}

	if err := table.Grant(conn, 1); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Grant() before Register = %v", err)
	}

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	if err := table.Grant(conn); err != nil {
		t.Fatalf("Grant() with no versions = %v", err)
	}

	if err := table.Grant(conn, 1, 1, 2); err != nil {
		t.Fatalf("Grant() = %v", err)
	}

	if err := table.Grant(conn, 1); err != nil {
		t.Fatalf("Grant() again = %v", err)
	}

	assertNotActivated(t, table, conn, 1)
	assertNotActivated(t, table, conn, 2)
	assertNotGranted(t, table, conn, 3)
}

func TestActivate(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)
	row := &stubRow{n: 4}

	if err := table.Activate(nil, 1, row); err == nil || !strings.Contains(err.Error(), "expected net.Conn, got nil") {
		t.Fatalf("Activate(nil) = %v", err)
	}

	if err := table.Activate(conn, 1, nil); err == nil || !strings.Contains(err.Error(), "expected RegistrationRow, got nil") {
		t.Fatalf("Activate(nil row) = %v", err)
	}

	if err := table.Activate(conn, 1, row); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Activate() before Register = %v", err)
	}

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	if err := table.Activate(conn, 1, row); err == nil || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("Activate() before Grant = %v", err)
	}

	if err := table.Grant(conn, 1, 2); err != nil {
		t.Fatalf("Grant() = %v", err)
	}

	if err := table.Activate(conn, 1, row); err != nil {
		t.Fatalf("Activate() = %v", err)
	}

	if err := table.Activate(conn, 1, &stubRow{n: 9}); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("Activate() again = %v", err)
	}

	got := mustGet(t, table, conn, 1)
	if got != row || got.Count() != 4 {
		t.Fatalf("Get() = %#v, want the first row", got)
	}

	assertNotActivated(t, table, conn, 2)
}

func TestGet(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)

	if _, err := table.Get(nil, 1); err == nil || !strings.Contains(err.Error(), "expected net.Conn, got nil") {
		t.Fatalf("Get(nil) = %v", err)
	}

	if _, err := table.Get(conn, 1); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Get() before Register = %v", err)
	}
}

func TestGrantKeepsAnActiveRow(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)
	row := &stubRow{n: 1}

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := table.Grant(conn, 1); err != nil {
		t.Fatalf("Grant() = %v", err)
	}
	if err := table.Activate(conn, 1, row); err != nil {
		t.Fatalf("Activate() = %v", err)
	}
	if err := table.Grant(conn, 1, 2); err != nil {
		t.Fatalf("Grant() again = %v", err)
	}

	if got := mustGet(t, table, conn, 1); got != row {
		t.Fatalf("Get() = %#v, want the active row", got)
	}
	assertNotActivated(t, table, conn, 2)
}

func TestConnectionsStaySeparate(t *testing.T) {
	table := NewConnectionsTable()
	first := newConn(t)
	second := newConn(t)
	firstRow := &stubRow{n: 1}
	secondRow := &stubRow{n: 2}

	for _, conn := range []net.Conn{first, second} {
		if err := table.Register(conn); err != nil {
			t.Fatalf("Register() = %v", err)
		}
	}

	if err := table.Grant(first, 1); err != nil {
		t.Fatalf("Grant(first) = %v", err)
	}
	if err := table.Activate(first, 1, firstRow); err != nil {
		t.Fatalf("Activate(first) = %v", err)
	}

	assertNotGranted(t, table, second, 1)

	if err := table.Grant(second, 1, 2); err != nil {
		t.Fatalf("Grant(second) = %v", err)
	}
	if err := table.Activate(second, 2, secondRow); err != nil {
		t.Fatalf("Activate(second) = %v", err)
	}

	if got := mustGet(t, table, first, 1); got != firstRow {
		t.Fatalf("Get(first) = %#v, want the first row", got)
	}
	if got := mustGet(t, table, second, 2); got != secondRow {
		t.Fatalf("Get(second) = %#v, want the second row", got)
	}
	assertNotActivated(t, table, second, 1)
	assertNotGranted(t, table, first, 2)
}

func TestTerminate(t *testing.T) {
	table := NewConnectionsTable()
	conn := newConn(t)
	other := newConn(t)
	row := &stubRow{n: 3}

	table.Terminate(nil)
	table.Terminate(conn)

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := table.Register(other); err != nil {
		t.Fatalf("Register(other) = %v", err)
	}
	if err := table.Grant(conn, 1); err != nil {
		t.Fatalf("Grant() = %v", err)
	}
	if err := table.Grant(other, 1); err != nil {
		t.Fatalf("Grant(other) = %v", err)
	}
	if err := table.Activate(conn, 1, row); err != nil {
		t.Fatalf("Activate() = %v", err)
	}
	if err := table.Activate(other, 1, &stubRow{n: 8}); err != nil {
		t.Fatalf("Activate(other) = %v", err)
	}

	table.Terminate(conn)

	if _, err := table.Get(conn, 1); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Get() after Terminate = %v", err)
	}
	if err := table.Grant(conn, 1); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Grant() after Terminate = %v", err)
	}
	if err := table.Activate(conn, 1, row); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("Activate() after Terminate = %v", err)
	}

	if got := mustGet(t, table, other, 1); got.Count() != 8 {
		t.Fatalf("Get(other) = %#v, want the other row", got)
	}

	if err := table.Register(conn); err != nil {
		t.Fatalf("Register() after Terminate = %v", err)
	}
	assertNotGranted(t, table, conn, 1)
}

func TestStats(t *testing.T) {
	table := NewConnectionsTable()
	first := newConn(t)
	second := newConn(t)
	firstV1 := &stubRow{n: 3}
	firstV2 := &stubRow{n: 0}
	secondV1 := &stubRow{n: 5}

	assertStats(t, table.Stats(), stats())

	if err := table.Register(first); err != nil {
		t.Fatalf("Register(first) = %v", err)
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
	}))

	if err := table.Activate(first, 1, firstV1); err == nil {
		t.Fatal("Activate() before Grant succeeded")
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
	}))

	if err := table.Grant(first, 1, 2); err != nil {
		t.Fatalf("Grant(first) = %v", err)
	}
	if err := table.Grant(first, 1); err != nil {
		t.Fatalf("Grant(first) again = %v", err)
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
		s.ConnectionsPerVersion[1] = 1
		s.ConnectionsPerVersion[2] = 1
		s.VersionsGranted = 2
	}))

	if err := table.Activate(first, 1, firstV1); err != nil {
		t.Fatalf("Activate(first, 1) = %v", err)
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
		s.ConnectionsPerVersion[1] = 1
		s.ConnectionsPerVersion[2] = 1
		s.VersionsGranted = 2
		s.VersionsActive = 1
		s.RequestsTotal = 3
		s.RequestsPerVersion[1] = 3
	}))

	if err := table.Activate(first, 2, firstV2); err != nil {
		t.Fatalf("Activate(first, 2) = %v", err)
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
		s.ConnectionsPerVersion[1] = 1
		s.ConnectionsPerVersion[2] = 1
		s.VersionsGranted = 2
		s.VersionsActive = 2
		s.RequestsTotal = 3
		s.RequestsPerVersion[1] = 3
		s.RequestsPerVersion[2] = 0
	}))

	if err := table.Register(second); err != nil {
		t.Fatalf("Register(second) = %v", err)
	}
	if err := table.Grant(second, 1); err != nil {
		t.Fatalf("Grant(second) = %v", err)
	}
	if err := table.Activate(second, 1, secondV1); err != nil {
		t.Fatalf("Activate(second, 1) = %v", err)
	}
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 2
		s.ConnectionsPerVersion[1] = 2
		s.ConnectionsPerVersion[2] = 1
		s.VersionsGranted = 3
		s.VersionsActive = 3
		s.RequestsTotal = 8
		s.RequestsPerVersion[1] = 8
		s.RequestsPerVersion[2] = 0
	}))

	table.Terminate(first)
	assertStats(t, table.Stats(), stats(func(s *ConnectionsTableStats) {
		s.ConnectionsTotal = 1
		s.ConnectionsPerVersion[1] = 1
		s.VersionsGranted = 1
		s.VersionsActive = 1
		s.RequestsTotal = 5
		s.RequestsPerVersion[1] = 5
	}))

	table.Terminate(second)
	assertStats(t, table.Stats(), stats())
}

func TestConcurrentConnections(t *testing.T) {
	table := NewConnectionsTable()
	const n = 32

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()

			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			conn := left

			version := fields.Version(i + 1)
			row := &stubRow{n: i}

			if err := table.Register(conn); err != nil {
				t.Errorf("Register() = %v", err)
				return
			}
			if err := table.Grant(conn, version, version); err != nil {
				t.Errorf("Grant() = %v", err)
				return
			}
			if err := table.Activate(conn, version, row); err != nil {
				t.Errorf("Activate() = %v", err)
				return
			}

			got, err := table.Get(conn, version)
			if err != nil {
				t.Errorf("Get() = %v", err)
				return
			}
			if got != row {
				t.Errorf("Get() = %#v, want %#v", got, row)
			}

			table.Terminate(conn)

			if _, err := table.Get(conn, version); err == nil {
				t.Error("Get() after Terminate succeeded")
			}
		}(i)
	}
	wg.Wait()
}

func TestStatsConcurrent(t *testing.T) {
	table := NewConnectionsTable()
	const n = 16

	var wg sync.WaitGroup
	wg.Add(n + 1)

	go func() {
		defer wg.Done()

		for range 200 {
			if err := checkStats(table.Stats()); err != nil {
				t.Error(err)
				return
			}
		}
	}()

	for i := range n {
		go func(i int) {
			defer wg.Done()

			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()

			version := fields.Version(i + 1)
			row := &stubRow{n: i + 1}

			if err := table.Register(left); err != nil {
				t.Errorf("Register() = %v", err)
				return
			}
			if err := table.Grant(left, version); err != nil {
				t.Errorf("Grant() = %v", err)
				return
			}
			if err := checkStats(table.Stats()); err != nil {
				t.Error(err)
				return
			}
			if err := table.Activate(left, version, row); err != nil {
				t.Errorf("Activate() = %v", err)
				return
			}
			if err := checkStats(table.Stats()); err != nil {
				t.Error(err)
				return
			}

			table.Terminate(left)
		}(i)
	}

	wg.Wait()
	assertStats(t, table.Stats(), stats())
}

func newConn(t *testing.T) net.Conn {
	t.Helper()

	left, right := net.Pipe()
	t.Cleanup(func() {
		left.Close()
		right.Close()
	})
	return left
}

func mustGet(t *testing.T, table *ConnectionsTable, conn net.Conn, version fields.Version) RegistrationRow {
	t.Helper()

	row, err := table.Get(conn, version)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	return row
}

func assertNotGranted(t *testing.T, table *ConnectionsTable, conn net.Conn, version fields.Version) {
	t.Helper()

	_, err := table.Get(conn, version)
	if err == nil || !strings.Contains(err.Error(), "not granted") {
		t.Fatalf("Get(%d) = %v, want not granted", version, err)
	}
}

func stats(apply ...func(*ConnectionsTableStats)) ConnectionsTableStats {
	got := NewConnectionsTableStats()
	for _, fn := range apply {
		fn(&got)
	}
	return got
}

func assertStats(t *testing.T, got, want ConnectionsTableStats) {
	t.Helper()

	if !sameStats(got, want) {
		t.Fatalf("Stats() = %+v, want %+v", got, want)
	}
}

func checkStats(s ConnectionsTableStats) error {
	granted := 0
	for _, count := range s.ConnectionsPerVersion {
		granted += count
	}
	if granted != s.VersionsGranted {
		return fmt.Errorf("VersionsGranted = %d, connections per version sum to %d", s.VersionsGranted, granted)
	}
	if s.VersionsActive > s.VersionsGranted {
		return fmt.Errorf("VersionsActive = %d, VersionsGranted = %d", s.VersionsActive, s.VersionsGranted)
	}

	requests := 0
	for version, count := range s.RequestsPerVersion {
		requests += count
		if _, ok := s.ConnectionsPerVersion[version]; !ok {
			return fmt.Errorf("requests recorded for version %d without a connection", version)
		}
	}
	if requests != s.RequestsTotal {
		return fmt.Errorf("RequestsTotal = %d, per version sum to %d", s.RequestsTotal, requests)
	}
	return nil
}

func sameStats(got, want ConnectionsTableStats) bool {
	if got.ConnectionsTotal != want.ConnectionsTotal ||
		got.RequestsTotal != want.RequestsTotal ||
		got.VersionsGranted != want.VersionsGranted ||
		got.VersionsActive != want.VersionsActive ||
		len(got.ConnectionsPerVersion) != len(want.ConnectionsPerVersion) ||
		len(got.RequestsPerVersion) != len(want.RequestsPerVersion) {
		return false
	}

	for version, count := range want.ConnectionsPerVersion {
		if got.ConnectionsPerVersion[version] != count {
			return false
		}
	}
	for version, count := range want.RequestsPerVersion {
		if got.RequestsPerVersion[version] != count {
			return false
		}
	}
	return true
}

func assertNotActivated(t *testing.T, table *ConnectionsTable, conn net.Conn, version fields.Version) {
	t.Helper()

	_, err := table.Get(conn, version)
	if err == nil || !strings.Contains(err.Error(), "not activated") {
		t.Fatalf("Get(%d) = %v, want not activated", version, err)
	}
}
