[![CI](https://github.com/dejitarudemon/axidb-go-server/actions/workflows/ci.yml/badge.svg)](https://github.com/dejitarudemon/axidb-go-server/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/github/license/dejitarudemon/axidb-go-server)](LICENSE)

# axidb-go-server

TCP server for the AxiDB protocol: Hello (v0), Handshake, and working version 1 with your Auth / Read / Write / Delete handlers.

[English](#english) · [Русский](#русский)

---

## English

A Go server on top of [axidb-go-protocol](https://github.com/dejitarudemon/axidb-go-protocol). It owns the socket, session table, idle Ping, and answer writer. Storage and access policy stay in handlers you register on the v1 runtime.

Packages currently live under `internal/` (import from code inside this module).

### Docs

- [How it works](docs/how-it-works.en.md)
- [Tutorials](docs/tutorial.en.md)
- [Benchmarks](docs/benchmarks.en.md)

### Requirements

Go 1.27.1 or newer.

### Quick start

```go
rt := runtime_v1.NewRuntimeBuilder(*rconfig.NewRuntimeBuilderConfig().WithBodyLimit(1<<20)).
    WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) { return true, nil }).
    WithHandlerRead(/* ... */).
    Build()

s := server.NewServer(sconfig.NewServerConfig(), nil)
_ = s.RegisterV1Runtime(&rt)
_ = s.Start("127.0.0.1", 4747)
defer s.Close()
```

Full walkthrough: [tutorials](docs/tutorial.en.md).

### Test / bench

```bash
export GOMODCACHE="${GOMODCACHE:-$(go env GOPATH)/pkg/mod}"
go test -race ./...
go test -bench=. -benchmem ./internal/server/ ./internal/runtime/v1/
```

---

## Русский

Go-сервер поверх [axidb-go-protocol](https://github.com/dejitarudemon/axidb-go-protocol). Держит сокет, таблицу сессий, idle Ping и writer ответов. Хранилище и доступ живут в хендлерах, которые вы вешаете на runtime v1.

Пакеты пока в `internal/` (импорт из кода внутри этого модуля).

### Документация

- [Как это работает](docs/how-it-works.md)
- [Туториалы](docs/tutorial.md)
- [Бенчмарки](docs/benchmarks.md)

### Требования

Go 1.27.1 или новее.

### Быстрый старт

```go
rt := runtime_v1.NewRuntimeBuilder(*rconfig.NewRuntimeBuilderConfig().WithBodyLimit(1<<20)).
    WithHandlerAuth(func(runtime_v1.Context, string, [32]byte) (bool, error) { return true, nil }).
    WithHandlerRead(/* ... */).
    Build()

s := server.NewServer(sconfig.NewServerConfig(), nil)
_ = s.RegisterV1Runtime(&rt)
_ = s.Start("127.0.0.1", 4747)
defer s.Close()
```

Подробно: [туториалы](docs/tutorial.md).

### Тесты / бенчи

```bash
export GOMODCACHE="${GOMODCACHE:-$(go env GOPATH)/pkg/mod}"
go test -race ./...
go test -bench=. -benchmem ./internal/server/ ./internal/runtime/v1/
```
