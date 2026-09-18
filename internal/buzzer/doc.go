// Package buzzer is the pure, deterministic fairness core of the SIGame
// buzzer ("AnchoredHybrid"). It has no goroutines, no I/O and no time.Now:
// every time value is a float64 of server monotonic milliseconds passed in
// by the room actor, and all randomness is derived by HMAC-SHA256 from a
// per-arm seed supplied by the caller. The room actor owns the transport,
// stamps receive/send times, schedules the messages the package asks for
// (ArmPlan.SendAt, Arm.NextDeadline) and feeds back what happened.
//
// The full design, the constants table, the reconciliation decisions and the
// host guide live in docs/buzzer.md; this is the executable summary.
//
// # Clock model per connection (Conn)
//
//   - App SYNC 4-tuple, interleaved: rtt = (c4−c1)−(s3−s2), off = ((s2−c1)+(s3−c4))/2;
//     the c4 of sample n arrives inside SYNC n+1 (a ring of 8 pending tuples
//     survives WAN bursts). Ring 64, rtt_min sliding 30 s, rtt_ewma α=1/8,
//     rttvar β=1/4 (RFC 6298), σ_app = max(rttvar, 1.4826·MAD16).
//     offset = median(off) over samples of the last 30 s with
//     rtt ≤ rtt_min + max(2, 0.10·rtt_min) (else lowest-RTT quartile).
//     Step detection: 3 consecutive samples off by > u+5 in one direction reset
//     the ring and mark the connection cold. No drift least-squares in v1.
//   - Anchors the client cannot forge from JS: WebSocket protocol ping/pong
//     (ring 32, σ_ws = 1.4826·MAD16) and, when the server terminates TCP itself,
//     kernel srtt/rttvar. The kernel anchor wins when present.
//     anchor_hi = (rtt_kernel + 10) ?? (rtt_ws_ewma + 15);
//     anchor_lo = kernel: srtt − 2·rttvar − 2; ws (≥ 8 pongs): rtt_ws_min30 − 1 − σ_ws.
//   - rttRef = clamp(rtt_app_ewma, anchor_lo, anchor_hi);
//     σ = clamp(min(σ_app, σ_anchor + 3), 0.5, 30), σ_anchor = the tightest
//     available tamper-proof jitter. An app sample below the floor is discarded
//     and flagged sync_lie (charged at most once per 2 s); rtt_app_ewma above
//     anchor_hi for 6 s flags rtt_inflated (so does ws RTT above the kernel RTT);
//     σ_app > 2σ_anchor + 5 for 6 s flags jitter_inflated.
//   - Derived: resid = clamp(0.10·rtt_min, 2, 10); tol = clamp(resid + 3σ + 2, 5, tolCap);
//     u = resid + 2σ + 1; lead = clamp(rttRef/2 + 3σ + 20, 25, 400).
//   - quality: unsynced (< 5 samples in 10 s, or cold) | good (rttRef < 30 ∧ σ < 5)
//     | fair (σ < 20) | poor. Only unsynced players (or rttRef > maxRttMs) are
//     armed onReceipt and scored server-side; poor is a badge.
//
// # Arming (Arm)
//
// T_arm = max(readEnd, now) + U(armJitterMin, armJitterMax) drawn from the seed.
// Every eligible player gets a frozen snapshot (ONE offset value is used for
// armAtLocal and for the press conversion) and a plan: send BUTTON_ARM at
// T_arm − lead_i (scheduled) or at T_arm − rttRef_i/2 (onReceipt).
// lateCreditMax_i = max(0, tSend_i + rttRef_i/2 + 3σ_i − T_arm) + min(2σ_i + 10,
// lateLightCreditMs); ARM_ACK gives ackExcess_i = max(0, (tAck − tSend_i) − rttRef_i).
//
// # Press scoring
//
// tArr = tRecv − rttRef/2. Superhuman floor from server facts only, in its most
// generous form: reactUB = tRecv − rttMin/2 − max(T_arm, tSend + rttMin/2) with
// rttMin the anchored minimum; reactUB < −u → falseStart (lockout), reactUB <
// minHuman → tooEarly (REJECTED, lockout, flag too_early). Client path
// (scheduled, trust ≥ conservative): tClient = pressLocal + offset,
// credit = clamp(min(litLocal − armAtLocal, ackExcess), 0, lateCreditMax),
// tEst = tClient − credit, tFinal = clamp(tEst, tArr − tol, min(tRecv, tArr + tol));
// a clamp > 5 ms is recorded as out_of_bounds (count only); trust is charged
// through clamp_ratio (> 30 % of the last 10 presses). Server paths: onReceipt
// → tFinal = T_arm + (tArr − tSeen); serverOnly → tArr; arrivalOnly → tRecv.
// reaction = tFinal − T_arm is checked again against −u and minHuman.
// Per-press error: client 2 ms; clamped tol (2 ms when the press is clean and
// was clamped at lo: an honest clamp means it happened even earlier); onReceipt
// resid + 2σ + 2; serverOnly rttRef/4 + 2σ + 2; arrival rttRef/2 + 2σ + 2.
//
// # Collection and decision
//
// The window closes as soon as no eligible player is pending, else at
// min(bestT + W_max + max_j min(rttRef_j/2 + tol_j, 200), firstRecv + maxCollectMs).
// Rank by tFinal; tie window W = tieMs (resolution) or
// clamp(2·hypot(err_i, err_j) + 2, tieMin, tieMax) (uncertainty); clean-first
// presumption (fully trusted, stamp-scored presses without a scoring flag beat
// the rest of the tie set); tie-break likelihood | mostLikely | random |
// lowestScore | fewestButtonsWon | rotate | allPlay with rng = HMAC(seed, "tie").
// Every decision is reproducible from Audit() and Result.Seed.
//
// # Trust ladder
//
// −1 per scoring flag, +1 per 5 clean presses (never above 0, and frozen while
// a sustained detector is active); ≤ −2 conservative (loses contested sets),
// ≤ −4 serverOnly, ≤ −6 arrivalOnly (strict: −1/−2/−3). Scoring flags:
// sync_lie, rtt_inflated, jitter_inflated, clamp_ratio, too_early, early_bias
// (median of 16 tClient − tArr < −(σ + resid + 2), re-charged every 8 presses
// while it persists), bot_like (≥ 8 presses, median < 170, MAD < 25), ack_late
// (ARM_ACK excess > lateAllow + 2σ + 5 in ≥ 4 of 5 arms), rate_limit.
// Count-only: out_of_bounds, stale, duplicate; audit-only: late_beat.
//
// # Compatibility modes
//
// serverArrival ranks by tRecv with no window; randomWindow collects
// RandomWindowMs after the first press and picks uniformly by seed;
// clientReaction ranks the client-reported PressLocal − LitLocal within
// AcceptWindowMs (SIGame FirstWinsClient, unsafe); writtenAll returns Kind
// "allPlay" immediately and the caller runs the written phase.
package buzzer
