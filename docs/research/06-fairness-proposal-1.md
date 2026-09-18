# SyncedHybrid buzzer: server-owned clock model + scheduled arming + RTT-bounded trust in client press stamps (complexity: high)

## Mechanism
## 0. Principles

1. **Server-authoritative, server-owned clock model.** The server measures RTT/offset/jitter for every connection itself. The client never sends "server time" estimates or reaction deltas it computed — only raw local monotonic timestamps (`performance.now()` time base). The server converts them.
2. **Fairness metric = reaction from the player's own light, with all lights scheduled to one common server instant `T_arm`.** Because the client lights the button at `armAtLocal = T_arm − offset_i` and the server converts the press with the same `offset_i`, `reaction_i = pressLocal − armAtLocal` is exact on the player's own monotonic clock; clock-sync error only moves *when* that player's light lit in absolute time (by a few ms), it does not distort the reaction. Ranking key: `t_final_i = T_arm + reaction_i`.
3. **Bounded trust.** The client timestamp is accepted only inside a plausibility interval built from server-side facts: arrival time `t_recv`, `rtt_ewma_i`, jitter `σ_i`, and an asymmetry allowance. Outside → clamped and flagged. A liar can gain at most `tol_i ≈ rtt_min_i/4 + 3σ_i + 2 ms` (≈ 6 ms on LAN).
4. **Collection window sized from RTTs.** After the first press arrives the server waits only as long as a slower player's *earlier* press could still be in flight, capped at `maxCollectMs`.

All times below are ms on the arbiter's monotonic clock (`performance.now()` in Node/Bun; stamp `t_recv` in the raw socket `message` handler before JSON parsing). One arbiter process per room (room affinity), so one clock.

---

## 1. Clock synchronization over WebSocket

### 1.1 Sampling (NTP-style, 4 timestamps, interleaved)
- Client sends `SYNC {seq, c1}` where `c1 = performance.now()`; server stamps `s2` on receipt, replies `SYNC_ACK {seq, c1, s2, s3}` with `s3` taken right before `socket.send`; the client stamps `c4` on receipt and reports it **in the next `SYNC`** as `prev: {seq, c4}`. So the server holds the full 4-tuple for every completed sample and does the math itself:
  - `rtt = (c4 − c1) − (s3 − s2)`
  - `off = ((s2 − c1) + (s3 − c4)) / 2` (server − client), error ≤ `rtt/2`, biased by half the path asymmetry.
- Schedule: burst of 10 samples at 100 ms on connect; steady state 1 sample / 1000 ms; **pre-arm burst** of 5 samples at 100 ms started when question reading begins (so the model is ≤ 1 s old at arming); a burst on `visibilitychange → visible` (background tabs throttle timers and stop sampling).
- Transport-level RTT: server sends **WebSocket ping frames** (`ws.ping()`) every 2 s; the browser's network stack answers pong without JS involvement → `rtt_ws` is a JS-tamper-proof lower bound on the real RTT.

### 1.2 Filtering (per connection, server side)
```
ring        = last 64 samples (~1 min), drop samples with rtt < rtt_ws_min − 1 (impossible → flag)
rtt_min     = min(rtt) over the last 30 s (sliding, so route changes are followed)
rtt_ewma    = EWMA(rtt, α = 1/8);  rttvar = EWMA(|rtt − rtt_ewma|, β = 1/4)      // RFC 6298 style
σ           = max(rttvar, MAD(last 16 rtt))                                           // jitter estimate
S           = samples with rtt ≤ rtt_min + max(2, 0.1·rtt_min);  if |S| < 3 → lowest-RTT quartile
offset      = median(off over S)                    // Cristian/timesync-style min-RTT filtering
drift b     = least-squares slope of off vs c1 over the ring (ppm); |b| > 500 ppm → quality "poor"
offset(t)   = offset + b·(t − t_median_of_S)        // drift-corrected prediction used at arm time
step detect = 3 consecutive samples deviating from offset(t) by > u_i + 5 ms in the same direction → reset ring, re-burst
asymAllow   = clamp(0.25·rtt_min, 2, 40)            // honest path asymmetry we tolerate
u_i         = asymAllow + 2σ + 1                    // stated offset uncertainty (shown in UI)
tol_i       = asymAllow + 3σ + EPS_INPUT(2)         // trust radius for client stamps
quality     = unsynced (<5 samples in 10 s) | good (rtt_min<30 && σ<5) | fair (σ<20) | poor
```
Every `SYNC_ACK` also returns `{offset, rttMin, rttEwma, jitter, u, quality}` so the client can show "ping 43 ms ± 6, sync ±9 ms" and so the client and server share the same model (the client never needs its own filter).

---

## 2. Scheduled arming ("button active at T")

1. When reading ends at `t_readEnd`, the arbiter picks `T_arm = t_readEnd + U[0, armingJitterMs]` (default 0–1500 ms when false-starts are on; kills anticipation of the reading rhythm).
2. For each eligible player `i` it computes `lead_i = clamp(rtt_ewma_i/2 + 3σ_i + 15, 20, 400)` and **sends `BUTTON_ARM` individually at `T_arm − lead_i`** (a timer per player) so the message arrives just before `T_arm` (minimal foreknowledge, tolerant to jitter). Payload carries `armAtLocal_i = T_arm − offset_i(T_arm)` (already in the client's own clock), `armAt = T_arm`, `armId` (128-bit nonce), `deadlineAt`.
3. The server records actual `t_send_i` (in the send callback) and freezes a model snapshot `m_i = {offset, rttMin, rttEwma, σ, asymAllow, tol, u}` in the arm record; also `lateCreditMax_i = max(0, t_send_i + rtt_ewma_i/2 + 3σ_i − T_arm)` — the most the light could plausibly lit late *without the client lying*. In normal operation this is 0 (only >0 when the server's own send was delayed).
4. Client: `delay = armAtLocal − performance.now()`; if `delay ≤ 0` light immediately; else `setTimeout(delay − 4)` then a short `requestAnimationFrame`/spin until `performance.now() ≥ armAtLocal`, then set the lit state (plus an audio beep — audio latency is more uniform than 60 Hz display latency). Record `litLocal`.
5. Unsynced players (quality = unsynced) get `mode: "onReceipt"` and their presses are scored server-only (see §4).
6. `BUTTON_DISARM {armId, reason}` at `T_arm + pressWindowMs`; the arm then stays in `draining` for `drainMs = max_j(rtt_ewma_j + 3σ_j) + tol_j` to accept in-flight presses with `t_final ≤ T_disarm`, then `BUTTON_RESULT {winner: null}`.

---

## 3. Client press

- Listen on `pointerdown` (not `click`, ~100 ms earlier for taps) and `keydown` (Space). Require `e.isTrusted`.
- `pressLocal = (now − e.timeStamp) ∈ [0, 60] ? e.timeStamp : now` (`e.timeStamp` is on the same DOMHighResTimeStamp base as `performance.now()` and precedes the handler by a few ms; Chrome 100 µs, Firefox/Safari 1 ms resolution — fine).
- Send `PRESS {armId, seq, pressLocal, litLocal, evTs: e.timeStamp, src}` immediately, before any rendering. One press per arm; UI shows "pressed — waiting for slower links".
- Presses before the local light (false-start mode) are not sent; the UI shakes and the client self-blocks for `lockoutMs` (server enforces anyway).

---

## 4. Server decision function

```ts
// --- state per arm ---
interface Arm { armId; questionId; T_arm; state: 'scheduled'|'armed'|'collecting'|'draining'|'resolved';
  players: Map<PlayerId, { m: ModelSnapshot; armAtLocal: number; tSend: number; lateCreditMax: number; mode: 'scheduled'|'onReceipt' }>;
  presses: Map<PlayerId, Press>; firstRecv?: number; bestT: number; closeTimer?: Timer }

const EPS_INPUT = 2, TIE_MS = 2, MIN_HUMAN = 100, MAX_COLLECT = 400;
const clamp = (x, lo, hi) => Math.min(hi, Math.max(lo, x));

function onPress(arm: Arm, i: PlayerId, msg: PressMsg, tRecv: number) {
  const P = arm.players.get(i);
  if (!P || !['armed','collecting','draining'].includes(arm.state)) return misfire(i, msg, tRecv);   // §4.2
  if (arm.presses.has(i)) return ack(i, 'duplicate');
  const { m } = P;

  const tArr = tRecv - m.rttEwma / 2;                              // server-only estimate (rewind by one-way delay)
  const lo   = tArr - m.tol;                                       // trust bounds
  const hi   = Math.min(tRecv, tArr + m.tol);                      // causality: cannot press after we received it
  let tFinal: number, source: 'client'|'clamped'|'server';

  if (P.mode === 'onReceipt' || trust(i) === 'serverOnly') {
    tFinal = tArr; source = 'server';
  } else {
    const tClient = msg.pressLocal + m.offset;                     // server converts the client's local stamp
    const credit  = clamp(msg.litLocal - P.armAtLocal, 0, P.lateCreditMax); // late light, bounded by server facts
    const tEst    = tClient - credit;
    tFinal = clamp(tEst, lo, hi);
    source = tFinal === tEst ? 'client' : 'clamped';
    if (source === 'clamped') flag(i, 'out_of_bounds', { tEst, lo, hi });
    biasDetector(i, tClient - tArr, m);                            // §anti-cheat
  }

  let reaction = tFinal - arm.T_arm;
  if (reaction < -m.u)        return falseStart(i, arm, tRecv);    // pressed before light (modded client / desync)
  if (reaction < MIN_HUMAN) { tFinal = arm.T_arm + MIN_HUMAN; reaction = MIN_HUMAN; flag(i, 'superhuman', { reaction }); }

  const press = { i, seq: msg.seq, tRecv, tFinal, reaction, source, model: m };
  arm.presses.set(i, press);
  audit(arm, press);
  send(i, PRESS_ACK{ armId, seq, status: 'pending' }); broadcast(PRESS_PENDING{ armId, playerId: i });

  if (arm.state === 'armed') { arm.state = 'collecting'; arm.firstRecv = tRecv; }
  arm.bestT = Math.min(arm.bestT ?? Infinity, tFinal);
  rescheduleClose(arm);
}

function rescheduleClose(arm: Arm) {
  // players whose press, if it were still in flight, could still land at or before bestT (+tie band)
  const pending = [...arm.players].filter(([j]) => !arm.presses.has(j) && eligible(j));   // not locked out, connected
  if (pending.length === 0) return resolve(arm);
  const waitFor = Math.max(...pending.map(([,P]) => P.m.rttEwma / 2 + P.m.tol));     // ≈ rtt_j + 3σ_j + asym_j
  const deadline = Math.min(arm.bestT + TIE_MS + waitFor, arm.firstRecv + MAX_COLLECT);
  clearTimeout(arm.closeTimer); arm.closeTimer = setTimeout(() => resolve(arm), deadline - now());
}

function resolve(arm: Arm) {
  arm.state = 'resolved';
  const ranking = [...arm.presses.values()].sort((a, b) => a.tFinal - b.tFinal);
  const tieSet  = ranking.filter(p => p.tFinal - ranking[0].tFinal <= tieMs(ranking[0], p));
  const winner  = tieSet.length === 1 ? tieSet[0] : tieBreak(tieSet, room.tieBreak); // random|lowestScore|fewestButtonsWon|allPlay
  broadcast(BUTTON_RESULT{ armId, winnerId: winner?.i ?? null, tie: tieSet.length > 1,
    ranking: ranking.map(p => ({ playerId: p.i, reactionMs: round(p.reaction, 1), source: p.source, rttMs: p.model.rttEwma, uMs: p.model.u })) });
}
// tieMs: fixed TIE_MS (2 ms, timer-resolution ties) by default; room option tieMode:'uncertainty' → clamp(max(u_i,u_j), 2, 40)
```
Late presses (arriving after `resolve`) get `PRESS_ACK{status:'late', tFinal}`; if `tFinal < winner.tFinal − TIE_MS` an audit entry `late_beat` is written for the host panel — decisions are final (NAQT principle), the host has a manual "re-open button" override.

### 4.2 Misfire / false start
Press with no active arm (or before `T_arm − u_i`): `PRESS_ACK{status:'rejected', reason:'false_start', lockoutUntilLocal}`; broadcast `FALSE_START{playerId, lockoutMs}`; lockout = `falseStartLockoutMs` (default 3000, SIGame; Jeopardy-style 250 possible), doubled for each repeat within 10 s, cap 6000. Locked-out players are removed from `eligible` for this arm (so the window does not wait for them).

---

## 5. Worked example (verified with a simulation of the code above)

Players: **A** RTT 10 ms (σ 0.5), **B** RTT 80 ms (σ 5), **C** RTT 250 ms (σ 15); symmetric paths. `T_arm = 700`. Model derived values:

| | asymAllow | tol | u | lead | armAtLocal sent at |
|---|---|---|---|---|---|
| A | 2.5 | 6.0 | 4.5 | 21.5 | 678.5 |
| B | 20 | 37 | 31 | 70 | 630 |
| C | 40 | 87 | 71 | 185 | 515 |

Physical presses (server clock): A 1000, B 990, C 985. Arrivals `t_recv = press + rtt/2`: **A 1005, B 1030, C 1110** — pure arrival order (`FirstWins`) would crown A, who reacted last. Assume residual offset errors after min-RTT filtering of +0.3 / −0.5 / +1.0 ms (these also shift each player's light by the same amount, so the reactions are measured exactly).

| player | t_recv | tClient = pressLocal+offset | tArr = t_recv−rtt/2 | [lo, hi] | tFinal | reaction | source |
|---|---|---|---|---|---|---|---|
| A | 1005 | 1000.3 | 1000 | [994.0, 1005] | **1000.3** | 300.3 | client |
| B | 1030 | 989.5 | 990 | [953, 1027] | **989.5** | 289.5 | client |
| C | 1110 | 986.0 | 985 | [898, 1072] | **986.0** | 286.0 | client |

Collection window: A arrives at 1005 → `bestT = 1000.3`, pending {B, C} → `deadline = 1000.3 + 2 + max(40+37, 125+87) = 1214.3` (cap 1405). B arrives at 1030 → `bestT = 989.5`, pending {C} → `deadline = 1203.5`. C arrives at 1110 → nobody pending → resolve at **1110**. Ranking `C (986.0) < B (989.5) < A (1000.3)`; gap C–B = 3.5 ms > TIE_MS → **winner C**, exactly the player who physically pressed first. Result published 105 ms after A's press reached the server (SIGame's default already waits 300 ms). Had C not pressed, B would have been declared at 1203.5; on a wired LAN (all RTT < 5 ms) the wait shrinks to ~10 ms.

Cheat bound in this example: a lying A can push `tFinal` no lower than `lo_A = 994.0` (max gain 6.3 ms) — still behind B and C. B's floor is 953 (36.5 ms budget), C's 898 (88 ms budget): budgets grow with a player's *own* RTT/jitter; that is the residual attack surface handled by the statistical checks (anti-cheat section).

Honest caveat: with a 250 ms link, an offset error of +6 ms on C (plausible on an asymmetric WAN path) would have ranked B first. A 5 ms physical difference is below what any network scheme can resolve at that RTT; what the design guarantees is that the error is **zero-mean and independent of who has the better ping** (A never gets a systematic edge), and that the residual is bounded by `u_i` and shown to players.

---

## 6. Packet loss, delays, reconnect

- **WebSocket = TCP → no loss, only delay spikes** (retransmit RTO ≥ ~200 ms). A delayed PRESS that still lands inside the window is scored with its client stamp but the clamp floor `lo = tArr − tol` moves with the spike, so the player loses `(spike − tol)` ms — the unavoidable price of bounded trust (a spike and a lie look identical). A PRESS after `resolve` → `late`. A delayed ARM → the light lits late; credit is limited to `lateCreditMax` (≈0) → the player loses that time and sees a "late signal ⚠" indicator; quality drops to `fair/poor`.
- **Keep the control socket tiny**: media (images/audio/video/GIF) never travel over the buzzer WebSocket — HTTP/CDN for media, `perMessageDeflate: false`, `TCP_NODELAY`, messages < 300 bytes, no head-of-line blocking from big frames. WAN option: a WebTransport datagram channel for `PRESS` sent 3× (0/15/30 ms), deduped by `(armId, seq)`, earliest arrival wins.
- **Reconnect**: resumable session token; a new socket is a new path → discard the model, run the 10-sample burst; until ≥ 5 samples the player is `unsynced` (ARM in `onReceipt` mode, press scored `server`). If an arm is `armed/collecting` when the player resumes, `BUTTON_ARM` is re-sent in `onReceipt` mode. Presses carrying an `armId` that is no longer active → `stale`. Disconnected players are dropped from `eligible` so the window never waits for them.
- **Server hiccups**: `t_recv` is stamped before parsing; ARM `t_send` is the real send time, so a slow event loop shows up as `lateCreditMax > 0` (players are credited) rather than as a silent penalty. Arms are keyed by nonce, the room actor is single-threaded, all timers are monotonic.

---

## 7. Room-level modes (API surface)
`buttonMode`: `syncedHybrid` (default, this design) | `serverOnly` (tFinal = tArr, no client stamps, BuzzIn.Live-like) | `arrival` (= SIGame `FirstWins`) | `clientReaction` (= SIGame `FirstWinsClient`, tol = ∞, compatibility only) | `randomWindow` (= `RandomWithinInterval`: tieMs = 300, tieBreak random). `tieBreak`: `random` (default) | `lowestScore` | `fewestButtonsWon` | `allPlay` (everyone in the tie set answers in writing). `tieMode`: `resolution` (2 ms, default) | `uncertainty` (clamp(max(u_i,u_j), 2, 40)).

## Protocol
## Transport
One small-message WebSocket per client (JSON or MessagePack, `perMessageDeflate: false`), protocol-level ping/pong every 2 s from the server. Media is served over HTTP, never on this socket. All `*Local` fields are the client's `performance.now()` base; all `*At` fields are arbiter server time.

## Message sequence

```
CLIENT                                                   SERVER (room arbiter)
  |-- HELLO {sessionToken, resume?} ------------------------>|
  |<-- WELCOME {playerId, syncPlan:{burst:10, every:100, steady:1000}} 
  |                                                          |
  |  [clock sync, repeated: burst on connect, 1/s steady,    |
  |   5-burst when reading starts, burst on tab visible]     |
  |-- SYNC {seq:12, c1:5031.4, prev:{seq:11, c4:4041.9}} -->|  stamps s2; completes sample 11 = (c1,s2,s3,c4)
  |<-- SYNC_ACK {seq:12, c1:5031.4, s2:80031.9, s3:80032.0,  |  filter: rtt_min, rtt_ewma, σ, offset(t), u, quality
  |      model:{offset:75000.2, rttMin:9.6, rttEwma:10.4,    |
  |             jitter:0.5, u:4.5, quality:"good"}}          |
  |<== WS ping frame ===================================     |  browser auto-pongs → rtt_ws (JS cannot touch it)
  |                                                          |
  |<-- QUESTION_READING {questionId, endsAt}                 |  (pre-arm sync burst starts)
  |                                                          |  T_arm = t_readEnd + U[0, armingJitterMs]
  |                                                          |  per player i at T_arm − lead_i:
  |<-- BUTTON_ARM {armId:"8f3c…", questionId, armAt:700.0,   |  lead_i = clamp(rttEwma/2 + 3σ + 15, 20, 400)
  |      armAtLocal:678.5, mode:"scheduled",                 |  record t_send_i, freeze model snapshot m_i,
  |      deadlineAt:5700.0, lockoutMs:3000}                  |  lateCreditMax_i = max(0, t_send_i + rttEwma/2 + 3σ − T_arm)
  |  (client lights button exactly at armAtLocal;            |
  |   if received late → lights now, litLocal = now)         |
  |-- ARM_ACK {armId, recvLocal} (optional diagnostics) --->|
  |                                                          |
  |  pointerdown / keydown (isTrusted)                       |
  |-- PRESS {armId, seq:1, pressLocal:978.9, litLocal:678.5, |  stamps t_recv before parsing
  |          evTs:977.6, src:"pointer"} -------------------->|  tClient = pressLocal + m.offset
  |                                                          |  tArr = t_recv − rttEwma/2; lo/hi = tArr ∓ tol (hi ≤ t_recv)
  |                                                          |  tFinal = clamp(tClient − credit, lo, hi); reaction = tFinal − T_arm
  |<-- PRESS_ACK {armId, seq, status:"pending"}              |  (or "rejected" {reason:"false_start", lockoutUntilLocal})
  |<-- PRESS_PENDING {armId, playerId}   (broadcast)         |  window: deadline = min(bestT + tieMs + max_j(rttEwma_j/2 + tol_j),
  |                                                          |                        firstRecv + maxCollectMs); early close when nobody pending
  |<-- BUTTON_RESULT {armId, winnerId:"C", tie:false,        |
  |      ranking:[{playerId:"C", reactionMs:286.0, source:"client", rttMs:250, uMs:71},
  |               {playerId:"B", reactionMs:289.5, ...}, {playerId:"A", reactionMs:300.3, ...}],
  |      windowMs:105}                                       |
  |                                                          |
  |<-- BUTTON_DISARM {armId, reason:"timeout"|"answered"|"cancelled"}   (arm stays 'draining' for max_j(rtt_j+3σ_j)+tol_j)
  |<-- FALSE_START {playerId, lockoutMs}  (broadcast)        |
  |<-- CONN_QUALITY {playerId, rttMs, jitterMs, uMs, quality} (broadcast every 5 s → shown next to each name, as Protobowl does)
```

## Field reference (compact)
- `SYNC`: `seq:int`, `c1:number`, `prev?:{seq:int, c4:number}`
- `SYNC_ACK`: `seq, c1, s2, s3, model:{offset, rttMin, rttEwma, jitter, u, quality:"unsynced"|"good"|"fair"|"poor"}`
- `BUTTON_ARM`: `armId:uuid/nonce128, questionId, armAt, armAtLocal, mode:"scheduled"|"onReceipt", deadlineAt, lockoutMs`
- `PRESS`: `armId, seq:int, pressLocal, litLocal, evTs, src:"pointer"|"key"|"gamepad"`
- `PRESS_ACK`: `armId, seq, status:"pending"|"rejected"|"duplicate"|"late"|"stale", reason?, lockoutUntilLocal?, tFinal?`
- `BUTTON_RESULT`: `armId, winnerId|null, tie:boolean, tieBreak?:"random"|"lowestScore"|"fewestButtonsWon"|"allPlay", ranking:[{playerId, reactionMs, source:"client"|"clamped"|"server", rttMs, uMs}], windowMs`
- Host-only `BUTTON_AUDIT {armId, entries:[{playerId, tRecv, tClient, tArr, lo, hi, credit, tFinal, source, flags[]}]}` and `BUTTON_REOPEN {armId}` (manual override).

## Reconnect
`HELLO{resume}` → `WELCOME` → mandatory burst (model discarded) → while `quality=unsynced` the player receives `BUTTON_ARM{mode:"onReceipt"}` and is scored server-only; an active arm is re-sent on resume; `PRESS` for an inactive `armId` → `PRESS_ACK{status:"stale"}`.

## Parameters
- SYNC_BURST_COUNT = 10, SYNC_BURST_INTERVAL_MS = 100 (on connect / reconnect / tab visible)
- SYNC_STEADY_INTERVAL_MS = 1000; SYNC_PREARM_BURST = 5 samples at 100 ms, started when question reading begins
- WS_TRANSPORT_PING_MS = 2000 (protocol ping frames → rtt_ws lower bound; app sample with rtt < rtt_ws_min − 1 ms is discarded and flagged)
- MODEL_RING_SAMPLES = 64; RTT_MIN_WINDOW_MS = 30000 (sliding min so route changes are followed)
- RTT_EWMA_ALPHA = 1/8, RTTVAR_BETA = 1/4 (RFC 6298); σ = max(rttvar, MAD of last 16 RTTs)
- OFFSET_SELECT: samples with rtt ≤ rtt_min + max(2 ms, 0.10·rtt_min), min 3, else lowest-RTT quartile; offset = median; drift slope via least squares, |slope| > 500 ppm → quality 'poor'
- STEP_DETECT: 3 consecutive samples deviating > u_i + 5 ms in one direction → reset ring and re-burst
- ASYM_ALLOW_i = clamp(0.25·rtt_min_i, 2, 40) ms
- EPS_INPUT_MS = 2 (timer/event-dispatch resolution)
- u_i (offset uncertainty, shown in UI) = asymAllow_i + 2σ_i + 1 ms
- tol_i (trust radius) = asymAllow_i + 3σ_i + EPS_INPUT_MS  → ≈ 6 ms on wired LAN, ≈ 37 ms at RTT 80/σ 5, ≈ 87 ms at RTT 250/σ 15
- ARM_LEAD_i = clamp(rtt_ewma_i/2 + 3σ_i + 15, 20, 400) ms; ARM sent per player at T_arm − lead_i
- ARMING_JITTER_MS = 0..1500 uniform (false-start mode; 0 disables randomization)
- PRESS_WINDOW_MS (button stays armed) = 5000 default; DRAIN_MS after disarm = max_j(rtt_ewma_j + 3σ_j) + tol_j
- MAX_COLLECT_MS = 400 (cap on the fairness window after the first press; range 100–600); early close as soon as no eligible player is pending
- TIE_MS = 2 (default, resolution ties); tieMode 'uncertainty' → clamp(max(u_i, u_j), 2, 40); randomWindow compatibility mode → 300
- TIE_BREAK = random | lowestScore | fewestButtonsWon | allPlay (default random)
- MIN_HUMAN_REACTION_MS = 100 (clamped + flag 'superhuman'); FAST_REACTION_MS = 150 (counted for stats)
- FALSE_START_LOCKOUT_MS = 3000 default (SIGame), options 250 (Jeopardy) … 3000; ×2 per repeat within 10 s, cap 6000
- ELIGIBILITY: ≥ 5 sync samples in the last 10 s, else mode 'onReceipt' + server-only scoring; disconnected players excluded from the window wait
- BOT_STATS: ≥ 8 presses in a game with median reaction < 160 ms AND MAD < 15 ms → 'bot-like' badge for the host
- BIAS_DETECT: running mean over last 10 presses of (tClient − tArr) < −(asymAllow_i + σ_i) → flag 'early_bias'
- TRUST_SCORE: each flag −1, +1 per 5 clean presses; score ≤ −3 → player switched to server-only scoring (host notified); host may kick or reset
- RATE_LIMIT: 1 accepted PRESS per arm; > 5 PRESS/s → drop + flag
- QUALITY_THRESHOLDS: good = rtt_min < 30 && σ < 5; fair = σ < 20; poor otherwise; CONN_QUALITY broadcast every 5 s

## Anti-cheat
**Threat model**: modded/scripted clients (SIGame already has a pixel-polling autoclicker, `sigame-click`, 1 ms polling), timestamp forgery, sync manipulation, replay/spam, anticipation of the arm moment.

1. **Server owns the clock model.** The client never sends server-time estimates or reaction deltas (unlike SIGame `FirstWinsClient`). It sends raw monotonic stamps; the server converts them with its own filtered offset and freezes the model per arm, so mid-window sync updates cannot move a conversion.
2. **Bounded trust on the press stamp.** `tFinal = clamp(tClient − credit, tArr − tol_i, min(t_recv, tArr + tol_i))` with `tol_i = asymAllow_i + 3σ_i + 2 ms`. A liar's maximum gain is `≈ tol_i` (~6 ms on wired LAN, ~37 ms at RTT 80, ~87 ms at RTT 250) — you can only "steal" a fraction of your own RTT, and a player on a bad link is exactly the player whose honest presses carry that same uncertainty. Every clamp is logged as `out_of_bounds` and decrements the trust score.
3. **Sync manipulation bound (tamper-proof floor).** WebSocket protocol ping/pong is answered by the browser network stack; JS cannot delay or forge it. App-level samples with `rtt < rtt_ws_min − 1 ms` are impossible → discarded + flag `sync_lie`. Inflating `c1` (to shrink apparent RTT / shift the offset) is thereby limited to the JS event-loop slack (a few ms). Sudden RTT drops > 20 % after ≥ 20 samples need 5 confirmations before the model follows them (route change vs. manipulation).
4. **Late-light credit is server-bounded.** `credit ≤ lateCreditMax_i = max(0, t_send_i + rtt_ewma_i/2 + 3σ_i − T_arm)`, computed from the server's own send time; a client claiming "my light was late" gains nothing unless the server itself sent late.
5. **Foreknowledge minimized + anticipation killed.** ARM is sent per player only `lead_i` (≈ one-way delay + 3σ + 15 ms) before `T_arm`, and `T_arm` includes a uniform 0–1500 ms random delay after reading ends; nobody can pre-press on rhythm, and a modded client that lights on receipt gains at most `lead_i`.
6. **Reaction floor and statistics.** `reaction < 100 ms` is physically impossible for a human reacting to an unpredictable light → clamped to 100 ms + flag `superhuman`. Per-game per-player stats (median, MAD of reactions): median < 160 ms with MAD < 15 ms over ≥ 8 presses → `bot-like` badge (humans: median 230–270 ms, SD ≈ 40 ms). `early_bias` detector: mean of `(tClient − tArr)` over 10 presses below `−(asymAllow_i + σ_i)` (honest asymmetry stays within asymAllow; a cheater hugging the floor does not).
7. **Trust score → automatic degradation.** Flags accumulate (−1 each, +1 per 5 clean presses); at ≤ −3 the player is silently switched to `serverOnly` scoring (`tFinal = tArr`, no client input used); the host sees the reason and can kick/reset. Rooms can set `buttonMode: serverOnly` globally (tournament mode) — zero trust in clients, at the cost of jitter-sized errors.
8. **Integrity of messages.** `armId` is a 128-bit nonce sent only to eligible players; PRESS with an unknown/expired `armId` → `stale`/flag; one accepted press per `(armId, playerId)`, `seq` dedupes retransmits; rate limit 5 PRESS/s; `e.isTrusted` and `evTs` plausibility (`now − evTs ∈ [0, 60]`) enforced client-side (bypassable by a mod, which is why 2–7 exist).
9. **Auditability instead of disputes.** Every press stores `tRecv, tClient, tArr, lo, hi, credit, tFinal, source, model snapshot, flags`; `BUTTON_AUDIT` shows the host "why C won" (reactions, ±u, which stamps were clamped, late presses that would have beaten the winner). Decisions are final (as NAQT/BuzzIn.Live), with a manual `BUTTON_REOPEN` for egregious cases.
10. **Environment hygiene.** Same time base on the client (`performance.now()` — monotonic, immune to wall-clock steps); background-tab throttling detected via missing samples → re-burst; single arbiter process per room; `t_recv` stamped before parsing so server load cannot bias players differently.

## Tradeoffs
**What you get**: no systematic ping advantage — the ranking key is each player's reaction from their own light, and lights are scheduled to a common instant; residual error is zero-mean and bounded by `u_i = asymAllow + 2σ + 1` (≈ 2–5 ms on wired LAN, 10–30 ms on Wi‑Fi, 30–70 ms on a 250 ms WAN link). Cheating is bounded by the cheater's own RTT and caught statistically. Results arrive ≤ `max RTT + jitter` (cap 400 ms) after the first press, typically < 20 ms on LAN.

**What it costs / cannot do**:
- **Residual noise is real at high RTT.** Path asymmetry is unobservable; at 250 ms RTT two presses 5 ms apart are genuinely inside the noise. Default `tieMode: resolution` decides them by point estimate (unbiased but noisy); `tieMode: uncertainty` turns them into coin flips/lowest-score, which can make one bad link degrade everyone's game into `RandomWithinInterval`. Either way, tell users (as NAQT does) that Wi‑Fi jitter of 20–50 ms cannot be compensated better than 20–50 ms; recommend Ethernet/5 GHz.
- **A delay spike and a lie look identical.** Bounded trust clamps an honest press that got stuck behind a TCP retransmit by `(spike − tol)`. Mitigations (WebTransport datagrams, tiny control socket) reduce but do not remove this.
- **High-RTT players have a larger cheat budget** (`tol_i` grows with RTT/jitter). Only statistics and trust scores close that gap; a careful cheater on a bad link who stays within `tol` and above 150 ms reactions is not provably detectable — but such a cheater gains no more than an honest player on that same link could have obtained by luck.
- **Foreknowledge window `lead_i`** (≈ one-way delay + 3σ + 15 ms) exists by construction; a modded client that lights on receipt gains up to that amount.
- **Decision latency**: the fairness window delays the "who pressed" reveal by up to ~max RTT + jitter (capped 400 ms) versus instant `FirstWins`; `PRESS_PENDING` feedback hides most of it, and SIGame's default already waits 300 ms.
- **Browser limits**: Firefox/Safari 1 ms timer resolution (hence TIE_MS = 2), background tabs stop syncing, 60 Hz displays add ±8 ms to when the light is actually seen (identical for everyone on average; the audio beep helps).
- **Engineering complexity**: per-connection filter with drift/step handling, per-player ARM timers, arm state machine (scheduled → armed → collecting → draining → resolved), trust scoring, audit log, room affinity for the arbiter process; ~800–1200 lines plus a deterministic simulator for tests. Simpler fallbacks (`serverOnly`, `arrival`, `randomWindow`) are exposed as room modes for rooms that do not need this precision.
