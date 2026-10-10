package server

import (
	"testing"
	"time"

	v0fields "github.com/dejitarudemon/axidb-go-protocol/v0/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	v1fields "github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	runtime_v1 "github.com/dejitarudemon/axidb-go-server/internal/runtime/v1"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
)

func TestChecksumMismatchReturnsAnswerAndKeepsConnection(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	raw := encodeClientPing(t, 3)
	raw[len(raw)-1] ^= 0xff
	writeAll(t, conn, raw)

	got := readV1Frame(t, conn, time.Second)
	if code := errorAnswerCode(t, got.Body); code != v1fields.MismatchedChecksum {
		t.Fatalf("code = %v, want MismatchedChecksum", code)
	}

	writeAll(t, conn, encodeClientPing(t, 4))
	ans := readV1Frame(t, conn, time.Second)
	if _, ok := ans.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T, want PingAnswer after soft decode error", ans.Body)
	}
}

func TestUnsupportedCompressionClosesConnection(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	raw := encodeClientPing(t, 5)
	if len(raw) < 9 {
		t.Fatalf("frame too short: %d", len(raw))
	}
	raw[8] = 9
	writeAll(t, conn, raw)

	expectConnClosed(t, conn, time.Second)
}

func TestUnregisteredPingAnswerIsIgnored(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodePingAnswer(t, 99))

	time.Sleep(50 * time.Millisecond)
	writeAll(t, conn, encodeClientPing(t, 6))
	ans := readV1Frame(t, conn, time.Second)
	if _, ok := ans.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T, want PingAnswer", ans.Body)
	}
}

func TestDuplicateRequestIDConflicts(t *testing.T) {
	gate := make(chan struct{})
	rt := runtime_v1.NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(v1TestLimit)).
		WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		WithHandlerRead(func(runtime_v1.Context, v1fields.Key) (value.V, error) {
			<-gate
			return nil, nil
		}).
		Build()

	s := startTestServer(t, testConfig(t), &rt)
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodeRead(t, 10, "a"))
	// Give the first Handle goroutine time to Register(10).
	time.Sleep(30 * time.Millisecond)
	writeAll(t, conn, encodeRead(t, 10, "b"))

	got := readV1IgnoringServerPings(t, conn, time.Second)
	if code := errorAnswerCode(t, got.Body); code != v1fields.RequestsConflict {
		t.Fatalf("code = %v, want RequestsConflict", code)
	}

	close(gate)
}

func TestGrantedButNotActivatedSkipsAuth(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	var serverConn *Connection
	s.active.Range(func(key, _ any) bool {
		serverConn, _ = key.(*Connection)
		return false
	})
	if serverConn == nil {
		t.Fatal("no active server connection")
	}
	if err := s.table.Grant(serverConn, versionV1); err != nil {
		t.Fatalf("Grant() = %v", err)
	}

	// Would reject if auth ran.
	s.runtimes.v1 = rejectAuthRuntime(t)

	writeAll(t, conn, encodeHandshake(t, "user", [32]byte{1}))
	got := readV1Frame(t, conn, time.Second)
	if _, ok := got.Body.(bodies.HandshakeAnswer); !ok {
		code := any(nil)
		if ans, ok := got.Body.(bodies.ErrorAnswer); ok && ans.Err != nil {
			code = ans.Err.Code()
		}
		t.Fatalf("body = %T (code=%v), want HandshakeAnswer with skipAuth", got.Body, code)
	}
}

func TestHandshakeWhenAlreadyActiveIsUnexpectedCommand(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodeHandshake(t, "user", [32]byte{1}))
	got := readV1Frame(t, conn, time.Second)
	if code := errorAnswerCode(t, got.Body); code != v1fields.UnexpectedCommand {
		t.Fatalf("code = %v, want UnexpectedCommand", code)
	}
}

func TestTruncatedV1FrameClosesConnection(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	// Magic + version only — Decode cannot recover a usable stream.
	writeAll(t, conn, []byte{0x0A, 0xDB, 0x01})
	expectConnClosed(t, conn, time.Second)
}

func TestBodyLimitExceededClosesConnection(t *testing.T) {
	// Limit fits a tiny handshake but not a long Read key.
	rt := runtime_v1.NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(128)).
		WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		Build()

	s := startTestServer(t, testConfig(t), &rt)
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	writeAll(t, conn, encodeHandshake(t, "u", [32]byte{1}))
	got := readV1Frame(t, conn, time.Second)
	if _, ok := got.Body.(bodies.HandshakeAnswer); !ok {
		t.Fatalf("activate body = %T, want HandshakeAnswer", got.Body)
	}

	key := string(make([]byte, 200))
	writeAll(t, conn, encodeRead(t, 50, key))
	expectConnClosed(t, conn, time.Second)
}

func TestPipelinedClientPings(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodeClientPing(t, 21))
	writeAll(t, conn, encodeClientPing(t, 22))
	writeAll(t, conn, encodeClientPing(t, 23))

	seen := map[v1fields.RequestID]bool{}
	for range 3 {
		got := readV1IgnoringServerPings(t, conn, time.Second)
		if _, ok := got.Body.(bodies.PingAnswer); !ok {
			t.Fatalf("body = %T, want PingAnswer", got.Body)
		}
		seen[got.RequestID] = true
	}
	for _, id := range []v1fields.RequestID{21, 22, 23} {
		if !seen[id] {
			t.Fatalf("missing answer for %d, got %v", id, seen)
		}
	}
}
