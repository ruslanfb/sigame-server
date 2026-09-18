# Judge 3

Winner: Design 1: SyncedHybrid

## Scores
### Design 1: SyncedHybrid (server-owned clock model + scheduled arming + RTT-bounded trust in client press stamps) — total 31
fairness=8 jitter=7 cheat=4 decisionLatency=8 implRisk=4

Best fairness metric of the three: ranking key = reaction on the player's own monotonic clock from a light scheduled to one server instant, so clock-offset error cancels out of the reaction (I checked the algebra: a consistent clock shift or an offset error moves the light and the converted press by the same amount, reaction stays exact). I re-ran the worked example and a Monte Carlo (A LAN rtt 10; B Wi-Fi rtt 80 with 15% bufferbloat spikes up to 60 ms; B physically 15 ms ahead): D1 picks the wrong winner 0.2% of the time; with rare spikes that the 16-sample sigma under-estimates (5%, up to 100 ms) it is 2.2% wrong and only ~3% of honest presses get clamped/flagged, so the trust ladder rarely fires on honest players. Remaining honest-fairness costs: an ARM that arrives later than the 3σ+15 margin gets no credit (heavy-tailed Wi-Fi → 5–15% of arms late by up to ~40 ms), absolute-time error at WAN is bounded by u_i but unobservable path asymmetry is real, and a spike beyond tol looks like a lie and costs (spike − tol). CHEAT BREAK (severe, contradicts the stated 6 ms LAN bound): σ = max(rttvar, MAD) is computed from client-timed SYNC samples and is uncapped; the ws-ping floor only guards RTT lies downward. A modded client that delays every second SYNC reply by 200 ms gets σ ≈ 107 (simulated), tol ≈ 326 ms, bias threshold −109 ms, u ≈ 217 (false-start floor loosened) and lead ≈ 341 ms, while offset selection (low-RTT half) stays accurate — so it can shave ~100 ms per press with no flag ('poor' quality is only a label), bounded only by MIN_HUMAN: max gain ≈ reaction − 100 ≈ 150 ms, i.e. it wins every race. The same trick griefs the room: its pending-wait becomes rtt/2 + tol ≈ 330 ms, so every decision hits the 400 ms cap. Secondary holes: superhuman presses are clamped to T_arm+100 and KEPT (they still win, only flagged) instead of rejected; the trust ladder (−1/flag, +1 per 5 clean) permits one full-tol steal per 5 presses indefinitely; foreknowledge 3σ+15 for a light-on-receipt mod. Implementation: the most complex of the three (~1000 LOC: 4-tuple sync with deferred c4, drift least-squares + step detection, per-player ARM timers, client setTimeout+rAF spin, 5-state arm machine, trust score, audit). Concrete trap: armAtLocal uses drift-predicted offset(T_arm) while conversion uses m.offset — if they differ the reaction is biased; one frozen value must be used for both. Drift LS is over-engineering for a 30 s window (100 ppm × 15 s ≈ 1.5 ms). Good: single arbiter, t_recv before parse, all decision logic is pure server code testable with a deterministic simulator; background-tab handled via visibility re-burst; reconnect = 1 s unsynced server-only.

### Design 2: ServerRewind (server RTT compensation with bounded correction, equalized ARMED, simultaneity window, calibrated tie-break; no client timestamps) — total 28
fairness=6 jitter=4 cheat=5 decisionLatency=7 implRisk=6

Conceptually the cleanest trust model: only message arrival times matter, corr is frozen per arm, the lottery is HMAC-seeded and reproducible, no client timers at all (immune to background-tab throttling and Firefox timer precision), and the 'uniform network delay gives no advantage' proof is correct. Easiest to test deterministically. FAIRNESS BREAK: with no client stamp, every ms of uplink jitter on PRESS is ranking error, and the MAD-based σ ignores exactly the heavy tail that Wi-Fi produces, so W is too narrow to catch it. Monte Carlo (A LAN rtt 10, B Wi-Fi rtt 80 with 15% spikes ≤ 60 ms, B physically 15 ms first): A is declared the OUTRIGHT winner 13% of the time, tie 7%; B 30 ms ahead: 8% wrong outright; with 30% spikes ≤ 100 ms: 14% wrong, 59% lottery; B 5 ms ahead: 14% wrong, 77% lottery. Late ARMED is forgiven only jAllow ≈ 15–20 ms, so one 60 ms spike on ARMED costs an honest player the question. The likelihood tie-break makes the noise unbiased but does not make it small. CHEAT BREAK (deployment-dependent): the claimed 20/40 ms bound exists only with an in-process TCP_INFO/QUIC anchor (Go/Rust/uWS with FFI) or a TLS handshake RTT, and requires 'no nginx/Cloudflare in front'. The user's target is a LAN party, which typically runs plain ws:// — then there is no anchor at all. A modded client that delays PONGs by X inflates corr by X/2: ARMED is sent X/2 early (foreknowledge) and the press is rewound X/2 more → gain = X, bounded only by C_MAX (X ≤ 2·C_MAX − rtt = 110 ms in the LAN profile, 290 in WAN) and by the SUPERHUMAN check, which uses the cheater-inflated rttMax16 (reactLB = r − X ≥ 100 → X ≤ 150). So ~110 ms undetected on a plain-ws LAN. Additional: delaying ARMED_ACK by < jAllow gains 15–20 ms without ACK_DELAYED firing; JITTER_INFLATED needs tcpi_rttvar, otherwise only the >30 ms rule applies, so a player can widen W for every pair involving him. Griefing: max corr delays everyone's lamp by up to E_MAX = 150 ms. The handshake anchor is single-shot: a genuine Wi-Fi degradation → 10 s of 'conservative' penalties → forced reconnect → cold with SIGMA_COLD 20. Latency fine (DECISION_MAX 350 / 200 LAN, dynamic deadline), plus ≤ 150 ms arming equalization. Implementation is medium: 5 Hz padded pings, ms-precision scheduler, u32 wrap, but the operational constraint (game process must own the socket, TLS or FFI for the anchor) is a real cost for a 'modern stack 2026' deployment behind a reverse proxy.

### Design 3: FairBuzz (Sync-Cue / Bounded-Delta / Tie-Rules with host presets, handicaps and allPlay written mode) — total 23
fairness=4 jitter=6 cheat=3 decisionLatency=5 implRisk=5

Best product thinking: presets, an explicit allPlay/written mode (the only truly ping-proof way to play on bad networks), forbidding countdown+first, rejected claims falling back to the NEUTRAL server estimate rather than the boundary, and a schedule sent ≥ 100 ms early so the lamp is never late. Most forgiving tolerance (rttEff/2 + 3σ + 10 with rttLast), so honest spikes rarely misrank. FAIRNESS BREAK: decide() ties everything within first.err + x.err = 40 ms even in resolution=first (the code uses max(wHost, err sum); the prose 'windowMs=0 → C wins' contradicts the code), and the default tieBreak is random. Monte Carlo: with B physically 15–30 ms ahead, 99–100% of races become lotteries, so the physically-first player wins 1/|tie|. Human reaction SD ≈ 40 ms means roughly half of 3-player races land inside 40 ms → by default about half the game is a coin flip, and the wired-LAN preset throws away a measurement that is accurate to ~2–5 ms. Adaptive handicap (+15 ms per button won) deliberately penalizes the fastest player. CHEAT BREAK: BUTTON_SCHEDULE hands armAt to every client 100–600 ms in advance; a modded client presses at armAt + U(110, 190): median ≈ 150, MAD ≈ 20 → evades both minReaction and the robotic detector (needs MAD < 12 AND median < 150), beats humans (230–270 ms) in every race under first, and is always inside the tie set under window. SPEC HOLE: the server needs rtt/σ/rttLast for tol, but the protocol never gives it t4 (PING{seq,t1} / PONG{seq,t1,t2,t3}) — it says the client sends off/offErr 'for audit', so either the server statistics are client-reported (forgeable: claim rttLast = 380 → tol 224, exactly their example 8) or unspecified; 'late' and 'synced' are client-declared too. Even with honest measurement, tol = rtt/2 + 3σ + 10 is 18/74/195 ms (LAN/Wi-Fi/WAN) and the bias detector tolerates sustained max(rtt/4, 3σ) → 3/24/62 ms shaving undetected. Latency is the worst: collect window from tA1 with a 600 ms cap, plus 100–600 ms schedule lead before the lamp. Implementation is medium: client-computed offset cannot be audited server-side, the written mode is a whole extra feature (normalization, review UI), many knobs (mitigated by presets).

## Synthesis
# Final design: **AnchoredHybrid** (SyncedHybrid v2)

Base = Design 1 (server-owned clock model, staggered scheduled arming, raw client stamps converted and clamped by the server, RTT-sized collection window, 2 ms ties). Patched with Design 2's ideas (tamper-proof anchor for RTT **and jitter**, hard caps, reject-not-clamp, ACK-verified late-light credit, contested/likelihood tie option, HMAC-seeded RNG, sanctions ladder, reproducible log) and Design 3's product layer (presets, allPlay/written mode, neutral fallback, forbidden combos). The Design 1 σ-inflation hole is closed by making the browser-answered WebSocket ping/pong the ceiling for everything the client could otherwise inflate.

## 1. Measurement (server side, one arbiter process per room, all times `performance.now()` ms)

**App sync (4-tuple, server does the math):** client `SYNC{seq,c1,prev:{seq,c4}}` → server stamps `s2`, replies `SYNC_ACK{seq,c1,s2,s3,model}`; `c4` of sample n arrives inside SYNC n+1. `rtt_app=(c4−c1)−(s3−s2)`, `off=((s2−c1)+(s3−c4))/2`. Schedule: burst 10×100 ms on connect/reconnect/tab-visible, 1/s steady, 5×100 ms burst when reading starts.

**Transport anchor:** server `ws.ping()` every 1000 ms idle, every 250 ms in `reading/armed`. The browser network stack pongs; JS cannot delay or forge it. Requires a runtime that exposes ping/pong (Node `ws`, Bun, uWS.js). Optional extra anchor when the process owns the socket (Go/Rust/uWS): `TCP_INFO` srtt/rttvar.

```
rtt_ws_min30 = min(ws rtt, 30 s);  rtt_ws_ewma (α=1/8);  σ_ws = 1.4826·MAD(last 16 ws rtt)
app sample with rtt_app < rtt_ws_min30 − 1  → drop + flag sync_lie
rtt_min   = min(accepted rtt_app, 30 s);  rtt_ewma (α=1/8);  rttvar (β=1/4)
σ_app     = max(rttvar, MAD(last 16 rtt_app))
σ         = min(σ_app, σ_ws + 2, SIGMA_MAX=30)                    // client cannot inflate: ws jitter is the ceiling
jitter_inflated flag if σ_app > 2·σ_ws + 5 in 3 consecutive 2 s windows
rtt_ref   = min(rtt_ewma, rtt_ws_ewma + 5, rtt_min + 2σ + 10)      // upward moves need transport confirmation
offset    = median(off) over samples with rtt_app ≤ rtt_min + max(2, 0.1·rtt_min) (≥3, else lowest-RTT quartile)
step      = 3 consecutive |off − offset| > u + 5 same sign → reset ring, re-burst   (no drift term in v1)
asymAllow = clamp(0.25·rtt_min, 2, 40)
u         = asymAllow + 2σ + 1                                        // shown in UI as "sync ±u"
tol       = min(asymAllow + 3σ + 2, TOL_MAX)                          // TOL_MAX 40 lan / 60 wifi / 80 wan
quality   = unsynced (<5 app samples in 10 s or no pong 6 s) | good (rtt_min<30 ∧ σ<5) | fair (σ<15) | poor
poor / unsynced / jitter_inflated → scored server-only (tFinal = tArr) and evaluated with uncertainty ties
```

## 2. Arming

```
T_arm = t_readEnd + U(armJitterMinMs, armJitterMaxMs)          // default 300..1200
for each eligible i (connected, not locked out, not already wrong on this question):
  m_i = freeze {offset, rtt_ref, σ, asymAllow, tol, u, quality}   // ONE offset value, used for armAtLocal AND conversion
  armAtLocal_i = T_arm − m_i.offset
  lead_i = clamp(rtt_ref/2 + 3σ + 20, 25, 250)                  // foreknowledge for a light-on-receipt mod ≈ 3σ + 20
  at T_arm − lead_i: send BUTTON_ARM{armId(128-bit), questionId, armAt:T_arm, armAtLocal_i, mode, deadlineAt, lockoutMs}
  tSend_i recorded in the send callback
  lateCreditMax_i = max(0, tSend_i + rtt_ref/2 + 3σ − T_arm) + LATE_ALLOW_i,  LATE_ALLOW_i = min(2σ + 10, lateLightCreditMs)
client: on receipt send ARM_ACK{armId} immediately; setTimeout(armAtLocal − now − 4) then rAF/spin until now ≥ armAtLocal; light + beep; litLocal = now (if already past → light now)
server: ackExcess_i = max(0, (tAckRecv_i − tSend_i) − rtt_ref)   // server-observed lateness of THIS delivery
mode = 'scheduled' (good/fair) | 'onReceipt' (unsynced/poor: light on receipt, server-only scoring)
BUTTON_DISARM at T_arm + pressWindowMs; arm stays 'draining' for max_j(rtt_ref_j + 3σ_j + tol_j) then BUTTON_RESULT{winner:null}
```

## 3. Press

Client: `pointerdown`/`keydown` with `isTrusted`; `pressLocal = (now − e.timeStamp ∈ [0,60]) ? e.timeStamp : now`; send `PRESS{armId, seq, pressLocal, litLocal, src}` before any rendering. A press before the local light is not sent: local shake + `MISFIRE{armId}`.

Server (`tRecv` stamped in the raw message handler before parsing):
```
unknown armId → stale;  duplicate → duplicate;  lockedUntil > tRecv → lockedOut
arm not in armed/collecting/draining → false start (lockout, FALSE_START broadcast)
tArr = tRecv − rtt_ref/2;   lo = tArr − tol;   hi = min(tRecv, tArr + tol)
if serverOnly(i):  tFinal = tArr; source = 'server'
else:
  tClient = pressLocal + m.offset
  credit  = clamp(min(litLocal − armAtLocal, ackExcess_i), 0, lateCreditMax_i)     // late light forgiven only as far as the server saw it
  tEst    = tClient − credit
  tFinal  = clamp(tEst, lo, hi);  source = (tFinal == tEst) ? 'client' : 'clamped'   // clamp > 5 ms → flag out_of_bounds
  biasDetector(i, tClient − tArr)
reaction = tFinal − T_arm
reaction < −u            → false start (lockout)
reaction < MIN_HUMAN=100 → REJECT: PRESS_ACK{tooEarly}, lockout, strike 'superhuman' (press is NOT ranked)
record {tRecv, tClient, tArr, lo, hi, credit, tFinal, reaction, source, m}; PRESS_ACK{pending}; broadcast PRESS_PENDING
bestT = min(bestT, tFinal); firstRecv ??= tRecv; rescheduleClose()
```

## 4. Collection window
```
pending  = eligible players without a press (disconnected/locked-out excluded)
waitFor  = max_j min(rtt_ref_j/2 + tol_j, WAIT_PER_PLAYER_MAX=200)
deadline = min(bestT + W_max + waitFor, firstRecv + maxCollectMs)     // W_max = tieMs or 60 in uncertainty mode
close immediately when pending is empty; late presses → PRESS_ACK{late}, audit 'late_beat' if they would have won
```

## 5. Decision
```
ranking = presses sorted by tFinal
W(lead,x) = tieMode=='resolution' ? TIE_MS(2) : clamp(2·hypot(u_lead,u_x) + 2, 2, 60)
tieSet = {x : x.tFinal − lead.tFinal ≤ W(lead,x)}
if tieSet has clean (unflagged, not conservative) members → drop flagged/conservative ones
winner = |tieSet|==1 ? tieSet[0] : tieBreak(tieSet)   // rng = HMAC(serverSecret, armId), reproducible for audit
tieBreak: likelihood (w_i = Π_j Φ((t_j − t_i)/hypot(u_i,u_j))) | random | lowestScore | fewestButtonsWon | rotate | allPlay
BUTTON_RESULT{armId, winnerId, contested, marginMs, resolutionMs, tieBreak, ranking:[{playerId, reactionMs, source, rttMs, uMs}], windowMs}
```
Decisions are final; host has `BUTTON_REOPEN`. Every press keeps the full record for `BUTTON_AUDIT` / `GET /rooms/{id}/buzz-log`.

## 6. Anti-cheat ladder
Flags: `sync_lie`, `jitter_inflated`, `out_of_bounds`, `superhuman`, `early_bias` (mean of last 10 `tClient − tArr` < −(asymAllow + σ)), `bot_like` (≥ 8 presses, median reaction < 160 ∧ MAD < 15), `ack_late` (ackExcess > LATE_ALLOW in ≥ 3 of 5 arms), `rate` (> 5 PRESS/s). Trust: −1 per flag, +1 per 5 clean presses; ≤ −2 → *conservative* (tol halved, loses contested sets to clean players); ≤ −4 → *serverOnly*; host notified via `INTEGRITY_ALERT`, kick is manual. Everyone sees rtt/σ/quality badges (`CONN_QUALITY` every 5 s).

**Resulting bounds.** Stamp forgery ≤ tol ≤ min(asymAllow + 3σ_ws + 2, TOL_MAX): wired LAN ≈ 6–10 ms, Wi-Fi ≈ 25–40, WAN ≤ 80. Sustained without any flag ≈ asymAllow + σ (LAN ≈ 3 ms). Foreknowledge ≈ 3σ + 20 (≈ 25 ms LAN). Late-credit abuse ≤ LATE_ALLOW (≤ 20 ms, flagged when frequent). Sync manipulation: a consistent clock shift gains nothing (reaction is clock-independent); RTT lies downward are dropped by the ws floor, jitter lies are capped by σ_ws + 2 and SIGMA_MAX. A humanized bot (110–190 ms randomized) remains undetectable by any network scheme.

## 7. Modes, presets, written play
`buttonMode`: `anchoredHybrid` (default) | `serverOnly` | `arrival` (SIGame FirstWins) | `randomWindow` (tieMs 300, random). `allPlay` (Design 3 §6): `WRITTEN_PHASE{questionId, participants, deadlineAt, scoring}` → `WRITTEN_ANSWER{questionId, text, reactionMs}` → `WRITTEN_RESULT`; used for SIQ `forAll` questions, for `tieBreak: allPlay`, and for the `noRace` preset. Validation: `countdown` arming is allowed only with `tieMode: uncertainty` or `allPlay`.

Presets: **lanWired** (resolution ties, TOL_MAX 40, maxCollect 200, lockout 1000, armJitter 300–800), **wifiParty** (default: uncertainty + likelihood, TOL_MAX 60, maxCollect 400, lockout 3000, allPlayFor [forAll]), **internet** (TOL_MAX 80, maxCollect 500, allPlayFor [forAll], warn host at rtt_ref > 300 and suggest allPlay for that player), **tournament** (resolution ties, lateLightCredit 0, auto-demote strict, serverOnly for anyone flagged), **noRace** (allPlay for all normal questions).

## 8. Message list
C→S: `HELLO{sessionToken, resume?}`, `SYNC{seq,c1,prev}`, `ARM_ACK{armId}`, `PRESS{armId,seq,pressLocal,litLocal,src}`, `MISFIRE{armId}`, `WRITTEN_ANSWER`, `RESUME{sessionToken,lastSeq}`.
S→C: `WELCOME{playerId,syncPlan}`, `SYNC_ACK{seq,c1,s2,s3,model:{offset,rttRef,jitter,u,quality}}`, ws ping frames, `QUESTION_READING{questionId,endsAt}`, `BUTTON_ARM{armId,questionId,armAt,armAtLocal,mode,deadlineAt,lockoutMs}`, `PRESS_ACK{armId,seq,status:pending|rejected|duplicate|late|stale|lockedOut|tooEarly,reason?,lockoutUntilLocal?,tFinal?}`, `PRESS_PENDING{armId,playerId}` (broadcast), `BUTTON_RESULT{...}` (broadcast; full `ranking` details only to host), `BUTTON_DISARM{armId,reason}`, `FALSE_START{playerId,lockoutMs}` (broadcast), `CONN_QUALITY{players:[{playerId,rttMs,jitterMs,uMs,quality,flags}]}` (5 s), `WRITTEN_PHASE`, `WRITTEN_RESULT`, `SNAPSHOT{...buzzer:{state,armId,armAt,armAtLocal,deadlineAt,lockedUntil}}`.
Host only: `BUTTON_AUDIT{armId, entries:[{playerId,tRecv,tClient,tArr,lo,hi,credit,tFinal,source,flags}], rngSeed}`, `INTEGRITY_ALERT{playerId,kind,stats,action}`, `BUTTON_REOPEN{armId}`; REST `GET /api/v1/buzzer-presets`, `PATCH /api/v1/rooms/{id}/buzzer-settings`, `GET /api/v1/rooms/{id}/buzz-log?questionId=`.

Reconnect: `RESUME` → `SNAPSHOT` → mandatory sync burst; until ≥ 5 app samples and one pong the player is `unsynced` (onReceipt, server-only); an active arm is re-sent in onReceipt mode; PRESS for an inactive armId → `stale`. Media never travels on this socket; `perMessageDeflate: false`, `TCP_NODELAY`, frames < 300 B.

## 9. Constants
SYNC burst 10×100 ms (connect/reconnect/visible), steady 1000 ms, pre-arm burst 5×100 ms · WS_PING 1000 ms idle / 250 ms reading+armed · rings: 64 app, 32 ws · RTT_MIN_WINDOW 30 s · α=1/8, β=1/4 · SIGMA_MAX 30 · JITTER_INFLATED: σ_app > 2σ_ws+5 in 3×2 s windows · ASYM_ALLOW clamp(0.25·rtt_min, 2, 40) · EPS_INPUT 2 · u = asym+2σ+1 · tol = min(asym+3σ+2, TOL_MAX) with TOL_MAX 40/60/80 (lan/wifi/wan) · ARM_LEAD clamp(rtt_ref/2+3σ+20, 25, 250) · ARM_JITTER U(300,1200) (lan 300–800) · LATE_ALLOW min(2σ+10, lateLightCreditMs=20) · PRESS_WINDOW 5000 · DRAIN max_j(rtt_ref_j+3σ_j+tol_j) · WAIT_PER_PLAYER_MAX 200 · MAX_COLLECT 400 (lan 200, wan 500) · TIE_MS 2; uncertainty W = clamp(2·hypot(u_i,u_j)+2, 2, 60) · MIN_HUMAN 100 (reject) · BOT_LIKE median<160 ∧ MAD<15 over ≥ 8 · EARLY_BIAS mean10 < −(asym+σ) · lockout 3000 (lan 1000; Jeopardy 250 option), ×2 per repeat within 10 s, cap 6000 · trust: −1/flag, +1 per 5 clean, −2 conservative, −4 serverOnly · RATE_LIMIT 5 PRESS/s · CONN_QUALITY 5 s · writtenAnswerMs 20000, writtenDuelMs 10000.

## 10. Host-configurable
`preset`; `buttonMode`; `tieMode` (resolution|uncertainty) and `tieMs`; `tieBreak`; `armJitterMinMs/MaxMs` (0 disables); `pressWindowMs`; `falseStartLockoutMs`, growth, cap; `maxCollectMs`; `tolMaxMs`; `lateLightCreditMs` (0 for tournaments); `allPlayFor` (forAll | forAll+simple | all); `writtenAnswerMs`, `writtenScoring` (accuracy|speedBonus); `autoDemote` on/off and thresholds; per-player force `serverOnly`/`arrival`/kick; manual `BUTTON_REOPEN`; `maxRttWarnMs`. Not configurable (safety invariants): MIN_HUMAN reject, SIGMA_MAX, ws-floor sample rejection, one offset snapshot per arm.

## 11. Engineering notes
Single-threaded room actor with monotonic timers; `tRecv` before JSON parse; model snapshot frozen per arm; all decision code is pure functions over `{arm, presses, models}` and must ship with a deterministic simulator (Gaussian jitter + bufferbloat spikes + TCP RTO stalls + adversarial clients: stamp shaving, SYNC delay, PONG delay, light-on-receipt, humanized bot) run in CI; drop drift least-squares from v1; a browser-side test for the setTimeout+rAF lighting error (expect ≤ 2 ms desktop, ≤ 5 ms mobile).

## Open risks
- A humanized autoclicker (light detected by pixel polling, press at +U(110,190) ms) is undetectable by any network scheme; MIN_HUMAN + bot_like stats only catch naive bots. Mitigation is social: visible reaction stats, host kick, or allPlay/uncertainty tie modes that remove the value of 1 ms wins.
- Path asymmetry is unobservable; at RTT 250 ms two presses 5–20 ms apart are inside the noise (u ≈ 30–70 ms). The design guarantees zero-mean, ping-independent error and shows ±u to players, not a correct answer.
- The ws ping/pong anchor assumes the runtime exposes ping/pong (Node `ws`, Bun, uWS.js — not Deno's upgradeWebSocket) and that no intermediary answers pings on the browser's behalf; verify per deployment (nginx relays frames; some managed WebSocket gateways terminate them). If the anchor is missing, fall back to SIGMA_MAX/TOL_MAX caps only (cheat bound = TOL_MAX).
- A root-level cheater can inflate σ_ws with real random delay (tc netem) and toggle it off before pressing; damage is capped at TOL_MAX (40–80 ms) and shows as bimodal RTT, but is not provably distinguishable from a bad link.
- Bounded late-light credit (LATE_ALLOW ≤ 20 ms via delayed ARM_ACK) is a deliberate, flagged, capped leak; tournaments should set lateLightCreditMs = 0 and accept that heavy-tailed Wi-Fi then costs honest players the excess over 3σ+20.
- TCP head-of-line stalls (RTO ≥ 200 ms) on PRESS or ARM exceed any tolerance and look identical to a lie; only WebTransport datagrams with 3× repeats fix this, which needs HTTP/3 and certificates that LAN parties often lack.
- Client timer precision: Firefox/Safari 1 ms, reduceTimerPrecision/RFP up to 100 ms, background-tab throttling; the visibility re-burst and onReceipt fallback cover most cases but a player returning to the tab < 1 s before arming plays server-only for that question.
- Server event-loop stalls (GC, big media broadcasts on the same process) shift tSend/tRecv; media must be kept off the arbiter socket and process, and lateCreditMax exposes server-caused lateness — but a stall between tRecv stamping and processing is still a shared bias.
- Uncertainty tie mode with likelihood lottery is correct but must be explained to players; one high-σ player widens W for every pair including him and pulls more races into lotteries (capped at 60 ms, he loses contested sets once flagged).
- Implementation complexity remains high (~900–1100 LOC + simulator); the highest-risk invariants are: one frozen offset per arm used for both armAtLocal and conversion, tRecv stamped before parsing, and the ws-floor/σ-ceiling checks actually being applied before tol is computed.
