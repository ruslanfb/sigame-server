// Package engine is the pure, deterministic game core of the SIGame server.
//
// The room actor owns one Game per room, feeds it Commands (client actions,
// timer expiries, buzzer decisions, validation verdicts, connection changes)
// and receives Events to broadcast per audience plus timer / buzzer requests.
// The engine never starts goroutines, never does I/O and never reads the
// clock: every random choice comes from a seeded PCG generator and every
// duration computation uses the AtMs stamp the room puts on each Command.
// Replaying the same command stream therefore yields the same events.
//
// # Stages
//
//	lobby ──START──▶ roundIntro ──▶ selecting ◀──────────────┐
//	                    │              │ CHOOSE / timeout      │
//	                    │ final round  ▼                       │ QUESTION_END
//	                    ▼           question ──────────────────┘
//	              finalThemes ──▶ (last theme) question ──▶ roundEnd ──▶ next round | gameEnd
//
// Question sub-states (QSub):
//
//	announcing → [secretTransfer → priceSelect] | [stakes] → content
//	   → buttonWait ⇄ answering → validating → reveal → end
//	   → answering (direct) → validating → reveal → end
//	   → hiddenAnswering → validating (sequential reveal) → reveal → end
//
// # Timers
//
// The engine emits TIMER_START{id, kind, durationMs}; the room schedules the
// deadline and sends CmdTimeout{TimerID} when it fires. TIMER_STOP cancels,
// TIMER_PAUSE/TIMER_RESUME carry the remaining time. Stale timeouts (unknown
// id) are ignored. A TIMER_START with kind "mediaFallback" and durationMs 0
// asks the room to pick a fallback duration for media of unknown length; the
// engine also accepts CmdMediaCompleted (per player, or aggregated with an
// empty PersonID) to move on earlier.
//
// # Buzzer
//
// Button fairness lives in internal/buzzer. The engine emits
// BUTTON_ARM_REQUEST when the button should open (with the eligible players
// and the remaining thinking time) and BUTTON_DISARM_REQUEST when it closes;
// the room answers with CmdButtonResult{WinnerID} or {Nobody}. After a wrong
// answer the engine re-arms for the remaining players and resumes the
// thinking timer with the remaining time.
//
// # Validation
//
// Text answers go to the showman as ASK_VALIDATE{autoAfterMs}. If the
// showman does not decide in time the engine emits VALIDATION_TIMEOUT (room
// only) and waits; the room runs its fuzzy/AI judge and replies with
// CmdValidate{Source: "auto"|"ai"}. Non-text answer types (select, number,
// point, client) are validated by the engine.
//
// # Projection
//
// Events for players/viewers never contain right answers, showman comments,
// wrong-answer lists, hidden stakes or unrevealed written answers; those go
// to the showman audience only. Snapshot applies the same rules.
package engine
