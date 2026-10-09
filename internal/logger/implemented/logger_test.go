package implemented

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerForwardsLevels(t *testing.T) {
	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := New(slogger)

	log.Debug("d", "k", 1)
	log.Info("i", "k", 2)
	log.Warn("w", "k", 3)
	log.Error("e", "k", 4)

	out := buf.String()
	for _, want := range []string{
		`level=DEBUG msg=d k=1`,
		`level=INFO msg=i k=2`,
		`level=WARN msg=w k=3`,
		`level=ERROR msg=e k=4`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q\n%s", want, out)
		}
	}
}

func TestNewNilUsesDefault(t *testing.T) {
	if New(nil) == nil {
		t.Fatal("New(nil) = nil")
	}
}
