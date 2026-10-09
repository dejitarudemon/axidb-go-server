package server

import (
	"math"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	v1decoder "github.com/dejitarudemon/axidb-go-protocol/v1/decoder"
	v1fields "github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

func TestIdlePingAfterActivate(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(40 * time.Millisecond).
		WithPingInterval(60 * time.Millisecond).
		WithPingTimeout(500 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	got := readV1Frame(t, conn, time.Second)
	if _, ok := got.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want server Ping", got.Body)
	}
	if got.RequestID == 0 {
		t.Fatal("ping request id = 0")
	}
}

func TestAnyActivityAfterIdlePingKeepsConnection(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(40 * time.Millisecond).
		WithPingInterval(60 * time.Millisecond).
		WithPingTimeout(200 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	ping := readV1Frame(t, conn, time.Second)
	if _, ok := ping.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want server Ping", ping.Body)
	}

	// Any client frame — not necessarily PingAnswer — resets the wait.
	writeAll(t, conn, encodeClientPing(t, 40))
	ans := readV1IgnoringServerPings(t, conn, time.Second)
	if _, ok := ans.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T, want PingAnswer to client ping", ans.Body)
	}
	if ans.RequestID != 40 {
		t.Fatalf("request id = %d, want 40", ans.RequestID)
	}

	// Still alive past the original pingTimeout window.
	time.Sleep(250 * time.Millisecond)
	writeAll(t, conn, encodeClientPing(t, 41))
	ans = readV1IgnoringServerPings(t, conn, time.Second)
	if ans.RequestID != 41 {
		t.Fatalf("request id = %d, want 41", ans.RequestID)
	}
}

func TestPingAnswerClearsIdleWait(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(40 * time.Millisecond).
		WithPingInterval(60 * time.Millisecond).
		WithPingTimeout(200 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	ping := readV1Frame(t, conn, time.Second)
	if _, ok := ping.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want server Ping", ping.Body)
	}

	writeAll(t, conn, encodePingAnswer(t, ping.RequestID))

	time.Sleep(250 * time.Millisecond)
	// Use a high id so a possible idle-ping flood does not collide with Reserve().
	id := v1fields.RequestID(math.MaxUint32)
	writeAll(t, conn, encodeClientPing(t, id))
	ans := readV1IgnoringServerPings(t, conn, time.Second)
	if _, ok := ans.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T (code=%v), want PingAnswer for id %d", ans.Body, errorAnswerCode(t, ans.Body), id)
	}
	if ans.RequestID != id {
		t.Fatalf("request id = %d, want %d", ans.RequestID, id)
	}
}

func TestIdlePingDoesNotFlood(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(20 * time.Millisecond).
		WithPingInterval(80 * time.Millisecond).
		WithPingTimeout(time.Second)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	// In ~200ms with an 80ms interval the server should send about 2–3 pings,
	// not one per read-timeout wake.
	deadline := time.Now().Add(220 * time.Millisecond)
	count := 0
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if err := conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			t.Fatalf("SetReadDeadline() = %v", err)
		}
		got, err := v1decoder.NewDecoder(v1TestLimit, nil).DecodeFrame(conn.r)
		if err != nil {
			if isTimeout(err) {
				break
			}
			t.Fatalf("DecodeFrame() = %v", err)
		}
		if _, ok := got.Body.(bodies.Ping); !ok {
			t.Fatalf("body = %T, want Ping", got.Body)
		}
		count++
	}

	if count > 4 {
		t.Fatalf("BUG: idle ping flood: got %d pings in ~220ms (interval=%v); spec is one ping per idle interval", count, cfg.PingInterval())
	}
	if count < 1 {
		t.Fatal("expected at least one idle ping")
	}
}

func TestIdlePingTimeoutCloses(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(40 * time.Millisecond).
		WithPingInterval(50 * time.Millisecond).
		WithPingTimeout(120 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	ping := readV1Frame(t, conn, time.Second)
	if _, ok := ping.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want server Ping", ping.Body)
	}

	// Do not answer — allow further idle pings, but the socket must close
	// shortly after pingTimeout from the first ping.
	expectConnClosed(t, conn, time.Second)
}

func TestIdlePingThenAnswerThenAnotherPing(t *testing.T) {
	cfg := testConfig(t).
		WithReadTimeout(40 * time.Millisecond).
		WithPingInterval(60 * time.Millisecond).
		WithPingTimeout(400 * time.Millisecond)

	s := startTestServer(t, cfg, testRuntime(t))
	conn := dialServer(t, s)
	registerAndActivate(t, conn)

	ping := readV1Frame(t, conn, time.Second)
	if _, ok := ping.Body.(bodies.Ping); !ok {
		t.Fatalf("body = %T, want server Ping", ping.Body)
	}

	writeAll(t, conn, encodePingAnswer(t, ping.RequestID))

	ping2 := readV1Frame(t, conn, time.Second)
	if _, ok := ping2.Body.(bodies.Ping); !ok {
		t.Fatalf("second body = %T, want server Ping", ping2.Body)
	}
	if ping2.RequestID == 0 {
		t.Fatal("second ping id = 0")
	}
}
