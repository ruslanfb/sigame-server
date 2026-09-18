# SIGame Server — Architecture Constitution (binding decisions)

These decisions are fixed. Every design document and every line of code must conform to them.
Research inputs live in `../research/*.md` (rules, .siq format, implementations, stack, buzzer fairness).

## 1. Product scope
- Self-hosted backend for «Своя игра» (SIGame-compatible), played over LAN (and optionally Internet).
- **Backend only**: REST API (OpenAPI 3.1 + Scalar docs UI), WebSocket game protocol, media serving. No production UI. A tiny static "API docs" page and the OpenAPI JSON are the only HTML served (plus an optional minimal debug page is NOT in scope).
- Roles: `host` (room owner, may also be showman), `showman` (human or AI), `player` (≤ 12), `viewer`.
- Answer judging: human showman **or** AI showman via OpenRouter (`OPENROUTER_API_KEY`, `OPENROUTER_MODEL` default `tencent/hy4-preview`, text-only) with fuzzy-match fallback.
- Packs: own JSON model (superset of SIQ v5 semantics) + **import .siq v4/v5** + **export .siq v5**. Full CRUD of packs/rounds/themes/questions via REST. Media: image (png/jpg/gif/webp), audio (mp3/ogg/m4a/wav), video (mp4/webm), html.
- Buzzer fairness: **AnchoredHybrid** (see `../research/07-fairness-judge-*.md` syntheses; consolidated in `40-buzzer-spec.md`). Go terminates TCP itself → kernel RTT anchor (`TCP_INFO` on Linux, `TCP_CONNECTION_INFO` on macOS, `SIO_TCP_INFO` on Windows if feasible, else WS-ping anchor only).

## 2. Stack (verified 2026-09-18)
- Go 1.27 (`go 1.27` in go.mod), module path **`sigame`** (no VCS host; single repo).
- HTTP router: `github.com/go-chi/chi/v5` v5.3.2. OpenAPI: `github.com/danielgtaylor/huma/v2` v2.39.1 with the chi adapter, Scalar docs renderer at `/docs`, spec at `/openapi.json` and `/openapi.yaml`.
- WebSocket: `github.com/coder/websocket` v1.8.15 (one goroutine reader + one writer per connection; `Ping(ctx)` used for the protocol-ping RTT anchor).
- DB: SQLite via `modernc.org/sqlite` v1.59.0 (pure Go, `CGO_ENABLED=0`), WAL mode, migrations via `github.com/pressly/goose/v3` embedded SQL files. `database/sql` + hand-written queries (no ORM).
- Config: env vars + optional `config.yaml` (`github.com/caarlos0/env/v11` for env). Logging: `log/slog` (JSON in prod, text in dev). IDs: UUIDv7 strings (`github.com/google/uuid` v1.6 `uuid.NewV7`). Tests: stdlib `testing` + `github.com/stretchr/testify` v1.12.
- Media probing: `ffprobe` if present on PATH (duration/width/height), otherwise best-effort (image headers via stdlib `image` decoders; audio/video duration unknown → client reports completion).
- Build: `go build ./cmd/sigame` → single static binary; `Makefile`/`Taskfile` targets: build, test, lint (`go vet`, `staticcheck` optional), run, cross-build (linux/amd64, linux/arm64, darwin/arm64, windows/amd64).

## 3. Repository layout
```
cmd/sigame/main.go            # wiring only
internal/config               # Config struct, env/yaml loading, defaults
internal/httpapi              # huma API: handlers, DTOs, error mapping, /docs
internal/ws                   # WebSocket transport: connection, envelope codec, hub registry, ping anchor, tcpinfo
internal/room                 # Room actor: mailbox, timers, session/roles, projections, snapshot/resume
internal/engine               # Pure game core: state machine, question scripts, scoring, stakes, final round, appeals. NO I/O, NO timers (receives Tick/Timeout commands)
internal/buzzer               # AnchoredHybrid: clock model per connection, arming, press scoring, collection window, decision, trust ladder, audit. Pure functions + a small stateful model; deterministic simulator in tests
internal/packs                # Domain model (Pack/Round/Theme/Question/ContentItem...), validation, repository (SQLite), JSON (de)serialization
internal/siq                  # .siq import (v4+v5) and export (v5): zip + content.xml codec, media extraction/insertion, compatibility report
internal/media                # Content-addressed store (sha256), MIME sniffing, ffprobe, HTTP Range serving, GC
internal/ai                   # OpenRouter client, answer judge prompt, JSON-verdict parsing, timeouts/fallback, showman replicas
internal/db                   # sqlite open/pragmas, goose migrations (embedded), tx helpers
internal/lan                  # LAN helpers: listen 0.0.0.0, print/QR join URL, optional mDNS advertisement (_sigame._tcp)
internal/clock                # Monotonic clock interface (real + fake for tests)
docs/                         # This design set, API guide, protocol spec (AsyncAPI-style markdown), buzzer spec, ops guide
testdata/siq/                 # 61 official sample packs + XSD (copied from research)
```
Dependency direction: `httpapi`/`ws` → `room` → `engine`/`buzzer`; `httpapi` → `packs`/`siq`/`media`/`ai`; nothing imports `httpapi`/`ws` except `cmd`.

## 4. Time and identifiers
- **Milliseconds everywhere** on the wire (`int64`). Wall-clock fields are Unix ms UTC (`*At`). Buzzer timing uses the server monotonic clock (`clock.Mono()`, ms as float64 internally, int on the wire when it is a duration).
- All entity IDs are UUIDv7 strings. Room join codes: 5 uppercase letters/digits without ambiguous chars (`ABCDEFGHJKLMNPQRSTUVWXYZ23456789`).
- Sequence numbers: server→client `seq` is per-connection monotonic (for resume/dedup); client→server `seq` is per-connection monotonic (idempotency).

## 5. Transport contract
- REST for everything that is not real-time: packs, media, rooms (create/list/get), room settings, buzzer presets/log, AI settings, health, LAN info.
- WebSocket `GET /ws?room=<CODE>&token=<sessionToken>` (token obtained from `POST /api/v1/rooms/{code}/join`). One connection per session; reconnect with the same token resumes (`SNAPSHOT` + replay from `lastSeq` when available, else full snapshot).
- Envelope (JSON, UTF-8): `{"t":"<TYPE>","seq":<int>,"p":{...}}`. `t` is `SCREAMING_SNAKE`. Server errors: `{"t":"ERROR","seq":n,"p":{"code":"...","message":"...","ref":<clientSeq?>}}`. Max frame 64 KiB; `perMessageDeflate` off; media never travels over WS (URLs only).
- Per-role **projections**: players/viewers never receive right answers, showman comments, unopened content, or other players' hidden stakes/answers before reveal.
- Room = single goroutine actor with a mailbox; all commands (client messages, timer expiries, REST changes, AI verdicts) are serialized. Engine and buzzer are pure and deterministic given the command stream.

## 6. API surface (top level; details in `20-rest-api.md`)
`/api/v1`: `packs` (CRUD + `import` .siq multipart + `export` .siq + `validate` + nested `rounds/themes/questions` editing endpoints + `duplicate`), `media` (upload, get metadata, `GET /media/{id}` streaming with Range, delete), `rooms` (create with pack+rules, get by code, list, join → session token, settings PATCH, buzzer settings PATCH, buzz-log, kick/ban, start/pause/next via WS only), `buzzer-presets`, `ai` (test connection, model list), `system` (`/health`, `/info` with LAN addresses + join URL, `/version`).
- Errors: RFC 9457 problem+json (huma default). Validation: huma struct tags + custom validators; 4xx with field pointers.

## 7. Game semantics to reproduce (from `../research/01-sigame-rules.md`)
- Modes: classic (table, chooser), simple/sport (sequential), quiz (forAll), turn-taking. Question types: simple, stake, stakeAll, secret, secretPublicPrice, secretNoQuestion, noRisk, forAll, custom (manual). Answer types: text, select, number(±deviation), point, client. Final round with theme deletion + hidden stakes + written answers. Appeals («Я прав»/«Я против»). Timers table (§8 of rules). Showman powers (validate with factor, CHANGE score, PAUSE, MOVE, TOGGLE, SETCHOOSER). Host powers (KICK/BAN/SETHOST/settings). Negative scores allowed.
- Buzzer modes selectable by host: `anchoredHybrid` (default), `serverArrival` (FirstWins), `randomWindow` (SIGame RandomWithinInterval), `clientReaction` (SIGame FirstWinsClient, for compatibility, marked "unsafe"), `writtenAll` (no race: everyone writes).

## 8. Non-goals (v1)
Accounts/auth beyond tokens; horizontal scaling; WebTransport; pack storage federation (SIStorage); voice chat; UI. Keep extension points (interfaces) but do not implement.

## 9. Documentation deliverables
`docs/README.md` (overview + quick start), `docs/api.md` (REST guide with examples; the OpenAPI JSON is the source of truth), `docs/protocol.md` (all WS messages with JSON examples and sequence diagrams for every question type), `docs/buzzer.md` (fairness design, constants, host guide, threat model), `docs/packs.md` (pack model + .siq mapping + compatibility matrix), `docs/ops.md` (config, LAN setup, ffprobe, backups, limits), `docs/adr/*.md` (decisions).
