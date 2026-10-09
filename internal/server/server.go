package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dejitarudemon/axidb-go-protocol/v0/builder"
	"github.com/dejitarudemon/axidb-go-protocol/v0/decoder"
	"github.com/dejitarudemon/axidb-go-protocol/v0/fields"
	"github.com/dejitarudemon/axidb-go-server/internal/logger"
	runtime_v1 "github.com/dejitarudemon/axidb-go-server/internal/runtime/v1"
	"github.com/dejitarudemon/axidb-go-server/internal/server/config"
	"github.com/dejitarudemon/axidb-go-server/internal/table"
)

const v0Limit = 263

// Server accepts TCP connections and dispatches protocol frames.
type Server struct {
	listener net.Listener
	table    *table.ConnectionsTable
	runtimes runtimes
	logger   logger.Logger

	started   atomic.Bool
	closeOnce sync.Once
	active    sync.Map // *Connection → context.CancelFunc

	shutdown chan struct{}
	done     chan struct{}

	decoder decoder.Decoder
	builder builder.FrameBuilder

	network      string
	readTimeout  time.Duration
	pingInterval time.Duration
	pingTimeout  time.Duration
	bufferSize   int
}

// NewServer returns a server that is not yet listening.
//
// cfg supplies timeouts, network, and buffer size; a nil cfg uses
// [config.NewServerConfig] defaults. A nil logger is allowed.
func NewServer(cfg *config.ServerConfig, logger logger.Logger) *Server {
	if cfg == nil {
		cfg = config.NewServerConfig()
	}

	return &Server{
		logger:       logger,
		table:        table.NewConnectionsTable(),
		decoder:      decoder.NewDecoder(),
		builder:      builder.NewFrameBuilder(v0Limit),
		network:      cfg.Network(),
		bufferSize:   cfg.BufferSize(),
		readTimeout:  cfg.ReadTimeout(),
		pingInterval: cfg.PingInterval(),
		pingTimeout:  cfg.PingTimeout(),
	}
}

// RegisterV1Runtime installs the v1 runtime used after Hello registration.
//
// It must be called before [Server.Start]. A nil runtime is rejected.
func (s *Server) RegisterV1Runtime(runtime *runtime_v1.Runtime) error {
	if runtime == nil {
		return errors.New("expected runtime_v1.Runtime, got nil")
	}

	if s.started.Load() {
		return errors.New("server is already running")
	}

	s.info("register new runtime", "version", fields.Version(1))
	s.runtimes.v1 = runtime
	return nil
}

// Start listens on addr:port and begins accepting connections.
//
// Each Start allocates fresh shutdown/done channels so the server can be
// started again after [Server.Close].
func (s *Server) Start(addr string, port uint16) error {
	if s.started.Load() {
		return errors.New("server is already running")
	}

	addr = joinAddr(addr, port)

	ln, err := net.Listen(s.network, addr)
	if err != nil {
		return err
	}

	s.shutdown = make(chan struct{})
	s.done = make(chan struct{})
	s.closeOnce = sync.Once{}
	s.listener = ln
	s.started.Store(true)

	s.info("server is started", "addr", addr, "network", s.network)

	go s.serve()
	return nil
}

// Close stops accepting, cancels active connections, and waits for the accept
// loop. It is safe to call more than once; later calls return the first result.
func (s *Server) Close() error {
	var err error

	s.closeOnce.Do(func() {
		if !s.started.Load() {
			err = errors.New("server is not running")
			return
		}

		s.started.Store(false)
		close(s.shutdown)

		s.active.Range(func(key, value any) bool {
			if cancel, ok := value.(context.CancelFunc); ok && cancel != nil {
				cancel()
			}
			if conn, ok := key.(*Connection); ok && conn != nil {
				_ = conn.Close()
			}
			return true
		})

		s.table.Close()

		if s.listener != nil {
			err = s.listener.Close()
		}

		<-s.done
	})

	return err
}

func (s *Server) serve() {
	defer close(s.done)

	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case <-s.shutdown:
			return

		default:
			conn, err := s.listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}

				s.error("got an error from listener", "error", err)
				continue
			}

			if conn == nil {
				s.warn("expected net.Conn, got nil")
				continue
			}

			wg.Go(func() { s.serveConn(ctx, newConnection(conn)) })
		}
	}
}
