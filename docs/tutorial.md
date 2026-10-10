Язык: [Русский](tutorial.md) · [English](tutorial.en.md)

# Туториалы

Публичный API сервера: собрать runtime, повесить хендлеры, запустить. Как устроено внутри - в [how-it-works](how-it-works.md). Кадры на проводе - в [туториалах протокола](https://github.com/dejitarudemon/ignicula-wire/blob/main/docs/tutorial.md).

Импорты идут от корня модуля (без префикса `internal/`).

---

## 1. Минимальный сервер

```go
package main

import (
	"log/slog"
	"os"
	"os/signal"
	"sync"

	"github.com/dejitarudemon/ignicula-wire/v1/fields"
	"github.com/dejitarudemon/ignicula-wire/v1/value"
	"github.com/dejitarudemon/ignicula-framework/logger/implemented"
	runtime_v1 "github.com/dejitarudemon/ignicula-framework/runtime/v1"
	rconfig "github.com/dejitarudemon/ignicula-framework/runtime/v1/config"
	"github.com/dejitarudemon/ignicula-framework/server"
	sconfig "github.com/dejitarudemon/ignicula-framework/server/config"
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
			return true, nil // демо: пускать всех
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

Порядок: `Build` → `RegisterV1Runtime(&rt)` **до** `Start` → `Close`. Без TLS `Start` предупредит (лог или stdout). Ping и Batch хендлерами не задаются - их делает runtime.

Клиент: Hello v0 → Handshake v1 → Read/Write/… 

---



## 2. Конфиг runtime

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


| Метод                       | Зачем вам                                                                                                  |
| --------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `WithBodyLimit`             | Макс. размер тела кадра. Дефолт 10 КиБ.                                                                    |
| `WithMaxGoroutinesPerBatch` | Параллелизм внутри parallel batch. Дефолт 4.                                                               |
| `WithCompressors`           | Какие алгоритмы сервер умеет. Без вызова - только uncompressed. Какой кодек взять на ответ решает runtime. |
| `WithStartUseCompressionAt` | С какого размера тела ответ могут сжать. Дефолт 5 КиБ.                                                     |
| `WithAllowedVersions`       | Версии после Activate (дефолт уже `[1]`).                                                                  |
| `WithHandler*`              | Auth / Read / Write / Delete. Без замены - stub `CommandNotImplemented`.                                   |
| `WithLogger`                | Лог runtime. Можно не вызывать.                                                                            |


`Build()` даёт значение `Runtime`; в сервер передаёте `&rt`.

---



## 3. Хендлеры

Первый аргумент - `runtime_v1.Context`: `Login()`, `RequestID()`, встроенный `context.Context`.


| Хендлер                 | Возврат                                                                       |
| ----------------------- | ----------------------------------------------------------------------------- |
| Auth `(bool, error)`    | `(true, nil)` пустить; `(false, nil)` Unauthorized; `err != nil` error-answer |
| Read `(value.V, error)` | `(v, nil)` ответ; `(nil, nil)` NotFound; `(nil, err)` ошибка                  |
| Write / Delete `error`  | `nil` успех; иначе error-answer                                               |


Пример Auth с проверкой хеша:

```go
WithHandlerAuth(func(_ runtime_v1.Context, login string, hash [32]byte) (bool, error) {
	user, err := users.Lookup(login)
	if err != nil {
		return false, nil
	}
	return subtle.ConstantTimeCompare(hash[:], user.Hash[:]) == 1, nil
})
```

Handshake даёт login и 32-байтовый Argon2id-хеш; сервер хеш сам не считает.

---



## 4. Конфиг сервера и TLS

```go
cfg := sconfig.NewServerConfig().
	WithNetwork("tcp").
	WithBufferSize(64).
	WithReadTimeout(30 * time.Second).
	WithPingInterval(30 * time.Second).
	WithPingTimeout(100 * time.Second).
	WithTLSFiles("/etc/ignicula/cert.pem", "/etc/ignicula/key.pem")
	// или WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}})
```


| Метод                                  | Зачем вам                                                                            |
| -------------------------------------- | ------------------------------------------------------------------------------------ |
| `WithNetwork`                          | Сеть listen. Дефолт `tcp4`.                                                          |
| `WithBufferSize`                       | Очередь закодированных ответов на соединение.                                        |
| `WithReadTimeout`                      | Дедлайн чтения (и таймаут Hello до регистрации). Дефолт 30 с.                        |
| `WithPingInterval` / `WithPingTimeout` | Idle Ping и сколько ждать активности после него. Дефолты 30 с / 100 с.               |
| `WithTLSFiles` / `WithTLSConfig`       | TLS. В production нужен: протокол сам шифрования не даёт. Слушатель не ниже TLS 1.3. |


Пустые пути / `WithTLSConfig(nil)` / неположительные длительности откатывают или игнорируют опцию. `WithTLSConfig` побеждает файлы, если заданы оба.

---



## 5. Частые ошибки


| Симптом                            | Что проверить                                     |
| ---------------------------------- | ------------------------------------------------- |
| CommandNotImplemented на Handshake | Есть ли `WithHandlerAuth`                         |
| Тишина после Hello                 | Вызван ли `RegisterV1Runtime` до `Start`          |
| Request Conflict                   | Уникальные ненулевые `RequestID` у живых запросов |
| Warn without TLS                   | Нет `WithTLS*` - ожидаемо                         |


Бенчмарки: [benchmarks.md](benchmarks.md). Паттерны клиента в тестах: `server/helpers_test.go`, `v1_flow_test.go`.