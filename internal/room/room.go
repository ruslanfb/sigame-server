package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"sigame/internal/buzzer"
	"sigame/internal/engine"
	"sigame/internal/packs"
)

// ShowmanMode says who validates answers.
type ShowmanMode string

// Showman modes.
const (
	ShowmanHuman  ShowmanMode = "human"  // a human showman validates; VALIDATION_TIMEOUT falls back to the fuzzy judge
	ShowmanAI     ShowmanMode = "ai"     // the AI judge validates automatically (no human showman seat)
	ShowmanHybrid ShowmanMode = "hybrid" // a human showman gets AI suggestions; the AI verdict applies after HybridConfirmMs
)

// Join modes.
const (
	JoinAny         = "any"
	JoinViewersOnly = "viewersOnly"
	JoinClosed      = "closed"
)

// Room statuses.
const (
	StatusLobby    = "lobby"
	StatusPlaying  = "playing"
	StatusFinished = "finished"
	StatusClosed   = "closed"
)

// mailboxSize is the capacity of the actor mailbox.
const mailboxSize = 1024

// Room is one game room: a single goroutine actor. Every field below the
// mailbox is owned by that goroutine; the atomics are the only state read
// from outside (Manager.List/Get, the anchor loops).
type Room struct {
	m   *Manager
	log *slog.Logger

	id, code  string
	createdAt int64
	updatedAt int64
	startedAt int64

	mailbox   chan func()
	done      chan struct{} // closed by the actor when it decides to stop
	stopped   chan struct{} // closed when the loop has exited
	closeOnce sync.Once

	// ---- actor-owned state ----
	name, password  string
	hostHash        [32]byte
	packID          string
	pack            *packs.Pack
	rules           engine.Rules
	times           engine.TimeSettings
	bz              buzzer.Settings
	showman         ShowmanMode
	hybridConfirmMs int64
	maxPlayers      int
	allowViewers    bool
	language        string
	joinMode        string
	status          string

	persons     map[string]*person
	order       []string
	bannedNames map[string]bool   // lower-cased banned names
	bannedIDs   map[string]string // banned person id → lower-cased name

	game          *engine.Game
	followUps     []engine.Command
	dispatchDepth int
	timers        map[string]*time.Timer

	arm            *armState
	armGen         int
	lastWinnerSeat int
	records        []ArmRecord

	hybridTimer  *time.Timer
	hybridKey    string
	qualityTimer *time.Timer
	closing      bool

	// ---- shared ----
	lastActivity atomic.Int64 // Unix ms
	infoVal      atomic.Pointer[Info]
	armActiveVal atomic.Bool
}

// ---------------------------------------------------------------------------
// actor plumbing
// ---------------------------------------------------------------------------

func (r *Room) run() {
	defer close(r.stopped)
	for {
		select {
		case fn := <-r.mailbox:
			fn()
		case <-r.done:
			return
		}
	}
}

// post enqueues fn for the actor; it reports false when the room is closed.
func (r *Room) post(fn func()) bool {
	select {
	case <-r.done:
		return false
	default:
	}
	select {
	case r.mailbox <- fn:
		return true
	case <-r.done:
		return false
	}
}

// call runs fn on the actor and waits for its result.
func (r *Room) call(ctx context.Context, fn func() error) error {
	reply := make(chan error, 1)
	if !r.post(func() { reply <- fn() }) {
		return ErrRoomClosed
	}
	select {
	case err := <-reply:
		return err
	case <-r.done:
		// The actor may still have run fn; prefer its answer when available.
		select {
		case err := <-reply:
			return err
		default:
			return ErrRoomClosed
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// after schedules fn on the actor after d (real timer; stamps use the clock).
func (r *Room) after(d time.Duration, fn func()) *time.Timer {
	if d < 0 {
		d = 0
	}
	return time.AfterFunc(d, func() { r.post(fn) })
}

func (r *Room) mono() float64  { return r.m.deps.Clock.Mono() }
func (r *Room) monoInt() int64 { return int64(r.mono()) }
func (r *Room) wallMs() int64  { return r.m.deps.Clock.Now().UnixMilli() }
func (r *Room) touch()         { r.lastActivity.Store(r.wallMs()) }

// ArmActive reports whether a buzzer arm is currently open (read by the
// anchor loops to pick the WS ping interval).
func (r *Room) ArmActive() bool { return r.armActiveVal.Load() }

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// ---------------------------------------------------------------------------
// info projection
// ---------------------------------------------------------------------------

func (r *Room) scoreMap() map[string]int {
	out := map[string]int{}
	if r.game == nil {
		return out
	}
	for _, p := range r.game.State().Players {
		out[p.ID] = p.Score
	}
	return out
}

func (r *Room) personViews() []PersonView {
	scores := r.scoreMap()
	out := make([]PersonView, 0, len(r.order))
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil {
			continue
		}
		out = append(out, p.view(scores[p.ID]))
	}
	return out
}

func (r *Room) buildInfo() Info {
	views := r.personViews()
	viewers := 0
	for _, v := range views {
		if v.Role == engine.RoleViewer {
			viewers++
		}
	}
	return Info{ID: r.id, Code: r.code, Name: r.name, PackID: r.packID, PackName: r.pack.Name, Status: r.status,
		Showman: r.showman, HasPassword: r.password != "", Players: views, Viewers: viewers, MaxPlayers: r.maxPlayers,
		AllowViewers: r.allowViewers, Language: r.language, HybridConfirmMs: r.hybridConfirmMs,
		CreatedAt: r.createdAt, UpdatedAt: r.updatedAt, Rules: r.rules, Times: r.times, Buzzer: r.bz, JoinMode: r.joinMode}
}

func (r *Room) publishInfo() {
	info := r.buildInfo()
	r.infoVal.Store(&info)
}

func (r *Room) info() Info { return *r.infoVal.Load() }

// ---------------------------------------------------------------------------
// persons
// ---------------------------------------------------------------------------

func (r *Room) personByToken(token string) *person {
	h := hashToken(token)
	for _, p := range r.persons {
		if p.tokenHash == h {
			return p
		}
	}
	return nil
}

func (r *Room) hostPerson() *person {
	for _, id := range r.order {
		if p := r.persons[id]; p != nil && p.IsHost {
			return p
		}
	}
	return nil
}

func (r *Room) humanShowman() *person {
	for _, id := range r.order {
		if p := r.persons[id]; p != nil && p.Role == engine.RoleShowman && !p.IsAI {
			return p
		}
	}
	return nil
}

func (r *Room) countRole(role engine.Role) int {
	n := 0
	for _, id := range r.order {
		if p := r.persons[id]; p != nil && p.Role == role && !p.IsAI {
			n++
		}
	}
	return n
}

func (r *Room) addPerson(p *person) {
	r.persons[p.ID] = p
	r.order = append(r.order, p.ID)
}

func (r *Room) removePerson(p *person) {
	delete(r.persons, p.ID)
	for i, id := range r.order {
		if id == p.ID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// join implements Manager.Join on the actor.
func (r *Room) join(jp JoinParams, hostToken string) (Session, error) {
	if r.closing {
		return Session{}, ErrRoomClosed
	}
	name := strings.TrimSpace(jp.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return Session{}, fmt.Errorf("%w: name must be 1..64 characters", ErrInvalidParams)
	}
	role := jp.Role
	if role == "" {
		role = engine.RolePlayer
	}
	switch role {
	case engine.RoleShowman, engine.RolePlayer, engine.RoleViewer:
	default:
		return Session{}, fmt.Errorf("%w: unknown role %q", ErrInvalidParams, role)
	}
	isHostToken := false
	if hostToken != "" {
		if hashToken(hostToken) != r.hostHash {
			return Session{}, ErrInvalidHostToken
		}
		isHostToken = true
	}
	if r.bannedNames[strings.ToLower(name)] {
		return Session{}, ErrBanned
	}
	if r.password != "" && !isHostToken && jp.Password != r.password {
		return Session{}, ErrBadPassword
	}

	// Reconnect by name: same name + role, currently disconnected.
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil || p.IsAI || !strings.EqualFold(p.Name, name) {
			continue
		}
		if p.Role != role || p.connected {
			return Session{}, ErrNameTaken
		}
		if _, banned := r.bannedIDs[p.ID]; banned {
			return Session{}, ErrBanned
		}
		tok := newToken()
		p.tokenHash = hashToken(tok)
		if isHostToken && r.hostPerson() == nil {
			p.IsHost = true
		}
		r.publishInfo()
		return Session{Token: tok, PersonID: p.ID, Role: p.Role, IsHost: p.IsHost, RoomCode: r.code}, nil
	}
	if !isHostToken {
		switch r.joinMode {
		case JoinClosed:
			return Session{}, ErrJoinClosed
		case JoinViewersOnly:
			if role != engine.RoleViewer {
				return Session{}, ErrJoinClosed
			}
		}
	}
	switch role {
	case engine.RoleShowman:
		if r.showman == ShowmanAI {
			return Session{}, fmt.Errorf("%w: the AI is the showman of this room", ErrBadRole)
		}
		if r.humanShowman() != nil {
			return Session{}, fmt.Errorf("%w: showman seat taken", ErrBadRole)
		}
	case engine.RolePlayer:
		if r.countRole(engine.RolePlayer) >= r.maxPlayers {
			return Session{}, ErrRoomFull
		}
		if r.game != nil && r.status != StatusLobby {
			n := 0
			for _, gp := range r.game.State().Players {
				if !gp.Kicked {
					n++
				}
			}
			if n >= engine.MaxPlayers {
				return Session{}, ErrRoomFull
			}
		}
	case engine.RoleViewer:
		if !r.allowViewers && !isHostToken {
			return Session{}, fmt.Errorf("%w: viewers are not allowed", ErrBadRole)
		}
	}
	tok := newToken()
	p := &person{ID: newID(), Name: name, Role: role, tokenHash: hashToken(tok), joinedAt: r.wallMs(),
		ring: newResumeRing(r.m.deps.Cfg.ResumeBufferSize)}
	if isHostToken && r.hostPerson() == nil {
		p.IsHost = true
	}
	r.addPerson(p)
	r.touch()
	r.publishInfo()
	r.broadcastPersons()
	return Session{Token: tok, PersonID: p.ID, Role: p.Role, IsHost: p.IsHost, RoomCode: r.code}, nil
}

// leave implements Manager.Leave on the actor. A seated player of a running
// game keeps the seat and score (disconnected; the same name rejoins it);
// everyone else is removed.
func (r *Room) leave(token string) error {
	p := r.personByToken(token)
	if p == nil {
		return ErrBadSession
	}
	r.disconnect(p, CloseNormal, "left")
	if p.Role == engine.RolePlayer && r.game != nil {
		p.tokenHash = [32]byte{}
	} else {
		r.removePerson(p)
	}
	r.publishInfo()
	r.broadcastPersons()
	return nil
}

// disconnect closes the live connection of p (if any) with a close code.
func (r *Room) disconnect(p *person, code int, reason string) {
	if p.conn == nil {
		return
	}
	c := p.conn
	if p.cancelAnchor != nil {
		p.cancelAnchor()
		p.cancelAnchor = nil
	}
	p.conn = nil
	p.connected = false
	p.prevLastSeq = p.lastSeq
	c.Close(code, reason)
	if r.game != nil && p.Role == engine.RolePlayer {
		r.submit(engine.Command{Type: engine.CmdPlayerLeft, Actor: systemActor, PersonID: p.ID})
	}
	if r.arm != nil {
		r.arm.arm.SetEligible(p.ID, false, r.mono())
		r.scheduleResolve()
	}
}

var systemActor = engine.Actor{Role: engine.RoleSystem}

// ---------------------------------------------------------------------------
// connections
// ---------------------------------------------------------------------------

// attach implements Manager.Attach on the actor.
func (r *Room) attach(token string, c Conn) (*Attachment, error) {
	if r.closing {
		return nil, ErrRoomClosed
	}
	p := r.personByToken(token)
	if p == nil || p.kicked {
		return nil, ErrBadSession
	}
	now := r.mono()
	if p.conn != nil {
		old := p.conn
		r.sendRaw(p, MsgSessionReplaced, nil)
		if p.cancelAnchor != nil {
			p.cancelAnchor()
			p.cancelAnchor = nil
		}
		p.conn = nil
		p.connected = false
		old.Close(CloseSessionReplaced, "session replaced")
	}
	p.gen++
	gen := p.gen
	p.prevLastSeq = p.lastSeq
	p.lastSeq = 0
	p.bconn = buzzer.NewConn(now)
	p.touchBuzzerSettings(r.bz)

	// Seat the player before registering the socket so that the PLAYERS
	// broadcast reaches the others and the snapshot below shows the new state.
	if r.game != nil && p.Role == engine.RolePlayer {
		r.submit(engine.Command{Type: engine.CmdPlayerJoined, Actor: systemActor, PersonID: p.ID, Name: p.Name})
	}
	p.conn = c
	p.connected = true
	r.touch()

	r.sendWelcome(p)
	r.sendSnapshot(p)
	r.publishInfo()
	r.broadcastPersons()
	if r.arm != nil {
		r.arm.arm.SetEligible(p.ID, false, now) // the old plan is void; a fresh arm re-includes the player
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.cancelAnchor = cancel
	r.m.wg.Add(1)
	go r.anchorLoop(ctx, p, gen, c.Info())

	att := &Attachment{
		Session: Session{PersonID: p.ID, Role: p.Role, IsHost: p.IsHost, RoomCode: r.code},
		Deliver: func(in Inbound) { r.post(func() { r.handleInbound(p, gen, in) }) },
		Detach:  func() { r.post(func() { r.detach(p, gen) }) },
	}
	return att, nil
}

func (p *person) touchBuzzerSettings(s buzzer.Settings) {
	if p.bconn == nil {
		return
	}
	p.bconn.TolCapMs = float64(s.TolCapMs)
	p.bconn.AutoTrust = s.AutoTrust
	p.bconn.StrictTrust = s.StrictTrust
}

func (r *Room) detach(p *person, gen int) {
	if p.gen != gen || p.conn == nil {
		return
	}
	if p.cancelAnchor != nil {
		p.cancelAnchor()
		p.cancelAnchor = nil
	}
	p.conn = nil
	p.connected = false
	p.prevLastSeq = p.lastSeq
	if r.game != nil && p.Role == engine.RolePlayer && !p.kicked {
		r.submit(engine.Command{Type: engine.CmdPlayerLeft, Actor: systemActor, PersonID: p.ID})
	}
	if r.arm != nil {
		r.arm.arm.SetEligible(p.ID, false, r.mono())
		r.scheduleResolve()
	}
	r.publishInfo()
	r.broadcastPersons()
}

// anchorLoop measures the tamper-proof RTT anchors of one connection: a WS
// protocol ping every second (250 ms while an arm is open) and the kernel
// RTT every second. It runs outside the actor and posts the samples in.
func (r *Room) anchorLoop(ctx context.Context, p *person, gen int, info ConnInfo) {
	defer r.m.wg.Done()
	var lastKernel float64 = -1e12
	for {
		interval := time.Duration(buzzer.WSPingIdleMs) * time.Millisecond
		if r.ArmActive() {
			interval = time.Duration(buzzer.WSPingArmedMs) * time.Millisecond
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if info.WSPing != nil {
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			rtt, err := info.WSPing(pctx)
			cancel()
			if err == nil {
				r.post(func() {
					if p.gen == gen && p.bconn != nil {
						p.bconn.OnWSPong(rtt, r.mono())
					}
				})
			}
		}
		if now := r.mono(); info.KernelRTT != nil && now-lastKernel >= 1000 {
			lastKernel = now
			if srtt, rttvar, ok := info.KernelRTT(); ok {
				r.post(func() {
					if p.gen == gen && p.bconn != nil {
						p.bconn.OnKernelRTT(srtt, rttvar, r.mono())
					}
				})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// sending
// ---------------------------------------------------------------------------

func marshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// sendRaw sends one envelope to p (no-op when disconnected) and records it
// in the resume ring.
func (r *Room) sendRaw(p *person, t string, raw json.RawMessage) {
	if p.conn == nil {
		return
	}
	seq, err := p.conn.Send(t, raw)
	if err != nil {
		r.log.Debug("room: send failed", "room", r.code, "person", p.ID, "type", t, "err", err)
		return
	}
	p.lastSeq = seq
	p.ring.push(ringEntry{gen: p.gen, seq: seq, t: t, p: raw})
}

func (r *Room) send(p *person, t string, v any) { r.sendRaw(p, t, marshal(v)) }

func (r *Room) broadcastRaw(t string, raw json.RawMessage, filter func(*person) bool) {
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil || p.conn == nil {
			continue
		}
		if filter != nil && !filter(p) {
			continue
		}
		r.sendRaw(p, t, raw)
	}
}

func (r *Room) broadcast(t string, v any) { r.broadcastRaw(t, marshal(v), nil) }

func (r *Room) broadcastPersons() {
	r.broadcast(MsgRoomPersons, RoomPersonsPayload{Persons: r.personViews()})
}

func (r *Room) sendError(p *person, code, msg string, ref int64) {
	r.send(p, MsgError, ErrorPayload{Code: code, Message: msg, Ref: ref})
}

// isStaff reports whether p sees showman-level detail (showman or host).
func isStaff(p *person) bool { return p.Role == engine.RoleShowman || p.IsHost }

func (r *Room) sendWelcome(p *person) {
	r.send(p, MsgWelcome, WelcomePayload{PersonID: p.ID, Name: p.Name, Role: p.Role, IsHost: p.IsHost, RoomCode: r.code,
		Showman: r.showman, ServerTimeMs: r.mono(), ServerWallMs: r.wallMs(),
		SyncPlan:       SyncPlan{Burst: buzzer.SyncBurstCount, IntervalMs: int64(buzzer.SyncBurstIntervalMs), SteadyMs: int64(buzzer.SyncSteadyIntervalMs)},
		BuzzerSettings: r.bz})
}

func (r *Room) sendSnapshot(p *person) {
	snap := SnapshotPayload{Room: r.buildInfo(), Buzzer: r.buzzerSnapshot(p), LastSeq: p.prevLastSeq}
	if r.game != nil {
		s := r.game.Snapshot(p.Role, p.ID)
		snap.Game = &s
	}
	r.send(p, MsgSnapshot, snap)
}

// ---------------------------------------------------------------------------
// engine plumbing
// ---------------------------------------------------------------------------

// enqueue schedules a room-originated command to run after the current
// event batch (never re-enter engine.Apply from an event handler).
func (r *Room) enqueue(cmd engine.Command) { r.followUps = append(r.followUps, cmd) }

// submit enqueues a room-originated command and runs it right away unless
// an event batch is being dispatched (then it runs after the batch).
func (r *Room) submit(cmd engine.Command) {
	r.enqueue(cmd)
	if r.dispatchDepth == 0 {
		r.runFollowUps()
	}
}

// runFollowUps drains the follow-up queue.
func (r *Room) runFollowUps() {
	if r.dispatchDepth > 0 {
		return
	}
	for len(r.followUps) > 0 && r.game != nil {
		cmd := r.followUps[0]
		r.followUps = r.followUps[1:]
		cmd.AtMs = r.monoInt()
		evs, err := r.game.Apply(cmd)
		if err != nil {
			r.log.Debug("room: follow-up rejected", "room", r.code, "cmd", cmd.Type, "err", err)
			continue
		}
		r.dispatch(evs)
	}
}

// apply runs a command from outside an event batch (client, timer, buzzer).
func (r *Room) apply(cmd engine.Command) error {
	if r.game == nil {
		return fmt.Errorf("%w: game not started", ErrBadState)
	}
	cmd.AtMs = r.monoInt()
	evs, err := r.game.Apply(cmd)
	if err != nil {
		return err
	}
	r.dispatch(evs)
	r.runFollowUps()
	return nil
}

// dispatch acts on and forwards a batch of engine events in order.
func (r *Room) dispatch(evs []engine.Event) {
	r.dispatchDepth++
	defer func() { r.dispatchDepth-- }()
	for _, e := range evs {
		if e.IsTimer() {
			e = r.handleTimerEvent(e)
		}
		if e.IsInternal() {
			r.handleInternal(e)
			continue
		}
		r.observe(e)
		r.forward(e)
	}
}

// observe lets the room react to events that also go to clients.
func (r *Room) observe(e engine.Event) {
	switch e.Type {
	case engine.EvAskValidate:
		if p, ok := e.Payload.(engine.AskValidatePayload); ok && r.judgesAutomatically() {
			r.onAskValidate(p)
		}
	case engine.EvAskSelectPlayer:
		if p, ok := e.Payload.(engine.AskSelectPlayerPayload); ok && p.DeciderID == AIShowmanID && len(p.Candidates) > 0 {
			pick := p.Candidates[rand.IntN(len(p.Candidates))]
			r.enqueue(engine.Command{Type: engine.CmdSelectPlayer, Actor: engine.Actor{PersonID: AIShowmanID, Role: engine.RoleShowman}, PersonID: pick})
		}
	case engine.EvValidation:
		r.cancelHybrid()
	case engine.EvQuestionEnd, engine.EvRoundEnd:
		r.cancelHybrid()
		if r.arm != nil {
			r.cancelArm("questionEnd")
		}
	case engine.EvGameEnd:
		r.cancelHybrid()
		if r.arm != nil {
			r.cancelArm("gameEnd")
		}
		r.status = StatusFinished
		r.updatedAt = r.wallMs()
		if p, ok := e.Payload.(engine.GameEndPayload); ok {
			r.persistResult(p)
		}
		r.persistRoom()
		r.publishInfo()
	case engine.EvPlayers, engine.EvSums, engine.EvPersonScore:
		r.publishInfo()
	}
}

// handleInternal acts on room-only events.
func (r *Room) handleInternal(e engine.Event) {
	switch e.Type {
	case engine.EvButtonArmRequest:
		if p, ok := e.Payload.(engine.ButtonArmRequestPayload); ok {
			r.startArm(p.QuestionID, p.EligiblePlayerIDs, p.Rearm)
		}
	case engine.EvButtonDisarm:
		r.cancelArm("engine")
	case engine.EvValidationTimeout:
		if p, ok := e.Payload.(engine.ValidationTimeoutPayload); ok {
			r.onValidationTimeout(p)
		}
	}
}

// forward sends an event to its audience. A host who is also the showman
// counts as showman; the AI showman never has a socket.
func (r *Room) forward(e engine.Event) {
	raw := marshal(e.Payload)
	t := string(e.Type)
	switch e.Audience.Kind {
	case engine.AudienceAll:
		r.broadcastRaw(t, raw, nil)
	case engine.AudienceRole:
		role := e.Audience.Role
		r.broadcastRaw(t, raw, func(p *person) bool { return p.Role == role })
	case engine.AudiencePerson:
		if p := r.persons[e.Audience.PersonID]; p != nil {
			r.sendRaw(p, t, raw)
		}
	case engine.AudienceExcept:
		id := e.Audience.PersonID
		r.broadcastRaw(t, raw, func(p *person) bool { return p.ID != id })
	}
}

func engineErrorCode(err error) string {
	switch {
	case errors.Is(err, engine.ErrNotAllowed):
		return ErrCodeNotAllowed
	case errors.Is(err, engine.ErrBadState), errors.Is(err, ErrBadState):
		return ErrCodeBadState
	case errors.Is(err, engine.ErrBadArgument), errors.Is(err, ErrInvalidParams), errors.Is(err, ErrPersonNotFound):
		return ErrCodeBadArgument
	}
	return ErrCodeBadState
}

// ---------------------------------------------------------------------------
// game lifecycle
// ---------------------------------------------------------------------------

func (r *Room) startGame(p *person) error {
	if r.game != nil {
		return fmt.Errorf("%w: game already started", ErrBadState)
	}
	if p.Role != engine.RoleShowman && !p.IsHost {
		return fmt.Errorf("%w: showman or host required", engine.ErrNotAllowed)
	}
	var inits []engine.PlayerInit
	for _, id := range r.order {
		q := r.persons[id]
		if q != nil && q.Role == engine.RolePlayer && q.connected {
			inits = append(inits, engine.PlayerInit{ID: q.ID, Name: q.Name})
		}
	}
	if len(inits) == 0 {
		return fmt.Errorf("%w: no connected players", ErrBadState)
	}
	showmanID := AIShowmanID
	if sm := r.humanShowman(); sm != nil {
		showmanID = sm.ID
	} else if r.showman == ShowmanHuman {
		return fmt.Errorf("%w: no showman has joined", ErrBadState)
	}
	g, err := engine.New(r.pack, r.rules, r.times, inits, showmanID, rand.Int64())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadState, err)
	}
	r.game = g
	r.status = StatusPlaying
	r.startedAt = r.wallMs()
	r.updatedAt = r.startedAt
	r.persistRoom()
	r.publishInfo()
	if err := r.apply(engine.Command{Type: engine.CmdStart, Actor: p.actor()}); err != nil {
		return err
	}
	return nil
}

// closeRoom ends the room: everyone gets ROOM_CLOSED and the sockets close.
func (r *Room) closeRoom(reason string) {
	if r.closing {
		return
	}
	r.closing = true
	r.cancelHybrid()
	if r.arm != nil {
		r.cancelArm("roomClosed")
	}
	for id, t := range r.timers {
		t.Stop()
		delete(r.timers, id)
	}
	if r.qualityTimer != nil {
		r.qualityTimer.Stop()
		r.qualityTimer = nil
	}
	r.broadcast(MsgRoomClosed, RoomClosedPayload{Reason: reason})
	code := CloseNormal
	if reason == "shutdown" {
		code = CloseGoingAway
	}
	for _, id := range r.order {
		p := r.persons[id]
		if p == nil || p.conn == nil {
			continue
		}
		c := p.conn
		if p.cancelAnchor != nil {
			p.cancelAnchor()
			p.cancelAnchor = nil
		}
		p.conn = nil
		p.connected = false
		c.Close(code, "room closed: "+reason)
	}
	r.status = StatusClosed
	r.updatedAt = r.wallMs()
	r.persistRoom()
	r.publishInfo()
	r.m.remove(r.code)
	r.closeOnce.Do(func() { close(r.done) })
}

// ---------------------------------------------------------------------------
// inbound client messages
// ---------------------------------------------------------------------------

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 || string(raw) == "null" {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, err
	}
	return v, nil
}

func (r *Room) handleInbound(p *person, gen int, in Inbound) {
	if p.gen != gen || p.conn == nil {
		return
	}
	r.touch()
	now := r.mono()
	switch in.T {
	case InHello:
		h, err := decode[HelloIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		r.replay(p, h.LastSeq)
	case InSync:
		s, err := decode[SyncIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		out := p.bconn.OnSync(buzzer.SyncIn{Seq: s.Seq, C1: s.C1, PrevSeq: s.PrevSeq, PrevC4: s.PrevC4, HasPrev: s.PrevSeq > 0}, in.TRecv, now)
		r.send(p, MsgSyncAck, SyncAckPayload{Seq: out.Seq, C1: out.C1, S2: out.S2, S3: out.S3, Model: out.Model})
	case InArmAck:
		a, err := decode[ArmAckIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		r.onArmAck(p, a, in.TRecv)
	case InPress:
		pr, err := decode[PressIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		r.onPress(p, pr, in.TRecv)
	case InMisfire:
		mf, err := decode[MisfireIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		r.onMisfire(p, mf, now)
	case InVisible:
		p.bconn.OnVisible(now)
	case InChat:
		c, err := decode[ChatIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		text := strings.TrimSpace(c.Text)
		if text == "" || utf8.RuneCountInString(text) > MaxChatLen {
			r.sendError(p, ErrCodeBadArgument, "text must be 1..500 characters", in.Seq)
			return
		}
		if p.lastChatAt > 0 && now-p.lastChatAt < 1000 {
			r.sendError(p, ErrCodeRateLimited, "one chat message per second", in.Seq)
			return
		}
		p.lastChatAt = now
		r.broadcast(MsgChat, ChatPayload{PersonID: p.ID, Name: p.Name, Text: text, AtMs: r.wallMs()})
	case InStart:
		if r.game == nil {
			if err := r.startGame(p); err != nil {
				r.sendError(p, engineErrorCode(err), err.Error(), in.Seq)
			}
			return
		}
		r.applyClient(p, engine.Command{Type: engine.CmdStart, Actor: p.actor()}, in.Seq)
	case InKick:
		k, err := decode[KickIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		if !p.IsHost {
			r.sendError(p, ErrCodeNotAllowed, "host required", in.Seq)
			return
		}
		if err := r.kick(k.PersonID, k.Ban); err != nil {
			r.sendError(p, engineErrorCode(err), err.Error(), in.Seq)
		}
	case InSetHost:
		s, err := decode[PersonIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		if !p.IsHost {
			r.sendError(p, ErrCodeNotAllowed, "host required", in.Seq)
			return
		}
		if err := r.transferHost(s.PersonID); err != nil {
			r.sendError(p, engineErrorCode(err), err.Error(), in.Seq)
		}
	case InButtonReopen:
		b, err := decode[ButtonReopenIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		if !p.IsHost {
			r.sendError(p, ErrCodeNotAllowed, "host required", in.Seq)
			return
		}
		if err := r.reopenButton(b.ArmID); err != nil {
			r.sendError(p, engineErrorCode(err), err.Error(), in.Seq)
		}
	case InSetTrust:
		s, err := decode[SetTrustIn](in.P)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		if !p.IsHost {
			r.sendError(p, ErrCodeNotAllowed, "host required", in.Seq)
			return
		}
		if err := r.setTrust(s.PersonID, s.Level); err != nil {
			r.sendError(p, engineErrorCode(err), err.Error(), in.Seq)
		}
	default:
		cmd, ok, err := r.clientCommand(p, in)
		if err != nil {
			r.sendError(p, ErrCodeBadPayload, err.Error(), in.Seq)
			return
		}
		if !ok {
			r.sendError(p, ErrCodeUnknownType, "unknown message type "+in.T, in.Seq)
			return
		}
		r.applyClient(p, cmd, in.Seq)
	}
}

func (r *Room) applyClient(p *person, cmd engine.Command, ref int64) {
	if r.game == nil {
		r.sendError(p, ErrCodeBadState, "game not started", ref)
		return
	}
	if err := r.apply(cmd); err != nil {
		r.sendError(p, engineErrorCode(err), err.Error(), ref)
	}
}

// clientCommand maps a client envelope to an engine command. ok is false for
// unknown types; err reports a bad payload.
func (r *Room) clientCommand(p *person, in Inbound) (cmd engine.Command, ok bool, err error) {
	cmd = engine.Command{Actor: p.actor()}
	switch in.T {
	case InReady:
		cmd.Type = engine.CmdPlayerReady
	case "CHOOSE_QUESTION":
		v, e := decode[ChooseIn](in.P)
		cmd.Type, cmd.Theme, cmd.Q, err = engine.CmdChooseQuestion, v.Theme, v.Q, e
	case "PASS":
		cmd.Type = engine.CmdPass
	case "ANSWER":
		v, e := decode[AnswerIn](in.P)
		cmd.Type, err = engine.CmdAnswer, e
		cmd.PersonID, cmd.Text, cmd.OptionLabel, cmd.Number, cmd.Point = v.PersonID, v.Text, v.OptionLabel, v.Number, v.Point
		if v.Right != nil {
			cmd.Right, cmd.RightSet = *v.Right, true
		}
	case "ANSWER_DRAFT":
		v, e := decode[DraftIn](in.P)
		cmd.Type, cmd.Text, err = engine.CmdAnswerDraft, v.Text, e
	case "VALIDATE":
		v, e := decode[ValidateIn](in.P)
		cmd.Type, err = engine.CmdValidate, e
		cmd.PersonID, cmd.Right, cmd.Factor, cmd.FactorZero, cmd.Source = v.PersonID, v.Right, v.Factor, v.FactorZero, "showman"
	case "SELECT_PLAYER":
		v, e := decode[PersonIn](in.P)
		cmd.Type, cmd.PersonID, err = engine.CmdSelectPlayer, v.PersonID, e
	case "SET_STAKE":
		v, e := decode[SetStakeIn](in.P)
		cmd.Type, cmd.StakeMode, cmd.Amount, err = engine.CmdSetStake, v.StakeMode, v.Amount, e
	case "DELETE_THEME":
		v, e := decode[DeleteThemeIn](in.P)
		cmd.Type, cmd.Theme, err = engine.CmdDeleteTheme, v.Theme, e
	case "APPELLATE":
		v, e := decode[AppellateIn](in.P)
		cmd.Type, cmd.For, err = engine.CmdAppellate, v.For, e
	case "VOTE_APPEAL":
		v, e := decode[VoteIn](in.P)
		cmd.Type, cmd.Right, err = engine.CmdVoteAppeal, v.Right, e
	case "MEDIA_LOADED":
		cmd.Type = engine.CmdMediaLoaded
	case InMediaComplete:
		cmd.Type, cmd.PersonID = engine.CmdMediaCompleted, p.ID
	case "PAUSE":
		v, e := decode[PauseIn](in.P)
		cmd.Type, cmd.On, err = engine.CmdShowmanPause, v.On, e
	case "MOVE":
		v, e := decode[MoveIn](in.P)
		cmd.Type, cmd.Dir, cmd.Round, err = engine.CmdMove, v.Dir, v.Round, e
	case "TOGGLE":
		v, e := decode[ToggleIn](in.P)
		cmd.Type, cmd.Theme, cmd.Q, err = engine.CmdToggle, v.Theme, v.Q, e
	case "CHANGE_SCORE":
		v, e := decode[ChangeScoreIn](in.P)
		cmd.Type, cmd.PersonID, cmd.NewSum, err = engine.CmdChangeScore, v.PersonID, v.NewSum, e
	case "SET_CHOOSER":
		v, e := decode[PersonIn](in.P)
		cmd.Type, cmd.PersonID, err = engine.CmdSetChooser, v.PersonID, e
	case "SET_OPTIONS":
		v, e := decode[SetOptionsIn](in.P)
		opts := v.Options
		cmd.Type, cmd.Options, err = engine.CmdSetOptions, &opts, e
	case "NEXT":
		cmd.Type = engine.CmdNext
	default:
		return cmd, false, nil
	}
	return cmd, true, err
}

// replay answers HELLO{lastSeq}: envelopes of the previous connection newer
// than lastSeq are re-sent when the buffer still covers them (best effort;
// the SNAPSHOT already sent is authoritative).
func (r *Room) replay(p *person, lastSeq int64) {
	prevGen := p.gen - 1
	var prev []ringEntry
	for _, e := range p.ring.entries() {
		if e.gen == prevGen {
			prev = append(prev, e)
		}
	}
	covered := false
	switch {
	case len(prev) == 0:
		covered = lastSeq == 0 && p.prevLastSeq == 0
	case lastSeq >= p.prevLastSeq:
		covered = true
	case lastSeq == 0:
		covered = prev[0].seq == 1
	default:
		for _, e := range prev {
			if e.seq == lastSeq {
				covered = true
				break
			}
		}
	}
	var toSend []ringEntry
	if covered {
		for _, e := range prev {
			if e.seq > lastSeq && replayable[e.t] {
				toSend = append(toSend, e)
			}
		}
	}
	r.send(p, MsgResume, ResumePayload{FromSeq: lastSeq, Count: len(toSend), Covered: covered})
	for _, e := range toSend {
		r.sendRaw(p, e.t, e.p)
	}
}

// ---------------------------------------------------------------------------
// host actions
// ---------------------------------------------------------------------------

func (r *Room) kick(personID string, ban bool) error {
	p := r.persons[personID]
	if p == nil || p.IsAI {
		return ErrPersonNotFound
	}
	if p.IsHost {
		return fmt.Errorf("%w: cannot kick the host", engine.ErrNotAllowed)
	}
	p.kicked = true
	if ban {
		r.bannedNames[strings.ToLower(p.Name)] = true
		r.bannedIDs[p.ID] = strings.ToLower(p.Name)
	}
	if p.conn != nil {
		r.send(p, MsgKicked, KickedPayload{Banned: ban})
		c := p.conn
		if p.cancelAnchor != nil {
			p.cancelAnchor()
			p.cancelAnchor = nil
		}
		p.conn = nil
		p.connected = false
		c.Close(CloseKicked, "kicked")
	}
	if r.game != nil && p.Role == engine.RolePlayer {
		if gp := r.gamePlayer(p.ID); gp != nil && !gp.Kicked {
			r.submit(engine.Command{Type: engine.CmdKickPlayer, Actor: systemActor, PersonID: p.ID})
		}
	}
	if r.arm != nil {
		r.arm.arm.SetEligible(p.ID, false, r.mono())
		r.scheduleResolve()
	}
	r.removePerson(p)
	r.publishInfo()
	r.broadcastPersons()
	return nil
}

func (r *Room) gamePlayer(id string) *engine.Player {
	if r.game == nil {
		return nil
	}
	for _, gp := range r.game.State().Players {
		if gp.ID == id {
			return gp
		}
	}
	return nil
}

func (r *Room) transferHost(personID string) error {
	p := r.persons[personID]
	if p == nil || p.IsAI {
		return ErrPersonNotFound
	}
	if old := r.hostPerson(); old != nil {
		old.IsHost = false
	}
	p.IsHost = true
	r.publishInfo()
	r.broadcast(MsgHostChanged, PersonIn{PersonID: p.ID})
	r.broadcastPersons()
	return nil
}

// MsgHostChanged announces a new host.
const MsgHostChanged = "HOST_CHANGED"

func (r *Room) setTrust(personID string, level buzzer.TrustLevel) error {
	p := r.persons[personID]
	if p == nil || p.bconn == nil {
		return ErrPersonNotFound
	}
	switch level {
	case "", buzzer.TrustFull, buzzer.TrustConservative, buzzer.TrustServerOnly, buzzer.TrustArrivalOnly:
	default:
		return fmt.Errorf("%w: unknown trust level %q", ErrInvalidParams, level)
	}
	p.bconn.ForceLevel = level
	return nil
}

func (r *Room) updateSettings(patch SettingsPatch) error {
	if r.closing {
		return ErrRoomClosed
	}
	running := r.game != nil && r.status == StatusPlaying
	if patch.Buzzer != nil {
		if err := checkBuzzer(*patch.Buzzer); err != nil {
			return err
		}
	}
	if patch.Showman != nil {
		switch *patch.Showman {
		case ShowmanHuman, ShowmanAI, ShowmanHybrid:
		default:
			return fmt.Errorf("%w: unknown showman mode %q", ErrInvalidParams, *patch.Showman)
		}
		if running {
			return fmt.Errorf("%w: showman mode can only change in the lobby", ErrBadState)
		}
		if *patch.Showman == ShowmanAI && r.humanShowman() != nil {
			return fmt.Errorf("%w: a human showman is present", ErrBadState)
		}
	}
	if patch.JoinMode != nil {
		switch *patch.JoinMode {
		case JoinAny, JoinViewersOnly, JoinClosed:
		default:
			return fmt.Errorf("%w: unknown join mode %q", ErrInvalidParams, *patch.JoinMode)
		}
	}
	if patch.Times != nil && running {
		return fmt.Errorf("%w: times can only be replaced in the lobby (use rules for buttonBlockingMs/partialImageMs)", ErrBadState)
	}
	if patch.Name != nil {
		n := strings.TrimSpace(*patch.Name)
		if n == "" || utf8.RuneCountInString(n) > 100 {
			return fmt.Errorf("%w: name must be 1..100 characters", ErrInvalidParams)
		}
		r.name = n
	}
	if patch.Password != nil {
		r.password = *patch.Password
	}
	if patch.JoinMode != nil {
		r.joinMode = *patch.JoinMode
	}
	if patch.Showman != nil {
		r.showman = *patch.Showman
		r.syncAIShowman()
	}
	if patch.Buzzer != nil {
		r.bz = *patch.Buzzer
		for _, p := range r.persons {
			p.touchBuzzerSettings(r.bz)
		}
	}
	if patch.Times != nil {
		r.times = *patch.Times
	}
	if patch.Rules != nil {
		if running {
			actor := engine.Actor{Role: engine.RoleShowman, IsHost: true}
			if h := r.hostPerson(); h != nil {
				actor = h.actor()
				actor.IsHost = true
			}
			opts := *patch.Rules
			if err := r.apply(engine.Command{Type: engine.CmdSetOptions, Actor: actor, Options: &opts}); err != nil {
				return err
			}
			st := r.game.State()
			r.rules, r.times = st.Rules, st.Times
		} else {
			applyRulesPatch(&r.rules, &r.times, patch.Rules)
		}
	}
	r.updatedAt = r.wallMs()
	r.persistRoom()
	r.publishInfo()
	r.broadcast(MsgRoomSettings, r.info())
	return nil
}

func applyRulesPatch(rules *engine.Rules, times *engine.TimeSettings, o *engine.RulesPatch) {
	if o.Oral != nil {
		rules.Oral = *o.Oral
	}
	if o.Managed != nil {
		rules.Managed = *o.Managed
	}
	if o.DisplayAnswerOptionsLabels != nil {
		rules.DisplayAnswerOptionsLabels = *o.DisplayAnswerOptionsLabels
	}
	if o.FalseStart != nil {
		rules.FalseStart = *o.FalseStart
	}
	if o.ReadingSpeed != nil && *o.ReadingSpeed >= 0 {
		rules.ReadingSpeed = *o.ReadingSpeed
	}
	if o.PartialText != nil {
		rules.PartialText = *o.PartialText
	}
	if o.UseAppellations != nil {
		rules.UseAppellations = *o.UseAppellations
	}
	if o.ButtonBlockingMs != nil {
		times.ButtonBlockingMs = *o.ButtonBlockingMs
	}
	if o.PartialImageMs != nil {
		times.PartialImageMs = *o.PartialImageMs
	}
}

// syncAIShowman keeps the AI pseudo-person in the person list in ai mode.
func (r *Room) syncAIShowman() {
	ai := r.persons[AIShowmanID]
	if r.showman == ShowmanAI {
		if ai == nil {
			r.addPerson(&person{ID: AIShowmanID, Name: r.m.deps.Cfg.AIShowmanName, Role: engine.RoleShowman, IsAI: true, joinedAt: r.wallMs(), ring: newResumeRing(1)})
		}
		return
	}
	if ai != nil {
		r.removePerson(ai)
	}
}

// checkBuzzer validates host-supplied buzzer settings and rejects the modes
// this version cannot play (writtenAll and the allPlay tie-break need the
// written duel, which is not implemented).
func checkBuzzer(s buzzer.Settings) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	if s.Mode == buzzer.ModeWrittenAll || s.TieBreak == buzzer.TieBreakAllPlay {
		return ErrUnsupportedBuzzerMode
	}
	return nil
}

func (r *Room) buzzLog(questionID string) []ArmRecord {
	out := make([]ArmRecord, 0, len(r.records))
	for _, rec := range r.records {
		if questionID == "" || rec.QuestionID == questionID {
			out = append(out, rec)
		}
	}
	return out
}
