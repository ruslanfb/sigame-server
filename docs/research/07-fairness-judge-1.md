# Judge 1

Winner: Design 1: SyncedHybrid

## Scores
### Design 1: SyncedHybrid (server-owned clock model + scheduled arming + RTT-bounded trust) — total 35
fairness=9 jitter=8 cheat=5 decisionLatency=8 implRisk=5

Monte Carlo (honest clients, 6 network scenarios, gaps 5–40 ms): the faster-reaction player wins 100 % everywhere; the physically-first player wins 79–99 % — the only loss is the scheduled-light residual (≈2–5 ms) when the physical gap is ≤5–10 ms. This is the only design whose netcode actually resolves WiFi jitter: the client stamp is exact on the player's own clock and jitter is absorbed inside tol. Concrete wrong-winner scenario: B (RTT 80, σ 5, tol 37) presses at 985 (285 ms), A (LAN) at 1000; B's PRESS hits an 80 ms WiFi stall → tArr = 1065, lo = 1028, tFinal_B = 1028 > 1000 → A wins and honest B is flagged out_of_bounds. Any single stall > tol on the press packet flips the result ('spike and lie look identical' is the stated price). Cheat hole, verified analytically: the WS ping/pong RTT is used only as a FLOOR, so a modded client can inflate app rtt_ewma and σ freely (fake c1/c4 on 4 of 5 samples, keep 1 clean so rtt_min/offset stay valid). WiFi RTT 80 → rtt_ewma ≈ 128, σ ≈ 25 → tol 97, tArr moves 24 ms earlier, early_bias threshold widens to −45 → ≈68 ms sustained undetected advantage (claimed 37). LAN: ≈57 ms (claimed 6). Second hole: reaction < 100 ms is CLAMPED to 100 rather than rejected → a client pressing on ARM receipt gets a guaranteed 100 ms reaction (beats every human) for 3 presses, and still gets 100 ms after demotion to serverOnly. Third: asymAllow = 25 % rtt_min (up to 40 ms) is unnecessary — under scheduled arming constant asymmetry shifts the light, not tClient − tArr (sim: 0 % clamps for up60/down20), so it is pure cheat budget. TIE_MS = 2 applies even to two server-sourced presses whose err is ±30–80 ms → false precision; no per-press err. Human foreknowledge with a modded client that lights on receipt ≈ lead_i − OWD ≈ 3σ+15 ms (18–60 ms), undetectable. Decision latency good (early close, ~10 ms LAN, 105 ms in the example, cap 400). Implementation is the heaviest: interleaved 4-tuple sync, drift LS, step detection, per-player ARM timers, 5-state arm machine, trust ledger — but it is fully specified with code, Node/Bun-only, and deterministic-simulator testable.

### Design 2: ServerRewind (zero client timestamps, RTT rewind, equalized ARMED, likelihood window) — total 32
fairness=5 jitter=4 cheat=8 decisionLatency=8 implRisk=7

Monte Carlo (honest clients): LAN–LAN is perfect, but on any jittery link resolution collapses to σ_up. LAN vs WiFi(j12), B faster by 5/10/20/40 ms: correct 73/80/91/99 %. WiFi player faster than a LAN player by 5/10/20 ms: wins only 48/56/75 % — the user's exact complaint ('better ping wins') is softened, not removed. WiFi–WiFi(j20): 55/59/68/81 %. Concrete: A LAN, B WiFi σ 12.7; B physically first by 25 ms but B's uplink jitter is +30 → tPressEst_B = T+295 vs A T+290 → contested (W 29.5) → lottery Φ(5/12.7) gives A 65 %. Light simultaneity rests on ONE ARMED delivery: −25 ms downlink jitter → B sees the light 25 ms early and gains 25 ms in react (clamp ≥ 0 never corrects early arrival); +25 ms late is forgiven only up to jAllow (18) → asymmetric. The likelihood lottery deliberately discards accuracy: vs mostLikely 55/57, 60/64, 70/76, 85/92 % (sim). Cheating: sustained PONG delay = anchor slack − 1 gives the FULL 19 ms (kernel anchor) / 39 ms (handshake anchor) in the reaction metric (both tSeen and tPressEst shift), no flag; plus ACK delay up to jAllow − 1 (10–17 ms) → ≈30–55 ms undetected on a Node/Bun stack. The handshake anchor is defeated by delaying only the TLS handshake (netem/proxy for the first 200 ms) → arbitrary anchor; the 'auto-reconnect when rttMed > anchor for 10 s' hack punishes honest RTT degradation (cold, excluded from contested sets, mid-question reconnect). tieBreak=random + σ inflation (JITTER_INFLATED only triggers > 30 ms without kernel data) → 45 ms coin-flip window. Honest player whose ARMED hits a TCP retransmit loses the question. Strong points: §9 uniform-delay proof holds (verified), react = R exactly under constant asymmetry, reactLB rejection (not clamping), frozen corr, HMAC-reproducible lottery, presumption for clean players. Deployment constraint: kernel anchor needs Go/Rust/uWS FFI and no nginx/Cloudflare in front of the buzzer socket; on Node/Bun only the weak handshake anchor remains. Simplest core to build and replay from logs.

### Design 3: FairBuzz (Sync-Cue / Bounded-Delta / host Tie-Rules) — total 23
fairness=3 jitter=6 cheat=3 decisionLatency=6 implRisk=5

The netcode is essentially Design 1's (claim = armAt + reactionMs is exact for honest clients; with windowMs=0 the sim gives 100 % faster-reaction wins, identical to D1) — and then the game layer throws it away. INPUT_ERR_MS = 20 per press makes the implicit tie window 40 ms even in resolution='first', and the default wifiParty preset uses ≥80 ms + random. Sim: every scenario, every gap ≤ 40 ms, including LAN–LAN → 50 %. Its own worked example crowns the physically-first player 1/3 of the time. The spec also contradicts itself (text says windowMs=0 lets C win; code uses max(wHost, err_i+err_j) = 40). Critical cheat hole: there is NO server-side check that est ≥ armAt + minReaction — minReaction is tested only on the client-claimed reactionMs. A player in serverOnly/unsynced (or anyone via the 'rejected → est' path) sends BUZZ to arrive at armAt + 1 → tPress = armAt + 1 − rtt/2 → wins every button with zero flags in serverOnly (one-line fix, but as written it is broken). Schedule leak: BUTTON_SCHEDULE carries armAt including the 'hidden' random delay 100–600 ms in advance → a modded client showing a countdown lets a human press at armAt + U(100,150): bias is negative (not flagged), MAD non-zero (not robotic) → 100–150 ms undetected edge; the random lamp delay is useless against modded clients. Server RTT measurement is unspecified: PING is client-initiated, so the server has no round trip of its own; if rttEff comes from client-reported values, tol and est are attacker-controlled. rttLast only sees stalls that span a 1 Hz ping; a stall on the BUZZ packet alone → claim rejected → tPress = est (full spike lost, worse than D1's lo clamp) + earlyClaim flag → REJECT_RATIO → honest flaky-WiFi player demoted to serverOnly. σ = sqrt(rttVar) is dimensionally wrong (RTTVAR is a mean deviation in ms). W_eff (30–80) is added to the collect window even on LAN → 45–95 ms decisions. Strong points worth keeping: presets, allPlay for SIQ 'forAll', written duel, open handicap display, REST buzz-log, countdown+first validation, neutral-est replacement for rejected claims (cheater never gets more than an honest player), reasonable bias detector.

## Synthesis

# AnchoredHybrid — Design 1 core, Design 2 anchors/rejection/likelihood, Design 3 game layer

**Why this merge.** Only a client-side reaction stamp resolves WiFi jitter (sim: 100 % vs 55–80 % for pure server rewind at ≤ 20 ms gaps). Its weakness is trust, and Design 1's specific hole is that its tamper-proof RTT (WS ping/pong) is used only as a floor. Close the hole with Design 2's anchor idea, use Design 2's reject-not-clamp rule and per-press error window, shrink the needless asymmetry budget (constant asymmetry cancels under scheduled arming — sim-verified), and put Design 3's host-facing presets/allPlay on top. Ranking key is **reaction from the player's own light**, lights scheduled to one server instant.

## 1. Per-connection clock & RTT model (server-side only)

- **SYNC 4-tuple, interleaved** (D1): client `SYNC{seq,c1,prev:{seq,c4}}` → server stamps `s2`, replies `SYNC_ACK{seq,c1,s2,s3,model}`; client reports `c4` in the next SYNC. Server computes `rtt = (c4−c1)−(s3−s2)`, `off = ((s2−c1)+(s3−c4))/2`. Schedule: burst 10×100 ms on connect/reconnect/tab-visible; 1/s steady; 5×100 ms pre-arm burst when reading starts.
- **Tamper-proof anchor**: server sends **WebSocket protocol ping** every `WS_PING_IDLE=1000` ms, `WS_PING_ARMED=250` ms in reading/armed; the browser's network process answers pong (JS cannot touch it) → `rtt_ws` ring(32), `rtt_ws_min30s`, `rtt_ws_ewma`, `σ_ws`. Optional stronger anchor when the game process terminates TCP/QUIC (Go/Rust/uWS): `rtt_k = tcpi_rtt / quic srtt`, `anchor_hi = rtt_k + 20`.
  - `anchor_lo = rtt_ws_min30s − 1`; `anchor_hi = rtt_ws_ewma + ANCHOR_SLACK(10)` (or `rtt_k + 20`).
  - App sample with `rtt < anchor_lo` → discard + flag `sync_lie`. App `rtt_ewma > anchor_hi` for 3 consecutive 2 s windows → flag `rtt_inflated`.
  - **`rtt_ref = clamp(rtt_app_ewma, anchor_lo, anchor_hi)`; `σ_ref = min(σ_app, σ_ws + 3)`** with `σ = max(rttvar_RFC6298, MAD16)`. (This is the fix: inflating app RTT/σ in JS can no longer widen tol or move tArr.)
- `rtt_min` = sliding 30 s min; `offset = median(off over samples with rtt ≤ rtt_min + max(2, 0.1·rtt_min))`, drift slope by least squares (|b| > 500 ppm → `poor`), step detect (3 samples > u+5 same direction → reset + re-burst) — all D1.
- Derived per player:
  - `resid = clamp(0.10·rtt_min, 2, 10)` (residual margin; NOT 25 %: constant path asymmetry cancels under scheduled arming, verified 0 % clamps for up60/down20).
  - `tol = resid + 3·σ_ref + EPS_INPUT(2)` (LAN ≈ 6, WiFi σ5 ≈ 19, WiFi σ15 ≈ 49, WAN 250/σ15 ≈ 57).
  - `u = resid + 2·σ_ref + 1` (shown in UI), `lead = clamp(rtt_ref/2 + 3·σ_ref + 15, 20, 400)`.
  - `quality`: `unsynced` (< 5 samples in 10 s) → onReceipt arming + server-only scoring; `good/fair/poor` badges (D1 thresholds), broadcast in `CONN_QUALITY` every 5 s.

## 2. Arming (D1 §2 + ARM_ACK)

1. `T_arm = t_readEnd + U(armJitterMin, armJitterMax)` (default 200–1200 ms; `countdown` mode: fixed 1000 ms with visible 3-2-1).
2. Per eligible player: timer at `T_arm − lead_i` sends `BUTTON_ARM{armId(128-bit), questionId, armAt:T_arm, armAtLocal:T_arm−offset_i(T_arm), mode:'scheduled'|'onReceipt', deadlineAt, lockoutMs}`; record real `t_send_i`, freeze snapshot `m_i = {offset, rtt_ref, σ_ref, resid, tol, u, rtt_max16}`, `lateCreditMax_i = max(0, t_send_i + rtt_ref/2 + 3σ_ref − T_arm)`.
3. Client: `setTimeout(delay−4)` + spin to `armAtLocal`, light + audio beep, `litLocal`; immediately `ARM_ACK{armId, recvLocal}` (diagnostics: "late signal ⚠" badge if `t_ackRecv − t_send > rtt_ref + 3σ_ref`; no credit is derived from it).
4. `BUTTON_DISARM` at `T_arm + pressWindowMs`; `draining` for `max_j(rtt_ref_j + 3σ_j) + tol_j`.

## 3. Press (client)

`pointerdown`/`keydown(Space)`, `e.isTrusted` required; `pressLocal = (now − e.timeStamp) ∈ [0,60] ? e.timeStamp : now`; send `PRESS{armId, seq, pressLocal, litLocal, evTs, src}` before rendering; local "pressed — waiting" state. Press before local light is not sent; client sends `MISFIRE{questionId, earlyByMs}` and self-locks (server enforces).

## 4. Server decision function

```ts
onPress(arm, i, msg, tRecv):                       // tRecv stamped before JSON parse
  P = arm.players[i]; m = P.m
  if state ∉ {armed, collecting, draining} or unknown armId → PRESS_ACK{stale}
  if presses.has(i) → duplicate; if lockedUntil_i > tRecv → lockedOut
  tArr     = tRecv − m.rtt_ref/2
  react_lb = (tRecv − m.rtt_max16/2) − arm.T_arm        // most generous, server-only quantities
  if react_lb < −m.u            → reject 'false_start', lockout, broadcast LOCKOUT
  if react_lb < MIN_HUMAN(100)  → reject 'too_early', lockout, strike SUPERHUMAN   // never clamp to 100
  if P.mode=='onReceipt' || trust(i)=='serverOnly':
       tFinal = tArr; err = m.rtt_ref/4 + 2·m.σ_ref + 2; source='server'
  else:
       tClient = msg.pressLocal + m.offset
       credit  = clamp(msg.litLocal − P.armAtLocal, 0, P.lateCreditMax)
       lo = tArr − m.tol; hi = min(tRecv, tArr + m.tol)
       tEst = tClient − credit; tFinal = clamp(tEst, lo, hi)
       if tFinal == tEst: source='client', err = EPS_INPUT(2)
       else:              source='clamped', err = m.tol, flag(i,'out_of_bounds')
       biasRing_i.push(tClient − tArr)                     // honest mean ≈ 0 ± σ
  reaction = tFinal − T_arm
  presses.set(i, {tFinal, reaction, err, source, m, flags})
  PRESS_ACK{pending}; broadcast PRESS_PENDING{playerId}
  if state=='armed': state='collecting', firstRecv=tRecv
  bestT = min(bestT, tFinal); rescheduleClose(arm)

rescheduleClose(arm):
  pending = eligible players (connected, not locked, not pressed)
  if none → resolve()
  waitFor  = max_j∈pending (m_j.rtt_ref/2 + m_j.tol)
  deadline = min(bestT + EPS_INPUT + waitFor, firstRecv + MAX_COLLECT[netProfile])
  timer(deadline → resolve)

resolve(arm):
  ranked = presses sorted by reaction
  lead = ranked[0]
  window(p) = max(lead.err + p.err, room.windowMs)          // windowMs default 0
  tie = ranked.filter(p => p.reaction − lead.reaction ≤ window(p))
  clean = tie.filter(p => p.source=='client' && no flags && !cold); if clean.length → tie = clean   // D2 presumption
  winner = tie.length==1 ? tie[0] : tieBreak(tie, room.tieBreak, rng=HMAC(secret, armId))
  broadcast BUTTON_RESULT{armId, winnerId, contested:tie.length>1, tieBreak, marginMs, windowMs,
                          ranking:[{playerId, reactionMs, errMs, source, rttMs, uMs}], windowMs:decidedAt−firstRecv}

tieBreak: 'likelihood' (default): w_i = Π_{j≠i} Φ((m_j − m_i)/hypot(err_i, err_j)); weighted pick
          'mostLikely' (tournament/LAN: +2–8 % accuracy) | 'random' | 'lowestScore' | 'fewestButtonsWon' | 'rotate' | 'allPlay' (written duel)
```
Late presses (after resolve) → `PRESS_ACK{late, tFinal}`; if they would have beaten the winner by > EPS_INPUT → `late_beat` audit entry. Decisions are final; host has `BUTTON_REOPEN`.

## 5. Anti-cheat ladder (merged)

Flags: `sync_lie`, `rtt_inflated`, `out_of_bounds`, `too_early`/`SUPERHUMAN`, `early_bias` (mean of last 10 `tClient − tArr` < −(σ_ref + resid + 2)), `bot_like` (≥ 8 presses, median < 160 ms, MAD < 15), `untrusted_event` (log only), `stale_nonce`, rate > 5 PRESS/s. Trust score: −1 per flag, +1 per 5 clean presses; ≤ −3 → `serverOnly` scoring (silent, host notified); 3 SUPERHUMAN strikes → `arrivalOnly` for the game; host may kick/reset. Every decision reproducible from `BUTTON_AUDIT{tRecv,tClient,tArr,lo,hi,credit,tFinal,source,model,flags,seed}`. Bounds after the fix: per-press gain ≤ tol_i (6/19/49/57 ms LAN/WiFi5/WiFi15/WAN), sustained undetected ≤ σ_ref + resid + 2 (≈5/12/25/30 ms), human foreknowledge ≤ lead_i − OWD ≈ 3σ + 15 ms.

## 6. Game layer (D3)

- `allPlayFor: ['forAll']` default → `WRITTEN_PHASE/WRITTEN_ANSWER/WRITTEN_RESULT` (client-local reactionMs, ping-free, accuracy scoring; optional speedBonus).
- Presets: `lanWired` (windowMs 0, tieBreak mostLikely, lockout 1000, MAX_COLLECT 200, armJitter 300–800), `wifiParty` (default: windowMs 0, likelihood, lockout 3000, MAX_COLLECT 400, armJitter 200–1200), `internetFair` (windowMs 20, likelihood, MAX_COLLECT 600, warn at RTT > 400, suggest allPlay), `noRace` (allPlay for all).
- `handicap: none|manual|adaptive` (off by default, always shown next to ping). Validation: `countdown` arming forces `windowMs ≥ 30` and `tieBreak ≠ mostLikely`.

## 7. Transport, loss, reconnect

Tiny control WebSocket (JSON/MessagePack, `perMessageDeflate:false`, `TCP_NODELAY`, < 300 B frames), media only over HTTP/CDN. Room = one single-threaded arbiter process (room affinity, monotonic clocks). If the kernel anchor is used, the buzzer socket must terminate in the game process (no nginx/Cloudflare hop); the WS-ping anchor survives frame-forwarding proxies. WAN option: WebTransport datagrams for PRESS/SYNC sent 3× (0/15/30 ms), dedup by `(armId, seq)`. Reconnect: `RESUME{sessionToken,lastSeq}` → `SNAPSHOT{buzzer state, active ARM in onReceipt mode}` → mandatory 10-sample burst, `unsynced` until 5 samples (server-only scoring, `cold` in ties). Disconnected/locked players never extend the window. Background tab: timers throttled → light late → credit 0 (physically consistent) + re-burst on `visibilitychange`.

## 8. Message list

C→S: `HELLO{sessionToken,resume?}`, `SYNC{seq,c1,prev?}`, `ARM_ACK{armId,recvLocal}`, `PRESS{armId,seq,pressLocal,litLocal,evTs,src}`, `MISFIRE{questionId,earlyByMs}`, `WRITTEN_ANSWER{questionId,text,reactionMs}`, `RESUME{sessionToken,lastSeq}`.
S→C: `WELCOME{playerId,syncPlan}`, `SYNC_ACK{seq,c1,s2,s3,model:{offset,rttRef,rttWs,jitter,u,quality}}`, WS ping frames, `QUESTION_READING{questionId,endsAt}`, `BUTTON_ARM{...}`, `PRESS_ACK{armId,seq,status:pending|rejected|duplicate|late|stale|lockedOut,reason?,lockoutUntilLocal?,tFinal?}`, `PRESS_PENDING{armId,playerId}`, `BUTTON_RESULT{...}`, `BUTTON_DISARM{armId,reason}`, `LOCKOUT{playerId,untilAt,durationMs,reason,strike}`, `CONN_QUALITY{players:[{playerId,rttMs,jitterMs,uMs,quality,handicapMs,flags[]}]}`, `WRITTEN_PHASE/WRITTEN_RESULT`, `SNAPSHOT{...}`; host-only `BUTTON_AUDIT`, `INTEGRITY_ALERT{playerId,kind,stats,action}`, `WRITTEN_REVIEW`; host→S `BUTTON_REOPEN{armId}`.
REST (host): `GET /api/v1/buzzer-presets`, `PATCH /api/v1/rooms/{id}/buzzer-settings`, `GET /api/v1/rooms/{id}/buzz-log?questionId=`.

## 9. Constants

| name | value |
|---|---|
| SYNC_BURST | 10 × 100 ms (connect/reconnect/visible); PREARM_BURST 5 × 100 ms; STEADY 1000 ms |
| WS_PING_IDLE / WS_PING_ARMED | 1000 / 250 ms; ANCHOR_SLACK 10 ms (kernel anchor slack 20) |
| RING 64 app / 32 ws samples; RTT_MIN_WINDOW 30 s; EWMA α 1/8, rttvar β 1/4; σ = max(rttvar, MAD16); σ_ref = min(σ_app, σ_ws+3) |
| resid = clamp(0.10·rtt_min, 2, 10); EPS_INPUT 2; tol = resid + 3σ_ref + 2; u = resid + 2σ_ref + 1 |
| lead = clamp(rtt_ref/2 + 3σ_ref + 15, 20, 400); ARM_JITTER U(200,1200) default |
| PRESS_WINDOW 5000; MAX_COLLECT lan 200 / wifi 400 / wan 600; DRAIN = max_j(rtt_ref_j + 3σ_j) + tol_j |
| MIN_HUMAN 100 (reject + lockout); err_client 2; err_server rtt_ref/4 + 2σ_ref + 2; err_clamped tol |
| windowMs default 0 (lanWired 0, internetFair 20); tieBreak default likelihood; RNG = HMAC(secret, armId) |
| LOCKOUT 3000 default (250–3000), ×2 within 10 s, cap 6000 |
| EARLY_BIAS: mean10(tClient−tArr) < −(σ_ref + resid + 2); BOT: ≥8 presses, median<160, MAD<15; TRUST ≤ −3 → serverOnly; 3 SUPERHUMAN → arrivalOnly |
| CONN_QUALITY every 5 s; RATE_LIMIT 5 PRESS/s; UNSYNCED < 5 samples/10 s |

## 10. Host-configurable

`netProfile (lan|wifi|wan)`, `buttonMode (anchoredHybrid|serverOnly|arrival|randomWindow)`, `arming (randomLamp|countdown|immediate)`, `armJitterMs [min,max]`, `windowMs`, `tieBreak`, `allPlayFor`, `writtenAnswerMs`, `writtenScoring`, `handicap`, `falseStartLockoutMs/growth/cap`, `pressWindowMs`, `maxCollectMs`, `minReactionMs` (advanced; ≥ 100 enforced), per-player forced `serverOnly/arrivalOnly`, kick, `BUTTON_REOPEN`. Everything else (tol, lead, err, σ) is derived and only displayed.


## Open risks
- Bots that press at cue + U(110, 200) ms are indistinguishable from an elite human in all three designs and in the synthesis; only statistical badges and host action exist. Any client that can see the cue can automate it.
- The WS ping/pong anchor is defeated by a local TLS-terminating MITM proxy that delays pong frames selectively; only a kernel TCP_INFO / QUIC srtt anchor (Go/Rust/uWS, direct termination, no reverse proxy) closes that. On a Node/Bun stack the residual undetected cheat is ≈ σ_ref + resid + 2 ms sustained and ≤ tol per press (up to ~50–60 ms for a WAN/bad-WiFi cheater).
- A TCP stall (RTO ≥ 200 ms) on the PRESS packet costs an honest player (spike − tol) and produces an out_of_bounds flag; on flaky WiFi this can accumulate trust-score damage. WebTransport datagram triple-send mitigates on WAN but needs HTTP/3 and certificates, which LAN deployments often lack.
- Reaction-from-own-light ranking means the physically-first press can lose when the scheduled lights differ by more than the physical gap (residual 2–5 ms on WiFi, up to u_i ≈ 30–70 ms if the model is stale or the path is unstable). Simulation shows 79–99 % physically-first agreement; the residual is zero-mean but not zero.
- A human using a modded client that lights the button on ARM receipt gains lead_i − OWD ≈ 3σ + 15 ms (18–60 ms) with no detectable signature; shrinking lead increases late-light risk on jittery links.
- The whole scheme depends on one single-threaded arbiter per room with ~1 ms timer accuracy; event-loop stalls (GC, large JSON, media metadata over the same socket) show up as lateCreditMax > 0 and widen windows. Horizontal scaling requires room affinity.
- The 0.10·rtt_min residual margin was validated for constant asymmetry and Gaussian jitter; asymmetric bursty jitter (WiFi power-save on uplink only) could clamp honest presses more often than the sim predicts. Host should be able to raise it via netProfile.
- Likelihood tie-break trades 2–8 % accuracy for cheat-value reduction and perceived fairness; hosts who do not understand 'contested' results may distrust the system. UI must show reactionMs ± err and the rule used.
- The written/allPlay path and the D3 presets add product surface (auto-answer matching, host override) that is out of the netcode's testable core and needs its own QA.
- Model freezes at ARM; a route change between the pre-arm burst and the press (mobile handover) is handled only via tol/σ and step detection, and step detection needs 3 samples (≥ 300 ms in burst) to react.
