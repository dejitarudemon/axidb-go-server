package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/dejitarudemon/ignicula-wire/v0/fields"
	"github.com/dejitarudemon/ignicula-framework/runtime/errs"
	"github.com/dejitarudemon/ignicula-framework/runtime/v1/row"
	serverconfig "github.com/dejitarudemon/ignicula-framework/server/config"
)

func TestIsDisconnect(t *testing.T) {
	if !isDisconnect(io.EOF) {
		t.Fatal("io.EOF: want true")
	}
	if !isDisconnect(io.ErrUnexpectedEOF) {
		t.Fatal("io.ErrUnexpectedEOF: want true")
	}
	if !isDisconnect(fmt.Errorf("reader eof: %w", io.EOF)) {
		t.Fatal("wrapped io.EOF: want true")
	}
	if isDisconnect(errors.New("boom")) {
		t.Fatal("other error: want false")
	}
}

func TestPeerEmptyWhenConnMissing(t *testing.T) {
	if got := peer(connContext{}); got != "" {
		t.Fatalf("peer() = %q, want empty", got)
	}
	if got := peer(connContext{conn: newConnection(nilAddrConn{})}); got != "" {
		t.Fatalf("peer(nil RemoteAddr) = %q, want empty", got)
	}
}

func TestLoggerWarnAndErrorAddTime(t *testing.T) {
	log := &memLogger{}
	s := NewServer(serverconfig.NewServerConfig().WithNetwork("tcp"), log)

	s.warn("w", "a", 1)
	s.error("e", "b", 2)

	if len(log.warn) != 1 || log.warn[0].msg != "w" {
		t.Fatalf("warn = %#v", log.warn)
	}
	if log.warn[0].args[0] != "time" {
		t.Fatalf("warn args = %#v", log.warn[0].args)
	}
	if len(log.err) != 1 || log.err[0].msg != "e" {
		t.Fatalf("err = %#v", log.err)
	}
	if log.err[0].args[0] != "time" {
		t.Fatalf("err args = %#v", log.err[0].args)
	}
}

func TestRuntimesPingErrors(t *testing.T) {
	r := runtimes{}
	if _, err := r.ping(fields.Version(1), nil, time.Second); err == nil {
		t.Fatal("ping with nil v1 runtime: want error")
	}
	if _, err := r.ping(fields.Version(99), nil, time.Second); err == nil {
		t.Fatal("ping unsupported version: want error")
	}

	rt := testRuntime(t)
	r.v1 = rt
	if _, err := r.ping(fields.Version(1), fakeRegRow{}, time.Second); err == nil {
		t.Fatal("ping with wrong row type: want error")
	}

	raw, err := r.ping(fields.Version(1), row.NewRequestRow("u", nil), time.Second)
	if err != nil {
		t.Fatalf("ping() = %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("ping() returned empty frame")
	}
}

func TestSendAnswerIgnoresEmpty(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	answers := make(chan []byte, 1)
	cctx := newConnContext(context.Background(), newConnection(discardConn{}), answers)

	s.sendAnswer(cctx, nil)
	s.sendAnswer(cctx, []byte{})
	select {
	case <-answers:
		t.Fatal("empty answer was enqueued")
	default:
	}
}

func TestSendAnswerDropsWhenCancelled(t *testing.T) {
	s := NewServer(testConfig(t), nil)

	for range 50 {
		answers := make(chan []byte, 1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		cctx := newConnContext(ctx, newConnection(discardConn{}), answers)
		s.sendAnswer(cctx, []byte{1})
		select {
		case <-answers:
			t.Fatal("answer enqueued on cancelled context")
		default:
		}
	}
}

func TestWriteLoopSkipsEmptyAndStopsOnCancel(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	answers := make(chan []byte, 2)
	ctx, cancel := newConnContextWithCancel(context.Background(), newConnection(discardConn{}), answers)

	var wg sync.WaitGroup
	wg.Go(func() { s.writeLoop(ctx) })

	answers <- nil
	answers <- []byte{}
	cancel()
	wg.Wait()
}

func TestWriteLoopCoalescesQueuedAnswers(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	answers := make(chan []byte, 4)
	ctx, cancel := newConnContextWithCancel(context.Background(), newConnection(server), answers)
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	wg.Go(func() { s.writeLoop(ctx) })

	want := []byte{1, 2, 3, 4, 5, 6}
	answers <- []byte{1, 2}
	answers <- []byte{3, 4}
	answers <- []byte{5, 6}

	got := make([]byte, len(want))
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() = %v", err)
	}
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("ReadFull() = %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	cancel()
	wg.Wait()
}

func TestWriteLoopWriteErrorCloses(t *testing.T) {
	log := &memLogger{}
	s := NewServer(testConfig(t), log)

	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })

	fail := &failWriteConn{Conn: server}
	answers := make(chan []byte, 1)
	ctx, cancel := newConnContextWithCancel(context.Background(), newConnection(fail), answers)
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	wg.Go(func() { s.writeLoop(ctx) })

	fail.fail = true
	answers <- []byte{0x0A, 0xDB, 0x01}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writeLoop did not exit after write error")
	}

	if len(log.err) == 0 {
		t.Fatal("expected error log on write failure")
	}
}

func TestDeliverV1AnswerCloseConnection(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	answers := make(chan []byte, 1)
	ctx, cancel := newConnContextWithCancel(context.Background(), newConnection(server), answers)
	t.Cleanup(cancel)

	stop := s.deliverV1Answer(ctx, nil, errs.CloseConnection(errors.New("boom")))
	if !stop {
		t.Fatal("deliverV1Answer CloseConnection: stop = false, want true")
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after CloseConnection")
	}
}

func TestDeliverV1AnswerLogAndIgnore(t *testing.T) {
	log := &memLogger{}
	s := NewServer(testConfig(t), log)
	answers := make(chan []byte, 1)
	ctx := newConnContext(context.Background(), newConnection(discardConn{}), answers)

	stop := s.deliverV1Answer(ctx, []byte{1, 2, 3}, errs.LogAndIgnore(errors.New("skip")))
	if stop {
		t.Fatal("LogAndIgnore: stop = true, want false")
	}
	// LogAndIgnore must not enqueue the answer payload.
	select {
	case <-answers:
		t.Fatal("LogAndIgnore enqueued an answer")
	default:
	}
	if len(log.err) != 1 {
		t.Fatalf("error logs = %d, want 1", len(log.err))
	}
}

func TestRespondRuntimeErrorClosesOnCloseConnection(t *testing.T) {
	s := NewServer(testConfig(t), nil)
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	answers := make(chan []byte, 1)
	ctx, cancel := newConnContextWithCancel(context.Background(), newConnection(server), answers)
	t.Cleanup(cancel)

	s.respondRuntimeError(ctx, []byte{9}, errs.CloseConnection(errors.New("x")), "decode failed")

	select {
	case got := <-answers:
		if len(got) != 1 || got[0] != 9 {
			t.Fatalf("answer = %v, want [9]", got)
		}
	case <-time.After(time.Second):
		t.Fatal("answer not enqueued")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled")
	}
}

// discardConn is a net.Conn that accepts writes and never reads.
type discardConn struct {
	net.Conn
}

func (discardConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (discardConn) Write(p []byte) (int, error)      { return len(p), nil }
func (discardConn) Close() error                     { return nil }
func (discardConn) LocalAddr() net.Addr              { return pipeAddr("local") }
func (discardConn) RemoteAddr() net.Addr             { return pipeAddr("remote") }
func (discardConn) SetDeadline(time.Time) error      { return nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }

type failWriteConn struct {
	net.Conn
	fail bool
}

func (c *failWriteConn) Write(p []byte) (int, error) {
	if c.fail {
		return 0, errors.New("forced write failure")
	}
	return c.Conn.Write(p)
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

type nilAddrConn struct{ discardConn }

func (nilAddrConn) RemoteAddr() net.Addr { return nil }

type fakeRegRow struct{}

func (fakeRegRow) Count() int { return 0 }
