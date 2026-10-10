Language: [Русский](how-it-works.md) · [English](how-it-works.en.md)

# How it works

This document describes `axidb-go-server`: component roles, process / connection / request lifecycles (by function calls), the error contract, and idle Ping / compression policy.

Public API examples are in the [tutorials](tutorial.en.md). Numbers are in [benchmarks](benchmarks.en.md). Frame layout is in [axidb-go-protocol](https://github.com/dejitarudemon/axidb-go-protocol).

Server packages live under `internal/` and are meant for use from code inside this module.

---

## 1. Purpose

The server accepts TCP (optionally TLS) connections, runs protocol v0 registration (Hello), activates working version v1 (Handshake), and serves asynchronous Read / Write / Delete / Ping / Batch requests.

The server does **not** store application keys and does **not** verify passwords itself. Those duties belong to handlers registered on `runtime_v1.Runtime`. The server owns the socket, the session table, read deadlines, the outbound frame queue, and idle Ping.

---

## 2. Components

```text
client
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
                       application handlers
                       Auth / Read / Write / Delete
```

| Component | Package | Responsibility |
| --- | --- | --- |
| `Server` | `internal/server` | Listen/accept, version peek, routing, idle, writing answers, close |
| `ServerConfig` | `internal/server/config` | Network, answer buffer, timeouts, TLS |
| `ConnectionsTable` | `internal/table` | Per-connection version state: registered / granted / activated + `RegistrationRow` |
| `Runtime` | `internal/runtime/v1` | Decode, Activate, Handle, Ping; handler calls; answer encoding |
| `RuntimeBuilder` / config | `internal/runtime/v1`, `.../config` | Runtime assembly: limits, compressors, handlers |
| `RequestRow` | `internal/runtime/v1/row` | On an activated session: live `RequestID`s, compressors, idle-ping slot |
| `Logger` | `internal/logger` | Optional log; `nil` is quiet (except the plain-TCP warning on stdout) |

Boundary: after `peekVersion` and version routing, the server calls concrete runtime methods and receives either encoded bytes or a policy error. It does not parse frame bodies or call `RequestRow` methods directly (aside from passing an opaque `RegistrationRow` into the `runtimes` adapter).

---

## 3. Server process lifecycle

1. `NewServer(cfg, logger)` - builds an instance from config (or defaults).
2. `RegisterV1Runtime(*Runtime)` - installs the v1 runtime. Only before start; a second call before `Start` replaces the runtime.
3. `Start(addr, port)`:
   - builds TLS (`WithTLSConfig` wins over `WithTLSFiles`); without credentials - plain TCP and a warning;
   - forces `MinVersion` to at least TLS 1.3;
   - `Listen` / `tls.Listen`, then `go serve()` (accept loop).
4. Each accept runs `serveConn` in its own goroutine.
5. `Close()` (idempotent via `sync.Once`):
   - cancels active connection contexts;
   - `table.Close()`, closes the listener;
   - waits for the accept loop (`<-done`).

After `Close`, a new `Start` is allowed (shutdown/done channels are recreated).

---

## 4. Connection lifecycle

Protocol-level diagram:

```text
client                         server
   |                              |
   |-------- Hello v0 ----------->|  Register
   |<------- Hello v0 ------------|  supportedVersions
   |                              |
   |-------- Handshake v1 ------->|  Activate → Grant → Activate(row)
   |<------- Handshake Answer ----|
   |                              |
   |   Read/Write/Delete/Ping/    |  Handle (goroutine per frame)
   |   Batch                      |
   |                              |
   |   (idle)                     |  Runtime.Ping(ttl) → writeLoop
```

### 4.1. Accept and `serveConn` skeleton

| Step | Function | Action |
| --- | --- | --- |
| 1 | `serve` → accept | New `net.Conn` |
| 2 | `serveConn` | Answer channel of size `BufferSize`; cancellable `connContext`; store in `active` |
| 3 | `writeLoop` (goroutine) | Reads `answers`, coalesces into `net.Buffers` (see §5.3) |
| 4 | read loop | `nextReadDeadline` → `SetReadDeadline` → `dispatch` |
| 5 | exit | `cancel` → wait `writeLoop` → `table.Terminate` → socket `Close` |

`dispatch`: if the connection is not in the table yet - `register`, else `dispatchRequest`.

### 4.2. Registration (Hello v0)

While `!table.IsRegistered(conn)`:

| Step | Function | Action |
| --- | --- | --- |
| 1 | `peekVersion` (`DecodePreamble`) | Version must be `0`; otherwise close (“skip registration”) |
| 2 | `decoder.DecodeFrame` | Full Hello; the server does not use the body for intersection (the reply is built from `supportedVersions`) |
| 3 | `noteActivity` | Reset idle accounting |
| 4 | `table.Register` | Connection registered; versions not granted yet |
| 5 | `sendHelloAnswer` | v0 Hello listing `runtimes.supportedVersions()` (currently `[1]` if a runtime is installed) via `sendAnswer` |

Timeout before Hello: `onReadIdle` on an unregistered connection closes it (“registration timed out”).

A second v0 frame after registration: `dispatchRequest` closes the connection.

### 4.3. Activation (Handshake v1)

After registration the first meaningful v1 frame is normally Handshake.

| Step | Function | Action |
| --- | --- | --- |
| 1 | `dispatchRequest` | `peekVersion` → on v1, `handleV1` |
| 2 | `noteActivity` | After a successful peek |
| 3 | `Runtime.Decode` | Full frame; if a protocol error already has an answer - `sendAnswer` immediately |
| 4 | `go serveV1Frame` | Frame handling is async relative to the read loop |
| 5 | `table.Get(conn, 1)` | `ErrorVersionNotGranted` → `activateV1(..., skipAuth=false)`; `ErrorVersionNotActivated` → `skipAuth=true` |
| 6 | `Runtime.Activate` | Validate Handshake; call Auth (unless skip); build Handshake Answer; create `RequestRow` |
| 7 | `table.Grant(1)` | Version granted |
| 8 | `table.Activate(1, requestRow)` | Row stored as `RegistrationRow` |
| 9 | `table.Grant(others)` | Versions from the runtime `allowedVersions` (no Activate), only when not `skipAuth` |
| 10 | `sendAnswer` | Handshake Answer bytes |

If Activate returns an error-answer (`requestRow == nil`), the server only writes the bytes and does not change the table.

A registered connection with no active version is closed on idle (`ErrorNoActiveVersion` in `onReadIdle`).

### 4.4. Steady state

For later frames the path is the same until `serveV1Frame`, but `table.Get` succeeds:

| Step | Function | Action |
| --- | --- | --- |
| 1 | `runtimes.handle` | Type-assert `RegistrationRow` → `*RequestRow`, call `Runtime.Handle` |
| 2 | iterate `FrameIterator` | Each `(answer, err)` → `deliverV1Answer` |
| 3 | `deliverV1Answer` | Error policy (see §6); on success `sendAnswer` |
| 4 | `writeLoop` | Actual TCP write |

Several frames may run in parallel (`go serveV1Frame` each). Answers are serialized by one `writeLoop` and may arrive out of request order. Matching is by `RequestID`.

---

## 5. Request lifecycle (v1)

Path for one already registered and activated connection.

### 5.1. Reading the frame (read loop, synchronous)

```text
nextReadDeadline
    → SetReadDeadline
    → peekVersion (DecodePreamble, cursor not advanced)
    → noteActivity
    → handleV1
         → Runtime.Decode(reader)   // consumes the frame
         → go serveV1Frame(request)
    → back to the loop (wait for the next frame)
```

`Decode` may:

- return `(frame, nil, nil)` - normal request;
- return `(..., answer, nil)` - protocol error already encoded; write answer, do not Handle;
- return an error with `ErrCloseConnection` - close after a possible answer;
- return timeout / EOF - idle or disconnect.

### 5.2. Handling (`serveV1Frame` + `Handle`, asynchronous)

```text
table.Get
    → Runtime.Handle(ctx, frame, *RequestRow)
         → register RequestID on RequestRow (conflict → error-answer)
         → NewContext(login, requestID, ...)
         → by Command:
              Read     → handlerRead
              Write    → handlerWrite
              Delete   → handlerDelete
              Ping     → PingAnswer (no application handler)
              Batch    → nested Read/Write/Delete (seq or pool)
              Answer   → external / registered checks → often ErrLogAndIgnore
              (Handshake on a non-activated version goes through Activate, not this switch)
         → encode + selectCompression
         → yield (bytes, err)
    → deliverV1Answer / sendAnswer
    → Terminate(RequestID) after the answer completes (for batch - after every part)
```

Cancellation rules inside Handle:

- if `ctx` is already cancelled before the handler - the handler is skipped, answer is `RequestInterrupted`;
- if cancellation happens during the handler - the handler finishes, then the result is replaced with `RequestInterrupted`.

### 5.3. Outbound write

`sendAnswer` enqueues bytes on `ctx.answers` (blocks until buffer space or context cancel). `writeLoop` is the only writer for that connection’s socket.

Coalesce policy (production):

| Condition | Behavior |
| --- | --- |
| Batch reaches `netBufferSize` (16 frames) | immediate `net.Buffers.WriteTo` (writev on supported conns) |
| `flushAfter` elapsed after the first frame in the batch | flush a partial batch - latency ceiling |
| Batch empty | timer is not armed (idle connections do not tick) |

The timer is required: without it a tail of 1..(cap-1) frames can wait for the next answer indefinitely. Flushing every frame immediately gives up coalesce when several answers are outstanding on one conn (pipeline / parallel `serveV1Frame`). Constants live in `internal/server/writer.go`.

---

## 6. Runtime → server error contract

Handle (and several Decode/Activate/Ping paths) share one policy:

| Condition | Server behavior |
| --- | --- |
| `err == nil` | `sendAnswer(bytes)`, keep the connection |
| `errors.Is(err, ErrCloseConnection)` | log, `closeConn` (answer bytes may already have been sent, e.g. from `respondRuntimeError`) |
| `errors.Is(err, ErrLogAndIgnore)` | log, **no** wire write, keep the connection |
| other `err` | log; if `answer` is non-empty, still `sendAnswer` (typical for a failed encode of part of a batch) |

Examples of `ErrLogAndIgnore`: Answer for an unregistered `RequestID`, Answer for a non-external request.

---

## 7. Idle Ping

Parameters (defaults match the protocol recommendation):

| Parameter | Default | Role |
| --- | --- | --- |
| `ReadTimeout` | 30 s | Upper bound waiting for bytes while no outstanding Ping |
| `PingInterval` | 30 s | Idle time after `lastActivity` before a Ping may be sent |
| `PingTimeout` | 100 s | From the first outstanding Ping until any client activity; else close |

### Flow

1. `nextReadDeadline` computes when the read loop should wake.
2. Deadline expiry → `onReadIdle`.
3. Not registered → close.
4. `firstPingAt` set and age ≥ `PingTimeout` → close; else wait (do not send another connection-level Ping).
5. Idle < `PingInterval` → nothing.
6. `MinActiveVersion` → `runtimes.ping(version, reg, pingTimeout)` → `Runtime.Ping(row, ttl)`.
7. `Ping` via `RequestRow.BeginIdlePing(deadline)`:
   - previous idle id still within TTL → `(nil, nil)`, server sends nothing;
   - expired → drop old id, reserve a new one, encode Ping;
   - id is marked external so a client PingAnswer is accepted by Handle.
8. On non-empty bytes: `markFirstPing`, `sendAnswer`.
9. Any later successful peek/frame: `noteActivity` clears `firstPingAt`.

So the runtime keeps at most one idle-ping `RequestID` with a TTL, and the connection keeps at most one outstanding keepalive window.

---

## 8. Compression

Application code only sets the compressor set (`WithCompressors`) and the threshold (`WithStartUseCompressionAt`). Per-answer codec selection is `Runtime.selectCompression`:

1. No compressors / body under threshold / `useCompressionAt > limit` → no compression.
2. Handshake, Ping, answers to Write/Delete/Handshake/Ping, and error-answers → no compression.
3. Otherwise, body ≳ 160 KiB and Zstd on both sides → Zstd.
4. Otherwise the first matching algorithm in the intersection (skipping Zstd already considered), or Zstd if it is the only shared one.

The client advertises algorithms in Handshake; they are stored on `RequestRow`. Without `WithCompressors`, compressed inbound frames are rejected and outbound bodies stay plain.

---

## 9. Connection table (states)

For each `(conn, version)` pair:

```text
(no entry)
    → Register(conn)              // after Hello
    → Grant(version)              // after successful Activate
    → Activate(version, row)      // RequestRow stored
    → Get / MinActiveVersion      // working requests and idle
    → Terminate(conn)             // on connection close
```

`RegistrationRow` is a narrow interface (`Count()` for stats). The concrete v1 type is `*row.RequestRow`.

---

## 10. Concurrency and ordering

| Level | Behavior |
| --- | --- |
| Accept | One accept-loop goroutine |
| Connection | One read loop + one write loop |
| v1 frame | Separate `serveV1Frame` goroutine |
| Parallel batch | Up to `MaxGoroutinesPerBatch` nested commands; answers may leave as they finish unless the client asked for one-answer |
| `RequestID` | Multiplexing; `0` reserved for Handshake; reuse of a live id → Request Conflict |

Clients must match answers by `RequestID`, not by byte order on the socket.

---

## 11. Configuration (summary)

**ServerConfig** (defaults): network `tcp4`, `BufferSize` 16, `ReadTimeout` 30 s, `PingInterval` 30 s, `PingTimeout` 100 s, TLS off.

**RuntimeBuilderConfig** (defaults): body 10 KiB, batch limit 64 (config field; current Handle does not enforce it), 4 goroutines per parallel batch, compression from 5 KiB, versions `[1]`, no compressors, stub handlers returning `CommandNotImplemented`.

`(nil, nil)` from Read means the key is missing (NotFound on the wire).
