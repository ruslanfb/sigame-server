package buzzer

import (
	"encoding/hex"
	"math"
	"sort"
	"strconv"
)

// Player is the room's view of a participant when an arm is created.
type Player struct {
	ID         string
	Score      int
	ButtonsWon int
	Conn       *Conn
	Eligible   bool // connected, allowed to press on this question
	Seat       int  // table order, used by the "rotate" tie-break
}

// ArmMsg is the BUTTON_ARM payload for one player.
type ArmMsg struct {
	ArmID      string  `json:"armId" doc:"128-bit nonce of this arm."`
	ArmAt      float64 `json:"armAt" doc:"Server monotonic time of the light."`
	ArmAtLocal float64 `json:"armAtLocal" doc:"The same instant on the player's own clock (performance.now)."`
	Mode       string  `json:"mode" doc:"scheduled: light at armAtLocal; onReceipt: light immediately."`
	DeadlineAt float64 `json:"deadlineAt" doc:"Server time when the button disarms."`
	LockoutMs  int64   `json:"lockoutMs" doc:"False-start lockout the client should apply locally."`
}

// ArmPlan tells the room to send Msg to PlayerID at SendAt (server monotonic)
// and to call Arm.OnSent with the real send time.
type ArmPlan struct {
	PlayerID string
	SendAt   float64
	Msg      ArmMsg
}

// PressIn is a PRESS message.
type PressIn struct {
	PlayerID   string
	Seq        int64
	PressLocal float64 // client clock at pointerdown
	LitLocal   float64 // client clock when the light went on
	Src        string  // pointer | key (diagnostic)
}

// PressAck is the PRESS_ACK payload.
type PressAck struct {
	Status       string  `json:"status" doc:"pending | rejected | duplicate | late | stale | lockedOut | tooEarly | falseStart."`
	Reason       string  `json:"reason,omitempty" doc:"Machine-readable detail."`
	LockoutUntil float64 `json:"lockoutUntil,omitempty" doc:"Server time until which the player is locked out."`
	ReactionMs   float64 `json:"reactionMs,omitempty" doc:"Scored reaction (pending/late)."`
}

// Press statuses.
const (
	StatusPending    = "pending"
	StatusRejected   = "rejected"
	StatusDuplicate  = "duplicate"
	StatusLate       = "late"
	StatusStale      = "stale"
	StatusLockedOut  = "lockedOut"
	StatusTooEarly   = "tooEarly"
	StatusFalseStart = "falseStart"
)

// Press sources.
const (
	SourceClient  = "client"  // client stamp inside the trust bounds
	SourceClamped = "clamped" // client stamp clamped to the bounds
	SourceServer  = "server"  // rewound arrival
	SourceArrival = "arrival" // raw arrival
)

// Arm states.
const (
	StateArmed      = "armed"
	StateCollecting = "collecting"
	StateDraining   = "draining"
	StateResolved   = "resolved"
	StateDisarmed   = "disarmed"
)

// Result kinds.
const (
	KindWinner  = "winner"
	KindNobody  = "nobody"
	KindAllPlay = "allPlay"
)

// Ranking is one line of the BUTTON_RESULT ranking.
type Ranking struct {
	PlayerID   string   `json:"playerId"`
	ReactionMs float64  `json:"reactionMs" doc:"Reaction from the player's own light (ms)."`
	ErrMs      float64  `json:"errMs" doc:"Per-press measurement error (ms)."`
	RttMs      float64  `json:"rttMs" doc:"Reference RTT of the player at arming."`
	UMs        float64  `json:"uMs" doc:"Displayed sync uncertainty of the player."`
	Source     string   `json:"source" doc:"client | clamped | server | arrival."`
	Flags      []string `json:"flags,omitempty" doc:"Flags raised by this press."`
}

// Result is the BUTTON_RESULT payload.
type Result struct {
	ArmID      string    `json:"armId"`
	WinnerID   string    `json:"winnerId,omitempty" doc:"Empty when nobody wins or the tie goes to a written duel."`
	Kind       string    `json:"kind" doc:"winner | nobody | allPlay."`
	Contested  bool      `json:"contested" doc:"More than one press was inside the tie window."`
	TieBreak   TieBreak  `json:"tieBreak" doc:"Configured tie-break rule."`
	Rule       string    `json:"rule" doc:"How the winner was chosen: single | cleanFirst | first | <tieBreak>."`
	MarginMs   float64   `json:"marginMs" doc:"Gap between the two best presses (ms)."`
	Ranking    []Ranking `json:"ranking"`
	Tie        []string  `json:"tie,omitempty" doc:"Players inside the tie window (for allPlay duels)."`
	ResolvedAt float64   `json:"resolvedAt"`
	CollectMs  float64   `json:"collectMs" doc:"Decision latency after the first press reached the server."`
	Seed       string    `json:"seed" doc:"Hex seed that reproduces the lottery."`
}

// AuditEntry records every press evaluation for BUTTON_AUDIT / the buzz log.
type AuditEntry struct {
	PlayerID string
	Status   string
	TRecv    float64
	TArr     float64
	TClient  float64
	Lo, Hi   float64
	Credit   float64
	TFinal   float64
	Reaction float64
	Source   string
	Flags    []string
	Model    Model
}

type armPlayer struct {
	id         string
	score      int
	buttonsWon int
	seat       int
	conn       *Conn
	m          snapshot
	level      TrustLevel
	mode       string
	eligible   bool

	armAtLocal    float64
	sendAt, tSend float64
	lateAllow     float64
	lateCreditMax float64
	acked         bool
	ackExcess     float64
	pressed       bool
}

type press struct {
	pid                                  string
	seq                                  int64
	tRecv, tArr, tClient, lo, hi, credit float64
	tEst, tFinal, reaction, err          float64
	source                               string
	clampDir                             int // -1 clamped at lo, +1 clamped at hi
	flags                                []string
	clean                                bool
	ap                                   *armPlayer
}

// Arm is one armed button (one question). Create with NewArm, drive with
// OnSent/OnArmAck/OnPress, and call Resolve at NextDeadline.
type Arm struct {
	s        Settings
	seed     []byte
	id       string
	created  float64
	tArm     float64
	disarmAt float64
	drainEnd float64
	state    string
	players  []*armPlayer
	byID     map[string]*armPlayer
	plans    []ArmPlan
	presses  []*press
	audit    []AuditEntry

	hasFirst  bool
	firstRecv float64
	bestT     float64
	closeAt   float64

	result      Result
	rotateAfter int
}

// NewArm freezes the per-player models, draws T_arm = max(readEnd, now) +
// U(armJitterMin, armJitterMax) from the seed and builds the send plans.
// In writtenAll mode the arm is resolved immediately with Kind "allPlay".
func NewArm(now, readEnd float64, players []Player, s Settings, seed []byte) *Arm {
	if len(seed) == 0 {
		seed = []byte("sigame-buzzer:" + strconv.FormatFloat(now, 'f', 3, 64))
	}
	a := &Arm{s: s, seed: seed, id: armID(seed), created: now, byID: map[string]*armPlayer{}, rotateAfter: -1}
	jitter := float64(s.ArmJitterMinMs) + unitFloat(seed, "tArm")*float64(s.ArmJitterMaxMs-s.ArmJitterMinMs)
	a.tArm = math.Max(readEnd, now) + jitter
	a.disarmAt = a.tArm + float64(s.PressWindowMs)
	drain := 0.0
	for _, p := range players {
		if !p.Eligible {
			continue
		}
		ap := &armPlayer{id: p.ID, score: p.Score, buttonsWon: p.ButtonsWon, seat: p.Seat, conn: p.Conn, eligible: true, level: TrustFull}
		if p.Conn != nil {
			p.Conn.TolCapMs = float64(s.TolCapMs)
			p.Conn.AutoTrust = s.AutoTrust
			p.Conn.StrictTrust = s.StrictTrust
			ap.m = p.Conn.snap(now)
			ap.level = p.Conn.Trust().Level
		} else {
			ap.m = NewConn(now).snap(now)
		}
		m := &ap.m
		synced := m.Quality != QualityUnsynced && m.RttRefMs <= float64(s.MaxRttMs)
		if s.Mode == ModeAnchoredHybrid && synced {
			ap.mode = "scheduled"
			ap.sendAt = a.tArm - m.LeadMs
		} else {
			ap.mode = "onReceipt"
			ap.sendAt = a.tArm
			if s.Mode == ModeAnchoredHybrid {
				ap.sendAt = a.tArm - m.RttRefMs/2
			}
		}
		if ap.sendAt < now {
			ap.sendAt = now
		}
		ap.tSend = ap.sendAt
		ap.armAtLocal = a.tArm - m.OffsetMs
		ap.lateAllow = math.Min(2*m.SigmaMs+10, float64(s.LateLightCreditMs))
		ap.recomputeCredit(a.tArm)
		if d := m.RttRefMs + 3*m.SigmaMs + m.TolMs; d > drain {
			drain = d
		}
		a.players = append(a.players, ap)
		a.byID[ap.id] = ap
		a.plans = append(a.plans, ArmPlan{PlayerID: ap.id, SendAt: ap.sendAt, Msg: ArmMsg{
			ArmID: a.id, ArmAt: a.tArm, ArmAtLocal: ap.armAtLocal, Mode: ap.mode, DeadlineAt: a.disarmAt,
			LockoutMs: s.FalseStartLockoutMs,
		}})
	}
	sort.SliceStable(a.plans, func(i, j int) bool { return a.plans[i].SendAt < a.plans[j].SendAt })
	a.drainEnd = a.disarmAt + drain
	a.state = StateArmed
	if s.Mode == ModeWrittenAll {
		a.state = StateResolved
		a.plans = nil
		a.result = Result{ArmID: a.id, Kind: KindAllPlay, Contested: true, TieBreak: TieBreakAllPlay, Rule: string(TieBreakAllPlay),
			ResolvedAt: now, Seed: hex.EncodeToString(seed)}
		for _, ap := range a.players {
			a.result.Tie = append(a.result.Tie, ap.id)
		}
	}
	return a
}

func (ap *armPlayer) recomputeCredit(tArm float64) {
	ap.lateCreditMax = math.Max(0, ap.tSend+ap.m.RttRefMs/2+3*ap.m.SigmaMs-tArm) + ap.lateAllow
}

// ID returns the arm nonce.
func (a *Arm) ID() string { return a.id }

// TArm returns the server instant of the light.
func (a *Arm) TArm() float64 { return a.tArm }

// Plans returns the BUTTON_ARM send plan sorted by SendAt.
func (a *Arm) Plans() []ArmPlan { return a.plans }

// State returns armed | collecting | draining | resolved | disarmed.
func (a *Arm) State() string { return a.state }

// SetRotateAfter tells the "rotate" tie-break which seat won the previous button (-1 = none).
func (a *Arm) SetRotateAfter(seat int) { a.rotateAfter = seat }

// SetEligible marks a player (dis)connected mid-arm; ineligible players never
// extend the collection window and cannot press.
func (a *Arm) SetEligible(playerID string, eligible bool, now float64) {
	ap := a.byID[playerID]
	if ap == nil {
		return
	}
	ap.eligible = eligible
	if a.state == StateCollecting {
		a.reschedule(now)
	}
}

// OnSent records the real send time of BUTTON_ARM for a player.
func (a *Arm) OnSent(playerID string, tSend float64) {
	ap := a.byID[playerID]
	if ap == nil {
		return
	}
	ap.tSend = tSend
	ap.recomputeCredit(a.tArm)
}

// OnArmAck records the ARM_ACK: the server-observed lateness of this delivery.
func (a *Arm) OnArmAck(playerID string, tRecv, recvLocal float64) {
	ap := a.byID[playerID]
	if ap == nil || ap.acked {
		return
	}
	ap.acked = true
	ap.ackExcess = math.Max(0, (tRecv-ap.tSend)-ap.m.RttRefMs)
	if ap.conn != nil {
		ap.conn.noteAck(ap.ackExcess, ap.lateAllow, ap.m.SigmaMs, tRecv)
	}
	_ = recvLocal // diagnostic only; no credit is derived from the client's stamp
}

// OnPress scores a PRESS received at tRecv (stamped before parsing).
func (a *Arm) OnPress(in PressIn, tRecv float64) PressAck {
	ap := a.byID[in.PlayerID]
	if ap == nil || !ap.eligible {
		return PressAck{Status: StatusStale, Reason: "notEligible"}
	}
	c := ap.conn
	if c != nil && !c.notePress(tRecv) {
		return PressAck{Status: StatusRejected, Reason: "rateLimit"}
	}
	switch a.state {
	case StateDisarmed:
		if c != nil {
			c.softFlag(FlagStale)
		}
		return PressAck{Status: StatusStale, Reason: "disarmed"}
	case StateResolved:
		return a.latePress(ap, in, tRecv)
	}
	if ap.pressed {
		if c != nil {
			c.softFlag(FlagDuplicate)
		}
		return PressAck{Status: StatusDuplicate}
	}
	if c != nil && c.lockedUntil > tRecv {
		return PressAck{Status: StatusLockedOut, LockoutUntil: c.lockedUntil}
	}
	pr, ack := a.score(ap, in, tRecv)
	if ack.Status == StatusPending && pr.tFinal > a.disarmAt {
		ack = PressAck{Status: StatusLate, Reason: "afterDisarm", ReactionMs: pr.reaction}
	}
	a.record(pr, ack.Status)
	if ack.Status != StatusPending {
		return ack
	}
	ap.pressed = true
	a.presses = append(a.presses, &pr)
	if !a.hasFirst {
		a.hasFirst = true
		a.firstRecv = tRecv
		a.bestT = pr.tFinal
	} else if pr.tFinal < a.bestT {
		a.bestT = pr.tFinal
	}
	a.state = StateCollecting
	a.reschedule(tRecv)
	return ack
}

func (a *Arm) latePress(ap *armPlayer, in PressIn, tRecv float64) PressAck {
	if ap.pressed {
		if ap.conn != nil {
			ap.conn.softFlag(FlagDuplicate)
		}
		return PressAck{Status: StatusDuplicate}
	}
	pr, ack := a.score(ap, in, tRecv)
	status := ack.Status
	if status == StatusPending {
		status = StatusLate
		ack = PressAck{Status: StatusLate, ReactionMs: pr.reaction}
		if a.result.Kind == KindWinner {
			for _, w := range a.presses {
				if w.pid == a.result.WinnerID && pr.tFinal < w.tFinal-EpsInputMs {
					pr.flags = append(pr.flags, FlagLateBeat)
				}
			}
		}
	}
	ap.pressed = true
	a.record(pr, status)
	return ack
}

func (a *Arm) record(pr press, status string) {
	a.audit = append(a.audit, AuditEntry{
		PlayerID: pr.pid, Status: status, TRecv: pr.tRecv, TArr: pr.tArr, TClient: pr.tClient, Lo: pr.lo, Hi: pr.hi,
		Credit: pr.credit, TFinal: pr.tFinal, Reaction: pr.reaction, Source: pr.source, Flags: pr.flags, Model: pr.ap.m.Model,
	})
}

// score evaluates a press without touching the arm state (Conn statistics are updated).
func (a *Arm) score(ap *armPlayer, in PressIn, tRecv float64) (press, PressAck) {
	pr := press{pid: ap.id, seq: in.Seq, tRecv: tRecv, ap: ap}
	m := &ap.m
	c := ap.conn
	lockout := func(status string) (press, PressAck) {
		ack := PressAck{Status: status}
		if c != nil {
			ack.LockoutUntil = c.lockout(tRecv, float64(a.s.FalseStartLockoutMs), a.s.LockoutGrowth, float64(a.s.LockoutCapMs))
			if status == StatusTooEarly {
				c.AddFlag(FlagTooEarly, tRecv)
				pr.flags = append(pr.flags, FlagTooEarly)
			}
		}
		ack.ReactionMs = pr.reaction
		return pr, ack
	}
	switch a.s.Mode {
	case ModeServerArrival, ModeRandomWindow:
		pr.tArr, pr.tFinal, pr.source = tRecv, tRecv, SourceArrival
		pr.reaction = tRecv - a.tArm
		if tRecv < a.tArm {
			return lockout(StatusFalseStart)
		}
	case ModeClientReaction:
		pr.tArr = tRecv
		if tRecv < a.tArm {
			pr.reaction = tRecv - a.tArm
			return lockout(StatusFalseStart)
		}
		r := in.PressLocal - in.LitLocal
		if in.LitLocal == 0 || r <= 0 || r > float64(a.s.AcceptWindowMs) {
			r = float64(a.s.AcceptWindowMs) // SIGame: missing/invalid delta counts as the worst
		}
		pr.tClient = in.PressLocal + m.OffsetMs
		pr.reaction, pr.tFinal, pr.source = r, a.tArm+r, SourceClient
	default:
		return a.scoreAnchored(ap, in, tRecv, pr, lockout)
	}
	pr.clean = true
	if c != nil {
		pr.flags = append(pr.flags, c.noteReaction(pr.reaction, true, false, tRecv)...)
	}
	return pr, PressAck{Status: StatusPending, ReactionMs: pr.reaction}
}

func (a *Arm) scoreAnchored(ap *armPlayer, in PressIn, tRecv float64, pr press, lockout func(string) (press, PressAck)) (press, PressAck) {
	m := &ap.m
	c := ap.conn
	minHuman := float64(a.s.MinHumanMs)
	pr.tArr = tRecv - m.RttRefMs/2
	// Most generous reaction consistent with server facts: the press travelled as
	// fast as the link has ever been seen to and the light was at T_arm, or on
	// receipt of an ARM that also travelled at the minimum (whichever is later).
	halfMin := m.rttMinRef / 2
	reactUB := tRecv - halfMin - math.Max(a.tArm, ap.tSend+halfMin)
	pr.reaction = reactUB
	if reactUB < -m.UMs {
		return lockout(StatusFalseStart)
	}
	if reactUB < minHuman {
		return lockout(StatusTooEarly)
	}
	clamped := false
	switch {
	case ap.level == TrustArrivalOnly:
		pr.tFinal, pr.source = tRecv, SourceArrival
		pr.err = m.RttRefMs/2 + 2*m.SigmaMs + EpsInputMs
	case ap.mode == "onReceipt":
		tSeen := ap.tSend + m.RttRefMs/2 + clamp(ap.ackExcess, 0, 2*m.SigmaMs+10)
		pr.tFinal, pr.source = a.tArm+(pr.tArr-tSeen), SourceServer
		pr.err = m.resid + 2*m.SigmaMs + EpsInputMs // light and press share the path: asymmetry cancels
	case ap.level == TrustServerOnly:
		pr.tFinal, pr.source = pr.tArr, SourceServer
		pr.err = m.RttRefMs/4 + 2*m.SigmaMs + EpsInputMs
	default:
		pr.tClient = in.PressLocal + m.OffsetMs
		pr.credit = clamp(math.Min(in.LitLocal-ap.armAtLocal, ap.ackExcess), 0, ap.lateCreditMax)
		pr.tEst = pr.tClient - pr.credit
		pr.lo = pr.tArr - m.TolMs
		pr.hi = math.Min(tRecv, pr.tArr+m.TolMs)
		pr.tFinal = clamp(pr.tEst, pr.lo, pr.hi)
		if pr.tFinal == pr.tEst {
			pr.source, pr.err = SourceClient, EpsInputMs
		} else {
			pr.source, pr.err = SourceClamped, m.TolMs
			clamped = true
			pr.clampDir = 1
			if pr.tEst < pr.lo {
				pr.clampDir = -1
			}
			if math.Abs(pr.tFinal-pr.tEst) > OutOfBoundsMinMs {
				pr.flags = append(pr.flags, FlagOutOfBounds)
				if c != nil {
					c.AddFlag(FlagOutOfBounds, tRecv)
				}
			}
		}
		if c != nil && c.noteBias(pr.tClient-pr.tArr, *m, tRecv) {
			pr.flags = append(pr.flags, FlagEarlyBias)
		}
		if ap.level == TrustConservative {
			pr.err = math.Max(pr.err, m.UMs)
		}
	}
	pr.reaction = pr.tFinal - a.tArm
	if pr.reaction < -m.UMs {
		return lockout(StatusFalseStart)
	}
	if pr.reaction < minHuman {
		return lockout(StatusTooEarly)
	}
	// clean-first presumption: fully trusted, stamp-scored, and no scoring flag
	// raised by this press. A single clamp (count-only out_of_bounds) stays clean:
	// WiFi tails must not hand a 40 ms lead to the second player.
	if c != nil {
		pr.flags = append(pr.flags, c.noteReaction(pr.reaction, len(pr.flags) == 0, clamped, tRecv)...)
	}
	pr.clean = ap.level == TrustFull && (pr.source == SourceClient || pr.source == SourceClamped)
	for _, f := range pr.flags {
		if scoringFlags[f] {
			pr.clean = false
		}
	}
	// A presumed-honest press clamped at lo really happened even earlier (the
	// spike ate more than tol), so for the tie window it is as exact as any
	// client stamp; the lie interpretation is what the ladder is for.
	if pr.clean && pr.clampDir < 0 {
		pr.err = EpsInputMs
	}
	return pr, PressAck{Status: StatusPending, ReactionMs: pr.reaction}
}

// OnMisfire handles a client-reported press before its light: lockout as a false start.
func (a *Arm) OnMisfire(playerID string, now float64) float64 {
	ap := a.byID[playerID]
	if ap == nil || ap.conn == nil {
		return 0
	}
	return ap.conn.lockout(now, float64(a.s.FalseStartLockoutMs), a.s.LockoutGrowth, float64(a.s.LockoutCapMs))
}

// reschedule recomputes the collection deadline.
func (a *Arm) reschedule(now float64) {
	var waitFor float64
	pending := false
	for _, p := range a.players {
		if p.pressed || !p.eligible {
			continue
		}
		if p.conn != nil && p.conn.lockedUntil > now {
			continue
		}
		pending = true
		if w := math.Min(p.m.RttRefMs/2+p.m.TolMs, WaitPerPlayerMaxMs); w > waitFor {
			waitFor = w
		}
	}
	if !pending {
		a.closeAt = now
		return
	}
	var deadline float64
	switch a.s.Mode {
	case ModeServerArrival:
		deadline = a.firstRecv
	case ModeRandomWindow:
		deadline = a.firstRecv + float64(a.s.RandomWindowMs)
	case ModeClientReaction:
		deadline = a.firstRecv + float64(a.s.AcceptWindowMs)
	default:
		wMax := float64(a.s.TieMs)
		if a.s.TieMode == TieUncertainty {
			wMax = float64(a.s.TieMaxMs)
		}
		deadline = math.Min(a.bestT+wMax+waitFor, a.firstRecv+float64(a.s.MaxCollectMs))
	}
	a.closeAt = math.Min(deadline, a.drainEnd)
}

// NextDeadline returns when the room must call Resolve next.
func (a *Arm) NextDeadline() (float64, bool) {
	switch a.state {
	case StateArmed:
		return a.disarmAt, true
	case StateCollecting:
		return a.closeAt, true
	case StateDraining:
		return a.drainEnd, true
	}
	return 0, false
}

// Resolve decides the button if a deadline is due; ok is false when nothing
// is due yet (the state may still change from armed to draining).
func (a *Arm) Resolve(now float64) (Result, bool) {
	switch a.state {
	case StateResolved, StateDisarmed:
		return a.result, true
	case StateArmed:
		if now < a.disarmAt {
			return Result{}, false
		}
		a.state = StateDraining
		if now < a.drainEnd {
			return Result{}, false
		}
	case StateDraining:
		if now < a.drainEnd {
			return Result{}, false
		}
	case StateCollecting:
		if now < a.closeAt {
			return Result{}, false
		}
	}
	return a.ForceResolve(now), true
}

// Disarm cancels the arm (showman action); later presses are stale.
func (a *Arm) Disarm(now float64, reason string) {
	if a.state == StateResolved || a.state == StateDisarmed {
		return
	}
	a.state = StateDisarmed
	a.result = Result{ArmID: a.id, Kind: KindNobody, Rule: reason, TieBreak: a.s.TieBreak, ResolvedAt: now, Seed: hex.EncodeToString(a.seed)}
}

// Audit returns every press evaluation of this arm.
func (a *Arm) Audit() []AuditEntry { return a.audit }

// ForceResolve decides the button now with whatever presses were collected.
func (a *Arm) ForceResolve(now float64) Result {
	if a.state == StateResolved || a.state == StateDisarmed {
		return a.result
	}
	a.state = StateResolved
	res := Result{ArmID: a.id, Kind: KindNobody, TieBreak: a.s.TieBreak, ResolvedAt: now, Seed: hex.EncodeToString(a.seed)}
	if a.hasFirst {
		res.CollectMs = now - a.firstRecv
	}
	ranked := append([]*press(nil), a.presses...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].tFinal < ranked[j].tFinal })
	for _, p := range ranked {
		res.Ranking = append(res.Ranking, Ranking{PlayerID: p.pid, ReactionMs: p.reaction, ErrMs: p.err,
			RttMs: p.ap.m.RttRefMs, UMs: p.ap.m.UMs, Source: p.source, Flags: p.flags})
	}
	if len(ranked) == 0 {
		a.result = res
		return res
	}
	if len(ranked) >= 2 {
		res.MarginMs = ranked[1].tFinal - ranked[0].tFinal
	}
	res.Kind = KindWinner
	switch a.s.Mode {
	case ModeServerArrival:
		res.WinnerID, res.Rule = ranked[0].pid, "first"
	case ModeRandomWindow:
		res.TieBreak, res.Rule = TieBreakRandom, string(TieBreakRandom)
		res.Contested = len(ranked) > 1
		res.WinnerID = ranked[int(unitFloat(a.seed, "tie")*float64(len(ranked)))].pid
		for _, p := range ranked {
			res.Tie = append(res.Tie, p.pid)
		}
	case ModeClientReaction:
		tie := []*press{ranked[0]}
		for _, p := range ranked[1:] {
			if p.tFinal == ranked[0].tFinal {
				tie = append(tie, p)
			}
		}
		res.Rule = "first"
		if len(tie) > 1 {
			res.Contested, res.TieBreak, res.Rule = true, TieBreakRandom, string(TieBreakRandom)
			for _, p := range tie {
				res.Tie = append(res.Tie, p.pid)
			}
		}
		res.WinnerID = tie[int(unitFloat(a.seed, "tie")*float64(len(tie)))].pid
	default:
		a.decide(ranked, &res)
	}
	a.result = res
	return res
}

func (a *Arm) tieWindow(lead, x *press) float64 {
	if a.s.TieMode == TieResolution {
		return float64(a.s.TieMs)
	}
	return clamp(2*math.Hypot(lead.err, x.err)+2, float64(a.s.TieMinMs), float64(a.s.TieMaxMs))
}

func (a *Arm) decide(ranked []*press, res *Result) {
	lead := ranked[0]
	tie := []*press{lead}
	for _, p := range ranked[1:] {
		if p.tFinal-lead.tFinal <= a.tieWindow(lead, p) {
			tie = append(tie, p)
		}
	}
	res.Rule = "single"
	if len(tie) == 1 {
		res.WinnerID = lead.pid
		return
	}
	res.Contested = true
	for _, p := range tie {
		res.Tie = append(res.Tie, p.pid)
	}
	var clean []*press
	for _, p := range tie {
		if p.clean {
			clean = append(clean, p)
		}
	}
	if len(clean) > 0 && len(clean) < len(tie) {
		tie = clean
		res.Rule = "cleanFirst"
		if len(tie) == 1 {
			res.WinnerID = tie[0].pid
			return
		}
	}
	res.Rule = string(a.s.TieBreak)
	u := unitFloat(a.seed, "tie")
	switch a.s.TieBreak {
	case TieBreakAllPlay:
		res.Kind, res.WinnerID = KindAllPlay, ""
		res.Tie = res.Tie[:0]
		for _, p := range tie {
			res.Tie = append(res.Tie, p.pid)
		}
		return
	case TieBreakRandom:
		res.WinnerID = tie[int(u*float64(len(tie)))].pid
	case TieBreakMostLikely:
		w := likelihood(tie)
		best := 0
		for i := range w {
			if w[i] > w[best] {
				best = i
			}
		}
		res.WinnerID = tie[best].pid
	case TieBreakLowestScore:
		res.WinnerID = pickMin(tie, u, func(p *press) int { return p.ap.score })
	case TieBreakFewestButtonsWon:
		res.WinnerID = pickMin(tie, u, func(p *press) int { return p.ap.buttonsWon })
	case TieBreakRotate:
		maxSeat := 0
		for _, p := range a.players {
			if p.seat > maxSeat {
				maxSeat = p.seat
			}
		}
		n := maxSeat + 1
		res.WinnerID = pickMin(tie, u, func(p *press) int { return ((p.ap.seat-a.rotateAfter-1)%n + n) % n })
	default: // likelihood
		res.WinnerID = tie[weightedPick(likelihood(tie), u)].pid
	}
}

// likelihood returns w_i = Π_{j≠i} Φ((t_j − t_i)/hypot(err_i, err_j)).
func likelihood(tie []*press) []float64 {
	w := make([]float64, len(tie))
	for i, p := range tie {
		w[i] = 1
		for j, q := range tie {
			if i == j {
				continue
			}
			d := math.Hypot(p.err, q.err)
			if d <= 0 {
				d = EpsInputMs
			}
			w[i] *= phi((q.tFinal - p.tFinal) / d)
		}
	}
	return w
}

func pickMin(tie []*press, u float64, key func(*press) int) string {
	best := key(tie[0])
	var cands []*press
	for _, p := range tie {
		if k := key(p); k < best {
			best = k
			cands = cands[:0]
			cands = append(cands, p)
		} else if k == best {
			cands = append(cands, p)
		}
	}
	return cands[int(u*float64(len(cands)))].pid
}
