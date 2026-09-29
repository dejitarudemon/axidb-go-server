package runtime_v1

import (
	"bufio"
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/dejitarudemon/axidb-go-protocol/v1/body/bodies"
	"github.com/dejitarudemon/axidb-go-protocol/v1/buffer"
	"github.com/dejitarudemon/axidb-go-protocol/v1/compressor"
	"github.com/dejitarudemon/axidb-go-protocol/v1/err/errs"
	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/frame"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
)

func TestNewRuntimerBuilderDefaults(t *testing.T) {
	rt := NewRuntimerBuilder(*NewRuntimeBuilderConfig()).Build()

	if !slices.Equal(rt.allowedVersions, []fields.Version{1}) {
		t.Errorf("versions = %v, want [1]", rt.allowedVersions)
	}

	if len(rt.allowedCompressions) != 0 {
		t.Errorf("compressions = %v, want empty", rt.allowedCompressions)
	}

	if rt.limit != int(defaultBodyLimit) {
		t.Errorf("limit = %d, want %d", rt.limit, defaultBodyLimit)
	}

	assertDefaultHandlers(t, rt)
	assertDecodesPing(t, rt)
}

func TestNewRuntimerBuilderZeroConfig(t *testing.T) {
	rt := NewRuntimerBuilder(RuntimeBuilderConfig{}).Build()

	if len(rt.allowedVersions) != 0 {
		t.Errorf("versions = %v, want empty", rt.allowedVersions)
	}

	if len(rt.allowedCompressions) != 0 {
		t.Errorf("compressions = %v, want empty", rt.allowedCompressions)
	}

	if rt.limit != 0 {
		t.Errorf("limit = %d, want 0", rt.limit)
	}

	assertDefaultHandlers(t, rt)

	raw := mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Read("x")}, nil)
	_, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))

	if _, ok := errors.AsType[errs.ErrorBodyLimitIsExceeded](err); !ok {
		t.Fatalf("error = %v, want ErrorBodyLimitIsExceeded", err)
	}
}

func TestRuntimeBuilderHandlers(t *testing.T) {
	sentinel := errors.New("sentinel")
	read := func(Context, fields.Key) (value.V, error) { return nil, sentinel }
	write := func(Context, fields.Key, value.V) error { return sentinel }
	del := func(Context, fields.Key) error { return sentinel }
	auth := func(Context, string, [32]byte) (bool, error) { return false, nil }

	rb := NewRuntimerBuilder(*NewRuntimeBuilderConfig())
	if rb.WithHandlerRead(nil).WithHandlerWrite(nil).WithHandlerDelete(nil).WithHandlerAuth(nil) != rb {
		t.Fatal("WithHandler returned a different builder")
	}

	assertDefaultHandlers(t, rb.Build())

	rb = NewRuntimerBuilder(*NewRuntimeBuilderConfig())
	rb.WithHandlerRead(read).WithHandlerWrite(write).WithHandlerDelete(del).WithHandlerAuth(auth)
	rt := rb.Build()

	if _, err := rt.handlerRead(Context{}, fields.Key("k")); !errors.Is(err, sentinel) {
		t.Errorf("read error = %v, want sentinel", err)
	}

	if err := rt.handlerWrite(Context{}, fields.Key("k"), nil); !errors.Is(err, sentinel) {
		t.Errorf("write error = %v, want sentinel", err)
	}

	if err := rt.handlerDelete(Context{}, fields.Key("k")); !errors.Is(err, sentinel) {
		t.Errorf("delete error = %v, want sentinel", err)
	}

	ok, err := rt.handlerAuth(Context{}, "user", [32]byte{})
	if err != nil {
		t.Fatalf("auth error = %v, want nil", err)
	}

	if ok {
		t.Fatal("authenticated = true, want false")
	}
}

func TestRuntimeBuilderNilHandlerKeepsCustom(t *testing.T) {
	sentinel := errors.New("sentinel")
	read := func(Context, fields.Key) (value.V, error) { return nil, sentinel }

	rb := NewRuntimerBuilder(*NewRuntimeBuilderConfig()).WithHandlerRead(read).WithHandlerRead(nil)
	rt := rb.Build()

	if _, err := rt.handlerRead(Context{}, nil); !errors.Is(err, sentinel) {
		t.Errorf("read error = %v, want sentinel", err)
	}
}

func TestRuntimeBuilderBuiltHandlersStay(t *testing.T) {
	rb := NewRuntimerBuilder(*NewRuntimeBuilderConfig())
	rt := rb.Build()

	rb.WithHandlerAuth(func(Context, string, [32]byte) (bool, error) {
		return false, nil
	})

	ok, err := rt.handlerAuth(Context{}, "user", [32]byte{1})
	if err != nil {
		t.Fatalf("auth error = %v, want nil", err)
	}

	if !ok {
		t.Fatal("authenticated = false, want true")
	}
}

func TestRuntimeBuilderVersionsAndCompressors(t *testing.T) {
	zstd := stubCompressor{code: fields.Zstd, id: 1}
	again := stubCompressor{code: fields.Zstd, id: 2}
	s2 := stubCompressor{code: fields.S2, id: 3}

	cfg := NewRuntimeBuilderConfig().
		WithAllowedVersions(fields.Version(2), fields.Version(2)).
		WithCompressors(zstd, again, nil, s2)

	rt := NewRuntimerBuilder(*cfg).Build()

	wantVersions := []fields.Version{1, 2}
	if !slices.Equal(rt.allowedVersions, wantVersions) {
		t.Errorf("versions = %v, want %v", rt.allowedVersions, wantVersions)
	}

	if got := rt.allowedCompressions[fields.Zstd]; got != zstd {
		t.Errorf("zstd = %#v, want %#v", got, zstd)
	}

	if got := rt.allowedCompressions[fields.S2]; got != s2 {
		t.Errorf("s2 = %#v, want %#v", got, s2)
	}

	if len(rt.allowedCompressions) != 2 {
		t.Errorf("compressions = %d, want 2", len(rt.allowedCompressions))
	}
}

func TestRuntimeBuilderBodyLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   fields.BodyLimit
		key     string
		wantErr bool
	}{
		{"zero limit empty ping", 0, "", false},
		{"zero limit one byte", 0, "x", true},
		{"limit equals body", 2, "ab", false},
		{"limit one below body", 1, "ab", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body frame.Frame
			if tt.key == "" {
				body = frame.Frame{RequestID: 1, Body: bodies.Ping{}}
			} else {
				body = frame.Frame{RequestID: 1, Body: bodies.Read(tt.key)}
			}

			cfg := NewRuntimeBuilderConfig().WithBodyLimit(tt.limit)
			rt := NewRuntimerBuilder(*cfg).Build()
			if rt.limit != int(tt.limit) {
				t.Fatalf("limit = %d, want %d", rt.limit, tt.limit)
			}

			_, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(mustEncodeFrame(t, body, nil))))

			_, got := errors.AsType[errs.ErrorBodyLimitIsExceeded](err)
			if got != tt.wantErr {
				t.Fatalf("body limit exceeded = %v, error = %v, want exceeded %v", got, err, tt.wantErr)
			}
		})
	}
}

func TestRuntimeBuilderDecoderUsesCompressor(t *testing.T) {
	zstd := stubCompressor{code: fields.Zstd, id: 1}
	cfg := NewRuntimeBuilderConfig().WithCompressors(zstd)
	rt := NewRuntimerBuilder(*cfg).Build()

	raw := mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}}, zstd)
	got, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("decode = %v", err)
	}

	if got.Body == nil || got.Body.Command() != fields.Ping {
		t.Fatalf("body = %#v, want ping", got.Body)
	}
}

func TestRuntimeBuilderDecoderRejectsUnknownCompression(t *testing.T) {
	rt := NewRuntimerBuilder(*NewRuntimeBuilderConfig()).Build()
	raw := mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}}, stubCompressor{code: fields.S2})

	_, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))

	if _, ok := errors.AsType[errs.ErrorUnsupportedCompression](err); !ok {
		t.Fatalf("error = %v, want ErrorUnsupportedCompression", err)
	}
}

func TestRuntimeBuilderNilPanics(t *testing.T) {
	var rb *RuntimeBuilder

	t.Run("nil handler does not panic", func(t *testing.T) {
		if got := rb.WithHandlerRead(nil); got != nil {
			t.Fatalf("builder = %v, want nil", got)
		}
	})

	t.Run("handler", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()

		rb.WithHandlerRead(func(Context, fields.Key) (value.V, error) { return nil, nil })
	})

	t.Run("build", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()

		rb.Build()
	})
}

func TestCompressorsToRuntimeMap(t *testing.T) {
	first := stubCompressor{code: fields.Zstd, id: 1}
	second := stubCompressor{code: fields.Zstd, id: 2}
	other := stubCompressor{code: fields.S2, id: 3}

	tests := []struct {
		name string
		in   []compressor.Compressor
		want map[fields.Compression]stubCompressor
	}{
		{"nil", nil, map[fields.Compression]stubCompressor{}},
		{"empty", []compressor.Compressor{}, map[fields.Compression]stubCompressor{}},
		{"duplicate code keeps last", []compressor.Compressor{first, second, other}, map[fields.Compression]stubCompressor{
			fields.Zstd: second,
			fields.S2:   other,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compressorsToRuntimeMap(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}

			for code, want := range tt.want {
				if got[code] != want {
					t.Errorf("code %v = %#v, want %#v", code, got[code], want)
				}
			}
		})
	}
}

func assertDefaultHandlers(t *testing.T, rt Runtime) {
	t.Helper()

	got, err := rt.handlerRead(Context{}, fields.Key("k"))
	if err != nil || got != nil {
		t.Fatalf("read = (%v, %v), want (nil, nil)", got, err)
	}

	if err = rt.handlerWrite(Context{}, fields.Key("k"), nil); err != nil {
		t.Fatalf("write error = %v, want nil", err)
	}

	if err = rt.handlerDelete(Context{}, fields.Key("k")); err != nil {
		t.Fatalf("delete error = %v, want nil", err)
	}

	ok, err := rt.handlerAuth(Context{}, "user", [32]byte{})
	if err != nil || !ok {
		t.Fatalf("auth = (%v, %v), want (true, nil)", ok, err)
	}
}

func assertDecodesPing(t *testing.T, rt Runtime) {
	t.Helper()

	raw := mustEncodeFrame(t, frame.Frame{RequestID: 1, Body: bodies.Ping{}}, nil)
	got, err := rt.decoder.DecodeFrame(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("decode = %v", err)
	}

	if got.Body == nil || got.Body.Command() != fields.Ping {
		t.Fatalf("body = %#v, want ping", got.Body)
	}
}

func mustEncodeFrame(t *testing.T, f frame.Frame, c compressor.Compressor) []byte {
	t.Helper()

	var buf buffer.Slice
	if err := f.Encode(&buf, c); err != nil {
		t.Fatalf("encode: %v", err)
	}

	return buf.Bytes()
}
