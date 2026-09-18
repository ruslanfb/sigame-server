# SIGame Server (Go) — repository conventions

Backend for «Своя игра» (SIGame-compatible): packs (own JSON + .siq import/export), LAN/Internet play over
WebSocket, REST API with OpenAPI (huma + chi), ping-fair "AnchoredHybrid" buzzer, AI showman via OpenRouter.
Binding architecture: `docs/design/00-constitution.md`. Authoritative game rules: `docs/research/01-sigame-rules.md`.
.siq format: `docs/research/02-siq-format.md` + `testdata/siq-spec/siq_5.xsd`. Buzzer design: `docs/research/07-fairness-judge-*.md`.

## Language
- Code, identifiers, comments, OpenAPI descriptions, commit messages: **English**.
- User-facing guides in `docs/*.md` (README, buzzer explanation for hosts, ops): **Russian**. Design docs: English.

## Layout
`cmd/sigame` (wiring only) · `internal/config` · `internal/httpapi` (huma) · `internal/ws` (coder/websocket transport) ·
`internal/room` (actor) · `internal/engine` (pure game core) · `internal/buzzer` (pure fairness core) · `internal/packs`
(domain + repo) · `internal/siq` (codec) · `internal/media` (store + Range serving) · `internal/ai` (OpenRouter judge) ·
`internal/db` (sqlite + goose) · `internal/lan` · `internal/clock`.
Dependency direction: httpapi/ws → room → engine/buzzer; httpapi → packs/siq/media/ai. Nothing imports httpapi/ws except cmd.

## Rules
- Go 1.27, `CGO_ENABLED=0` must build. Module path `sigame`.
- **Do not run `go get`, `go mod tidy` or edit go.mod/go.sum** inside a package task; all approved dependencies are already
  pinned (see `tools.go`). If a new dependency is truly needed, stop and report it.
- Milliseconds everywhere on the wire (`int64`), `*At` = Unix ms UTC, `*Ms` = duration. Buzzer/timers use `clock.Clock.Mono()`.
- IDs: UUIDv7 (`uuid.Must(uuid.NewV7()).String()`); media IDs = sha256 hex.
- JSON: camelCase tags; add `doc:"..."` tags on DTO fields (huma turns them into OpenAPI descriptions).
- Engine and buzzer are pure: no goroutines, no I/O, no `time.Now()`; they receive commands and return events/timer requests.
- Errors: wrap with context (`fmt.Errorf("...: %w", err)`), sentinel errors per package (`ErrNotFound` ...).
- Tests: stdlib `testing` + `testify/require`; table-driven; deterministic (fake clock, fixed seeds). `go test -race ./...` must pass.
- Formatting: `gofmt`; keep `go vet ./...` clean.
- Keep packages independent: only import the contracts defined in `internal/packs/model.go`, `internal/media/media.go`,
  `internal/clock` unless the task says otherwise.

## Commands
`go build ./...` · `go test -race ./...` · `go run ./cmd/sigame` · `make build|test|run|cross`
