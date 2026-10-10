package runtime_v1

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor/compressors"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value/values"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

const benchBodyLimit = 1 << 20

func benchRuntime(b *testing.B, valueByKey map[string]value.V) Runtime {
	b.Helper()

	return NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithBodyLimit(benchBodyLimit)).
		WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
			return true, nil
		}).
		WithHandlerRead(func(_ Context, key fields.Key) (value.V, error) {
			if valueByKey == nil {
				return nil, nil
			}
			return valueByKey[string(key)], nil
		}).
		WithHandlerWrite(func(Context, fields.Key, value.V) error {
			return nil
		}).
		WithHandlerDelete(func(Context, fields.Key) error {
			return nil
		}).
		Build()
}

func benchEncodeFrame(b *testing.B, f frame.Frame) []byte {
	b.Helper()

	var buf buffer.Slice
	if err := f.Encode(&buf, nil); err != nil {
		b.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func drainHandle(b *testing.B, rt Runtime, ctx context.Context, requestRow *row.RequestRow, req frame.Frame, wantAnswers int) {
	b.Helper()

	n := 0
	for encoded, err := range rt.Handle(ctx, req, requestRow) {
		if err != nil {
			b.Fatalf("Handle() = %v", err)
		}
		if len(encoded) == 0 {
			b.Fatal("empty answer")
		}
		n++
	}
	if n != wantAnswers {
		b.Fatalf("answers = %d, want %d", n, wantAnswers)
	}
}

func batchReadRequests(n int) []bodies.Request {
	out := make([]bodies.Request, n)
	for i := range n {
		out[i] = bodies.Request{
			Number: fields.RequestNumber(i + 1),
			Body:   bodies.Read("k"),
		}
	}
	return out
}

func BenchmarkDecodePing(b *testing.B) {
	rt := benchRuntime(b, nil)
	raw := benchEncodeFrame(b, frame.Frame{RequestID: 1, Body: bodies.Ping{}})

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		got, answer, err := rt.Decode(bufio.NewReader(bytes.NewReader(raw)))
		if err != nil || answer != nil || got.Body == nil {
			b.Fatalf("Decode() = (%v, %v, %v)", got, answer, err)
		}
	}
}

func BenchmarkActivate(b *testing.B) {
	rt := benchRuntime(b, nil)
	req := handshakeRequest()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		row, _, answer, err := rt.Activate(ctx, req, nil, false)
		if err != nil || row == nil || answer == nil {
			b.Fatalf("Activate() = (%v, %v, %v)", row, answer, err)
		}
	}
}

func BenchmarkHandlePing(b *testing.B) {
	rt := benchRuntime(b, nil)
	requestRow := row.NewRequestRow("user", nil)
	req := frame.Frame{RequestID: 1, Body: bodies.Ping{}}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkHandleRead(b *testing.B) {
	rt := benchRuntime(b, map[string]value.V{
		"k": values.String("v"),
	})
	requestRow := row.NewRequestRow("user", nil)
	req := frame.Frame{RequestID: 1, Body: bodies.Read("k")}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkHandleWrite(b *testing.B) {
	rt := benchRuntime(b, nil)
	requestRow := row.NewRequestRow("user", nil)
	req := frame.Frame{
		RequestID: 1,
		Body:      bodies.Write{Key: fields.Key("k"), Value: values.String("v")},
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkHandleDelete(b *testing.B) {
	rt := benchRuntime(b, nil)
	requestRow := row.NewRequestRow("user", nil)
	req := frame.Frame{RequestID: 1, Body: bodies.Delete("k")}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkRuntimePing(b *testing.B) {
	rt := benchRuntime(b, nil)
	requestRow := row.NewRequestRow("user", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		raw, err := rt.Ping(requestRow, time.Second)
		if err != nil {
			b.Fatalf("Ping() = %v", err)
		}
		if len(raw) == 0 {
			b.Fatal("empty ping")
		}
		got, decErr := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
		if decErr != nil {
			b.Fatalf("decode ping = %v", decErr)
		}
		requestRow.Terminate(got.RequestID)
	}
}

func BenchmarkHandleReadCompressed(b *testing.B) {
	const payload = 8 << 10 // 8 KiB — soft path (S2), below Zstd floor

	s2, err := compressors.NewS2(benchBodyLimit)
	if err != nil {
		b.Fatalf("NewS2() = %v", err)
	}
	zstd, err := compressors.NewZstd(benchBodyLimit)
	if err != nil {
		b.Fatalf("NewZstd() = %v", err)
	}

	payloadStr := strings.Repeat("x", payload)
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(benchBodyLimit).
		WithStartUseCompressionAt(1 << 9).
		WithCompressors(s2, zstd)).
		WithHandlerRead(func(Context, fields.Key) (value.V, error) {
			return values.String(payloadStr), nil
		}).
		Build()

	requestRow := row.NewRequestRow("user", []fields.Compression{fields.S2, fields.Zstd})
	req := frame.Frame{RequestID: 1, Body: bodies.Read("k")}
	ctx := context.Background()

	b.SetBytes(int64(payload))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkHandleReadCompressedZstd(b *testing.B) {
	payload := useHardestCompressionIfBodySizeAtLeast + 4<<10

	s2, err := compressors.NewS2(benchBodyLimit)
	if err != nil {
		b.Fatalf("NewS2() = %v", err)
	}
	zstd, err := compressors.NewZstd(benchBodyLimit)
	if err != nil {
		b.Fatalf("NewZstd() = %v", err)
	}

	payloadStr := strings.Repeat("y", payload)
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(benchBodyLimit).
		WithStartUseCompressionAt(1 << 9).
		WithCompressors(s2, zstd)).
		WithHandlerRead(func(Context, fields.Key) (value.V, error) {
			return values.String(payloadStr), nil
		}).
		Build()

	requestRow := row.NewRequestRow("user", []fields.Compression{fields.S2, fields.Zstd})
	req := frame.Frame{RequestID: 1, Body: bodies.Read("k")}
	ctx := context.Background()

	b.SetBytes(int64(payload))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		drainHandle(b, rt, ctx, requestRow, req, 1)
	}
}

func BenchmarkHandleBatch(b *testing.B) {
	for _, n := range []int{8, 32} {
		for _, sequential := range []bool{true, false} {
			for _, oneAnswer := range []bool{true, false} {
				mode := "parallel"
				if sequential {
					mode = "seq"
				}
				answer := "multi"
				if oneAnswer {
					answer = "one"
				}
				name := fmt.Sprintf("n=%d/%s/%s", n, mode, answer)
				b.Run(name, func(b *testing.B) {
					rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
						WithBodyLimit(benchBodyLimit).
						WithMaxGoroutinesPerBatch(8)).
						WithHandlerRead(func(Context, fields.Key) (value.V, error) {
							return values.String("v"), nil
						}).
						Build()

					requestRow := row.NewRequestRow("user", nil)
					req := frame.Frame{
						RequestID: 1,
						Body: bodies.Batch{
							IsSequentialExecution: sequential,
							IsOneAnswer:           oneAnswer,
							Requests:              batchReadRequests(n),
						},
					}
					ctx := context.Background()
					want := n
					if oneAnswer {
						want = 1
					}

					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						drainHandle(b, rt, ctx, requestRow, req, want)
					}
				})
			}
		}
	}
}
