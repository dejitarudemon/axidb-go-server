package server

import (
	"testing"
	"time"

	v0fields "github.com/dejitarudemon/axidb-go-protocol/v0/fields"
)

func TestRegistrationTimeoutClosesConnection(t *testing.T) {
	cfg := testConfig(t).WithReadTimeout(80 * time.Millisecond)
	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)

	expectConnClosed(t, conn, time.Second)
}

func TestSkipRegistrationWithV1PreambleCloses(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	// Shared magic + version 1 — not a v0 Hello.
	writeAll(t, conn, []byte{0x0A, 0xDB, 0x01})
	expectConnClosed(t, conn, time.Second)
}

func TestGarbageAfterConnectCloses(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, []byte{0x00, 0x01, 0x02, 0x03})
	expectConnClosed(t, conn, time.Second)
}

func TestSecondHelloCloses(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	expectConnClosed(t, conn, time.Second)
}

func TestUnsupportedVersionAfterHelloCloses(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	// Version 99 after magic — not in runtimes.
	writeAll(t, conn, []byte{0x0A, 0xDB, 0x63})
	expectConnClosed(t, conn, time.Second)
}

func TestIdleWithoutActiveVersionCloses(t *testing.T) {
	// Hello registers the connection but nothing activates a version.
	cfg := testConfig(t).
		WithReadTimeout(50 * time.Millisecond).
		WithPingInterval(50 * time.Millisecond).
		WithPingTimeout(200 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	expectConnClosed(t, conn, time.Second)
}

func TestHelloWithoutV1RuntimeStillAnswers(t *testing.T) {
	// No RegisterV1Runtime — Hello should still get an answer (possibly empty versions).
	s := startTestServer(t, testConfig(t), nil)
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)
}

func TestClientCloseDuringIdleIsTolerated(t *testing.T) {
	cfg := testConfig(t).WithReadTimeout(50 * time.Millisecond)
	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	if err := conn.Close(); err != nil {
		t.Fatalf("client Close() = %v", err)
	}

	// Server Close must still finish cleanly (via t.Cleanup).
	time.Sleep(100 * time.Millisecond)
}

func TestPartialHelloThenTimeoutCloses(t *testing.T) {
	cfg := testConfig(t).WithReadTimeout(80 * time.Millisecond)
	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)

	// Magic only — not a full Hello frame.
	writeAll(t, conn, []byte{0x0A, 0xDB})
	expectConnClosed(t, conn, time.Second)
}
