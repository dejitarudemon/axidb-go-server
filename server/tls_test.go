package server

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dejitarudemon/ignicula-wire/v1/body/bodies"
	v1fields "github.com/dejitarudemon/ignicula-wire/v1/fields"
)

func TestStartWithoutTLSWarnsLogger(t *testing.T) {
	log := &memLogger{}
	s := NewServer(testConfig(t), log)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}
	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	found := false
	for _, rec := range log.warn {
		if rec.msg == unprotectedTLSMessage {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warn logs = %#v, want %q", log.warn, unprotectedTLSMessage)
	}
}

func TestStartWithoutTLSWarnsStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() = %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = old
		_ = r.Close()
	})

	s := NewServer(testConfig(t), nil)
	if err := s.RegisterV1Runtime(testRuntime(t)); err != nil {
		t.Fatalf("RegisterV1Runtime() = %v", err)
	}
	if err := s.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom() = %v", err)
	}
	if !strings.Contains(buf.String(), unprotectedTLSMessage) {
		t.Fatalf("stdout = %q, want %q", buf.String(), unprotectedTLSMessage)
	}
}

func TestStartWithTLSAcceptsTLSClient(t *testing.T) {
	tlsCfg, roots := testServerTLS(t)
	s := startTestServer(t, testConfig(t).WithTLSConfig(tlsCfg), testRuntime(t))

	conn := dialTLSServer(t, s, roots)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodeClientPing(t, 1))
	got := readV1Frame(t, conn, time.Second)
	if _, ok := got.Body.(bodies.PingAnswer); !ok {
		t.Fatalf("body = %T, want PingAnswer", got.Body)
	}
	if got.RequestID != 1 {
		t.Fatalf("request id = %d, want 1", got.RequestID)
	}
}

func TestStartWithTLSFiles(t *testing.T) {
	certPath, keyPath, roots := writeTestTLSFiles(t)
	s := startTestServer(t, testConfig(t).WithTLSFiles(certPath, keyPath), testRuntime(t))

	conn := dialTLSServer(t, s, roots)
	registerAndActivate(t, conn)

	writeAll(t, conn, encodeRead(t, v1fields.RequestID(3), "k"))
	got := readV1Frame(t, conn, time.Second)
	if got.RequestID != 3 {
		t.Fatalf("request id = %d, want 3", got.RequestID)
	}
}

func TestStartWithBadTLSFiles(t *testing.T) {
	cfg := testConfig(t).WithTLSFiles("/no/such/cert.pem", "/no/such/key.pem")
	s := NewServer(cfg, nil)
	err := s.Start("127.0.0.1", 0)
	if err == nil {
		_ = s.Close()
		t.Fatal("Start() = nil, want TLS load error")
	}
	if s.started.Load() {
		t.Fatal("started = true after failed Start")
	}
}

func dialTLSServer(t testing.TB, s *Server, roots *x509.CertPool) *testConn {
	t.Helper()

	raw, err := tls.Dial("tcp", s.listener.Addr().String(), &tls.Config{
		RootCAs:    roots,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS13,
	})
	if err != nil {
		t.Fatalf("tls.Dial() = %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return &testConn{Conn: raw, r: bufio.NewReader(raw)}
}

func testServerTLS(t testing.TB) (*tls.Config, *x509.CertPool) {
	t.Helper()

	certPEM, keyPEM := mustGenerateTestCert(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() = %v", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM failed")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}, roots
}

func TestBuildTLSConfigEnforcesTLS13(t *testing.T) {
	s := NewServer(testConfig(t).WithTLSConfig(&tls.Config{
		MinVersion: tls.VersionTLS12,
	}), nil)

	cfg, err := s.buildTLSConfig()
	if err != nil {
		t.Fatalf("buildTLSConfig() = %v", err)
	}
	if cfg == nil {
		t.Fatal("buildTLSConfig() = nil, want config")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("MinVersion = %#x, want TLS 1.3", cfg.MinVersion)
	}
}

func writeTestTLSFiles(t testing.TB) (certPath, keyPath string, roots *x509.CertPool) {
	t.Helper()

	certPEM, keyPEM := mustGenerateTestCert(t)
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(cert) = %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(key) = %v", err)
	}

	roots = x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	return certPath, keyPath, roots
}

func mustGenerateTestCert(t testing.TB) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() = %v", err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("serial = %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() = %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() = %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}
