// Package room hosts the game rooms of the server. A Room is a single
// goroutine actor with a mailbox: client envelopes (stamped with their
// monotonic receive time by the transport before JSON decoding), timer
// expiries, connection attach/detach, host REST changes, AI verdicts,
// buzzer send-plan ticks and TTL/close requests are all serialised through
// it, so the pure engine (internal/engine) and buzzer (internal/buzzer) are
// only ever touched from that goroutine.
//
// # Manager
//
// Manager creates rooms (5-character join codes), joins persons (session
// tokens), applies host actions (settings, kick/ban, host transfer, buzz
// log, close), attaches WebSocket connections (Attach → Attachment with
// Deliver/Detach) and closes idle rooms after RoomTTL. Get/List read a cached
// projection (Info) and never wait for the actor.
//
// # Connections
//
// A person has at most one live connection; a new attach replaces the old
// one (SESSION_REPLACED, close 4001). On attach the room sends WELCOME, then
// SNAPSHOT (room Info, the role-projected engine snapshot or null in the
// lobby, and the buzzer state). A client that lost a connection sends
// HELLO{lastSeq} and receives RESUME followed by the replayable envelopes of
// its previous connection (chat, lockouts, timer progress); the snapshot is
// authoritative for everything else. Server seq numbers are assigned per
// connection by the transport; the room records them in a per-person ring
// of ResumeBufferSize envelopes.
//
// # Timers
//
// Engine timers are real time.AfterFunc timers that post TIMEOUT commands to
// the mailbox; only the AtMs stamps and buzzer times come from clock.Clock.
// mediaFallback timers take the longest probed media duration of the current
// content group, or MediaFallbackMaxMs when nothing is known.
//
// # Buzzer
//
// BUTTON_ARM_REQUEST creates a buzzer.Arm from the seated players (eligible
// when connected, allowed by the engine and not locked out), schedules every
// BUTTON_ARM at its planned send time, forwards PRESS/ARM_ACK/MISFIRE, and
// resolves the arm at its next deadline: BUTTON_RESULT (full ranking to the
// showman/host, decision + own reaction to players), BUTTON_AUDIT to the host,
// then CmdButtonResult. Per connection the room measures the tamper-proof RTT
// anchors (WebSocket ping every 1000 ms, 250 ms while an arm is open; kernel
// RTT every 1000 ms) and broadcasts CONN_QUALITY every ConnQualityEveryMs.
// The writtenAll buzzer mode and the allPlay tie-break are rejected with
// ErrUnsupportedBuzzerMode (the written duel is not implemented in v1).
//
// # Showman modes
//
// human: a human showman validates; the fuzzy judge decides on
// VALIDATION_TIMEOUT. ai: the AI pseudo-person "ai-showman" is the engine's
// showman; ASK_VALIDATE goes to the judge and ties are resolved at random.
// hybrid: the human showman receives AI_SUGGESTION; the AI verdict applies
// after HybridConfirmMs without a human verdict.
package room
