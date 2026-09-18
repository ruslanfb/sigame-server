package room

import (
	"fmt"
	"time"

	"sigame/internal/buzzer"
	"sigame/internal/engine"
)

// maxArmRecords caps the per-room buzz log.
const maxArmRecords = 200

// armState is the room's view of one open buzzer arm.
type armState struct {
	arm        *buzzer.Arm
	gen        int
	questionID string
	sends      []*time.Timer
	deadline   *time.Timer
	msgs       map[string]buzzer.ArmMsg
	rearm      bool
}

// engineEligible lists the players the engine currently lets press.
func (r *Room) engineEligible() []string {
	var out []string
	if r.game == nil {
		return out
	}
	for _, gp := range r.game.State().Players {
		if gp.Active() && gp.CanPress {
			out = append(out, gp.ID)
		}
	}
	return out
}

func (r *Room) lockedUntil(p *person) float64 {
	m := p.lockedUntil
	if p.bconn != nil && p.bconn.LockedUntil() > m {
		m = p.bconn.LockedUntil()
	}
	return m
}

// buzzerPlayers builds the arm participants: every seated player, eligible
// when connected, allowed by the engine and not locked out.
func (r *Room) buzzerPlayers(eligible []string) []buzzer.Player {
	now := r.mono()
	elig := map[string]bool{}
	for _, id := range eligible {
		elig[id] = true
	}
	var out []buzzer.Player
	if r.game == nil {
		return out
	}
	for seat, gp := range r.game.State().Players {
		p := r.persons[gp.ID]
		if p == nil || gp.Kicked {
			continue
		}
		out = append(out, buzzer.Player{ID: gp.ID, Score: gp.Score, ButtonsWon: p.buttonsWon, Conn: p.bconn, Seat: seat,
			Eligible: p.connected && p.bconn != nil && elig[gp.ID] && r.lockedUntil(p) <= now})
	}
	return out
}

// startArm opens a new arm for questionID (BUTTON_ARM_REQUEST / re-arm /
// host reopen). Any previous arm is disarmed first.
func (r *Room) startArm(questionID string, eligible []string, rearm bool) {
	if r.arm != nil {
		r.cancelArm("rearm")
	}
	now := r.mono()
	players := r.buzzerPlayers(eligible)
	arm := buzzer.NewArm(now, now, players, r.bz, newSeed())
	if r.lastWinnerSeat >= 0 {
		arm.SetRotateAfter(r.lastWinnerSeat)
	}
	r.armGen++
	st := &armState{arm: arm, gen: r.armGen, questionID: questionID, msgs: map[string]buzzer.ArmMsg{}, rearm: rearm}
	r.arm = st
	r.armActiveVal.Store(true)
	for _, plan := range arm.Plans() {
		plan := plan
		st.msgs[plan.PlayerID] = plan.Msg
		d := time.Duration((plan.SendAt - now) * float64(time.Millisecond))
		st.sends = append(st.sends, r.after(d, func() { r.onArmSend(st.gen, plan) }))
	}
	r.scheduleResolve()
}

func (r *Room) onArmSend(gen int, plan buzzer.ArmPlan) {
	st := r.arm
	if st == nil || st.gen != gen {
		return
	}
	p := r.persons[plan.PlayerID]
	if p == nil || p.conn == nil {
		return
	}
	r.send(p, MsgButtonArm, plan.Msg)
	st.arm.OnSent(plan.PlayerID, r.mono())
}

// scheduleResolve (re)arms the resolve timer at the arm's next deadline.
func (r *Room) scheduleResolve() {
	st := r.arm
	if st == nil {
		return
	}
	if st.deadline != nil {
		st.deadline.Stop()
		st.deadline = nil
	}
	at, ok := st.arm.NextDeadline()
	if !ok {
		return
	}
	gen := st.gen
	st.deadline = r.after(time.Duration((at-r.mono())*float64(time.Millisecond)), func() { r.onArmResolve(gen) })
}

func (r *Room) onArmResolve(gen int) {
	st := r.arm
	if st == nil || st.gen != gen {
		return
	}
	res, ok := st.arm.Resolve(r.mono())
	if !ok {
		r.scheduleResolve()
		return
	}
	r.finishArm(st, res)
}

func (r *Room) stopArmTimers(st *armState) {
	for _, t := range st.sends {
		t.Stop()
	}
	st.sends = nil
	if st.deadline != nil {
		st.deadline.Stop()
		st.deadline = nil
	}
}

func (r *Room) recordArm(st *armState, res buzzer.Result) {
	rec := ArmRecord{ArmID: res.ArmID, QuestionID: st.questionID, TArm: st.arm.TArm(), Result: res, Audit: auditView(st.arm.Audit()), ResolvedAt: r.wallMs()}
	r.records = append(r.records, rec)
	if len(r.records) > maxArmRecords {
		r.records = r.records[len(r.records)-maxArmRecords:]
	}
	if h := r.hostPerson(); h != nil {
		r.send(h, MsgButtonAudit, ButtonAuditPayload{ArmID: res.ArmID, QuestionID: st.questionID, Seed: res.Seed, TArm: st.arm.TArm(), Entries: rec.Audit})
	}
}

// finishArm publishes a decision and feeds it to the engine.
func (r *Room) finishArm(st *armState, res buzzer.Result) {
	r.stopArmTimers(st)
	r.arm = nil
	r.armActiveVal.Store(false)
	r.recordArm(st, res)

	// Staff get the full ranking; players and viewers only the decision plus
	// their own scored reaction.
	full := marshal(res)
	r.broadcastRaw(MsgButtonResult, full, isStaff)
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil || p.conn == nil || isStaff(p) {
			continue
		}
		pub := ButtonResultPublic{ArmID: res.ArmID, WinnerID: res.WinnerID, Kind: res.Kind, Contested: res.Contested, MarginMs: res.MarginMs, Rule: res.Rule}
		for _, rk := range res.Ranking {
			if rk.PlayerID == p.ID {
				pub.ReactionMs, pub.Source = rk.ReactionMs, rk.Source
			}
		}
		r.send(p, MsgButtonResult, pub)
	}

	if r.game == nil || r.closing {
		return
	}
	switch res.Kind {
	case buzzer.KindWinner:
		if p := r.persons[res.WinnerID]; p != nil {
			p.buttonsWon++
		}
		for seat, gp := range r.game.State().Players {
			if gp.ID == res.WinnerID {
				r.lastWinnerSeat = seat
			}
		}
		r.submit(engine.Command{Type: engine.CmdButtonResult, Actor: systemActor, WinnerID: res.WinnerID})
	default: // nobody (allPlay is rejected at settings time)
		st2 := r.game.State()
		q := st2.Question
		if q != nil && st2.Stage == engine.StageQuestion && q.Armed && q.Sub == engine.SubContent && !st2.Paused {
			// The press window elapsed while content is still playing (falseStart
			// disabled): keep the button open with a fresh arm; the engine's own
			// thinking timer decides "nobody" once the content is over.
			r.startArm(st.questionID, r.engineEligible(), true)
			return
		}
		r.submit(engine.Command{Type: engine.CmdButtonResult, Actor: systemActor, Nobody: true})
	}
}

// cancelArm disarms the open arm (engine disarm, question end, reopen, close).
func (r *Room) cancelArm(reason string) {
	st := r.arm
	if st == nil {
		return
	}
	r.stopArmTimers(st)
	r.arm = nil
	r.armActiveVal.Store(false)
	st.arm.Disarm(r.mono(), reason)
	if len(st.arm.Audit()) > 0 {
		r.recordArm(st, st.arm.ForceResolve(r.mono()))
	}
}

func (r *Room) onPress(p *person, in PressIn, tRecv float64) {
	st := r.arm
	if st == nil || in.ArmID != st.arm.ID() {
		ack := PressAckPayload{ArmID: in.ArmID, PressAck: buzzer.PressAck{Status: buzzer.StatusStale, Reason: "noArm"}}
		if st == nil && r.pressIsMisfire() {
			until := r.roomLockout(p, tRecv)
			ack.PressAck = buzzer.PressAck{Status: buzzer.StatusFalseStart, Reason: "misfire", LockoutUntil: until}
			r.broadcast(MsgLockout, LockoutPayload{PlayerID: p.ID, UntilAt: until, DurationMs: int64(until - tRecv), Reason: "misfire"})
		}
		r.send(p, MsgPressAck, ack)
		return
	}
	ack := st.arm.OnPress(buzzer.PressIn{PlayerID: p.ID, Seq: in.Seq, PressLocal: in.PressLocal, LitLocal: in.LitLocal, Src: in.Src}, tRecv)
	r.send(p, MsgPressAck, PressAckPayload{ArmID: in.ArmID, PressAck: ack})
	switch ack.Status {
	case buzzer.StatusPending:
		r.broadcast(MsgPressPending, PressPendingPayload{ArmID: in.ArmID, PlayerID: p.ID})
	case buzzer.StatusFalseStart, buzzer.StatusTooEarly:
		r.broadcast(MsgLockout, LockoutPayload{PlayerID: p.ID, UntilAt: ack.LockoutUntil, DurationMs: int64(ack.LockoutUntil - tRecv), Reason: ack.Status})
	}
	r.scheduleResolve()
}

// pressIsMisfire reports whether a press without an open arm is a false
// start (a button question is being read and the light is not on yet).
func (r *Room) pressIsMisfire() bool {
	if r.game == nil {
		return false
	}
	st := r.game.State()
	q := st.Question
	return q != nil && st.Stage == engine.StageQuestion && q.Button && !q.Armed && (q.Sub == engine.SubContent || q.Sub == engine.SubButtonWait)
}

// roomLockout applies the SIGame ButtonBlockingMs lockout outside an arm.
func (r *Room) roomLockout(p *person, now float64) float64 {
	dur := r.times.ButtonBlockingMs
	if r.game != nil {
		dur = r.game.State().Times.ButtonBlockingMs
	}
	if dur <= 0 {
		dur = 3000
	}
	until := now + float64(dur)
	if until > p.lockedUntil {
		p.lockedUntil = until
	}
	return p.lockedUntil
}

func (r *Room) onArmAck(p *person, a ArmAckIn, tRecv float64) {
	st := r.arm
	if st == nil || a.ArmID != st.arm.ID() {
		return
	}
	st.arm.OnArmAck(p.ID, tRecv, a.RecvLocal)
}

func (r *Room) onMisfire(p *person, mf MisfireIn, now float64) {
	var until float64
	if st := r.arm; st != nil && (mf.ArmID == "" || mf.ArmID == st.arm.ID()) {
		until = st.arm.OnMisfire(p.ID, now)
		if st.arm.State() == buzzer.StateCollecting {
			r.scheduleResolve()
		}
	}
	if until <= 0 {
		until = r.roomLockout(p, now)
	}
	r.broadcast(MsgLockout, LockoutPayload{PlayerID: p.ID, UntilAt: until, DurationMs: int64(until - now), Reason: "misfire"})
}

// reopenButton (host) replaces the open arm with a fresh one.
func (r *Room) reopenButton(armID string) error {
	if r.game == nil {
		return fmt.Errorf("%w: game not started", ErrBadState)
	}
	st := r.game.State()
	q := st.Question
	if q == nil || st.Stage != engine.StageQuestion || !q.Armed {
		return fmt.Errorf("%w: the button is not open", ErrBadState)
	}
	if armID != "" && (r.arm == nil || r.arm.arm.ID() != armID) {
		return fmt.Errorf("%w: arm %q is not active", ErrInvalidParams, armID)
	}
	r.cancelArm("reopen")
	r.startArm(q.ID, r.engineEligible(), true)
	return nil
}

func (r *Room) buzzerSnapshot(p *person) BuzzerSnapshot {
	out := BuzzerSnapshot{State: "idle", LockedUntil: r.lockedUntil(p)}
	st := r.arm
	if st == nil {
		return out
	}
	out.State = st.arm.State()
	out.ArmID = st.arm.ID()
	out.ArmAt = st.arm.TArm()
	out.DeadlineAt = st.arm.TArm() + float64(r.bz.PressWindowMs)
	for _, m := range st.msgs {
		out.DeadlineAt = m.DeadlineAt
		break
	}
	return out
}

// ---- CONN_QUALITY ----------------------------------------------------------

func (r *Room) startQualityTicker() {
	every := r.m.deps.Cfg.ConnQualityEveryMs
	if every <= 0 {
		every = int64(buzzer.ConnQualityIntervalMs)
	}
	r.qualityTimer = r.after(time.Duration(every)*time.Millisecond, r.tickQuality)
}

func (r *Room) tickQuality() {
	if r.closing {
		return
	}
	r.broadcastConnQuality()
	r.startQualityTicker()
}

func (r *Room) broadcastConnQuality() {
	now := r.mono()
	var pub, staff []ConnQualityEntry
	anyConn := false
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil {
			continue
		}
		if p.conn != nil {
			anyConn = true
		}
		if p.Role != engine.RolePlayer || p.bconn == nil {
			continue
		}
		m := p.bconn.Model(now)
		e := ConnQualityEntry{PlayerID: p.ID, Quality: m.Quality, Connected: p.connected}
		if r.bz.ShowPing {
			e.RttMs, e.JitterMs, e.UMs = m.RttRefMs, m.SigmaMs, m.UMs
		}
		pub = append(pub, e)
		s := e
		s.RttMs, s.JitterMs, s.UMs = m.RttRefMs, m.SigmaMs, m.UMs
		s.Flags = m.Flags
		t := p.bconn.Trust()
		s.Trust = &t
		staff = append(staff, s)
	}
	if !anyConn {
		return
	}
	r.broadcastRaw(MsgConnQuality, marshal(ConnQualityPayload{Players: staff}), isStaff)
	r.broadcastRaw(MsgConnQuality, marshal(ConnQualityPayload{Players: pub}), func(p *person) bool { return !isStaff(p) })
}
