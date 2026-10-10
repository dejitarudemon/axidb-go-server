package runtime_v1

import (
	"strings"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value/values"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/row"
)

func TestSelectCompression(t *testing.T) {
	zstd := stubCompressor{code: fields.Zstd, id: 1}
	s2 := stubCompressor{code: fields.S2, id: 2}

	const threshold = 100

	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(useHardestCompressionIfBodySizeAtLeast * 2).
		WithStartUseCompressionAt(threshold).
		WithCompressors(zstd, s2)).
		Build()

	both := row.NewRequestRow("u", []fields.Compression{fields.Zstd, fields.S2})
	onlyS2 := row.NewRequestRow("u", []fields.Compression{fields.S2})
	onlyZstd := row.NewRequestRow("u", []fields.Compression{fields.Zstd})
	none := row.NewRequestRow("u", nil)

	smallRead := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", 20))}
	mediumRead := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", 200))}
	belowZstdFloor := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", useHardestCompressionIfBodySizeAtLeast/2))}
	zstdReady := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", useHardestCompressionIfBodySizeAtLeast+100))}

	if smallRead.Size() >= threshold {
		t.Fatalf("small body size = %d, want < %d", smallRead.Size(), threshold)
	}
	if mediumRead.Size() < threshold {
		t.Fatalf("medium body size = %d, want >= %d", mediumRead.Size(), threshold)
	}
	if belowZstdFloor.Size() > useHardestCompressionIfBodySizeAtLeast {
		t.Fatalf("belowZstdFloor size = %d, want <= %d", belowZstdFloor.Size(), useHardestCompressionIfBodySizeAtLeast)
	}
	if zstdReady.Size() <= useHardestCompressionIfBodySizeAtLeast {
		t.Fatalf("zstdReady size = %d, want > %d", zstdReady.Size(), useHardestCompressionIfBodySizeAtLeast)
	}

	tests := []struct {
		name string
		row  *row.RequestRow
		body any
		want compressor.Compressor
		any  bool // non-nil, either registered compressor
	}{
		{"nil row", nil, mediumRead, nil, false},
		{"nil body", both, nil, nil, false},
		{"below threshold", both, smallRead, nil, false},
		{"write answer", both, bodies.WriteAnswer{}, nil, false},
		{"delete answer", both, bodies.DeleteAnswer{}, nil, false},
		{"ping answer", both, bodies.PingAnswer{}, nil, false},
		{"handshake answer", both, bodies.HandshakeAnswer{}, nil, false},
		{"ping request", both, bodies.Ping{}, nil, false},
		{"no client compressions", none, mediumRead, nil, false},
		{"medium any shared", both, mediumRead, nil, true},
		{"below zstd floor any shared", both, belowZstdFloor, nil, true},
		{"zstd when large enough", both, zstdReady, zstd, false},
		{"large but client only s2", onlyS2, zstdReady, s2, false},
		{"medium client only s2", onlyS2, mediumRead, s2, false},
		{"medium client only zstd", onlyZstd, mediumRead, zstd, false},
		{"zstdReady client only zstd", onlyZstd, zstdReady, zstd, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectCompressionForTest(t, rt, tt.row, tt.body)
			if tt.any {
				if got != zstd && got != s2 {
					t.Fatalf("selectCompression() = %v, want zstd or s2", got)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("selectCompression() = %v, want %v", got, tt.want)
			}
		})
	}
}

func selectCompressionForTest(t *testing.T, rt Runtime, reqRow *row.RequestRow, body any) compressor.Compressor {
	t.Helper()
	switch b := body.(type) {
	case nil:
		return rt.selectCompression(reqRow, nil)
	case bodies.ReadAnswer:
		return rt.selectCompression(reqRow, b)
	case bodies.WriteAnswer:
		return rt.selectCompression(reqRow, b)
	case bodies.DeleteAnswer:
		return rt.selectCompression(reqRow, b)
	case bodies.PingAnswer:
		return rt.selectCompression(reqRow, b)
	case bodies.HandshakeAnswer:
		return rt.selectCompression(reqRow, b)
	case bodies.Ping:
		return rt.selectCompression(reqRow, b)
	default:
		t.Fatalf("unsupported body type %T", body)
		return nil
	}
}

func TestSelectCompressionDisabledWhenThresholdAboveLimit(t *testing.T) {
	zstd := stubCompressor{code: fields.Zstd, id: 1}
	s2 := stubCompressor{code: fields.S2, id: 2}

	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(100).
		WithStartUseCompressionAt(200).
		WithCompressors(zstd, s2)).
		Build()

	reqRow := row.NewRequestRow("u", []fields.Compression{fields.Zstd, fields.S2})
	body := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", 90))}
	if got := rt.selectCompression(reqRow, body); got != nil {
		t.Fatalf("got %v, want nil when useCompressionAt > limit", got)
	}
}

func TestSelectCompressionNoRegisteredCompressors(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().
		WithBodyLimit(1000).
		WithStartUseCompressionAt(10)).
		Build()

	reqRow := row.NewRequestRow("u", []fields.Compression{fields.Zstd, fields.S2})
	body := bodies.ReadAnswer{Value: values.String(strings.Repeat("a", 50))}
	if got := rt.selectCompression(reqRow, body); got != nil {
		t.Fatalf("got %v, want nil with empty compressor set", got)
	}
}

func TestNewRuntimeBuilderCopiesCompressionThreshold(t *testing.T) {
	rt := NewRuntimeBuilder(*config.NewRuntimeBuilderConfig().WithStartUseCompressionAt(42)).Build()
	if rt.useCompressionAt != 42 {
		t.Fatalf("useCompressionAt = %d, want 42", rt.useCompressionAt)
	}
}
