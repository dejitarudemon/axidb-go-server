Язык: [Русский](how-it-works.md) · [English](how-it-works.en.md)

# Как это работает

Документ описывает устройство `ignicula-framework`: роли компонентов, жизненный цикл процесса сервера, соединения и одного запроса (по вызовам функций), контракт ошибок и политику idle Ping / сжатия.

Примеры публичного API - в [туториалах](tutorial.md). Измерения - в [бенчмарках](benchmarks.md). Формат кадров - в документации [ignicula-wire](https://github.com/dejitarudemon/ignicula-wire).

Пакеты сервера расположены у корня модуля и импортируются из других модулей.

---

## 1. Назначение

Сервер принимает TCP- (опционально TLS-) соединения, выполняет регистрацию по протоколу v0 (Hello), активацию рабочей версии v1 (Handshake) и обслуживает асинхронные запросы Read / Write / Delete / Ping / Batch.

Сервер **не** хранит пользовательские ключи и **не** проверяет пароль самостоятельно. Эти обязанности делегированы хендлерам, зарегистрированным на `runtime_v1.Runtime`. Сервер владеет сокетом, таблицей сессий, дедлайнами чтения, очередью исходящих кадров и idle Ping.

---

## 2. Компоненты

```text
клиент
  |
  | TCP / TLS
  v
Server (accept, dispatch, idle, writeLoop)
  |                          |
  v                          v
ConnectionsTable           Runtime v1
Register / Grant /         Decode / Activate /
Activate / Get             Handle / Ping
                             |
                             v
                       хендлеры приложения
                       Auth / Read / Write / Delete
```

| Компонент | Пакет | Ответственность |
| --- | --- | --- |
| `Server` | `server` | Listen/accept, peek версии, маршрутизация, idle, запись ответов, закрытие |
| `ServerConfig` | `server/config` | Сеть, буфер ответов, таймауты, TLS |
| `ConnectionsTable` | `table` | Состояние соединения по версиям: зарегистрирован / granted / activated + `RegistrationRow` |
| `Runtime` | `runtime/v1` | Decode, Activate, Handle, Ping; вызов хендлеров; кодирование ответов |
| `RuntimeBuilder` / config | `runtime/v1`, `.../config` | Сборка runtime: лимиты, компрессоры, хендлеры |
| `RequestRow` | `runtime/v1/row` | На активированной сессии: занятые `RequestID`, компрессоры, idle-ping слот |
| `Logger` | `logger` | Опциональный журнал; `nil` - без логов (кроме предупреждения о plain TCP в stdout) |

Граница ответственности: после `peekVersion` и выбора ветки версии сервер вызывает методы конкретного runtime и получает либо закодированные байты, либо ошибку-политику. Сервер не разбирает тело кадра и не вызывает методы `RequestRow` напрямую (кроме передачи opaque `RegistrationRow` в адаптер `runtimes`).

---

## 3. Жизненный цикл процесса сервера

1. `NewServer(cfg, logger)` - создаёт экземпляр с копией параметров конфига (или дефолтами).
2. `RegisterV1Runtime(*Runtime)` - устанавливает runtime v1. Допустимо только пока сервер не запущен; повторный вызов до `Start` заменяет runtime.
3. `Start(addr, port)`:
   - собирает TLS (`WithTLSConfig` имеет приоритет над `WithTLSFiles`); при отсутствии учёток - plain TCP и предупреждение;
   - принудительно поднимает `MinVersion` до TLS 1.3;
   - `Listen` / `tls.Listen`, `go serve()` (accept-loop).
4. На каждый accept: `serveConn` в отдельной горутине.
5. `Close()` (идемпотентно через `sync.Once`):
   - отмена контекстов активных соединений;
   - `table.Close()`, закрытие listener;
   - ожидание завершения accept-loop (`<-done`).

После `Close` допускается новый `Start` (каналы shutdown/done создаются заново).

---

## 4. Жизненный цикл соединения

Диаграмма на уровне протокола:

```text
клиент                         сервер
   |                              |
   |-------- Hello v0 ----------->|  Register
   |<------- Hello v0 ------------|  supportedVersions
   |                              |
   |-------- Handshake v1 ------->|  Activate → Grant → Activate(row)
   |<------- Handshake Answer ----|
   |                              |
   |   Read/Write/Delete/Ping/    |  Handle (горутина на кадр)
   |   Batch                      |
   |                              |
   |   (idle)                     |  Runtime.Ping(ttl) → writeLoop
```

### 4.1. Accept и каркас `serveConn`

| Шаг | Функция | Действие |
| --- | --- | --- |
| 1 | `serve` → accept | Новое `net.Conn` |
| 2 | `serveConn` | Канал ответов `answers` ёмкости `BufferSize`; `connContext` с cancel; запись в `active` |
| 3 | `writeLoop` (горутина) | Читает `answers`, coalesce в `net.Buffers` (см. §5.3) |
| 4 | цикл чтения | `nextReadDeadline` → `SetReadDeadline` → `dispatch` |
| 5 | выход | `cancel` → wait `writeLoop` → `table.Terminate` → `Close` сокета |

`dispatch`: если соединение ещё не в таблице - `register`, иначе `dispatchRequest`.

### 4.2. Регистрация (Hello v0)

Пока `!table.IsRegistered(conn)`:

| Шаг | Функция | Действие |
| --- | --- | --- |
| 1 | `peekVersion` (`DecodePreamble`) | Версия должна быть `0`; иначе close («skip registration») |
| 2 | `decoder.DecodeFrame` | Полный Hello; тело на сервере не используется для пересечения (ответ строится из `supportedVersions`) |
| 3 | `noteActivity` | Сброс idle-учёта |
| 4 | `table.Register` | Соединение зарегистрировано, версии ещё не granted |
| 5 | `sendHelloAnswer` | v0 Hello со списком из `runtimes.supportedVersions()` (сейчас `[1]`, если runtime есть) через `sendAnswer` |

Таймаут до Hello: `onReadIdle` при незарегистрированном соединении закрывает его («registration timed out»).

Повторный кадр v0 после регистрации: `dispatchRequest` закрывает соединение.

### 4.3. Активация (Handshake v1)

После регистрации первый осмысленный v1-кадр обычно Handshake.

| Шаг | Функция | Действие |
| --- | --- | --- |
| 1 | `dispatchRequest` | `peekVersion` → при v1 `handleV1` |
| 2 | `noteActivity` | После успешного peek |
| 3 | `Runtime.Decode` | Полный кадр; при протокольной ошибке с готовым answer - сразу `sendAnswer` |
| 4 | `go serveV1Frame` | Обработка кадра асинхронна относительно read-loop |
| 5 | `table.Get(conn, 1)` | `ErrorVersionNotGranted` → `activateV1(..., skipAuth=false)`; `ErrorVersionNotActivated` → `skipAuth=true` |
| 6 | `Runtime.Activate` | Валидация Handshake; вызов Auth (если не skip); сборка Handshake Answer; создание `RequestRow` |
| 7 | `table.Grant(1)` | Версия выдана |
| 8 | `table.Activate(1, requestRow)` | Row сохранён как `RegistrationRow` |
| 9 | `table.Grant(остальные)` | Версии из `allowedVersions` runtime (без Activate), только если не `skipAuth` |
| 10 | `sendAnswer` | Байты Handshake Answer |

Если Activate вернул error-answer (`requestRow == nil`), сервер только отдаёт байты и не меняет таблицу.

Соединение, зарегистрированное, но без активной версии, при idle закрывается (`ErrorNoActiveVersion` в `onReadIdle`).

### 4.4. Рабочий режим

Для последующих кадров путь тот же до `serveV1Frame`, но `table.Get` успешен:

| Шаг | Функция | Действие |
| --- | --- | --- |
| 1 | `runtimes.handle` | Type-assert `RegistrationRow` → `*RequestRow`, вызов `Runtime.Handle` |
| 2 | итерация `FrameIterator` | Каждая пара `(answer, err)` → `deliverV1Answer` |
| 3 | `deliverV1Answer` | Политика ошибок (см. §6); при успехе `sendAnswer` |
| 4 | `writeLoop` | Фактическая запись в TCP |

Несколько кадров могут обрабатываться параллельно (`go serveV1Frame` на каждый). Ответы сериализуются одним `writeLoop` и могут прийти клиенту не в порядке запросов. Сопоставление - по `RequestID`.

---

## 5. Жизненный цикл запроса (v1)

Ниже - путь одного уже зарегистрированного и активированного соединения.

### 5.1. Чтение кадра (read-loop, синхронно)

```text
nextReadDeadline
    → SetReadDeadline
    → peekVersion (DecodePreamble, курсор не сдвигается)
    → noteActivity
    → handleV1
         → Runtime.Decode(reader)   // потребляет кадр
         → go serveV1Frame(request)
    → return в цикл (ждать следующий кадр)
```

`Decode` может:

- вернуть `(frame, nil, nil)` - нормальный запрос;
- вернуть `(..., answer, nil)` - протокольная ошибка уже закодирована, писать answer, Handle не вызывать;
- вернуть ошибку с `ErrCloseConnection` - закрыть после возможного answer;
- вернуть timeout / EOF - idle или disconnect.

### 5.2. Обработка (`serveV1Frame` + `Handle`, асинхронно)

```text
table.Get
    → Runtime.Handle(ctx, frame, *RequestRow)
         → регистрация RequestID в RequestRow (конфликт → error-answer)
         → NewContext(login, requestID, ...)
         → по Command:
              Read     → handlerRead
              Write    → handlerWrite
              Delete   → handlerDelete
              Ping     → PingAnswer (без хендлера приложения)
              Batch    → вложенные Read/Write/Delete (seq или pool)
              Answer   → проверка external / registered → часто ErrLogAndIgnore
              (Handshake на неактивированной версии идёт через Activate, не через этот switch)
         → encode + selectCompression
         → yield (bytes, err)
    → deliverV1Answer / sendAnswer
    → Terminate(RequestID) после завершения ответа (для batch - после всех частей)
```

Правила отмены контекста внутри Handle:

- если `ctx` уже отменён до хендлера - хендлер не вызывается, ответ `RequestInterrupted`;
- если отмена произошла во время хендлера - хендлер дорабатывает, результат подменяется на `RequestInterrupted`.

### 5.3. Исходящая запись

`sendAnswer` кладёт байты в `ctx.answers` (блокируясь, пока буфер не освободится, либо пока контекст не отменён). `writeLoop` единственный пишет в сокет данного соединения.

Политика coalesce (production):

| Условие | Поведение |
| --- | --- |
| Батч достиг `netBufferSize` (16 кадров) | немедленный `net.Buffers.WriteTo` (writev на поддерживаемых conn) |
| После первого кадра в батче прошло `flushAfter` | flush частичного батча - верхняя граница latency |
| Батч пуст | таймер не вооружён (idle-соединение не тикает) |

Таймер нужен: без него хвост из 1..(cap-1) кадров ждёт следующего ответа неограниченно долго. Сразу flush’ить каждый кадр - отказ от coalesce при нескольких outstanding на одном conn (pipeline / параллельные `serveV1Frame`). Константы - в `server/writer.go`.

---

## 6. Контракт ошибок runtime → server

Итератор Handle и ряд путей Decode/Activate/Ping используют одну политику:

| Условие | Поведение сервера |
| --- | --- |
| `err == nil` | `sendAnswer(bytes)`, соединение продолжает работу |
| `errors.Is(err, ErrCloseConnection)` | лог, `closeConn` (байты answer могли быть отправлены ранее, например из `respondRuntimeError`) |
| `errors.Is(err, ErrLogAndIgnore)` | лог, **без** записи на провод, соединение живёт |
| другая `err` | лог; при ненулевых `answer` - всё равно `sendAnswer` (типично для сбоя кодирования части batch) |

Примеры `ErrLogAndIgnore`: Answer на незарегистрированный `RequestID`, Answer на не-external запрос.

---

## 7. Idle Ping

Параметры (дефолты как в рекомендации протокола):

| Параметр | Дефолт | Роль |
| --- | --- | --- |
| `ReadTimeout` | 30 с | Верхняя граница ожидания байт, пока нет outstanding Ping |
| `PingInterval` | 30 с | Простой после `lastActivity`, после которого можно слать Ping |
| `PingTimeout` | 100 с | С момента первого outstanding Ping до любой активности клиента; иначе close |

### Поток

1. `nextReadDeadline` вычисляет момент пробуждения read-loop.
2. Истечение дедлайна → `onReadIdle`.
3. Не зарегистрирован → close.
4. Есть `firstPingAt` и прошло ≥ `PingTimeout` → close; иначе ждать (не слать второй Ping на уровне соединения).
5. Простой < `PingInterval` → ничего.
6. `MinActiveVersion` → `runtimes.ping(version, reg, pingTimeout)` → `Runtime.Ping(row, ttl)`.
7. `Ping` через `RequestRow.BeginIdlePing(deadline)`:
   - если предыдущий idle id ещё не истёк → `(nil, nil)`, сервер ничего не шлёт;
   - если истёк → снять старый id, зарезервировать новый, закодировать Ping;
   - id помечен external, чтобы клиентский PingAnswer принял Handle.
8. При ненулевых байтах: `markFirstPing`, `sendAnswer`.
9. Любой следующий успешный peek/кадр: `noteActivity` сбрасывает `firstPingAt`.

Таким образом на стороне runtime одновременно живёт не более одного idle-ping `RequestID` с TTL, а на стороне соединения - не более одного outstanding keepalive-окна.

---

## 8. Сжатие

Прикладной код задаёт только множество компрессоров (`WithCompressors`) и порог (`WithStartUseCompressionAt`). Выбор кодека на конкретный ответ выполняет `Runtime.selectCompression`:

1. Нет компрессоров / тело меньше порога / `useCompressionAt > limit` → без сжатия.
2. Handshake, Ping, ответы на Write/Delete/Handshake/Ping и error-answer → без сжатия.
3. Иначе при размере тела ≳ 160 КиБ и наличии Zstd у runtime и соединения → Zstd.
4. Иначе первый подходящий алгоритм из пересечения (кроме уже рассмотренного Zstd), либо Zstd, если он единственный общий.

Клиент объявляет алгоритмы в Handshake; они сохраняются в `RequestRow`. Без `WithCompressors` сжатые входящие кадры отвергаются, исходящие не сжимаются.

---

## 9. Таблица соединений (состояния)

Для каждой пары `(conn, version)` таблица проходит:

```text
(нет записи)
    → Register(conn)              // после Hello
    → Grant(version)              // после успешного Activate
    → Activate(version, row)      // RequestRow сохранён
    → Get / MinActiveVersion      // рабочие запросы и idle
    → Terminate(conn)             // при закрытии соединения
```

`RegistrationRow` - узкий интерфейс (`Count()` для статистики). Фактический тип для v1 - `*row.RequestRow`.

---

## 10. Параллельность и упорядоченность

| Уровень | Поведение |
| --- | --- |
| Accept | Одна горутина accept-loop |
| Соединение | Одна read-loop + одна write-loop |
| Кадр v1 | Отдельная горутина `serveV1Frame` |
| Parallel batch | До `MaxGoroutinesPerBatch` вложенных команд; ответы могут уходить по мере готовности, если клиент не запросил one-answer |
| `RequestID` | Мультиплексирование; `0` зарезервирован для Handshake; повтор живого id → Request Conflict |

Клиент обязан сопоставлять ответы по `RequestID`, а не по порядку байт на сокете.

---

## 11. Конфигурация (сводка)

**ServerConfig** (дефолты): сеть `tcp4`, `BufferSize` 16, `ReadTimeout` 30 с, `PingInterval` 30 с, `PingTimeout` 100 с, TLS выключен.

**RuntimeBuilderConfig** (дефолты): тело 10 КиБ, batch limit 64 (поле конфига; текущий Handle по нему не режет), 4 горутины на parallel batch, сжатие с 5 КиБ, версия `[1]`, без компрессоров, stub-хендлеры с `CommandNotImplemented`.

`(nil, nil)` из Read интерпретируется как отсутствие ключа (NotFound на проводе).
