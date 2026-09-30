package config

import (
	"slices"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
)

type stubCompressor struct {
	code fields.Compression
	id   int
}

func (s stubCompressor) Code() fields.Compression { return s.code }

func (s stubCompressor) Compress(buf []byte) ([]byte, error) {
	return append([]byte(nil), buf...), nil
}

func (s stubCompressor) Decompress(buf []byte) ([]byte, error) {
	return append([]byte(nil), buf...), nil
}

func TestNewRuntimeBuilderConfigDefaults(t *testing.T) {
	cfg := NewRuntimeBuilderConfig()

	if cfg.limits.body != defaultBodyLimit {
		t.Errorf("body = %d, want %d", cfg.limits.body, defaultBodyLimit)
	}

	if cfg.limits.batch != defaultBatchLimit {
		t.Errorf("batch = %d, want %d", cfg.limits.batch, defaultBatchLimit)
	}

	if cfg.limits.goroutines != defaultGoroutinesPerParrallelBatch {
		t.Errorf("goroutines = %d, want %d", cfg.limits.goroutines, defaultGoroutinesPerParrallelBatch)
	}

	if !slices.Equal(cfg.versions, []fields.Version{1}) {
		t.Errorf("versions = %v, want [1]", cfg.versions)
	}

	if cfg.compressors == nil || len(cfg.compressors) != 0 {
		t.Errorf("compressors = %v, want empty non-nil slice", cfg.compressors)
	}
}

func TestRuntimeBuilderConfigDefaultsAreIndependent(t *testing.T) {
	first := NewRuntimeBuilderConfig()
	first.WithAllowedVersions(fields.Version(2))
	first.versions[0] = 9

	second := NewRuntimeBuilderConfig()
	if !slices.Equal(second.versions, []fields.Version{1}) {
		t.Errorf("versions = %v, want [1]", second.versions)
	}

	if !slices.Equal(defaultVersions, []fields.Version{1}) {
		t.Errorf("defaultVersions = %v, want [1]", defaultVersions)
	}
}

func TestRuntimeBuilderConfigLimits(t *testing.T) {
	tests := []struct {
		name  string
		body  fields.BodyLimit
		batch fields.BatchLimit
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"defaults", defaultBodyLimit, defaultBatchLimit},
		{"max", fields.BodyLimit(^uint32(0)), fields.BatchLimit(^uint32(0))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRuntimeBuilderConfig().WithBodyLimit(tt.body).WithBatchLimit(tt.batch)

			if cfg.limits.body != tt.body {
				t.Errorf("body = %d, want %d", cfg.limits.body, tt.body)
			}

			if cfg.limits.batch != tt.batch {
				t.Errorf("batch = %d, want %d", cfg.limits.batch, tt.batch)
			}
		})
	}
}

func TestRuntimeBuilderConfigGoroutines(t *testing.T) {
	tests := []struct {
		name string
		set  int
		want int
	}{
		{"zero becomes one", 0, 1},
		{"negative becomes one", -3, 1},
		{"one", 1, 1},
		{"above default", 8, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRuntimeBuilderConfig().WithMaxGoroutinesPerBatch(tt.set)
			if cfg.MaxGoroutinesPerBatch() != tt.want {
				t.Errorf("goroutines = %d, want %d", cfg.MaxGoroutinesPerBatch(), tt.want)
			}
		})
	}
}

func TestRuntimeBuilderConfigLimitOverwrite(t *testing.T) {
	cfg := NewRuntimeBuilderConfig().WithBodyLimit(1).WithBodyLimit(2).WithBatchLimit(3).WithBatchLimit(4)

	if cfg.limits.body != 2 {
		t.Errorf("body = %d, want 2", cfg.limits.body)
	}

	if cfg.limits.batch != 4 {
		t.Errorf("batch = %d, want 4", cfg.limits.batch)
	}
}

func TestRuntimeBuilderConfigWithCompressors(t *testing.T) {
	first := stubCompressor{code: fields.Zstd, id: 1}
	duplicate := stubCompressor{code: fields.Zstd, id: 2}
	second := stubCompressor{code: fields.S2, id: 3}

	cfg := NewRuntimeBuilderConfig()
	if cfg.WithCompressors(nil, first, nil, duplicate, second) != cfg {
		t.Fatal("WithCompressors returned a different config")
	}

	got := cfg.compressors
	want := []compressor.Compressor{first, second}
	if len(got) != len(want) {
		t.Fatalf("compressors = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("compressors[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestRuntimeBuilderConfigWithCompressorsEmpty(t *testing.T) {
	cfg := NewRuntimeBuilderConfig()
	cfg.WithCompressors()

	if len(cfg.compressors) != 0 {
		t.Errorf("compressors = %v, want empty", cfg.compressors)
	}
}

func TestRuntimeBuilderConfigWithAllowedVersions(t *testing.T) {
	cfg := NewRuntimeBuilderConfig()
	if cfg.WithAllowedVersions(2, 1, 2, 3, 0) != cfg {
		t.Fatal("WithAllowedVersions returned a different config")
	}

	want := []fields.Version{1, 2, 3, 0}
	if !slices.Equal(cfg.versions, want) {
		t.Errorf("versions = %v, want %v", cfg.versions, want)
	}
}

func TestRuntimeBuilderConfigWithAllowedVersionsEmpty(t *testing.T) {
	cfg := NewRuntimeBuilderConfig()
	cfg.WithAllowedVersions()

	if !slices.Equal(cfg.versions, []fields.Version{1}) {
		t.Errorf("versions = %v, want [1]", cfg.versions)
	}
}

func TestRuntimeBuilderConfigNilPanics(t *testing.T) {
	var cfg *RuntimeBuilderConfig

	tests := []struct {
		name string
		call func()
	}{
		{"body", func() { cfg.WithBodyLimit(1) }},
		{"batch", func() { cfg.WithBatchLimit(1) }},
		{"goroutines", func() { cfg.WithMaxGoroutinesPerBatch(1) }},
		{"compressors", func() { cfg.WithCompressors(stubCompressor{code: fields.Zstd}) }},
		{"versions", func() { cfg.WithAllowedVersions(1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()

			tt.call()
		})
	}
}
