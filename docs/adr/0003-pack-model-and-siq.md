# ADR-0003: Native JSON pack model as a superset of SIQ v5

Status: accepted (2026-09-18)

## Context
Thousands of community packs exist in `.siq` (v4 legacy and v5 current). The server needs a first-class, API-editable
model with media stored on the server, while keeping lossless interchange with SIGame/SIQuester.

## Decision
- `internal/packs.Pack` mirrors SIQ v5 semantics (rounds → themes → questions; params `question/answer/theme/price/
  selectionMode/answerType/answerOptions/answerDeviation/answerDuration`; content items with placement/duration/noWait)
  and adds server concepts (UUIDv7 IDs, versioning, `mediaId` references) plus escape hatches (`extraParams`, `script`,
  `extra.globalAuthors/Sources`) so unknown SIQ data survives a round-trip.
- `.siq` import accepts v4 and v5 (v4 is upgraded with the reference algorithm: auction→stake, cat/bagcat→secret*,
  sponsored→noRisk, scenario atoms→content items); export always writes v5.
- Media inside a pack is copied into the content-addressed store; the ZIP entry name quirks (percent-encoding, NFD,
  `@` prefix, cp866) are resolved by a tolerant index; a compatibility report lists everything that could not be mapped.
- Packs are persisted as JSON documents in SQLite with extracted search columns; media reference counts are kept by
  the pack repository so unreferenced files can be garbage-collected.

## Consequences
- Editing via REST works on the JSON model; the OpenAPI schema is derived from the same Go structs.
- Custom question types/scripts are preserved but played manually (as in SIGame).
