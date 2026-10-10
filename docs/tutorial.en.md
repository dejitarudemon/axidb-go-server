Language: [Русский](tutorial.md) · [English](tutorial.en.md)

# Tutorials

Public server API: build a runtime, attach handlers, start listening. Internals are in [how-it-works](how-it-works.en.md). Wire frames are in the [protocol tutorials](https://github.com/dejitarudemon/axidb-go-protocol/blob/main/docs/tutorial.en.md).

Packages live under `internal/`: examples are for code inside this module (`cmd/`, tests).

---

## 1. Minimal server

```go
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"

	"github.com/dejitarudemon/axidb-go-protocol/v1/fields"
	"github.com/dejitarudemon/axidb-go-protocol/v1/value"
	"github.com/dejitarudemon/axidb-go-server/internal/logger/implemented"
	runtime_v1 "github.com/dejitarudemon/axidb-go-server/internal/runtime/v1"
	rconfig "github.com/dejitarudemon/axidb-go-server/internal/runtime/v1/config"
	"github.com/dejitarudemon/axidb-go-server/internal/server"
	sconfig "github.com/dejitarudemon/axidb-go-server/internal/server/config"
)

type store struct {
	mu   sync.RWMutex
	data map[string]value.V
}

func main() {
	kv := &store{data: map[string]value.V{}}
	log := implemented.New(slog.Default())

	rt := runtime_v1.NewRuntimeBuilder(*rconfig.NewRuntimeBuilderConfig().
		WithBodyLimit(1 << 20)).
		WithLogger(log).
		WithHandlerAuth(func(_ runtime_v1.Context, _ string, _ [32]byte) (bool, error) {
			return true, nil // demo: allow everyone
		}).
		WithHandlerRead(func(_ runtime_v1.Context, key fields.Key) (value.V, error) {
			kv.mu.RLock()
			defer kv.mu.RUnlock()
			v, ok := kv.data[string(key)]
			if !ok {
				return nil, nil // NotFound
			}
			return v, nil
		}).
		WithHandlerWrite(func(_ runtime_v1.Context, key fields.Key, v value.V) error {
			kv.mu.Lock()
			defer kv.mu.Unlock()
			kv.data[string(key)] = v
			return nil
		}).
		WithHandlerDelete(func(_ runtime_v1.Context, key fields.Key) error {
			kv.mu.Lock()
			defer kv.mu.Unlock()
			delete(kv.data, string(key))
			return nil
		}).
		Build()

	s := server.NewServer(sconfig.NewServerConfig(), log)
	if err := s.RegisterV1Runtime(&rt); err != nil {
		log.Error("register", "error", err)
		os.Exit(1)
	}
	if err := s.Start("127.0.0.1", 4747); err != nil {
		log.Error("start", "error", err)
		os.Exit(1)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	_ = s.Close()
}
```

Order: `Build` → `RegisterV1Runtime(&rt)` **before** `Start` → `Close`. Without TLS, `Start` warns (log or stdout). Ping and Batch are not wired with handlers - the runtime owns them.

Client: Hello v0 → Handshake v1 → Read/Write/… (frame building is in the protocol tutorial).

---

## 2. Runtime config

```go
cfg := rconfig.NewRuntimeBuilderConfig().
	WithBodyLimit(1 << 20).
	WithMaxGoroutinesPerBatch(8).
	WithCompressors(s2, zstd). // NewS2 / NewZstd → (Compressor, error)
	WithStartUseCompressionAt(5 << 10).
	WithAllowedVersions(fields.Version(1))

rt := runtime_v1.NewRuntimeBuilder(*cfg).
	WithHandlerAuth(...).
	WithHandlerRead(...).
	WithHandlerWrite(...).
	WithHandlerDelete(...).
	WithLogger(log).
	Build()
```

| Method | Why you care |
| --- | --- |
| `WithBodyLimit` | Max frame body size. Default 10 KiB. |
| `WithMaxGoroutinesPerBatch` | Parallelism inside a parallel batch. Default 4. |
| `WithCompressors` | Algorithms the server supports. Omit for uncompressed only. The runtime picks the codec per answer. |
| `WithStartUseCompressionAt` | Body size from which answers may be compressed. Default 5 KiB. |
| `WithAllowedVersions` | Versions after Activate (default already `[1]`). |
| `WithHandler*` | Auth / Read / Write / Delete. Unset stubs return `CommandNotImplemented`. |
| `WithLogger` | Runtime log. Optional. |

`Build()` returns a `Runtime` value; pass `&rt` into the server.

---

## 3. Handlers

First argument is `runtime_v1.Context`: `Login()`, `RequestID()`, embedded `context.Context`.

| Handler | Returns |
| --- | --- |
| Auth `(bool, error)` | `(true, nil)` accept; `(false, nil)` Unauthorized; `err != nil` error-answer |
| Read `(value.V, error)` | `(v, nil)` value; `(nil, nil)` NotFound; `(nil, err)` error |
| Write / Delete `error` | `nil` success; else error-answer |

Auth with a hash check:

```go
WithHandlerAuth(func(_ runtime_v1.Context, login string, hash [32]byte) (bool, error) {
	user, err := users.Lookup(login)
	if err != nil {
		return false, nil
	}
	return subtle.ConstantTimeCompare(hash[:], user.Hash[:]) == 1, nil
})
```

Handshake carries login and a 32-byte Argon2id hash; the server does not compute the hash itself.

---

## 4. Server config and TLS

```go
cfg := sconfig.NewServerConfig().
	WithNetwork("tcp").
	WithBufferSize(64).
	WithReadTimeout(30 * time.Second).
	WithPingInterval(30 * time.Second).
	WithPingTimeout(100 * time.Second).
	WithTLSFiles("/etc/axidb/cert.pem", "/etc/axidb/key.pem")
	// or WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}})
```

| Method | Why you care |
| --- | --- |
| `WithNetwork` | Listen network. Default `tcp4`. |
| `WithBufferSize` | Per-connection queue of encoded answers. |
| `WithReadTimeout` | Read deadline (and Hello timeout before registration). Default 30 s. |
| `WithPingInterval` / `WithPingTimeout` | Idle Ping and how long to wait for activity after it. Defaults 30 s / 100 s. |
| `WithTLSFiles` / `WithTLSConfig` | TLS. Required in production: the protocol has no encryption of its own. Listener floor is TLS 1.3. |

Empty paths / `WithTLSConfig(nil)` / non-positive durations clear or ignore the option. `WithTLSConfig` wins over files when both are set.

---

## 5. Common mistakes

| Symptom | Check |
| --- | --- |
| CommandNotImplemented on Handshake | Missing `WithHandlerAuth` |
| Silence after Hello | `RegisterV1Runtime` before `Start` |
| Request Conflict | Unique non-zero `RequestID`s for live requests |
| Warn without TLS | No `WithTLS*` - expected |

Benchmarks: [benchmarks.en.md](benchmarks.en.md). Client patterns in tests: `internal/server/helpers_test.go`, `v1_flow_test.go`.
