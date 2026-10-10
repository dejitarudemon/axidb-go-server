![Ignicula](./docs/ignicula-badge.svg)

[![CI](https://github.com/dejitarudemon/ignicula-framework/actions/workflows/ci.yml/badge.svg)](https://github.com/dejitarudemon/ignicula-framework/actions/workflows/ci.yml)
![coverage](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2Fdejitarudemon%2Fb71fd6149421abe8196d4e6b1e88a93f%2Fraw%2Fcoverage.json)
[![Go Reference](https://pkg.go.dev/badge/github.com/dejitarudemon/ignicula-framework.svg)](https://pkg.go.dev/github.com/dejitarudemon/ignicula-framework)
[![Release](https://img.shields.io/github/v/tag/dejitarudemon/ignicula-framework?label=release)](https://github.com/dejitarudemon/ignicula-framework/releases)
[![License: MIT](https://img.shields.io/github/license/dejitarudemon/ignicula-framework)](LICENSE)

# ignicula-framework

A Go framework for Ignicula servers. Bring your storage. Ship a binary protocol API.

[English](#english) · [Русский](#русский)

---

## English

**ignicula-framework** is a framework on top of [ignicula-wire](https://github.com/dejitarudemon/ignicula-wire): connections, version negotiation, auth, then Read / Write / Delete / Ping / Batch - with your handlers in the middle.

You own the data model and access rules. The framework owns the transport: sessions, keep-alive, answers on the wire, optional TLS. Register handlers and run.

Built for typed values, multiplexing by request id, compression when you need it, and a clear path from Hello to your first successful Read.

### Docs

- [How it works](docs/how-it-works.en.md)
- [Tutorials](docs/tutorial.en.md) - start here for a minimal server and config
- [Benchmarks](docs/benchmarks.en.md)

Wire format and client-side framing: [ignicula-wire](https://github.com/dejitarudemon/ignicula-wire).

### Install

Go 1.27.1 or newer.

```bash
go get github.com/dejitarudemon/ignicula-framework@latest
```

### Get started

Step-by-step setup, handlers, TLS, and compression: [tutorials](docs/tutorial.en.md).

### Tests and benchmarks

```bash
go test ./...
go test -bench=. -benchmem ./internal/server/ ./internal/runtime/v1/
```

Load scenarios and reference numbers: [benchmarks](docs/benchmarks.en.md).

---

## Русский

**ignicula-framework** - фреймворк поверх [ignicula-wire](https://github.com/dejitarudemon/ignicula-wire): соединения, согласование версии, auth, затем Read / Write / Delete / Ping / Batch - а посередине ваши хендлеры.

Данные и правила доступа - ваши. Транспорт - у фреймворка: сессии, keep-alive, ответы на проводе, при необходимости TLS. Зарегистрировали хендлеры - запускаете.

Рассчитан на типизированные значения, мультиплексирование по request id, сжатие по желанию и понятный путь от Hello до первого успешного Read.

### Документация

- [Как это работает](docs/how-it-works.md)
- [Туториалы](docs/tutorial.md) - отсюда минимальный сервер и конфиг
- [Бенчмарки](docs/benchmarks.md)

Формат кадров и клиентский framing: [ignicula-wire](https://github.com/dejitarudemon/ignicula-wire).

### Установка

Нужен Go 1.27.1 или новее.

```bash
go get github.com/dejitarudemon/ignicula-framework@latest
```

### С чего начать

Пошаговый запуск, хендлеры, TLS и сжатие: [туториалы](docs/tutorial.md).

### Тесты и бенчмарки

```bash
go test ./...
go test -bench=. -benchmem ./internal/server/ ./internal/runtime/v1/
```

Нагрузка и референсные цифры: [бенчмарки](docs/benchmarks.md).
