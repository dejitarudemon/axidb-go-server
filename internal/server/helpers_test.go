package server

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"

	v0builder "github.com/dejitarudemon/ignicula-wire/v0/builder"
	v0decoder "github.com/dejitarudemon/ignicula-wire/v0/decoder"
	v0fields "github.com/dejitarudemon/ignicula-wire/v0/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/body"
	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	"github.com/dejitarudemon/ignicula-wire/v1/buffer"
	v1builder "github.com/dejitarudemon/ignicula-wire/v1/builder"
	v1decoder "github.com/dejitarudemon/ignicula-wire/v1/decoder"
	v1fields "github.com/dejitarudemon/ignicula-wire/v1/fields"
	v1frame "github.com/dejitarudemon/ignicula-wire/v1/frame"
	"github.com/dejitarudemon/ignicula-wire/v1/value/values"
	runtime_v1 "github.com/dejitarudemon/ignicula-framework/internal/runtime/v1"
	"github.com/dejitarudemon/ignicula-framework/internal/runtime/v1/config"
	serverconfig "github.com/dejitarudemon/ignicula-framework/internal/server/config"
)

const v1TestLimit = 1 << 20

// Shared protocol helpers for tests/benchmarks. Decoder and FrameBuilder hold
// only config (limit / compressor map); reusing them avoids counting
// NewDecoder / NewFrameBuilder in -benchmem.
var (
	testV0Decoder = v0decoder.NewDecoder()
	testV1Decoder = v1decoder.NewDecoder(v1TestLimit, nil)
	testV1Builder = v1builder.NewFrameBuilder(v1TestLimit)
)

func testConfig(t testing.TB) *serverconfig.ServerConfig {
	t.Helper()
	return serverconfig.NewServerConfig().
		WithNetwork("tcp").
		WithReadTimeout(200 * time.Millisecond).
		WithPingInterval(100 * time.Millisecond).
		WithPingTimeout(300 * time.Millisecond).
		WithBufferSize(4)
}

func startTestServer(t testing.TB, cfg *serverconfig.ServerConfig, rt *runtime_v1.Runtime) *Server {
	t.Helper()

	if cfg == nil {
		cfg = testConfig(t)
	}

	s := NewServer(cfg, nil)
	if rt != nil {
		if err := s.RegisterV1Runtime(rt); err != nil {
			t.Fatalf("RegisterV1Runtime() = %v", err)
		}
	}

	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func testRuntime(t testing.TB) *runtime_v1.Runtime {
	t.Helper()

	rt := NewRuntimeBuilderForTest(t)
	return &rt
}

// NewRuntimeBuilderForTest builds a v1 runtime with accepting auth for server tests.
func NewRuntimeBuilderForTest(t testing.TB) runtime_v1.Runtime {
	t.Helper()

	return runtime_v1.NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(v1TestLimit)).
		WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		Build()
}

func rejectAuthRuntime(t testing.TB) *runtime_v1.Runtime {
	t.Helper()

	rt := runtime_v1.NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(v1TestLimit)).
		WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) {
			return false, nil
		}).
		Build()
	return &rt
}

type testConn struct {
	net.Conn
	r *bufio.Reader
}

func dialServer(t testing.TB, s *Server) *testConn {
	t.Helper()

	if s.listener == nil {
		t.Fatal("server has no listener")
	}

	conn, err := net.DialTimeout(s.listener.Addr().Network(), s.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("Dial() = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &testConn{Conn: conn, r: bufio.NewReader(conn)}
}

func encodeV0Hello(t testing.TB, versions []v0fields.Version) []byte {
	t.Helper()

	frame, err := v0builder.NewFrameBuilder(v0Limit).NewHello(versions)
	if err != nil {
		t.Fatalf("NewHello() = %v", err)
	}

	buf := buffer.Slice{}
	buf.Preallocate(frame.Size())
	if err := frame.Encode(&buf); err != nil {
		t.Fatalf("Encode() = %v", err)
	}
	return buf.Bytes()
}

func writeAll(t testing.TB, w io.Writer, raw []byte) {
	t.Helper()

	n, err := w.Write(raw)
	if err != nil {
		t.Fatalf("Write() = %v", err)
	}
	if n != len(raw) {
		t.Fatalf("Write() wrote %d, want %d", n, len(raw))
	}
}

func expectConnClosed(t testing.TB, conn *testConn, wait time.Duration) {
	t.Helper()

	deadline := time.Now().Add(wait)
	buf := make([]byte, 4096)
	chunks := 0

	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline() = %v", err)
		}

		_, err := conn.r.Read(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return
		}
		chunks++
	}

	t.Fatalf("connection still open after %v (%d chunks still arriving)", wait, chunks)
}

func readHelloAnswer(t testing.TB, conn *testConn) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() = %v", err)
	}

	if _, err := testV0Decoder.DecodeFrame(conn.r); err != nil {
		t.Fatalf("hello DecodeFrame() = %v", err)
	}
}

func encodeV1Frame(t testing.TB, f v1frame.Frame) []byte {
	t.Helper()

	var buf buffer.Slice
	if err := f.Encode(&buf, nil); err != nil {
		t.Fatalf("v1 Encode() = %v", err)
	}
	return buf.Bytes()
}

func encodeHandshake(t testing.TB, login string, hash [32]byte) []byte {
	t.Helper()

	f, err := testV1Builder.NewHandshake(login, hash, nil)
	if err != nil {
		t.Fatalf("NewHandshake() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodeClientPing(t testing.TB, id v1fields.RequestID) []byte {
	t.Helper()

	f, err := testV1Builder.NewPing(id)
	if err != nil {
		t.Fatalf("NewPing() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodePingAnswer(t testing.TB, id v1fields.RequestID) []byte {
	t.Helper()

	f, err := testV1Builder.NewPingAnswer(id)
	if err != nil {
		t.Fatalf("NewPingAnswer() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodeRead(t testing.TB, id v1fields.RequestID, key string) []byte {
	t.Helper()

	f, err := testV1Builder.NewRead(id, v1fields.Key(key))
	if err != nil {
		t.Fatalf("NewRead() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodeWrite(t testing.TB, id v1fields.RequestID, key, value string) []byte {
	t.Helper()

	f, err := testV1Builder.NewWrite(id, v1fields.Key(key), values.String(value))
	if err != nil {
		t.Fatalf("NewWrite() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodeDelete(t testing.TB, id v1fields.RequestID, key string) []byte {
	t.Helper()

	f, err := testV1Builder.NewDelete(id, v1fields.Key(key))
	if err != nil {
		t.Fatalf("NewDelete() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func encodeBatchReads(t testing.TB, id v1fields.RequestID, n int, sequential, oneAnswer bool) []byte {
	t.Helper()

	batch := v1builder.NewBatchRequestsBuilder().
		SequentialExecution(sequential).
		OneAnswer(oneAnswer)
	for i := 0; i < n; i++ {
		batch.AddRead(v1fields.Key("k"))
	}

	f, err := testV1Builder.NewBatch(id, *batch)
	if err != nil {
		t.Fatalf("NewBatch() = %v", err)
	}
	return encodeV1Frame(t, f)
}

func readV1Frame(t testing.TB, conn *testConn, d time.Duration) v1frame.Frame {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatalf("SetReadDeadline() = %v", err)
	}

	got, err := testV1Decoder.DecodeFrame(conn.r)
	if err != nil {
		t.Fatalf("DecodeFrame() = %v", err)
	}
	return got
}

// readV1IgnoringServerPings reads the next non-Ping frame.
// Server idle Pings are skipped so tests can sync on application traffic.
func readV1IgnoringServerPings(t testing.TB, conn *testConn, d time.Duration) v1frame.Frame {
	t.Helper()

	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		got := readV1Frame(t, conn, remaining)
		if _, ok := got.Body.(bodies.Ping); ok {
			continue
		}
		return got
	}
	t.Fatal("timed out waiting for a non-ping frame")
	return v1frame.Frame{}
}

func registerAndActivate(t testing.TB, conn *testConn) {
	t.Helper()

	writeAll(t, conn, encodeV0Hello(t, []v0fields.Version{1}))
	readHelloAnswer(t, conn)

	writeAll(t, conn, encodeHandshake(t, "user", [32]byte{1}))
	got := readV1Frame(t, conn, time.Second)
	if _, ok := got.Body.(bodies.HandshakeAnswer); !ok {
		t.Fatalf("activate body = %T, want HandshakeAnswer", got.Body)
	}
}

func errorAnswerCode(t testing.TB, b body.Body) v1fields.Error {
	t.Helper()

	ans, ok := b.(bodies.ErrorAnswer)
	if !ok {
		t.Fatalf("body = %T, want ErrorAnswer", b)
	}
	return ans.Err.Code()
}
