package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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

const (
	v0Limit = 263

	// minTLSVersion is the lowest TLS protocol version the server accepts.
	minTLSVersion = tls.VersionTLS13
)

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

	tlsConfig   *tls.Config
	tlsCertFile string
	tlsKeyFile  string
}

// NewServer returns a server that is not yet listening.
//
// cfg supplies timeouts, network, buffer size, and optional TLS credentials;
// a nil cfg uses [config.NewServerConfig] defaults. A nil logger is allowed.
func NewServer(cfg *config.ServerConfig, logger logger.Logger) *Server {
	if cfg == nil {
		cfg = config.NewServerConfig()
	}

	var tlsCfg *tls.Config
	if cfg.TLSConfig() != nil {
		tlsCfg = cfg.TLSConfig().Clone()
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
		tlsConfig:    tlsCfg,
		tlsCertFile:  cfg.TLSCertFile(),
		tlsKeyFile:   cfg.TLSKeyFile(),
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
// When TLS credentials are configured ([config.ServerConfig.WithTLSConfig] or
// [config.ServerConfig.WithTLSFiles]), the listener is a TLS listener that
// requires at least TLS 1.3. Otherwise the server listens in plain TCP and
// emits [unprotectedTLSMessage]: through the logger at warn level when one is
// set, or to stdout when the logger is nil.
//
// Each Start allocates fresh shutdown/done channels so the server can be
// started again after [Server.Close].
func (s *Server) Start(addr string, port uint16) error {
	if s.started.Load() {
		return errors.New("server is already running")
	}

	addr = joinAddr(addr, port)

	tlsCfg, err := s.buildTLSConfig()
	if err != nil {
		return err
	}

	var ln net.Listener
	if tlsCfg != nil {
		ln, err = tls.Listen(s.network, addr, tlsCfg)
	} else {
		ln, err = net.Listen(s.network, addr)
	}
	if err != nil {
		return err
	}

	s.shutdown = make(chan struct{})
	s.done = make(chan struct{})
	s.closeOnce = sync.Once{}
	s.listener = ln
	s.started.Store(true)

	if tlsCfg == nil {
		s.alertUnprotected()
	}

	s.info("server is started", "addr", addr, "network", s.network, "tls", tlsCfg != nil)

	go s.serve()
	return nil
}

// buildTLSConfig returns the TLS config for listening, or nil for plain TCP.
//
// A config from [config.ServerConfig.WithTLSConfig] wins. Otherwise PEM files
// from [config.ServerConfig.WithTLSFiles] are loaded. Missing credentials yield
// nil without error. In all TLS cases [minTLSVersion] (TLS 1.3) is enforced.
func (s *Server) buildTLSConfig() (*tls.Config, error) {
	if s.tlsConfig != nil {
		return enforceMinTLSVersion(s.tlsConfig.Clone()), nil
	}
	if s.tlsCertFile == "" || s.tlsKeyFile == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(s.tlsCertFile, s.tlsKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS credentials: %w", err)
	}

	return enforceMinTLSVersion(&tls.Config{
		Certificates: []tls.Certificate{cert},
	}), nil
}

// enforceMinTLSVersion raises cfg.MinVersion to at least [minTLSVersion].
func enforceMinTLSVersion(cfg *tls.Config) *tls.Config {
	if cfg.MinVersion < minTLSVersion {
		cfg.MinVersion = minTLSVersion
	}
	return cfg
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
