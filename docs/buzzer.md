# Buzzer fairness: AnchoredHybrid (`internal/buzzer`)

Status: implemented and simulator-verified (2026-09-19). Inputs: `docs/research/05-netcode-research.md`,
`06-fairness-proposal-1.md` (SyncedHybrid core), `07-fairness-judge-{1,2,3}.md` (three syntheses).
This document reconciles the three syntheses into one specification, lists every decision taken where they
differ, and ends with a guide for hosts (in Russian).

## 1. Problem

Human simple reaction time has a median of ≈ 250 ms with an intra-person SD of ≈ 40 ms. Wired LAN jitter is
< 2 ms, WiFi jitter 4–48 ms with 60–80 ms bufferbloat spikes, WAN RTT 200–300 ms. The original SIGame modes
either rank by server arrival (`FirstWins`: the lowest ping wins), draw a lottery among everyone who pressed
in 300 ms (`RandomWithinInterval`), or trust a client-reported reaction (`FirstWinsClient`: trivially forged).
AnchoredHybrid measures the reaction on the player's own clock from a light scheduled to one server instant,
which is exact for honest players regardless of RTT, and bounds what a dishonest client can gain with
transport measurements the browser cannot forge.

## 2. Architecture

```
client                              server (room actor → internal/buzzer)
  SYNC{seq,c1,prev{seq,c4}} ───────▶ Conn.OnSync(s2,s3)  → SYNC_ACK{c1,s2,s3,model}
  ◀──── WS protocol ping ──── pong ▶ Conn.OnWSPong(rtt)   (browser network stack answers; JS cannot delay it)
                                     Conn.OnKernelRTT(srtt,rttvar)   (TCP_INFO / TCP_CONNECTION_INFO)
  reading ends ─────────────────────▶ NewArm(now, readEnd, players, settings, seed) → T_arm, per-player plans
  ◀── BUTTON_ARM{armAt, armAtLocal} ─ sent at plan.SendAt = T_arm − lead_i;  Arm.OnSent(tSend)
  ARM_ACK ──────────────────────────▶ Arm.OnArmAck(tRecv)             (server-observed delivery lateness)
  light at armAtLocal; press ───────▶ PRESS{pressLocal, litLocal} → Arm.OnPress(tRecv) → PRESS_ACK
                                     Arm.NextDeadline() → room timer → Arm.Resolve(now) → BUTTON_RESULT
```

The package is pure: no goroutines, no I/O, no `time.Now`; all times are server monotonic milliseconds
(`clock.Clock.Mono()`) supplied by the room; randomness is `HMAC-SHA256(seed, label)`, so every decision is
replayable from `Result.Seed` and `Arm.Audit()`.

## 3. Clock model (per connection)

| quantity | definition |
|---|---|
| sample | `rtt = (c4−c1)−(s3−s2)`, `off = ((s2−c1)+(s3−c4))/2` (server − client); c4 of sample n rides in SYNC n+1; 8 pending tuples are kept |
| rings | 64 app samples, 32 ws pongs, MAD over the last 16 |
| `rtt_min` | sliding 30 s minimum |
| `rtt_ewma`, `rttvar` | RFC 6298, α = 1/8, β = 1/4 |
| `σ_app` | `max(rttvar, 1.4826·MAD16)` |
| `offset` | median of `off` over samples of the last 30 s with `rtt ≤ rtt_min + max(2, 0.10·rtt_min)`; lowest-RTT quartile if fewer than 3 |
| step detection | 3 consecutive samples deviating by more than `u + 5` in one direction → ring reset, connection cold |
| ws anchor | `rtt_ws_min30`, `rtt_ws_ewma`, `σ_ws = 1.4826·MAD16(ws)`; the kernel anchor (srtt, rttvar) wins when available |
| `anchor_hi` | `rtt_kernel + 10` or `rtt_ws_ewma + 15` |
| `anchor_lo` | kernel: `srtt − 2·rttvar − 2`; ws (after 8 pongs): `rtt_ws_min30 − 1 − σ_ws` |
| **`rttRef`** | `clamp(rtt_ewma, anchor_lo, anchor_hi)` |
| **`σ`** | `clamp(min(σ_app, σ_anchor + 3), 0.5, 30)`, `σ_anchor` = tightest of `σ_ws`, kernel rttvar |
| `resid` | `clamp(0.10·rtt_min, 2, 10)` |
| **`tol`** | `clamp(resid + 3σ + 2, 5, tolCapMs)` — LAN ≈ 6, WiFi σ 8 ≈ 32, WAN σ 15 ≈ 57 |
| **`u`** | `resid + 2σ + 1` (shown as "sync ±u") |
| **`lead`** | `clamp(rttRef/2 + 3σ + 20, 25, 400)` |
| quality | `unsynced` (< 5 samples in 10 s or cold) · `good` (rttRef < 30 ∧ σ < 5) · `fair` (σ < 20) · `poor` |

Flags raised by the model: `sync_lie` (sample below the floor, ≤ 1 charge per 2 s), `rtt_inflated`
(`rtt_ewma > anchor_hi` for 6 s, or ws RTT above the kernel RTT), `jitter_inflated`
(`σ_app > 2σ_anchor + 5` for 6 s).

## 4. Arming

1. `T_arm = max(readEnd, now) + U(armJitterMinMs, armJitterMaxMs)` from `HMAC(seed, "tArm")`.
2. For every eligible player the model is **frozen** (`offset, rttRef, σ, resid, tol, u, lead, rttMin`) — the same
   offset value is used for `armAtLocal = T_arm − offset` and for the press conversion.
3. `scheduled` (quality ≠ unsynced ∧ rttRef ≤ maxRttMs): send at `T_arm − lead_i`;
   `onReceipt` otherwise: send at `T_arm − rttRef_i/2` (delivery equalisation), the client lights on receipt.
4. `lateCreditMax_i = max(0, tSend_i + rttRef_i/2 + 3σ_i − T_arm) + min(2σ_i + 10, lateLightCreditMs)`;
   `ackExcess_i = max(0, (tAck − tSend_i) − rttRef_i)` from ARM_ACK.
5. `BUTTON_DISARM` at `T_arm + pressWindowMs`; the arm drains for `max_j(rttRef_j + 3σ_j + tol_j)`.

## 5. Press scoring

```
tArr    = tRecv − rttRef/2
reactUB = tRecv − rttMin/2 − max(T_arm, tSend + rttMin/2)     // most generous reaction from server facts
reactUB < −u        → falseStart  (lockout, no trust charge)
reactUB < minHuman  → tooEarly    (REJECTED, lockout, flag too_early)
arrivalOnly:   tFinal = tRecv                                  err = rttRef/2 + 2σ + 2
onReceipt:     tFinal = T_arm + (tArr − tSeen),                err = resid + 2σ + 2
               tSeen = tSend + rttRef/2 + clamp(ackExcess, 0, 2σ + 10)
serverOnly:    tFinal = tArr                                   err = rttRef/4 + 2σ + 2
client path:   tClient = pressLocal + offset
               credit  = clamp(min(litLocal − armAtLocal, ackExcess), 0, lateCreditMax)
               tEst    = tClient − credit
               tFinal  = clamp(tEst, tArr − tol, min(tRecv, tArr + tol))
               source  = client (err 2) | clamped (err tol; clamp > 5 ms → out_of_bounds, count only)
reaction = tFinal − T_arm;  reaction < −u → falseStart;  reaction < minHuman → tooEarly
```

## 6. Collection window

`pending` = eligible, connected, not locked-out players without a press. The window closes immediately when
`pending` is empty, else at `min(bestT + W_max + max_j min(rttRef_j/2 + tol_j, 200), firstRecv + maxCollectMs)`
where `W_max = tieMs` (resolution) or `tieMaxMs` (uncertainty). Presses after the decision are `late`; one
that would have won is recorded as `late_beat` in the audit (decisions are final; the host may reopen).

## 7. Decision

1. Rank by `tFinal`.
2. Tie window `W(lead, x) = tieMs` (resolution) or `clamp(2·hypot(err_lead, err_x) + 2, tieMinMs, tieMaxMs)`
   (uncertainty); honest client–client ties are ≈ 7.7 ms.
3. Clean-first presumption: if the tie set contains presses that are fully trusted, stamp-scored and carry no
   scoring flag, the others are dropped (`rule = cleanFirst`).
4. Tie-break with `rng = HMAC(seed, "tie")`: `likelihood` (default, `w_i = Π_j Φ((t_j − t_i)/hypot(err_i, err_j))`),
   `mostLikely` (argmax), `random`, `lowestScore`, `fewestButtonsWon`, `rotate` (next seat after the previous
   winner, `Arm.SetRotateAfter`), `allPlay` (`Result.Kind = "allPlay"`, `Result.Tie` lists the duellists).

## 8. Anti-cheat

| flag | condition | trust |
|---|---|---|
| `sync_lie` | app RTT below the transport floor | −1 (≤ 1 per 2 s) |
| `rtt_inflated` | app RTT above `anchor_hi` for 6 s; ws RTT above kernel RTT | −1 per episode |
| `jitter_inflated` | `σ_app > 2σ_anchor + 5` for 6 s | −1 per episode |
| `out_of_bounds` | stamp clamped by > 5 ms | count only |
| `clamp_ratio` | > 30 % of the last 10 presses clamped | −1, again every 10 presses while it persists |
| `too_early` | rejected superhuman press | −1 + lockout |
| `early_bias` | median of the last 16 `tClient − tArr` < −(σ + resid + 2) | −1, again every 8 presses while it persists |
| `bot_like` | ≥ 8 presses, median reaction < 170 ms and MAD < 25 ms | −1, again every 8 presses |
| `ack_late` | ARM_ACK excess > lateAllow + 2σ + 5 in ≥ 4 of the last 5 arms | −1 per episode |
| `rate_limit` | > 5 PRESS per second | −1 per second, message dropped |
| `stale`, `duplicate` | press for an inactive arm / second press | count only |

Ladder: `+1` per 5 clean presses, never above 0 and frozen while a sustained detector is active; `≤ −2`
**conservative** (loses contested sets, `err ≥ u`), `≤ −4` **serverOnly**, `≤ −6` **arrivalOnly**
(tournament ladder: −1/−2/−3). `autoTrust=false` keeps the flags but never demotes; the host can force a
level per player (`Conn.ForceLevel`).

Residual bounds (browser-JS attacker, ws anchor): one-shot stamp lie ≤ `tol` (LAN ≈ 6, WiFi ≈ 30–40, WAN ≤ 80),
flagged from the 5th press; sustained shave below the bias threshold ≈ `σ + resid + 2` (LAN ≈ 5, WiFi ≈ 18,
WAN ≈ 30); RTT/σ inflation ≤ anchor slack (7.5 ms via rttRef, 9 ms via tol); foreknowledge with a
light-on-receipt mod ≈ `3σ + 20` (the reaction floor still applies). Non-browser client without a kernel
anchor: ≤ `(maxRttMs − rtt)/2`, visible as a high ping badge; with the kernel anchor ≤ 5 ms and flagged.

## 9. Modes and presets

| mode | behaviour |
|---|---|
| `anchoredHybrid` | this document (default) |
| `serverArrival` | SIGame `FirstWins`: rank by `tRecv`, no window, press before `T_arm` = false start |
| `randomWindow` | SIGame `RandomWithinInterval`: `randomWindowMs` after the first press, uniform pick by seed |
| `clientReaction` | SIGame `FirstWinsClient`: rank by `pressLocal − litLocal` inside `acceptWindowMs`; missing/invalid delta = worst. **Unsafe** |
| `writtenAll` | `NewArm` returns `Kind = "allPlay"` at once; the room runs the written phase |

| preset | profile | ties | tie-break | jitter | maxCollect | tolCap | lockout | maxRtt | notes |
|---|---|---|---|---|---|---|---|---|---|
| `lanWired` | lan | resolution 2 ms | mostLikely | 300–800 | 200 | 40 | 1000 | 80 | |
| `wifiParty` (default) | wifi | uncertainty 3–40 | likelihood | 200–1200 | 400 | 60 | 3000 ×2 / 10 s, cap 6000 | 200 | |
| `internetFair` | wan | uncertainty 3–60 | likelihood | 200–1200 | 600 | 80 | 3000 | 350 | use when a remote guest joins |
| `tournament` | lan | resolution 2 ms | mostLikely | 300–1200 | 200 | 40 | 1000 | 80 | lateLightCredit 0, strict ladder |
| `noRace` | — | — | allPlay | — | — | — | — | — | mode `writtenAll` |

## 10. Constants

| name | value |
|---|---|
| SYNC burst / steady / pre-arm | 10 × 100 ms on connect, reconnect, visible · 1000 ms · 5 × 100 ms when reading starts |
| WS ping | 1000 ms idle / 250 ms reading+armed · pending SYNC tuples 8 |
| rings | 64 app / 32 ws · MAD window 16 · RTT_MIN_WINDOW 30 s · EWMA α 1/8 · rttvar β 1/4 · MAD scale 1.4826 |
| anchors | slack 10 ms kernel / 15 ms ws · floor margin 1 ms + σ_ws (after 8 pongs) · σ slack 3 · σ ∈ [0.5, 30] · σ_cold 20 |
| derived | resid = clamp(0.10·rtt_min, 2, 10) · EPS_INPUT 2 · tol = clamp(resid + 3σ + 2, 5, tolCap) · u = resid + 2σ + 1 · lead = clamp(rttRef/2 + 3σ + 20, 25, 400) |
| windows | PRESS_WINDOW 5000 · MAX_COLLECT lan 200 / wifi 400 / wan 600 · WAIT_PER_PLAYER_MAX 200 · DRAIN max_j(rttRef_j + 3σ_j + tol_j) |
| ties | TIE_MS 2 · uncertainty W = clamp(2·hypot(err_i, err_j) + 2, tieMin, tieMax) · client–client ≈ 7.7 ms |
| floors | MIN_HUMAN 100 (reject + lockout) · false start below −u · lockout 3000, ×2 within 10 s, cap 6000 |
| detectors | early_bias median16 < −(σ + resid + 2) · bot ≥ 8, median < 170, MAD < 25 · clamp_ratio > 30 %/10 · ack_late ≥ 4/5 · rate 5 PRESS/s |
| trust | −1/flag · +1 per 5 clean · conservative −2 · serverOnly −4 · arrivalOnly −6 (strict −1/−2/−3) |
| quality | unsynced < 5 samples/10 s · good rttRef < 30 ∧ σ < 5 · fair σ < 20 · CONN_QUALITY every 5 s |

## 11. Decisions taken (where the syntheses differ)

1. **σ definition** — `max(rttvar, 1.4826·MAD16)` (judge 1/3 used raw MAD, judge 2 scaled MAD for ws only): one scale
   for app and ws so the `+3` slack compares like with like.
2. **σ ceiling** — `min(σ_app, σ_anchor + 3)` with the tightest tamper-proof anchor (judge 3's `σ_ws + 2`, judge 2's
   `3σ_ws + 3`): +3 covers browser scheduling noise without leaving room for inflation.
3. **rttRef** — `clamp(rtt_ewma, anchor_lo, anchor_hi)` with `anchor_hi = kernel + 10` or `ws + 15`: the
   kernel srtt is smoother than pongs answered by the browser process, so it gets the smaller slack.
4. **Floor** — jitter-aware (`rtt_ws_min − 1 − σ_ws`, after 8 pongs) and kernel-first: a downward RTT lie has no
   payoff, so the floor is a plausibility check and must not produce false `sync_lie` on bursty links.
5. **resid instead of asymAllow** — `clamp(0.10·rtt_min, 2, 10)` (judge 1 & 2 verified that constant asymmetry
   cancels under scheduled arming); judge 3's 25 % budget is pure cheat budget.
6. **tol / u / lead** — `resid + 3σ + 2` (cap per profile), `resid + 2σ + 1`, `rttRef/2 + 3σ + 20` clamped to
   [25, 400]: judge 3's +20 lead base over judge 1's +15 for late-ARM robustness; judge 2's 400 cap keeps WAN arms alive.
7. **Late-light credit** — `min(litLocal − armAtLocal, ackExcess)` capped by `lateCreditMax` (judge 3): credit only as
   far as the server itself saw the delivery late; tournaments set `lateLightCreditMs = 0`.
8. **Reaction floor** — judge 2's `reactLB = tRecv − tSend − rttMax16` is a *lower* bound and rejected honest 150 ms
   reactions on spiky WiFi in simulation; we reject on the *most generous* bound
   `reactUB = tRecv − rttMin/2 − max(T_arm, tSend + rttMin/2)` from anchored server facts, then re-check the final
   reaction. Reject, never clamp (judge 3).
9. **Per-press error** — client 2 ms (judge 1's EPS_INPUT rather than judge 2's 1.5), clamped `tol` except a clean
   press clamped at `lo` (2 ms: an honest clamp means the press was even earlier — the lie case is the ladder's job),
   onReceipt `resid + 2σ + 2` (light and press share the path, asymmetry cancels), serverOnly `rttRef/4 + 2σ + 2`.
10. **Tie window** — uses per-press `err`, not the model's `u` (judge 2): reaction from the own light cancels the
    offset error, so honest client–client ties are ≈ 7.7 ms, not 2u.
11. **Clean-first** — judged on trust level and *scoring* flags only (judge 2's "single clamps never touch trust"):
    counting a single `out_of_bounds` handed 40 ms leads to the second player in simulation.
12. **Trust charging** — `out_of_bounds` counts only; `clamp_ratio > 30 %/10`, `early_bias` (median of 16, not mean of
    10: 15 % WiFi spikes tripped the mean) and `bot_like` re-charge while they persist and freeze the clean streak,
    closing judge 3's "one steal per five presses" leak.
13. **ack_late** — threshold `lateAllow + 2σ + 5` in ≥ 4 of 5 arms: honest WiFi spikes hit `lateAllow + 2` in 20 % of
    arms, which demoted honest players.
14. **poor quality is a badge** — scheduled arming with client stamps beats arrival scoring for honest players at any
    σ; only `unsynced` and `rttRef > maxRttMs` fall back to `onReceipt`.
15. **Bot detector** — median < 170 ∧ MAD < 25 over the last 16 (judge 2's 170/20, judge 3's 160/15): catches the
    `U(110, 190)` bot the judges describe while an elite human (median ≥ 190, MAD ≈ 27) stays clear.
16. **Rotate tie-break** — the room passes seats and `SetRotateAfter(seat)`; the package keeps no cross-arm state.
17. **Seed** — the room supplies a per-arm seed; `armId`, `T_arm` jitter and every lottery derive from it, and
    `Result.Seed` publishes it so the host can replay a decision.

## 12. Simulator results

Event-driven, deterministic (`math/rand` with fixed seeds), three honest players with reaction `N(250, 40)`
floored at 150 ms, 400 questions per scenario, half-normal one-way jitter, 15 % bufferbloat spikes ≤ 60 ms on
WiFi, 0.2 % 200 ms TCP RTO stalls in `wifi+rto`. "Faster wins" = the player with the smallest drawn reaction wins.

| scenario (preset) | gap ≥ 10 ms | gap ≥ 20 ms | gap ≥ 40 ms | contested | clamped presses |
|---|---|---|---|---|---|
| lan 5–15 ms, σ 1 (lanWired) | 100.0 % (316) | 100.0 % (243) | 100.0 % (136) | 18 | 0 |
| wifi 40–80 ms, σ 5–15 + spikes (wifiParty) | 99.7 % (316) | 99.6 % (251) | 100.0 % (133) | 62 | 40 / 1200 |
| wan 200–300 ms, σ 10–20 (internetFair) | 100.0 % (324) | 100.0 % (256) | 100.0 % (143) | 58 | 0 |
| mixed lan + wifi + wan (internetFair) | 99.1 % (323) | 100.0 % (254) | 100.0 % (149) | 65 | 30 |
| wifi + RTO stalls (wifiParty) | 99.7 % (319) | 99.6 % (254) | 100.0 % (132) | 69 | 27 |
| mixed on wifiParty (WAN guest > maxRtt → onReceipt) | 82.1 % | 87.9 % | 96.0 % | 227 | 18 |
| mixed, mode serverArrival (SIGame FirstWins) | 38.7 % | 37.3 % | 48.1 % | 0 | 0 |

Adversaries (3 players, cheater X): 30 ms stamp shaving on LAN — every press clamped, gain ≤ tol (6.4 ms),
`clamp_ratio` from press 5, serverOnly after 32 questions; on WiFi — inside tol (gain ≤ 36 ms per press),
`early_bias` after 16 presses, conservative after 32. SYNC-delay σ inflation — σ 8.3 vs honest 11.2, tol 32 vs 41,
`jitter_inflated` + `rtt_inflated`. c4 inflation (+60 ms) — rttRef capped at ws + 15, gain ≤ tol, `rtt_inflated`.
Pong delay (+50 ms, non-browser) — with the kernel anchor: gain 0, `rtt_inflated`; ws-only: every app sample below
the floor, `sync_lie` storm → arrivalOnly. Light-on-receipt — every press rejected (`tooEarly`/`falseStart`),
0 wins. Humanized bot `U(110, 190)` — wins ≥ 9 of 12 buttons (nothing in the netcode can stop it) and is badged
`bot_like` after 8 presses. A single 200 ms RTO stall on the fastest player's PRESS costs `stall − tol`, is
recorded `out_of_bounds` without a trust charge, and a 260 ms lead still wins.

## 13. Open risks

- A humanized autoclicker is indistinguishable from an elite human by any network scheme; `bot_like` is a badge
  for the host, not proof.
- The ws ping anchor assumes no intermediary answers pings on the browser's behalf; deploy the game process
  facing the socket (the kernel anchor needs that anyway).
- A TCP RTO stall on PRESS or ARM costs the honest player `stall − tol` (or `late − lateAllow`); WebTransport
  datagrams are not implemented.
- `mixed/wifiParty`: a guest above `maxRttMs` is scored server-side and turns his close races into calibrated
  lotteries — hosts must switch to `internetFair` (the UI should suggest it when a player's ping exceeds the cap).
- Non-browser clients without a kernel anchor can pose as slower by up to `(maxRttMs − rtt)/2`; only the badge
  and the host's judgement remain.
- No clock-drift model: 100 ppm over the 30 s offset window is ≈ 3 ms, inside `resid` on WiFi/WAN but comparable
  to `tol` on wired LAN; the pre-arm burst keeps the window short.
- The ladder still forgives an isolated `≤ tol` steal every ~10 presses (clamp_ratio needs 4 of 10).

## 14. Integration notes (room actor)

- One `Conn` per WebSocket connection (`NewConn(mono)`); call `OnSync` with `s2` stamped before parsing and `s3`
  right before the write; call `OnWSPong` from the pong callback; `OnKernelRTT` from `TCP_INFO` polling;
  `OnVisible` when the client reports `visibilitychange`. Copy `Settings.TolCapMs/AutoTrust/StrictTrust`
  onto the `Conn` (NewArm does it too).
- Per question: `NewArm(mono, readEnd, players, settings, seed)`, send `Plans()[i].Msg` at `SendAt`, then
  `OnSent(id, mono)` in the write callback; `OnArmAck` on ARM_ACK; `OnPress` with `tRecv` stamped before parsing;
  after every call re-read `NextDeadline()` and arm a timer that calls `Resolve(mono)`; `State() == draining` after
  `Resolve` returns false means "send BUTTON_DISARM". `SetEligible(id, false, mono)` on disconnect. `OnMisfire` on
  MISFIRE. Lockouts live in `Conn.LockedUntil()` and survive arms.
- `Result.Kind == "allPlay"` → run the written phase for `Result.Tie`; `Audit()` feeds `BUTTON_AUDIT` and the buzz log.

---

# Для ведущего

**Почему кнопка честная.** Сервер не сравнивает, чьё нажатие *дошло* первым — при таком подходе всегда
выигрывает тот, у кого меньше пинг. Вместо этого сервер заранее договаривается с каждым клиентом о точном моменте,
когда загорится лампочка (часы игроков синхронизируются по протоколу, похожему на NTP), и измеряет **реакцию от
собственной лампочки игрока** по его же часам. Игрок с пингом 250 мс, нажавший через 285 мс после лампочки,
побеждает игрока с пингом 10 мс, нажавший через 300 мс, хотя его пакет пришёл на сервер на 100 мс позже. Сервер ждёт
ровно столько, сколько нужно, чтобы нажатие более медленного канала успело дойти (обычно 10–150 мс), и только затем
объявляет победителя. Ошибка измерения для честного игрока не зависит от пинга и показывается всем как «±u».

**Как это защищено от обмана.** Клиентская метка времени принимается только в коридоре ±tol вокруг серверной оценки,
а сам коридор считается из величин, которые браузер подделать не может: сервер меряет пинг служебными WebSocket-пингами
(на них отвечает сетевой стек браузера, а не скрипт страницы) и, если процесс игры сам держит TCP-соединение, из ядра
ОС. Поэтому «накрутить» себе пинг или джиттер, чтобы расширить коридор, нельзя. Нажатие раньше, чем через 100 мс после
лампочки, отклоняется как нечеловеческое и блокирует кнопку (фальстарт — 3 с, при повторе — 6 с). Максимум, что
может выиграть модифицированный клиент за одно нажатие, — это tol (≈ 6 мс в проводной сети, 30–40 мс по Wi‑Fi), и после
нескольких таких нажатий он попадает под флаги и лестницу доверия.

**Значки и флаги.** *Качество синхронизации*: `good` — точные часы (пинг < 30 мс, джиттер < 5 мс), `fair` — нормальная
игра, `poor` — плохой канал, `unsynced` — часы ещё не сверены (после входа, переподключения или свёрнутой вкладки);
такой игрок в этом вопросе играет «по прибытию» пакета. *Спорный результат* (`contested`) означает, что два нажатия
оказались внутри окна погрешности и победитель выбран по правилу розыгрыша (по умолчанию — взвешенная лотерея
`likelihood`, где шанс пропорционален правдоподобию; в турнирном пресете — детерминированный `mostLikely`).
*Флаги честности*: `sync_lie`, `rtt_inflated`, `jitter_inflated` — клиент сообщает про сеть неправду; `out_of_bounds` —
метка нажатия вышла за коридор (одиночный случай на Wi‑Fi — норма, серия — `clamp_ratio`); `too_early` — нечеловеческая
реакция; `early_bias` — нажатия систематически «раньше, чем возможно»; `bot_like` — подозрительно ровные быстрые
реакции (это только подсказка: автокликер с «человеческим» разбросом отличить нельзя); `ack_late` — подтверждения
приходят с задержкой. *Уровень доверия*: `full` → `conservative` (проигрывает спорные розыгрыши честным) → `serverOnly`
(его метки не используются) → `arrivalOnly` (считается только время прихода). Ведущий видит всё это в `CONN_QUALITY` и
`BUTTON_AUDIT`, может выключить автоматическое понижение (`autoTrust`), принудительно понизить игрока, заново открыть
кнопку или перевести вопрос в письменный режим. Для игры через интернет выбирайте пресет `internetFair`, для
турнира — `tournament`, если гонка не нужна — `noRace`.
