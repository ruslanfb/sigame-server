# ADR-0002: AnchoredHybrid buzzer instead of server-arrival order

Status: accepted (2026-09-18)

## Context
With server-arrival ordering the player with the lowest ping wins button races regardless of who reacted first —
the failure the product must not have. SIGame's own mitigations are either random (`RandomWithinInterval`) or
trivially spoofable (`FirstWinsClient`, client-reported reaction time). Three independent designs were produced and
adversarially judged (docs/research/06-*, 07-*); all three judges converged on the same merge.

## Decision
Implement **AnchoredHybrid** in `internal/buzzer` (pure, deterministic) driven by the room actor:
1. Server-owned clock model per connection: app-level 4-tuple SYNC (offset, RTT, jitter) **anchored** by quantities a
   browser script cannot inflate — WebSocket protocol ping/pong RTT and, when available, the kernel's `TCP_INFO` RTT.
2. The button is *scheduled*: every client lights it at the same server instant `T_arm` (converted to local time with the
   frozen offset), so downlink differences do not matter.
3. Presses carry the client's local press stamp; the server converts it, **clamps** it into the plausibility window
   `[tArr − tol, tArr + tol]` derived from the anchored RTT/jitter, rejects super-human reactions below 100 ms using a
   server-only lower bound, and ranks by reaction from the light.
4. A short collection window (bounded by the slowest pending player's RTT/2 + tol, capped per network profile) closes
   early when everyone pressed; ties inside the measurement uncertainty are resolved by a host-selectable rule with an
   HMAC-seeded, auditable RNG; clean players beat flagged ones.
5. Trust ladder and flags (sync_lie, rtt_inflated, jitter_inflated, out_of_bounds, superhuman, early_bias, bot_like)
   degrade a cheater to server-only or arrival-only scoring; everything is visible to the host (badges, audit log).
6. Compatibility modes remain selectable: `serverArrival`, `randomWindow`, `clientReaction` (unsafe), `writtenAll`.

## Consequences
- Honest players are ranked by their true reaction; residual error is bounded by ±u (shown in the UI) and is zero-mean.
- A browser-JS cheater gains at most ≈ tol per press (≈ 6–10 ms LAN, ≤ 60 ms WAN) and is flagged when persistent.
- Requires the game process to face client sockets directly for the kernel anchor; behind a proxy only the WS-ping
  anchor remains (documented in docs/ops.md).
- Humanized bots pressing on the visual cue are indistinguishable from fast humans by any network scheme; only
  statistics and host judgement apply.
