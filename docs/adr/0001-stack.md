# ADR-0001: Go + chi + huma + coder/websocket + SQLite (modernc)

Status: accepted (2026-09-18)

## Context
A self-hosted SIGame server must run on Windows/macOS/Linux from a single download, serve a REST API with generated
OpenAPI docs, drive a low-latency WebSocket buzzer, store packs with media (images/audio/video/html, HTTP Range) and
import/export `.siq` (zip + xml). The stack comparison (docs/research/04-stack-2026.md) scored Go + huma and
Bun + Hono within noise; the user chose Go for zero-churn operations.

## Decision
- Go 1.27, `CGO_ENABLED=0`, module `sigame`, single static binary, cross-compiled from one host.
- HTTP: `go-chi/chi/v5`; OpenAPI 3.1 + Scalar docs via `danielgtaylor/huma/v2` (struct-tag driven, RFC 9457 errors).
- WebSocket: `coder/websocket` (maintained successor of nhooyr; exposes `Ping` and `OnPongReceived` for RTT).
- Persistence: SQLite via `modernc.org/sqlite` (pure Go) with goose SQL migrations; packs stored as JSON documents
  plus a few indexed metadata columns; media content-addressed on disk.
- Kernel RTT anchor via `golang.org/x/sys/unix` (`TCP_INFO` / `TCP_CONNECTION_INFO`) because the process terminates TCP itself.

## Consequences
- No shared TypeScript types with a future client: generate them from `/openapi.json` (`openapi-typescript`).
- One SQLite connection (single writer) keeps the DB simple; LAN scale (≤ 50 rooms) does not need more.
- `ffprobe` is an optional external tool for media duration; absence degrades gracefully.
