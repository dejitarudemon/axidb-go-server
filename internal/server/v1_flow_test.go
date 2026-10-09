package server

import (
	"testing"
	"time"

	v0fields "github.com/dejitarudemon/axidb-go-protocol/v0/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	v1fields "github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

func TestHelloHandshakeReadKeepsConnection(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	registerAndActivate(t, conn)

	writeAll(t, conn, encodeRead(t, 7, "missing"))
	got := readV1Frame(t, conn, time.Second)

	if got.RequestID != 7 {
		t.Fatalf("request id = %d, want 7", got.RequestID)
	}
	if code := errorAnswerCode(t, got.Body); code != v1fields.CommandNotImplemented {
		t.Fatalf("code = %v, want CommandNotImplemented", code)
	}

	// Connection must still accept another frame.
	writeAll(t, conn, encodeClientPing(t, 8))
	pingAns := readV1Frame(t, conn, time.Second)
	if _, ok := pingAns.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T, want PingAnswer", pingAns.Body)
	}
}

func TestHandshakeUnauthorizedThenIdleCloses(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(50 * time.Millisecond).
		WithPingInterval(50 * time.Millisecond)

	s := startTestServer(t, cfg, rejectAuthRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	writeAll(t, conn, encodeHandshake(t, "user", [32]byte{1}))
	got := readV1Frame(t, conn, time.Second)

	if code := errorAnswerCode(t, got.Body); code != v1fields.Unauthorized {
		t.Fatalf("code = %v, want Unauthorized", code)
	}

	// Registered but not activated — idle closes (no active version).
	expectConnClosed(t, conn, time.Second)
}

func TestHandshakeThenNonHandshakeIsHandled(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	conn := dialServer(t, s)

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	// First v1 frame must be handshake for activation; a bare Ping should fail activation.
	writeAll(t, conn, encodeClientPing(t, 1))
	got := readV1Frame(t, conn, time.Second)

	if code := errorAnswerCode(t, got.Body); code != v1fields.UnexpectedCommand {
		t.Fatalf("code = %v, want UnexpectedCommand", code)
	}
}

func TestTwoClientsIndependent(t *testing.T) {
	s := startTestServer(t, testConfig(t), testRuntime(t))
	a := dialServer(t, s)
	b := dialServer(t, s)

	registerAndActivate(t, a)
	registerAndActivate(t, b)

	writeAll(t, a, encodeClientPing(t, 11))
	writeAll(t, b, encodeClientPing(t, 22))

	ansA := readV1Frame(t, a, time.Second)
	ansB := readV1Frame(t, b, time.Second)

	if ansA.RequestID != 11 {
		t.Fatalf("client A id = %d, want 11", ansA.RequestID)
	}
	if ansB.RequestID != 22 {
		t.Fatalf("client B id = %d, want 22", ansB.RequestID)
	}
}

func TestCloseDropsActiveClient(t *testing.T) {
	cfg := testConfig(t)
	s := NewServer(cfg, nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}
	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	if err := s.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	expectConnClosed(t, conn, time.Second)
}
