package server

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	v1fields "github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

var loadClientCounts = []int{1, 8, 32, 64}

// loadOp is one client round-trip. Must not call testing.TB (runs off the test goroutine).
type loadOp func(conn *testConn, id *v1fields.RequestID) error

// percentile returns nearest-rank percentile for p in [0,1] on a sorted slice.
// Index is ceil(p*(n-1)), clamped to the last element.
func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	idx := int(math.Ceil(p * float64(n-1)))
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func writeLoadFrame(conn *testConn, raw []byte) error {
	n, err := conn.Write(raw)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return fmt.Errorf("Write() wrote %d, want %d", n, len(raw))
	}
	return nil
}

func loadPingOnce(conn *testConn, id *v1fields.RequestID) error {
	reqID := nextRequestID(id)
	f, err := testV1Builder.NewPing(reqID)
	if err != nil {
		return err
	}
	var buf buffer.Slice
	if err := f.Encode(&buf, nil); err != nil {
		return err
	}
	if err := writeLoadFrame(conn, buf.Bytes()); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	got, err := testV1Decoder.DecodeFrame(conn.r)
	if err != nil {
		return err
	}
	if got.RequestID != reqID {
		return fmt.Errorf("request id = %d, want %d", got.RequestID, reqID)
	}
	if _, ok := got.Body.(bodies.PingAnswer); !ok {
		return fmt.Errorf("body = %T, want PingAnswer", got.Body)
	}
	return nil
}

func loadReadOnce(conn *testConn, id *v1fields.RequestID) error {
	reqID := nextRequestID(id)
	f, err := testV1Builder.NewRead(reqID, v1fields.Key("k"))
	if err != nil {
		return err
	}
	var buf buffer.Slice
	if err := f.Encode(&buf, nil); err != nil {
		return err
	}
	if err := writeLoadFrame(conn, buf.Bytes()); err != nil {
		return err
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	got, err := testV1Decoder.DecodeFrame(conn.r)
	if err != nil {
		return err
	}
	if got.RequestID != reqID {
		return fmt.Errorf("request id = %d, want %d", got.RequestID, reqID)
	}
	ans, ok := got.Body.(bodies.ReadAnswer)
	if !ok {
		return fmt.Errorf("body = %T, want ReadAnswer", got.Body)
	}
	if ans.Value == nil {
		return fmt.Errorf("nil read value")
	}
	return nil
}

// runLoad fans b.N round-trips across a fixed number of activated clients.
func runLoad(b *testing.B, clients int, op loadOp) {
	b.Helper()

	s := startTestServer(b, benchConfig(b), benchRuntime(b))
	conns := make([]*testConn, clients)
	for i := range conns {
		conns[i] = dialServer(b, s)
		registerAndActivate(b, conns[i])
	}

	samples := make([]time.Duration, b.N)
	var cursor atomic.Int64
	var errMsg atomic.Value

	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	wg.Add(clients)
	for i := 0; i < clients; i++ {
		go func(conn *testConn) {
			defer wg.Done()
			var id v1fields.RequestID
			for {
				n := cursor.Add(1) - 1
				if n >= int64(len(samples)) {
					return
				}
				start := time.Now()
				if err := op(conn, &id); err != nil {
					errMsg.Store(err.Error())
					return
				}
				samples[n] = time.Since(start)
			}
		}(conns[i])
	}
	wg.Wait()
	b.StopTimer()

	if v := errMsg.Load(); v != nil {
		b.Fatal(v)
	}

	// Workers may exit early on error leaving zero samples; only sort completed prefix if needed.
	// On success every slot is filled.
	slices.Sort(samples)
	b.ReportMetric(float64(percentile(samples, 0.50).Nanoseconds()), "p50-ns/op")
	b.ReportMetric(float64(percentile(samples, 0.99).Nanoseconds()), "p99-ns/op")
}

func BenchmarkServerLoadPing(b *testing.B) {
	for _, clients := range loadClientCounts {
		b.Run(fmt.Sprintf("clients=%d", clients), func(b *testing.B) {
			runLoad(b, clients, loadPingOnce)
		})
	}
}

func BenchmarkServerLoadRead(b *testing.B) {
	for _, clients := range loadClientCounts {
		b.Run(fmt.Sprintf("clients=%d", clients), func(b *testing.B) {
			runLoad(b, clients, loadReadOnce)
		})
	}
}
