# Judge 2

Winner: Design 2: ServerRewind — the only design whose stated cheat bound (20 ms with a kernel anchor, 40 ms with the handshake anchor, always with a flag) survives an in-browser attacker; Design 1's and Design 3's bounds collapse to 'unbounded' under a one-line PONG/c4 delay because their RTT models have no upper anchor. Design 1 nevertheless has the best honest-player ranking key and is the core the synthesis is built on, once anchored.

## Scores
### Design 1: SyncedHybrid (server-owned clock model + scheduled arming + RTT-bounded trust in client stamps) — total 31
fairness=9 jitter=7 cheat=4 decisionLatency=7 implRisk=4

STRENGTHS. The ranking key (reaction measured on the player's own monotonic clock from a light scheduled to a common server instant, converted with the same frozen offset snapshot) is the only one of the three that is exact for honest players regardless of RTT, jitter or asymmetry. I verified by simulation that stationary path asymmetry cancels: an honest player on a 150/100 ms asymmetric WAN path with a 24.7 ms offset error still yields tClient = tArr = R (250.0 / 251.5 / 250.0 ms), so the design's own 'honest caveat' (a +6 ms offset error could rank B before C) is wrong in its favour — the light schedule and the conversion share the bias. Server-owned model, frozen snapshot per arm, lateCredit bounded by the server's own send time, causality bound hi <= tRecv, 128-bit armId, per-press audit and a manual re-open are all correct. On LAN the window closes in ~10 ms.

BROKEN (CONFIRMED by simulation): the RTT model is client-inflatable and nothing anchors it from above. The client reports c4 (in the next SYNC) — a one-line browser mod adds X to c4. Then rtt_ewma -> r + X, offset -> offset − X/2, tArr = tRecv − (r+X)/2 moves X/2 earlier, and tClient = pressLocal + offset moves X/2 earlier too, so the stamp and the server estimate agree and no clamp fires. Sim: LAN cheater posing as RTT 250 with an honest 250 ms reflex is scored as reaction 130.0 ms, clamped=false, bias=+0.2 -> no flag. Add a 30 ms stamp lie: reaction 100.0 (floor), bias −30.3, still above the early_bias threshold −(asymAllow 40 + σ). Net ~150 ms, i.e. the cheater appears as a permanent 100–130 ms reactor (humans 230–270). The WS protocol ping is used only as a LOWER bound ('rtt < rtt_ws_min − 1 -> sync_lie'); no check compares rtt_app >> rtt_ws. The step detector never fires because the inflation exists from the first sample. Downgrading to serverOnly does not help (tArr uses the same inflated rtt_ewma); only 'arrival' mode is immune. Detection is purely social (CONN_QUALITY shows 'ping 250' on a LAN party). Second hole: tol = asymAllow + 3σ + 2 with asymAllow up to 40 ms is over-generous (asymmetry cancels, see above), so the honest-looking lie budget hugging the early_bias threshold is asymAllow + σ ≈ 3 / 25 / 55 ms at RTT 10 / 80 / 250; the stated 6 ms LAN bound only holds for stamp lies with an honest RTT model (sim: 30 ms stamp lie on LAN -> clamped to 244.6, gain 5.4 ms, flagged). Third: no server-side reaction lower bound from the ARM send time; MIN_HUMAN is checked on tFinal, which the cheater controls via the above. Pre-lighting on ARM receipt gains 3σ + 15 (small, good). BOT_STATS (median < 160 AND MAD < 15) is trivially evaded by a bot that draws reactions from N(175, 30).

WRONG-WINNER SCENARIOS (honest players): (a) WiFi PRESS delay spike > tol: A physically first by 10 ms, σ estimated 5 -> tol 37; A's PRESS hits a +60 ms spike -> tArr = P + 60, lo = P + 23, tFinal clamped to P + 23 -> A now 13 ms behind B -> B wins AND A is flagged out_of_bounds (trust −1). Phone WiFi power-save produces exactly this pattern (σ from MAD stays tiny, tails are 50–150 ms), so an honest phone player is degraded to serverOnly after three such presses. (b) Any ARM delivery delay > 3σ + 15 (a single TCP retransmit ≈ 200 ms): the light lights late, lateCreditMax = 0 because the server sent on time, the player loses ~200 ms uncredited; the deliberately minimal lead makes this the most fragile arming path of the three. (c) Firefox resistFingerprinting (100 ms performance.now) -> every stamp clamped and flagged. (d) Unsynced/reconnected players scored by tArr with full jitter and no tie band (TIE_MS = 2 ms fixed) — jitter of ±20 ms decides them by point estimate.

GRIEFING: a never-pressing or RTT-inflated player forces every window to the cap (400 ms) and lead to 400 ms; mild. Late-press 'late_beat' audit entries can be manufactured to lobby the host for BUTTON_REOPEN.

ENGINEERING: high complexity (c4-in-next-SYNC bookkeeping, least-squares drift, step detection, per-player ARM timers, client spin-wait, trust scoring, five-state arm machine); the missing upper anchor shows the surface is hard to reason about. Testability is good (deterministic simulator), tuning burden is low because most constants derive from measurements. Background tabs handled (re-burst on visible, onReceipt while unsynced). Reconnect discards the model — correct. Decision latency: LAN ~10 ms, mixed ≈ slowest pending rtt/2 + tol after bestT, cap 400.

### Design 2: ServerRewind (server RTT compensation with transport anchors, equalized ARMED, simultaneity window, calibrated tie-break; zero trust in client time) — total 32
fairness=6 jitter=6 cheat=7 decisionLatency=7 implRisk=6

STRENGTHS. The only design that names the real attack class ('measured RTT vs experienced RTT') and anchors the measurement to something JS cannot delay (kernel tcpi_rtt / QUIC srtt, or the TLS handshake RTT). Zero trust in client timestamps; corr frozen at ARMED; server-side reaction lower bound reactLB = tRecv − armedSentAt − rttMax16 computed from server facts only (the correct way to enforce a 100 ms floor); padded PING frames; HMAC(secret, nonce)-seeded lottery that the host can replay; likelihood tie-break that gives neither low ping nor low jitter a systematic edge; conservative -> arrivalOnly -> kick ladder; flagged/cold players lose contested sets to clean ones. Asymmetry cancels here too (the light is the ARMED arrival and the round trip contains both directions), and the formal argument that a uniform uplink delay yields no gain is right. In-browser JS cheat bound: I re-derived 20 ms with the kernel anchor (10 ms early light + 10 ms extra rewind) and 40 ms with the handshake anchor; both come with an RTT_INFLATED flag. That is the only stated bound on this panel that survives an in-browser attacker.

HOLES. (1) On Node/Bun — the likely 'modern 2026' stack — there is no kernel RTT, only the handshake anchor, and a NON-browser client (a 40-line Python/Node WebSocket script, or a local TCP relay that delays the first ~1 s of the connection then goes transparent) sets rttHandshake arbitrarily. anchor = fake + 40, then PONGs are delayed by X consistently, rttMed <= anchor, no flag, gain = X unbounded. The 'auto-reconnect when rttMed > anchor for 10 s' rule even hands the cheater a re-anchoring mechanism. (2) With the kernel anchor, a root-level shaper on the client (tc/nftables matching outbound pure-ACK segments, no payload inspection needed) delays ACKs but not data: server tcpi_rtt inflates by X while PRESS still arrives in r/2 -> gain X. The padding defence only addresses content-based classification. (3) Jitter inflation just under the JITTER_INFLATED threshold (30 ms, and no kernel rttvar on Node): sigmaRtt 29 -> sigma 20.5 -> W(cheater, LAN leader) ≈ 45 ms, so a cheater 20 ms behind enters the lottery with Φ(−20/20.5) ≈ 16 % to steal, every question, never flagged. (4) ACK_DELAYED fires at '>= 3 of 5 questions'; delaying ARMED_ACK on 2 of every 5 gives +jAllow (12–30 ms) on 40 % of questions silently. (5) Tie exclusion of 'flagged' players uses flags that honest slow/backgrounded clients also collect (see below), a soft griefing vector against them.

WRONG-WINNER SCENARIOS (honest): the ranking key contains the PRESS uplink jitter directly and only late (not early) ARMED delivery is corrected. A (Ethernet, σ 1) vs B (WiFi, sigmaRtt 30 -> σ 21): B physically first by 15 ms, B's PRESS hits a +40 ms spike -> B appears 25 ms behind; W = 46 -> contested; likelihood gives B 12 %. Design 1 scores the same event exactly. Asymmetric adaptation: a WiFi link that degrades from 20 to 80 ms RTT is under-compensated by 30 ms for 30 s (the player loses every close button for half a minute). C_MAX = 60 in the LAN profile: a remote guest at RTT 200 is permanently 40 ms behind. Early ARMED delivery is never debited (clamp >= 0) -> a free head start of |j| on lucky deliveries. A TCP retransmit of ARMED/PRESS (RTO 200+ ms) loses the question with only jAllow forgiveness — same as Design 1, worse than Design 3.

GRIEFING / OPS: never-press -> 350 ms per question; an RTT-inflated player delays everyone's ARMED by E_MAX = 150 ms per arm (WAN profile) and is flagged only after 3×2 s. Reverse proxy in front of the socket (nginx/Caddy/Cloudflare — extremely common) silently turns the anchor into 'proxy RTT + 40': honest WAN players get RTT_INFLATED, are capped and lose contested sets — 'ping wins' comes back with a flag on the victim. Background tab: PONGs late -> RTT_INFLATED false positive after ~6 s (should decay, currently permanent 'conservative').

ENGINEERING: medium. No clock sync at all is a big simplification; needs a ~1 ms scheduler, u32 wrap handling, Φ, HMAC, frame padding. Full strength requires in-process TCP/QUIC termination (uWebSockets.js with FFI, Go, Rust) — a stack constraint that should be a headline, not a footnote. Testable and reproducible from the log. Latency: dynamic deadline, cap 350 (200 LAN), plus up to 150 ms of arming equalization that slows the game's pace on WAN rooms.

### Design 3: FairBuzz (scheduled cue + bounded client delta + host-configurable fairness rules / Sync-Cue / Bounded-Delta / Tie-Rules) — total 27
fairness=5 jitter=7 cheat=3 decisionLatency=6 implRisk=6

STRENGTHS. Clean layering (netcode emits Press{tPress, err, source}; the game layer never sees raw network time); the long schedule lead (>= 100 ms, up to 600) makes lamp timing the most robust of the three to ARM delivery delay — a 200 ms retransmit of BUTTON_SCHEDULE usually still lands before armAt, and BUTTON_ARMED at armAt is a second chance. Rejected claims are replaced by the neutral server estimate, not by the bound (a cheater cannot do better than an honest player on the same link by getting rejected). countdown + first is validated away. Presets, open ping/handicap display, written/allPlay mode for 'forAll' questions, REST buzz-log and host overrides are the best product packaging on the panel.

HOLES (cheating). The client delta reactionMs is trusted within tol = rttEff/2 + 3σ + 10 (LAN 18, WiFi 74, WAN 195 ms) and there is NO anchor of any kind — not even a lower bound from protocol pings. rttEff = max(rttEwma, rttLast): delaying a single PONG before the buzz inflates rttLast, which shifts est earlier AND widens tol for that very press. Persistent inflation X of the app RTT (delay every PONG in JS) gives est = tRecv − (r+X)/2 and tol = (r+X)/2 + 3σ + 10. The bias detector fires only at biasEwma > max(rttEff/4, 3σ), so a cheater claiming claim = est − rttEff/4 + ε is never flagged: gain ≈ 3X/4 + r/4 — with r = 10, X = 100 that is ~77 ms undetected, and X is bounded only by the host noticing a '110 ms ping' on a LAN party. minReactionMs is checked on the CLAIMED reactionMs; there is no server-side floor from the schedule send time, so 'reactionMs = 101' every question is accepted. The robotic detector (MAD < 12) is evaded by adding noise. Net: a browser mod appears as a permanent ~100–120 ms reactor; in 'window' mode it still enters every tie set and wins outright against anyone more than windowMs slower — the 'game layer as anti-cheat' argument only removes the payoff of being first by less than the window.

GRIEFING (severe, follows directly from the formula): windowMs = auto = clamp(2·max_j σ_j + 20, 30, 250) uses the ROOM-WIDE maximum σ. One player who randomly delays PONGs by ±100 ms (σ ≈ 60, still 'synced' because the threshold is σ < 100) pushes the window to 140–250 ms for everyone: every button in the room becomes a random draw among all players within 250 ms, and collectMs (W + rtt/2 + 3σ of the pending griefer) hits the 600 ms cap. No detector targets σ inflation; MAX_RTT_WARN only fires at 400 ms. Design 2's pairwise W and Design 1's fixed/pairwise tie band do not have this failure.

WRONG-WINNER (by design, not by bug): default preset wifiParty = window >= 80 ms + tieBreak random -> the physically first presser wins 1/|tie| (the worked example gives C, the true first, 33 %). Even resolution = first keeps an implicit tie of err_C + err_B = 40 ms built from a CONSTANT INPUT_ERR = 20 (not a measurement), and human reaction SD ≈ 40 ms, so roughly half of three-player buttons become coin flips; only windowMs = 0 restores Design-1-like precision. lowestScore invites sandbagging; adaptive handicap (+15 ms per button won) is gameable. Honest TCP stall on BUZZ: the stall IS the BUZZ segment, so the previous ping's rttLast does not see it (the §8 example assumes it does); the claim is rejected as earlyClaim, the player loses the question and collects an integrity flag; REJECT_RATIO > 30 % over 10 presses then demotes an honest lossy-WiFi player to serverOnly.

JITTER: client-side delta is immune to uplink jitter like Design 1, lamp timing is the most robust, but rttEff = max(ewma, last) makes tol and est twitchy, and the room-wide window converts one player's jitter into randomness for all.

ENGINEERING: medium. Client-computed offset (min-RTT sample) is simpler than Design 1's server-side model, but the product surface is large (presets, written mode, handicaps, REST, SNAPSHOT), and the server needs t4 from the client to compute RTT (unspecified). Background-tab throttling delays the lamp timer -> late claim, no flag — acceptable. Latency: collect cap 600 ms, plus a 100–600 ms lead before the lamp on every question.

## Synthesis
# AnchoredHybrid — final buzzer design

**One sentence:** Design 1's scheduled-light + client-reaction ranking (exact for honest players; asymmetry provably cancels), with the client-inflatable RTT model closed by Design 2's *transport anchor* idea using the WebSocket protocol ping that Design 1 already collects (plus kernel RTT when the stack allows), Design 2's server-side reaction floor, frozen per-arm snapshot, reproducible HMAC-seeded tie lottery and trust ladder, and Design 3's host presets, written/allPlay fallback, longer arm lead and open ping display. Design 3's room-wide max-σ window and adaptive handicap are dropped (griefable).

## 1. Measurement (server side, per connection; all times ms on the room process's monotonic clock)

1. **App sync** exactly as Design 1 §1.1: `SYNC{seq, c1, prev{seq, c4}}` → `SYNC_ACK{seq, c1, s2, s3, model}`; server holds the 4-tuple, computes `rtt_app` and `off`. Bursts 10×100 ms on connect/resume/`visibilitychange→visible`, 1/s steady, 5×100 ms pre-arm burst when reading starts.
2. **Transport anchor** (JS-tamper-proof): server sends a WS *protocol* ping frame (payload = seq) every 1000 ms; the browser network stack answers → `rtt_ws` ring (32). Derive `rtt_ws_min30s`, `rtt_ws_ewma` (α 1/8), `σ_ws = 1.4826·MAD(16)`. Optional stronger anchor when the game process terminates TCP/QUIC itself (uWebSockets.js FFI / Go / Rust): `rtt_kern = tcpi_rtt` or QUIC `srtt`.
3. **Filter** (Design 1 §1.2): ring 64, `rtt_min` sliding 30 s, `rtt_ewma` α 1/8, `rttvar` β 1/4, `σ_app = max(rttvar, MAD16)`, `offset = median(off over samples with rtt ≤ rtt_min + max(2, 0.1·rtt_min))`, drift slope, step detector.
4. **Anchored reference (the fix):**
```
anchor   = (rtt_kern ?? rtt_ws_ewma) + ANCHOR_SLACK        // 10 ms kernel, 15 ms ws
rttRef   = clamp(rtt_ewma, rtt_ws_min30s − 1, anchor)        // both a floor and a ceiling
σ        = clamp(min(σ_app, 3·σ_ws + 3), 1, 30)              // jitter cannot be inflated past the transport's jitter
tol      = clamp(4σ + 2, 5, tolCapMs=60)                     // no asymAllow: asymmetry cancels (verified by simulation)
u_server = 2σ + 1 ;  u_client = 1.5                          // per-press uncertainty by source
lead     = clamp(rttRef/2 + 3σ + 25, 25, 400)
sample with rtt_app < rtt_ws_min − 1            → discard + flag SYNC_LIE
rtt_ewma > anchor in 3 consecutive 2-s windows  → flag RTT_INFLATED
σ_app > 3σ_ws + 3 in 3 consecutive windows      → flag JITTER_INFLATED
eligible(scheduled) = ≥5 sync samples in 10 s ∧ quality ≠ poor ∧ rttRef ≤ maxRttMs ; else mode = onReceipt
```
`SYNC_ACK.model = {offset, rttRef, rttWs, jitter, u, quality}` so UI and server share one model.

## 2. Arming
```
T_arm = t_readEnd + U(armJitterMinMs, armJitterMaxMs)         // default 200..1200
for each eligible player i (connected, not locked out, not already wrong on this question):
  m_i = snapshot{offset(T_arm), rttRef, σ, tol, u, lead, rttMax16}   // frozen for this arm
  scheduled: send BUTTON_ARM at T_arm − lead_i with armAtLocal = T_arm − offset_i(T_arm)
  onReceipt: send BUTTON_ARM at T_arm − rttRef_i/2                    // Design 2 delivery equalization
  record tSend_i in the send callback; lateCreditMax_i = max(0, tSend_i + rttRef_i/2 + 3σ_i − T_arm)
```
Client: light exactly at `armAtLocal` (setTimeout(delay−4) + rAF/spin), or on receipt if late/onReceipt; record `litLocal`; reply `ARM_ACK{armId, recvLocal}` immediately (gives `rttAck_i = tAckRecv − tSend_i`). Presses before the light are blocked locally and reported as `MISFIRE`.

## 3. Press scoring (server; `tRecv` stamped in the raw socket handler before parsing)
```
P = arm.players[i]; if !P or arm.state ∉ {armed, collecting, draining} → misfire/stale; if pressed → duplicate
tArr    = tRecv − m.rttRef/2
lo, hi  = tArr − m.tol, min(tRecv, tArr + m.tol)
reactLB = tRecv − tSend_i − m.rttMax16                      // most generous server-only reaction (Design 2)
if reactLB < MIN_HUMAN_MS(100) → PRESS_ACK{rejected, tooEarly} + lockout + strike SUPERHUMAN; return

if P.mode == scheduled ∧ trust(i) ≥ conservative:
    credit  = clamp(msg.litLocal − P.armAtLocal, 0, lateCreditMax_i)
    tEst    = msg.pressLocal + m.offset − credit
    tKey    = clamp(tEst, lo, hi); source = (tKey == tEst) ? 'client' : 'clamped'
    biasWin.push(tEst − tArr)                               // honest mean ≈ 0 (asymmetry cancels), sd ≈ σ/√10
    if mean(last 10) < −(σ_i + 3) → flag EARLY_BIAS
else:                                                       // onReceipt / serverOnly / cold
    tSeen_i = tSend_i + m.rttRef/2 + clamp(rttAck_i − m.rttRef, 0, 2σ_i + 10)
    tKey    = T_arm + (tArr − tSeen_i); source = 'server'
reaction = tKey − T_arm; if reaction < 100 → tKey = T_arm + 100, flag SUPERHUMAN_SOFT
u_press  = source == client ? u_client : source == clamped ? m.tol : m.u
store {i, seq, tRecv, tArr, tEst, lo, hi, credit, tKey, reaction, source, u_press, m, flags}
PRESS_ACK{pending}; broadcast PRESS_PENDING; if state == armed → collecting, firstRecv = tRecv
bestT = min(bestT, tKey); rescheduleClose()
```
Single clamps never touch the trust score (WiFi tails); only `CLAMP_RATIO > 30 %` over 10 presses does.

## 4. Collection window and decision
```
rescheduleClose: pending = eligible ∧ not pressed ∧ connected ∧ not locked out
  if none → resolve
  waitFor  = max_j(rttRef_j/2 + tol_j)
  deadline = min(bestT + TIE_MAX + waitFor, firstRecv + maxCollectMs)     // 400 wan / 200 lan
resolve:
  ranked = presses sorted by tKey; lead = ranked[0]
  W(p,q) = clamp(2·hypot(u_p, u_q), tieMinMs=3, tieMaxMs=40)   // honest client–client ties ≈ 4 ms; server-scored ≈ 4σ
  tie = ranked.filter(q → q.tKey − lead.tKey ≤ W(lead, q))
  clean = tie.filter(!flagged ∧ !cold); if clean.length → tie = clean          // Design 2 presumption
  winner = tie.length == 1 ? tie[0] : tieBreak(tie, rng = HMAC(serverSecret, armId))
  tieBreak: likelihood (default: w_i = Π_{j≠i} Φ((tKey_j − tKey_i)/hypot(u_i,u_j))) | mostLikely | random | lowestScore | fewestButtonsWon | rotate | allPlay(written duel)
  broadcast BUTTON_RESULT; late presses → PRESS_ACK{late}; audit 'late_beat' if they would have won
```
Late presses never reopen a decision (NAQT rule); the host has `BUTTON_REOPEN`.

## 5. Anti-cheat and residual bounds
- **Flags:** SYNC_LIE, RTT_INFLATED, JITTER_INFLATED, EARLY_BIAS, SUPERHUMAN (rejected), CLAMP_RATIO, BOT_LIKE (≥ 8 presses, median reaction < 170 ms ∧ MAD < 20), STALE_NONCE/REPLAY, RATE_LIMIT.
- **Trust ladder (Design 2):** −1 per flag event, +1 per 5 clean presses. ≤ −2 `conservative` (loses contested sets; u := u_server); ≤ −4 `serverOnly` (no client stamps); ≤ −6 `arrivalOnly` + ANOMALY to host; host may kick or reset. All visible in CONN_QUALITY badges.
- **Room `maxRttMs`** (LAN preset 80, WAN 350): above it a player is `onReceipt` + badged. This is what bounds a *non-browser* client that fakes pong timing: gain ≤ (maxRttMs − realRtt)/2 ≈ 35 ms in a LAN room, visible as a high ping to everyone.
- **Quantified maximum advantage after the merge, browser-JS attacker:** pose-as-slower via app RTT ≤ ANCHOR_SLACK/2 = 7.5 ms (flagged if persistent); stamp lie hugging the bias threshold ≈ σ + 3 ms (LAN 3.5, WiFi 11, WAN 18); one-shot lie clamped at tol (5–60) and counted; pre-lighting on ARM receipt ≤ 3σ + 25 but `reactLB` from `tSend` enforces the 100 ms floor server-side; jitter inflation capped at 3σ_ws + 3. Non-browser/root attacker: ≤ (maxRttMs − realRtt)/2, or ≤ 5 ms with the kernel anchor.
- Nonce armId (128-bit) per arm; `(armId, playerId)` accepts one press; `seq` dedupes; PRESS ≤ 1/arm, SYNC ≤ 20/s, payload ≤ 1 KB; `e.isTrusted`/`evTs` checked client-side only (diagnostic).

## 6. Messages
**C→S:** `HELLO{sessionToken, resume?, lastSeq?}` · `SYNC{seq, c1, prev?{seq, c4}}` · `ARM_ACK{armId, recvLocal}` · `PRESS{armId, seq, pressLocal, litLocal, evTs, src}` · `MISFIRE{questionId, earlyByMs}` · `WRITTEN_ANSWER{questionId, text, reactionMs}`
**S→C:** `WELCOME{playerId, syncPlan, buzzerSettings}` · `SYNC_ACK{seq, c1, s2, s3, model}` · WS ping frame (seq payload, 1/s) · `QUESTION_READING{questionId, endsAt}` · `BUTTON_ARM{armId, questionId, armAt, armAtLocal?, mode:'scheduled'|'onReceipt', deadlineAt, lockoutMs}` · `PRESS_ACK{armId, seq, status:'pending'|'rejected'|'duplicate'|'late'|'stale', reason?, lockoutUntilLocal?}` · `PRESS_PENDING{armId, playerId}` · `BUTTON_RESULT{armId, winnerId|null, contested, marginMs, resolutionMs, tieBreak, ranking:[{playerId, reactionMs, source, rttMs, uMs, flagged}]}` · `BUTTON_DISARM{armId, reason}` · `LOCKOUT{playerId, untilAt, durationMs, reason, strike}` · `CONN_QUALITY{players:[{playerId, rttMs, jitterMs, uMs, quality, flags[]}]}` (every 2 s) · `WRITTEN_PHASE{questionId, participants, deadlineAt, scoring}` · `WRITTEN_RESULT{...}` · `SNAPSHOT{state, arm?, lockedUntil}` (on resume)
**Host only:** `BUTTON_AUDIT{armId, entries:[{playerId, tRecv, tArr, tEst, lo, hi, credit, tKey, source, uMs, flags[]}], rngSeed}` · `ANOMALY{playerId, code, details, action}` · `BUTTON_REOPEN{armId}` · `SET_TRUST{playerId, mode}` · REST `GET /buzzer-presets`, `PATCH /rooms/{id}/buzzer-settings`, `GET /rooms/{id}/buzz-log?questionId=`.

## 7. Constants
`SYNC_BURST 10×100 ms · SYNC_STEADY 1000 · PREARM_BURST 5×100 · WS_PING 1000 · RING 64 · RTT_MIN_WINDOW 30 s · EWMA α 1/8 · RTTVAR β 1/4 · σ ∈ [1, 30] · ANCHOR_SLACK 15 (ws) / 10 (kernel) · tol = clamp(4σ+2, 5, 60) · u_client 1.5 · u_server 2σ+1 · lead = clamp(rttRef/2+3σ+25, 25, 400) · ARM_JITTER 200–1200 · PRESS_WINDOW 5000 · DRAIN = max_j(rttRef_j + 3σ_j + tol_j) · MAX_COLLECT 400 wan / 200 lan · TIE_MIN 3 · TIE_MAX 40 · MIN_HUMAN 100 (reaction and reactLB) · BOT: ≥8, median<170, MAD<20 · EARLY_BIAS: mean10 < −(σ+3) · CLAMP_RATIO 30 %/10 · TRUST −1/flag, +1/5 clean, thresholds −2/−4/−6 · LOCKOUT 2000 ×2 within 5 s cap 6000 · maxRttMs 80 lan / 350 wan · RATE: PRESS 1/arm, SYNC 20/s, payload 1 KB.`

## 8. Reconnect, loss, background
WebSocket = TCP: a retransmitted ARM lights late (credited only up to `lateCreditMax`, ≈ 0) and a retransmitted PRESS is clamped by `(spike − tol)` — the unavoidable price of bounded trust; WAN profile may carry ARM/ARM_ACK/PRESS as WebTransport datagrams ×3 (0/15/30 ms) deduped by `(armId, seq)`. Resume: `HELLO{resume}` → `SNAPSHOT` → mandatory sync burst; `cold` (< 5 samples) players get `onReceipt` and lose contested sets to clean players; an active arm is re-sent in `onReceipt` mode. Background tab: missing samples → re-burst on visible; while unsynced → `onReceipt`. Disconnected/locked-out players leave `eligible` so the window never waits for them. Media never travels on the buzzer socket; `perMessageDeflate:false`, `TCP_NODELAY`; single room process, monotonic clock; measure event-loop lag and add it to `u_server` when > 5 ms.

## 9. Host-configurable
`netProfile: lan|wan` (sets maxCollectMs, maxRttMs, tolCapMs) · `buttonMode: anchoredHybrid|serverOnly|arrival|randomWindow` · `armJitterMs:[min,max]` (0 disables) · `pressWindowMs` · `maxCollectMs` · `tieMinMs/tieMaxMs` · `tieBreak` · `tolCapMs` · `maxRttMs` · `minHumanMs` (100; 0 only for a visible countdown, which is forbidden together with tieBreak=mostLikely/first-style resolution) · `falseStartLockoutMs, lockoutGrowth, lockoutCapMs` · `allPlayFor: ['forAll'] | ['forAll','simple'] | 'all'` · `writtenAnswerMs, writtenScoring` · `autoTrustActions: on|off` (off = flags only, host decides) · `manualHandicapMs[player]` (0–300, shown to all; no adaptive handicap) · `showPing: true`. Presets: `lanWired` (tieMin 3, arrival-equivalent precision), `wifiParty` (default), `internetFair` (tieMax 40, likelihood, allPlayFor forAll, warn at rtt > 350), `noRace` (written answers only).

## Open risks
- The WS protocol ping anchor is answered by the browser network stack, but the server-side pong timestamp is taken in JS: event-loop lag on the server adds noise common to all players; confirm that the chosen runtime (Bun / uWebSockets.js / ws) exposes per-socket pong events with usable timing before relying on ANCHOR_SLACK = 15 ms.
- Reverse proxies: nginx/Caddy forward WS control frames, but some edges (e.g. certain CDN WebSocket proxies) may answer pings locally — that would collapse the anchor to the proxy RTT and flag honest WAN players as RTT_INFLATED. Deployment guide must require the game process to face the client socket directly, or verify pass-through.
- Non-browser clients (scripts, local TCP relays) can still pose as a slow link; without the kernel anchor the bound is only (maxRttMs − realRtt)/2 plus social visibility of the ping badge. The kernel/QUIC anchor requires uWebSockets.js FFI, Go or Rust — decide the stack with this in mind.
- Bots with a human-like reaction distribution centred at ~170 ms (MAD ≈ 30) are statistically indistinguishable from a very fast human in every design on the panel; only the host's judgement and the public reaction leaderboard address them.
- tol = 4σ + 2 with a MAD-based σ under-covers heavy WiFi tails (phone power-save bursts of 50–150 ms); such presses are clamped (lose up to spike − tol) and, above a 30 % clamp ratio, degrade trust. Monitor clamp ratio per room in the first deployments and consider σ from an upper quantile instead of MAD if honest players get demoted.
- A retransmitted ARM or PRESS on TCP still costs the honest player the question (credit limited to lateCreditMax / clamp to tol); WebTransport datagrams for the WAN profile are unimplemented and unavailable on many LANs without certificates.
- The likelihood tie-break is only calibrated if u_press is honest; the JITTER_INFLATED cap relies on σ_ws, whose 16-sample MAD at 1 ping/s takes ~16 s to settle after connect — cold players should use SIGMA_COLD (20) until then.
- Room maxRttMs forces genuine far-away players into onReceipt (server-only, u ≈ 2σ + 1) — fair but coarser; the host must be told this explicitly, and internetFair rooms should default to allPlay/written mode for such players.
- Host BUTTON_REOPEN remains a social attack surface (players lobbying with 'late_beat' audit entries); keep the audit log immutable and the override rare.
- Same-room LAN parties: players can hear each other's physical presses and screens light at slightly different absolute times (± offset error); this is outside the netcode but affects perceived fairness — the audio beep and a shared host display mitigate it.
