package server

import (
	"net"
	"time"
)

const (
	// netBufferSize is the max number of encoded answer frames coalesced into
	// one writev before a forced flush.
	netBufferSize = 16
	// flushAfter is the production latency ceiling for a partial batch.
	// Without it, a non-full batch would wait for more answers indefinitely.
	flushAfter = time.Microsecond
)

// writeLoop drains encoded answers for one connection until the context ends.
//
// Production coalesce policy:
//   - flush when the batch reaches [netBufferSize], or
//   - flush when [flushAfter] elapses after the first queued frame;
//   - the timer is armed only while the batch is non-empty (idle conns do not tick).
//
// Immediate per-frame write would skip coalesce under pipeline / parallel
// serveV1Frame completions; size-only flush would stall partial batches.
func (s *Server) writeLoop(ctx connContext) {
	batch := make(net.Buffers, 0, netBufferSize)
	timer := time.NewTimer(flushAfter)
	stopTimer(timer)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case answer := <-ctx.answers:
			if len(answer) == 0 {
				continue
			}
			batch = append(batch, answer)
			if len(batch) < cap(batch) {
				if len(batch) == 1 {
					timer.Reset(flushAfter)
				}
				continue
			}

		case <-timer.C:
			if len(batch) == 0 {
				continue
			}
		}

		if _, err := batch.WriteTo(ctx.conn); err != nil {
			s.error("failed to answer", "error", err, "source", peer(ctx))
			s.closeConn(ctx)
			return
		}
		stopTimer(timer)
	}
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// sendAnswer enqueues answer for [Server.writeLoop].
// A nil or empty answer is ignored. A cancelled context drops the answer.
func (s *Server) sendAnswer(ctx connContext, answer []byte) {
	if len(answer) == 0 {
		return
	}
	if ctx.Err() != nil {
		return
	}

	select {
	case <-ctx.Done():
	case ctx.answers <- answer:
	}
}
