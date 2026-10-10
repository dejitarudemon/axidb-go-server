package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	v1fields "github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/value"
	"github.com/dejitarudemon/ignicula-wire/v1/value/values"
	runtime_v1 "github.com/dejitarudemon/ignicula-framework/runtime/v1"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/config"
	serverconfig "github.com/dejitarudemon/ignicula-framework/server/config"
)

// benchConfig disables idle Ping interference during long runs.
func benchConfig(b *testing.B) *serverconfig.ServerConfig {
	b.Helper()
	return serverconfig.NewServerConfig().
		WithNetwork("tcp").
		WithReadTimeout(30 * time.Second).
		WithPingInterval(30 * time.Second).
		WithPingTimeout(100 * time.Second).
		WithBufferSize(64)
}

func benchRuntime(b *testing.B) *runtime_v1.Runtime {
	b.Helper()

	rt := runtime_v1.NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(v1TestLimit).
		WithMaxGoroutinesPerBatch(8)).
		WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		WithHandlerRead(func(_ runtime_v1.Context, key v1fields.Key) (value.V, error) {
			return values.String("ok:" + string(key)), nil
		}).
		WithHandlerWrite(func(runtime_v1.Context, v1fields.Key, value.V) error {
			return nil
		}).
		WithHandlerDelete(func(runtime_v1.Context, v1fields.Key) error {
			return nil
		}).
		Build()
	return &rt
}

// nextRequestID advances past 0; handlers run asynchronously so IDs must not overlap.
func nextRequestID(id *v1fields.RequestID) v1fields.RequestID {
	*id++
	if *id == 0 {
		*id = 1
	}
	return *id
}

func BenchmarkServerPing(b *testing.B) {
	s := startTestServer(b, benchConfig(b), benchRuntime(b))
	conn := dialServer(b, s)
	registerAndActivate(b, conn)

	var id v1fields.RequestID

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reqID := nextRequestID(&id)
		writeAll(b, conn, encodeClientPing(b, reqID))
		got := readV1Frame(b, conn, time.Second)
		if got.RequestID != reqID {
			b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
		}
		if _, ok := got.Body.(bodies.PingAnswer); !ok {
			b.Fatalf("body = %T, want PingAnswer", got.Body)
		}
	}
}

func BenchmarkServerRead(b *testing.B) {
	s := startTestServer(b, benchConfig(b), benchRuntime(b))
	conn := dialServer(b, s)
	registerAndActivate(b, conn)

	var id v1fields.RequestID

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reqID := nextRequestID(&id)
		writeAll(b, conn, encodeRead(b, reqID, "k"))
		got := readV1Frame(b, conn, time.Second)
		if got.RequestID != reqID {
			b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
		}
		ans, ok := got.Body.(bodies.ReadAnswer)
		if !ok {
			b.Fatalf("body = %T, want ReadAnswer", got.Body)
		}
		if ans.Value == nil {
			b.Fatal("nil read value")
		}
	}
}

func BenchmarkServerWrite(b *testing.B) {
	s := startTestServer(b, benchConfig(b), benchRuntime(b))
	conn := dialServer(b, s)
	registerAndActivate(b, conn)

	var id v1fields.RequestID

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reqID := nextRequestID(&id)
		writeAll(b, conn, encodeWrite(b, reqID, "k", "v"))
		got := readV1Frame(b, conn, time.Second)
		if got.RequestID != reqID {
			b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
		}
		if _, ok := got.Body.(bodies.WriteAnswer); !ok {
			b.Fatalf("body = %T, want WriteAnswer", got.Body)
		}
	}
}

func BenchmarkServerDelete(b *testing.B) {
	s := startTestServer(b, benchConfig(b), benchRuntime(b))
	conn := dialServer(b, s)
	registerAndActivate(b, conn)

	var id v1fields.RequestID

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		reqID := nextRequestID(&id)
		writeAll(b, conn, encodeDelete(b, reqID, "k"))
		got := readV1Frame(b, conn, time.Second)
		if got.RequestID != reqID {
			b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
		}
		if _, ok := got.Body.(bodies.DeleteAnswer); !ok {
			b.Fatalf("body = %T, want DeleteAnswer", got.Body)
		}
	}
}

func BenchmarkServerPingParallel(b *testing.B) {
	s := startTestServer(b, benchConfig(b), benchRuntime(b))

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		conn := dialServer(b, s)
		registerAndActivate(b, conn)

		var id v1fields.RequestID
		for pb.Next() {
			reqID := nextRequestID(&id)
			writeAll(b, conn, encodeClientPing(b, reqID))
			got := readV1Frame(b, conn, time.Second)
			if got.RequestID != reqID {
				b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
			}
			if _, ok := got.Body.(bodies.PingAnswer); !ok {
				b.Fatalf("body = %T, want PingAnswer", got.Body)
			}
		}
	})
}

func BenchmarkServerBatch(b *testing.B) {
	for _, n := range []int{8, 32} {
		for _, sequential := range []bool{true, false} {
			mode := "parallel"
			if sequential {
				mode = "seq"
			}
			// one-answer keeps the wire to a single frame (fair TCP compare).
			name := fmt.Sprintf("n=%d/%s/one", n, mode)
			b.Run(name, func(b *testing.B) {
				s := startTestServer(b, benchConfig(b), benchRuntime(b))
				conn := dialServer(b, s)
				registerAndActivate(b, conn)

				var id v1fields.RequestID

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					reqID := nextRequestID(&id)
					writeAll(b, conn, encodeBatchReads(b, reqID, n, sequential, true))
					got := readV1Frame(b, conn, 5*time.Second)
					if got.RequestID != reqID {
						b.Fatalf("request id = %d, want %d", got.RequestID, reqID)
					}
					if _, ok := got.Body.(bodies.BatchAnswer); !ok {
						b.Fatalf("body = %T, want BatchAnswer", got.Body)
					}
				}
			})
		}
	}
}

// BenchmarkServerPingPipeline sends a window of pings before reading answers.
// Answers may complete out of order because handleV1 runs Handle in a goroutine.
func BenchmarkServerPingPipeline(b *testing.B) {
	for _, window := range []int{8, 32} {
		b.Run(fmt.Sprintf("window=%d", window), func(b *testing.B) {
			s := startTestServer(b, benchConfig(b), benchRuntime(b))
			conn := dialServer(b, s)
			registerAndActivate(b, conn)

			var id v1fields.RequestID
			pending := make(map[v1fields.RequestID]struct{}, window)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				clear(pending)
				for range window {
					reqID := nextRequestID(&id)
					pending[reqID] = struct{}{}
					writeAll(b, conn, encodeClientPing(b, reqID))
				}
				for len(pending) > 0 {
					got := readV1Frame(b, conn, time.Second)
					if _, ok := got.Body.(bodies.PingAnswer); !ok {
						b.Fatalf("body = %T, want PingAnswer", got.Body)
					}
					if _, ok := pending[got.RequestID]; !ok {
						b.Fatalf("unexpected answer id %d", got.RequestID)
					}
					delete(pending, got.RequestID)
				}
			}
		})
	}
}
