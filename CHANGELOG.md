# Changelog

## 0.8.0 - 2026-10-10

First tagged release of the Go server framework for Ignicula.

Added:

- Server with Hello (v0), Handshake, and v1 Read / Write / Delete / Ping / Batch
- Optional TLS with a TLS 1.3 floor
- Idle Ping with request-id TTL
- Outbound answer coalesce via `net.Buffers` (flush on batch size or timer)
- Dynamic compression selection for answers
- Optional slog-backed logger
- How-it-works, tutorials, and benchmarks (RU/EN)
- Multi-client load benches with p50/p99
