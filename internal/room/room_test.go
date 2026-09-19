package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sigame/internal/ai"
	"sigame/internal/buzzer"
	"sigame/internal/clock"
	"sigame/internal/engine"
	"sigame/internal/packs"
)

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type env struct {
	T   string
	Seq int64
	P   json.RawMessage
}

// fakeConn records every envelope; expect() consumes them by type.
type fakeConn struct {
	mu      sync.Mutex
	seq     int64
	msgs    chan env
	pending []env
	closed  chan struct{}
	once    sync.Once
	code    int
	reason  string
}

func newFakeConn() *fakeConn {
	return &fakeConn{msgs: make(chan env, 4096), closed: make(chan struct{})}
}

func (c *fakeConn) Send(t string, p json.RawMessage) (int64, error) {
	c.mu.Lock()
	c.seq++
	seq := c.seq
	c.mu.Unlock()
	c.msgs <- env{T: t, Seq: seq, P: p}
	return seq, nil
}

func (c *fakeConn) Close(code int, reason string) {
	c.once.Do(func() {
		c.mu.Lock()
		c.code, c.reason = code, reason
		c.mu.Unlock()
		close(c.closed)
	})
}

func (c *fakeConn) Info() ConnInfo { return ConnInfo{RemoteAddr: "fake"} }

func (c *fakeConn) closeCode(t *testing.T) int {
	t.Helper()
	select {
	case <-c.closed:
	case <-time.After(3 * time.Second):
		t.Fatalf("connection not closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code
}

// expect returns the first buffered or incoming envelope of type typ,
// keeping the others for later expects.
func (c *fakeConn) expectIn(t *testing.T, typ string, timeout time.Duration) env {
	t.Helper()
	for i, e := range c.pending {
		if e.T == typ {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return e
		}
	}
	deadline := time.After(timeout)
	for {
		select {
		case e := <-c.msgs:
			if e.T == typ {
				return e
			}
			c.pending = append(c.pending, e)
		case <-deadline:
			var seen []string
			for _, e := range c.pending {
				seen = append(seen, e.T)
			}
			t.Fatalf("timeout waiting for %s (buffered: %v)", typ, seen)
			return env{}
		}
	}
}

func (c *fakeConn) expect(t *testing.T, typ string) env { return c.expectIn(t, typ, 4*time.Second) }

// none asserts that no envelope of type typ arrives within d.
func (c *fakeConn) none(t *testing.T, typ string, d time.Duration) {
	t.Helper()
	for _, e := range c.pending {
		require.NotEqual(t, typ, e.T, "unexpected %s", typ)
	}
	deadline := time.After(d)
	for {
		select {
		case e := <-c.msgs:
			require.NotEqual(t, typ, e.T, "unexpected %s", typ)
			c.pending = append(c.pending, e)
		case <-deadline:
			return
		}
	}
}

func (c *fakeConn) drain() {
	c.pending = nil
	for {
		select {
		case <-c.msgs:
		default:
			return
		}
	}
}

func payload[T any](t *testing.T, e env) T {
	t.Helper()
	var v T
	if len(e.P) > 0 {
		require.NoError(t, json.Unmarshal(e.P, &v), "payload of %s", e.T)
	}
	return v
}

type mapPacks map[string]*packs.Pack

func (m mapPacks) Get(_ context.Context, id string) (*packs.Pack, error) {
	p, ok := m[id]
	if !ok {
		return nil, packs.ErrNotFound
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

func testPack() *packs.Pack {
	q := func(id string, price int, right string) packs.Question {
		return packs.Question{ID: id, Price: price, Type: packs.QSimple, Right: []string{right},
			Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Q?"}}}}
	}
	return &packs.Pack{ID: "p1", Name: "Test pack", Language: "en", Rounds: []packs.Round{{
		ID: "r1", Name: "Round 1", Type: packs.RoundStandard, Themes: []packs.Theme{
			{ID: "t1", Name: "T1", Questions: []packs.Question{q("q11", 100, "a1"), q("q12", 200, "a2")}},
			{ID: "t2", Name: "T2", Questions: []packs.Question{q("q21", 100, "b1"), q("q22", 200, "b2")}},
		}}}}
}

func fastTimes() engine.TimeSettings {
	t := engine.DefaultTimeSettings()
	t.QuestionSelectionMs = 10_000
	t.ButtonPressingMs = 2500
	t.AnsweringMs = 5000
	t.SoloAnsweringMs = 5000
	t.ShowmanDecisionMs = 5000
	t.ReflectionMs = 100
	t.ButtonBlockingMs = 400
	t.RoundMs = 120_000
	return t
}

func fastRules() engine.Rules {
	r := engine.DefaultRules()
	r.ReadingSpeed = 2000
	r.UseAppellations = false
	return r
}

func fastBuzzer() buzzer.Settings {
	s, _ := buzzer.Preset(buzzer.PresetLANWired)
	s.ArmJitterMinMs, s.ArmJitterMaxMs = 50, 120
	s.PressWindowMs = 800
	s.MaxCollectMs = 100
	s.FalseStartLockoutMs = 300
	s.LockoutCapMs = 1000
	return s
}

func newTestManager(t *testing.T, judge ai.Judge, cfg RoomConfig) *Manager {
	t.Helper()
	if cfg.SweepEvery == 0 {
		cfg.SweepEvery = time.Hour
	}
	if cfg.ConnQualityEveryMs == 0 {
		cfg.ConnQualityEveryMs = 60_000
	}
	m := NewManager(ManagerDeps{Packs: mapPacks{"p1": testPack()}, Judge: judge, Clock: clock.NewReal(), Cfg: cfg})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	return m
}

// client is a connected test participant with an honest clock model.
type client struct {
	t      *testing.T
	m      *Manager
	code   string
	sess   Session
	conn   *fakeConn
	att    *Attachment
	seq    int64
	offset float64 // server − client clock (ms)

	syncSeq int64
	prevSeq int64
	prevC4  float64
}

func (c *client) mono() float64  { return c.m.deps.Clock.Mono() }
func (c *client) local() float64 { return c.mono() - c.offset }

func (c *client) send(typ string, p any) int64 {
	c.seq++
	var raw json.RawMessage
	if p != nil {
		b, err := json.Marshal(p)
		require.NoError(c.t, err)
		raw = b
	}
	c.att.Deliver(Inbound{T: typ, Seq: c.seq, P: raw, TRecv: c.mono()})
	return c.seq
}

// attach connects (or reconnects) with a fresh fake connection.
func (c *client) attach() {
	c.t.Helper()
	c.conn = newFakeConn()
	att, err := c.m.Attach(c.code, c.sess.Token, c.conn)
	require.NoError(c.t, err)
	c.att = att
}

// syncBurst runs n interleaved SYNC exchanges.
func (c *client) syncBurst(n int) SyncAckPayload {
	c.t.Helper()
	var last SyncAckPayload
	for i := 0; i < n; i++ {
		c.syncSeq++
		c.send(InSync, SyncIn{Seq: c.syncSeq, C1: c.local(), PrevSeq: c.prevSeq, PrevC4: c.prevC4})
		ack := payload[SyncAckPayload](c.t, c.conn.expect(c.t, MsgSyncAck))
		c.prevSeq, c.prevC4 = ack.Seq, c.local()
		last = ack
		time.Sleep(2 * time.Millisecond)
	}
	return last
}

// press behaves like an honest browser: waits until the light and presses
// reaction ms later, stamping pointerdown on its own clock.
func (c *client) press(arm buzzer.ArmMsg, reaction float64) {
	c.t.Helper()
	lightAt := arm.ArmAt
	if arm.Mode != "scheduled" || arm.ArmAtLocal+c.offset < c.mono() {
		lightAt = c.mono()
	}
	litLocal := lightAt - c.offset
	target := lightAt + reaction
	if d := target - c.mono(); d > 0 {
		time.Sleep(time.Duration(d * float64(time.Millisecond)))
	}
	c.send(InPress, PressIn{ArmID: arm.ArmID, Seq: c.seq + 1, PressLocal: litLocal + reaction, LitLocal: litLocal, Src: "pointer"})
}

type table struct {
	t       *testing.T
	m       *Manager
	code    string
	host    string
	showman *client
	players []*client
}

func (tb *table) all() []*client {
	out := append([]*client{}, tb.players...)
	if tb.showman != nil {
		out = append(out, tb.showman)
	}
	return out
}

// newTable creates a room and joins/attaches a showman (host) and n players.
func newTable(t *testing.T, m *Manager, cp CreateParams, n int, showmanJoins bool) *table {
	t.Helper()
	if cp.PackID == "" {
		cp.PackID = "p1"
	}
	if cp.Times == (engine.TimeSettings{}) {
		cp.Times = fastTimes()
	}
	if cp.Rules.Mode == "" {
		cp.Rules = fastRules()
	}
	if cp.Buzzer.Mode == "" {
		cp.Buzzer = fastBuzzer()
	}
	info, hostTok, err := m.Create(context.Background(), cp)
	require.NoError(t, err)
	tb := &table{t: t, m: m, code: info.Code, host: hostTok}
	join := func(name string, role engine.Role, tok string, offset float64) *client {
		s, err := m.Join(context.Background(), info.Code, JoinParams{Name: name, Role: role}, tok)
		require.NoError(t, err)
		c := &client{t: t, m: m, code: info.Code, sess: s, offset: offset}
		c.attach()
		c.conn.expect(t, MsgWelcome)
		c.conn.expect(t, MsgSnapshot)
		return c
	}
	if showmanJoins {
		tb.showman = join("Showman", engine.RoleShowman, hostTok, 5000)
	}
	for i := 0; i < n; i++ {
		tok := ""
		if !showmanJoins && i == 0 {
			tok = hostTok
		}
		tb.players = append(tb.players, join(fmt.Sprintf("P%d", i+1), engine.RolePlayer, tok, float64(i+1)*123456.5))
	}
	for _, p := range tb.players {
		ack := p.syncBurst(buzzer.SyncBurstCount + 1)
		require.GreaterOrEqual(t, ack.Model.Samples, 5)
		require.Equal(t, buzzer.QualityGood, ack.Model.Quality)
	}
	for _, c := range tb.all() {
		c.conn.drain()
	}
	return tb
}

// start starts the game and resolves the chooser tie to chooser.
func (tb *table) start(starter *client, chooser *client) {
	tb.t.Helper()
	starter.send(InStart, nil)
	if tb.showman != nil {
		ask := payload[engine.AskSelectPlayerPayload](tb.t, tb.showman.conn.expect(tb.t, string(engine.EvAskSelectPlayer)))
		require.Equal(tb.t, "chooser", ask.Reason)
		tb.showman.send("SELECT_PLAYER", PersonIn{PersonID: chooser.sess.PersonID})
	}
	sc := payload[engine.SetChooserPayload](tb.t, chooser.conn.expect(tb.t, string(engine.EvSetChooser)))
	if tb.showman != nil {
		require.Equal(tb.t, chooser.sess.PersonID, sc.PersonID)
	}
	chooser.conn.expect(tb.t, string(engine.EvAskChoose))
}

// choose picks a cell and returns the BUTTON_ARM every eligible player got.
func (tb *table) choose(chooser *client, theme, q int, eligible ...*client) map[*client]buzzer.ArmMsg {
	tb.t.Helper()
	chooser.send("CHOOSE_QUESTION", ChooseIn{Theme: theme, Q: q})
	out := map[*client]buzzer.ArmMsg{}
	for _, p := range eligible {
		out[p] = payload[buzzer.ArmMsg](tb.t, p.conn.expect(tb.t, MsgButtonArm))
	}
	return out
}

func checkLeaks(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	t.Fatalf("goroutine leak: %d before, %d after\n%s", before, runtime.NumGoroutine(), buf[:n])
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestCreateJoinAttach(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{})
	info, hostTok, err := m.Create(context.Background(), CreateParams{PackID: "p1", Name: "Room", Password: "pw", Times: fastTimes(), Rules: fastRules(), Buzzer: fastBuzzer()})
	require.NoError(t, err)
	require.Len(t, info.Code, 5)
	require.Equal(t, StatusLobby, info.Status)
	require.True(t, info.HasPassword)
	require.Equal(t, "Test pack", info.PackName)

	_, err = m.Join(context.Background(), info.Code, JoinParams{Name: "A", Role: engine.RolePlayer}, "")
	require.ErrorIs(t, err, ErrBadPassword)
	_, err = m.Join(context.Background(), info.Code, JoinParams{Name: "A", Role: engine.RolePlayer}, "wrong-host-token")
	require.ErrorIs(t, err, ErrInvalidHostToken)
	_, err = m.Join(context.Background(), "ZZZZZ", JoinParams{Name: "A"}, "")
	require.ErrorIs(t, err, ErrRoomNotFound)

	host, err := m.Join(context.Background(), info.Code, JoinParams{Name: "Host", Role: engine.RoleShowman}, hostTok)
	require.NoError(t, err)
	require.True(t, host.IsHost)
	require.Equal(t, engine.RoleShowman, host.Role)

	a, err := m.Join(context.Background(), info.Code, JoinParams{Name: "A", Role: engine.RolePlayer, Password: "pw"}, "")
	require.NoError(t, err)
	require.False(t, a.IsHost)
	_, err = m.Join(context.Background(), info.Code, JoinParams{Name: "Other", Role: engine.RoleShowman, Password: "pw"}, "")
	require.ErrorIs(t, err, ErrBadRole)

	c := newFakeConn()
	att, err := m.Attach(info.Code, a.Token, c)
	require.NoError(t, err)
	w := payload[WelcomePayload](t, c.expect(t, MsgWelcome))
	require.Equal(t, a.PersonID, w.PersonID)
	require.Equal(t, info.Code, w.RoomCode)
	require.Equal(t, buzzer.SyncBurstCount, w.SyncPlan.Burst)
	s := payload[SnapshotPayload](t, c.expect(t, MsgSnapshot))
	require.Nil(t, s.Game)
	require.Equal(t, "idle", s.Buzzer.State)
	require.Len(t, s.Room.Players, 2)
	persons := payload[RoomPersonsPayload](t, c.expect(t, MsgRoomPersons))
	require.Len(t, persons.Persons, 2)

	// same name while connected → taken; reconnect by name after detach
	_, err = m.Join(context.Background(), info.Code, JoinParams{Name: "a", Role: engine.RolePlayer, Password: "pw"}, "")
	require.ErrorIs(t, err, ErrNameTaken)
	att.Detach()
	require.Eventually(t, func() bool {
		i, _ := m.Get(info.Code)
		for _, p := range i.Players {
			if p.ID == a.PersonID {
				return !p.Connected
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
	a2, err := m.Join(context.Background(), info.Code, JoinParams{Name: "A", Role: engine.RolePlayer, Password: "pw"}, "")
	require.NoError(t, err)
	require.Equal(t, a.PersonID, a2.PersonID)
	require.NotEqual(t, a.Token, a2.Token)

	_, err = m.Attach(info.Code, "bogus", newFakeConn())
	require.ErrorIs(t, err, ErrBadSession)

	list := m.List()
	require.Len(t, list, 1)
	require.Equal(t, info.Code, list[0].Code)

	// unsupported buzzer mode
	noRace, _ := buzzer.Preset(buzzer.PresetNoRace)
	_, _, err = m.Create(context.Background(), CreateParams{PackID: "p1", Buzzer: noRace})
	require.ErrorIs(t, err, ErrUnsupportedBuzzerMode)
	_, _, err = m.Create(context.Background(), CreateParams{PackID: "nope"})
	require.ErrorIs(t, err, ErrPackNotFound)
}

func TestSessionReplaced(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{})
	tb := newTable(t, m, CreateParams{}, 1, true)
	p := tb.players[0]
	old := p.conn
	p.attach()
	p.conn.expect(t, MsgWelcome)
	p.conn.expect(t, MsgSnapshot)
	old.expect(t, MsgSessionReplaced)
	require.Equal(t, CloseSessionReplaced, old.closeCode(t))
}

func TestFullQuestionFlow(t *testing.T) {
	before := runtime.NumGoroutine()
	m := newTestManager(t, nil, RoomConfig{})
	tb := newTable(t, m, CreateParams{}, 3, true)
	sm, A, B, C := tb.showman, tb.players[0], tb.players[1], tb.players[2]

	tb.start(sm, A)
	// everyone saw the round start
	for _, c := range tb.all() {
		c.conn.expect(t, string(engine.EvRoundStart))
		c.conn.expect(t, string(engine.EvTable))
	}

	arms := tb.choose(A, 0, 0, A, B, C)
	for _, c := range tb.all() {
		qs := payload[engine.QuestionStartPayload](t, c.conn.expect(t, string(engine.EvQuestionStart)))
		require.Equal(t, "q11", qs.QuestionID)
		c.conn.expect(t, string(engine.EvContent))
	}
	require.Equal(t, "scheduled", arms[A].Mode)
	require.Equal(t, arms[A].ArmID, arms[B].ArmID)
	require.InDelta(t, arms[A].ArmAt-A.offset, arms[A].ArmAtLocal, 3, "armAtLocal follows the measured offset")

	// A reacts in 220 ms, B in 330 ms → A wins.
	A.press(arms[A], 220)
	ackA := payload[PressAckPayload](t, A.conn.expect(t, MsgPressAck))
	require.Equal(t, buzzer.StatusPending, ackA.Status, ackA.Reason)
	require.InDelta(t, 220, ackA.ReactionMs, 60)
	pend := payload[PressPendingPayload](t, sm.conn.expect(t, MsgPressPending))
	require.Equal(t, A.sess.PersonID, pend.PlayerID)
	B.press(arms[B], 330)
	B.conn.expect(t, MsgPressAck)

	res := payload[buzzer.Result](t, sm.conn.expect(t, MsgButtonResult))
	require.Equal(t, buzzer.KindWinner, res.Kind)
	require.Equal(t, A.sess.PersonID, res.WinnerID)
	require.NotEmpty(t, res.Ranking)
	require.Equal(t, A.sess.PersonID, res.Ranking[0].PlayerID)
	pub := payload[ButtonResultPublic](t, B.conn.expect(t, MsgButtonResult))
	require.Equal(t, A.sess.PersonID, pub.WinnerID)
	audit := payload[ButtonAuditPayload](t, sm.conn.expect(t, MsgButtonAudit))
	require.Equal(t, res.ArmID, audit.ArmID)
	require.NotEmpty(t, audit.Entries)

	// A answers wrongly → showman validates → re-arm for B and C.
	ask := payload[engine.AskAnswerPayload](t, A.conn.expect(t, string(engine.EvAskAnswer)))
	require.Equal(t, A.sess.PersonID, ask.PersonID)
	A.send("ANSWER", AnswerIn{Text: "nope"})
	v := payload[engine.AskValidatePayload](t, sm.conn.expect(t, string(engine.EvAskValidate)))
	require.Equal(t, "nope", v.Answer)
	require.Equal(t, []string{"a1"}, v.Rights)
	B.conn.none(t, string(engine.EvAskValidate), 50*time.Millisecond)
	sm.send("VALIDATE", ValidateIn{Right: false})
	for _, c := range tb.all() {
		val := payload[engine.ValidationPayload](t, c.conn.expect(t, string(engine.EvValidation)))
		require.False(t, val.Right)
		require.Equal(t, "showman", val.Source)
		score := payload[engine.PersonScorePayload](t, c.conn.expect(t, string(engine.EvPersonScore)))
		require.Equal(t, -100, score.Score)
	}

	armB := payload[buzzer.ArmMsg](t, B.conn.expect(t, MsgButtonArm))
	C.conn.expect(t, MsgButtonArm)
	A.conn.none(t, MsgButtonArm, 50*time.Millisecond)
	require.NotEqual(t, arms[B].ArmID, armB.ArmID)
	B.press(armB, 250)
	res2 := payload[buzzer.Result](t, sm.conn.expect(t, MsgButtonResult))
	require.Equal(t, B.sess.PersonID, res2.WinnerID)
	B.conn.expect(t, string(engine.EvAskAnswer))
	B.send("ANSWER", AnswerIn{Text: "a1"})
	sm.conn.expect(t, string(engine.EvAskValidate))
	sm.send("VALIDATE", ValidateIn{Right: true})
	val2 := payload[engine.ValidationPayload](t, A.conn.expect(t, string(engine.EvValidation)))
	require.True(t, val2.Right)
	score2 := payload[engine.PersonScorePayload](t, A.conn.expect(t, string(engine.EvPersonScore)))
	require.Equal(t, B.sess.PersonID, score2.PersonID)
	require.Equal(t, 100, score2.Score)
	A.conn.expect(t, string(engine.EvRightAnswer))
	A.conn.expect(t, string(engine.EvQuestionEnd))
	sums := payload[engine.SumsPayload](t, A.conn.expect(t, string(engine.EvSums)))
	require.Len(t, sums.Scores, 3)
	B.conn.expect(t, string(engine.EvAskChoose)) // B answered right → chooser

	// Buzz log holds both arms.
	log, err := m.BuzzLog(tb.code, tb.host, "")
	require.NoError(t, err)
	require.Len(t, log, 2)
	require.Equal(t, "q11", log[0].QuestionID)
	require.NotEmpty(t, log[0].Audit)
	_, err = m.BuzzLog(tb.code, "bad", "")
	require.ErrorIs(t, err, ErrInvalidHostToken)

	// Misfire during reading → LOCKOUT; then nobody presses → engine nobody path.
	for _, c := range tb.all() {
		c.conn.drain()
	}
	B.send("CHOOSE_QUESTION", ChooseIn{Theme: 0, Q: 1})
	C.conn.expect(t, string(engine.EvQuestionStart))
	C.send(InMisfire, MisfireIn{})
	lo := payload[LockoutPayload](t, A.conn.expect(t, MsgLockout))
	require.Equal(t, C.sess.PersonID, lo.PlayerID)
	require.Equal(t, "misfire", lo.Reason)
	require.Greater(t, lo.DurationMs, int64(0))
	A.conn.expect(t, MsgButtonArm)
	B.conn.expect(t, MsgButtonArm)
	C.conn.none(t, MsgButtonArm, 50*time.Millisecond) // locked out at arm time
	res3 := payload[buzzer.Result](t, sm.conn.expectIn(t, MsgButtonResult, 5*time.Second))
	require.Equal(t, buzzer.KindNobody, res3.Kind)
	A.conn.expect(t, string(engine.EvRightAnswer))
	A.conn.expect(t, string(engine.EvQuestionEnd))

	info, ok := m.Get(tb.code)
	require.True(t, ok)
	require.Equal(t, StatusPlaying, info.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Shutdown(ctx)
	for _, c := range tb.all() {
		c.conn.expect(t, MsgRoomClosed)
		require.Equal(t, CloseGoingAway, c.conn.closeCode(t))
	}
	_, ok = m.Get(tb.code)
	require.False(t, ok)
	checkLeaks(t, before)
}

func TestResumeReplay(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{ResumeBufferSize: 50})
	tb := newTable(t, m, CreateParams{}, 2, true)
	A, B := tb.players[0], tb.players[1]
	B.send(InChat, ChatIn{Text: "hello"})
	chat := A.conn.expect(t, MsgChat)
	lastSeen := chat.Seq - 1 // pretend A never saw the chat line
	B.send(InChat, ChatIn{Text: "again"})
	B.conn.expect(t, MsgError) // rate limited: 1/s
	A.att.Detach()
	time.Sleep(20 * time.Millisecond)

	A.attach()
	A.conn.expect(t, MsgWelcome)
	snap := payload[SnapshotPayload](t, A.conn.expect(t, MsgSnapshot))
	require.Equal(t, chat.Seq, snap.LastSeq)
	A.send(InHello, HelloIn{LastSeq: lastSeen})
	res := payload[ResumePayload](t, A.conn.expect(t, MsgResume))
	require.True(t, res.Covered)
	require.Equal(t, 1, res.Count)
	replayed := payload[ChatPayload](t, A.conn.expect(t, MsgChat))
	require.Equal(t, "hello", replayed.Text)

	// A lastSeq the buffer cannot cover → nothing replayed.
	A.att.Detach()
	time.Sleep(20 * time.Millisecond)
	A.attach()
	A.conn.expect(t, MsgSnapshot)
	A.send(InHello, HelloIn{LastSeq: 999})
	res = payload[ResumePayload](t, A.conn.expect(t, MsgResume))
	require.True(t, res.Covered) // nothing newer than 999 was sent
	require.Equal(t, 0, res.Count)
}

func TestAIShowman(t *testing.T) {
	var reqMu sync.Mutex
	var reqs []ai.Request
	judge := ai.JudgeFunc(func(_ context.Context, req ai.Request) (ai.Verdict, error) {
		reqMu.Lock()
		reqs = append(reqs, req)
		reqMu.Unlock()
		if req.PlayerAnswer == "yes" {
			return ai.Verdict{Right: true, Factor: 1, Source: ai.SourceAI, Reason: "ok"}, nil
		}
		return ai.Verdict{Uncertain: true, Source: ai.SourceAI}, nil
	})
	m := newTestManager(t, judge, RoomConfig{})
	tb := newTable(t, m, CreateParams{Showman: ShowmanAI}, 2, false)
	A, B := tb.players[0], tb.players[1]
	require.True(t, A.sess.IsHost)
	info, _ := m.Get(tb.code)
	require.Len(t, info.Players, 3, "AI showman is listed")

	A.send(InStart, nil)
	sc := payload[engine.SetChooserPayload](t, A.conn.expect(t, string(engine.EvSetChooser)))
	chooser := A
	if sc.PersonID == B.sess.PersonID {
		chooser = B
	}
	chooser.conn.expect(t, string(engine.EvAskChoose))
	arms := tb.choose(chooser, 0, 0, A, B)
	A.press(arms[A], 210)
	A.conn.expect(t, string(engine.EvAskAnswer))
	A.send("ANSWER", AnswerIn{Text: "yes"})
	// Players only learn that an automatic verdict was applied; the outcome
	// arrives with VALIDATION (the full verdict is showman-only).
	verdict := payload[AIVerdictPublic](t, B.conn.expect(t, MsgAIVerdict))
	require.True(t, verdict.Applied)
	require.Equal(t, A.sess.PersonID, verdict.PersonID)
	val := payload[engine.ValidationPayload](t, B.conn.expect(t, string(engine.EvValidation)))
	require.True(t, val.Right)
	require.Equal(t, "ai", val.Source)
	B.conn.expect(t, string(engine.EvQuestionEnd))

	// Uncertain → rejected without penalty.
	A.conn.drain()
	B.conn.drain()
	A.conn.expect(t, string(engine.EvAskChoose))
	arms = tb.choose(A, 1, 0, A, B)
	B.press(arms[B], 210)
	B.conn.expect(t, string(engine.EvAskAnswer))
	B.send("ANSWER", AnswerIn{Text: "hmm"})
	verdict = payload[AIVerdictPublic](t, A.conn.expect(t, MsgAIVerdict))
	require.True(t, verdict.Applied)
	val = payload[engine.ValidationPayload](t, A.conn.expect(t, string(engine.EvValidation)))
	require.False(t, val.Right)
	require.Equal(t, 0.0, val.Factor)

	reqMu.Lock()
	defer reqMu.Unlock()
	require.Len(t, reqs, 2)
	require.Equal(t, "Q?", reqs[0].QuestionText)
	require.Equal(t, []string{"a1"}, reqs[0].Right)
	require.Equal(t, "q11", reqs[0].QuestionID)
	require.Equal(t, "en", reqs[0].Language)
	require.Equal(t, []string{"b1"}, reqs[1].Right)
}

func TestHybridTimeoutAppliesAIVerdict(t *testing.T) {
	judge := ai.JudgeFunc(func(_ context.Context, req ai.Request) (ai.Verdict, error) {
		return ai.Verdict{Right: true, Factor: 1, Source: ai.SourceAI, Reason: "sure"}, nil
	})
	m := newTestManager(t, judge, RoomConfig{})
	tb := newTable(t, m, CreateParams{Showman: ShowmanHybrid, HybridConfirmMs: 300}, 2, true)
	sm, A, B := tb.showman, tb.players[0], tb.players[1]
	tb.start(sm, A)
	arms := tb.choose(A, 0, 0, A, B)
	A.press(arms[A], 210)
	A.conn.expect(t, string(engine.EvAskAnswer))
	A.send("ANSWER", AnswerIn{Text: "whatever"})
	sm.conn.expect(t, string(engine.EvAskValidate))
	sug := payload[engine.AISuggestionPayload](t, sm.conn.expect(t, string(engine.EvAISuggestion)))
	require.True(t, sug.Right)
	B.conn.none(t, string(engine.EvValidation), 150*time.Millisecond)
	val := payload[engine.ValidationPayload](t, B.conn.expect(t, string(engine.EvValidation)))
	require.True(t, val.Right)
	require.Equal(t, "ai", val.Source)
}

func TestHumanShowmanValidationTimeoutUsesFuzzy(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{})
	times := fastTimes()
	times.ShowmanDecisionMs = 300
	tb := newTable(t, m, CreateParams{Times: times}, 2, true)
	sm, A, B := tb.showman, tb.players[0], tb.players[1]
	tb.start(sm, A)
	arms := tb.choose(A, 0, 0, A, B)
	A.press(arms[A], 210)
	A.conn.expect(t, string(engine.EvAskAnswer))
	A.send("ANSWER", AnswerIn{Text: "A1"})
	sm.conn.expect(t, string(engine.EvAskValidate))
	val := payload[engine.ValidationPayload](t, B.conn.expect(t, string(engine.EvValidation)))
	require.True(t, val.Right)
	require.Equal(t, "auto", val.Source)
	verdict := payload[AIVerdictPayload](t, B.conn.expect(t, MsgAIVerdict))
	require.True(t, verdict.Applied)
}

func TestKickBanAndHostTransfer(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{})
	tb := newTable(t, m, CreateParams{}, 2, true)
	sm, A, B := tb.showman, tb.players[0], tb.players[1]

	require.ErrorIs(t, m.Kick(tb.code, "bad", B.sess.PersonID, true), ErrInvalidHostToken)
	// A (not host) cannot kick over WS.
	A.send(InKick, KickIn{PersonID: B.sess.PersonID})
	e := payload[ErrorPayload](t, A.conn.expect(t, MsgError))
	require.Equal(t, ErrCodeNotAllowed, e.Code)

	sm.send(InKick, KickIn{PersonID: B.sess.PersonID, Ban: true})
	k := payload[KickedPayload](t, B.conn.expect(t, MsgKicked))
	require.True(t, k.Banned)
	require.Equal(t, CloseKicked, B.conn.closeCode(t))
	_, err := m.Join(context.Background(), tb.code, JoinParams{Name: "p2", Role: engine.RolePlayer}, "")
	require.ErrorIs(t, err, ErrBanned)
	require.ErrorIs(t, m.Unban(tb.code, tb.host, "nobody"), ErrPersonNotFound)
	require.NoError(t, m.Unban(tb.code, tb.host, B.sess.PersonID))
	_, err = m.Join(context.Background(), tb.code, JoinParams{Name: "P2", Role: engine.RolePlayer}, "")
	require.NoError(t, err)

	require.NoError(t, m.TransferHost(tb.code, tb.host, A.sess.PersonID))
	hc := payload[PersonIn](t, A.conn.expect(t, MsgHostChanged))
	require.Equal(t, A.sess.PersonID, hc.PersonID)
	info, _ := m.Get(tb.code)
	for _, p := range info.Players {
		require.Equal(t, p.ID == A.sess.PersonID, p.IsHost)
	}

	// settings: name + buzzer patch + invalid mode
	name := "Renamed"
	upd, err := m.UpdateSettings(tb.code, tb.host, SettingsPatch{Name: &name})
	require.NoError(t, err)
	require.Equal(t, "Renamed", upd.Name)
	A.conn.expect(t, MsgRoomSettings)
	noRace, _ := buzzer.Preset(buzzer.PresetNoRace)
	_, err = m.UpdateSettings(tb.code, tb.host, SettingsPatch{Buzzer: &noRace})
	require.ErrorIs(t, err, ErrUnsupportedBuzzerMode)
	bad := ShowmanMode("robot")
	_, err = m.UpdateSettings(tb.code, tb.host, SettingsPatch{Showman: &bad})
	require.ErrorIs(t, err, ErrInvalidParams)

	require.ErrorIs(t, m.Close(tb.code, "bad"), ErrInvalidHostToken)
	require.NoError(t, m.Close(tb.code, tb.host))
	rc := payload[RoomClosedPayload](t, A.conn.expect(t, MsgRoomClosed))
	require.Equal(t, "host", rc.Reason)
	require.Equal(t, CloseNormal, A.conn.closeCode(t))
	require.Eventually(t, func() bool { _, ok := m.Get(tb.code); return !ok }, 2*time.Second, 10*time.Millisecond)
}

func TestTTLClosesIdleRoom(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{RoomTTL: 150 * time.Millisecond, SweepEvery: 30 * time.Millisecond})
	tb := newTable(t, m, CreateParams{}, 1, true)
	tb.players[0].conn.expect(t, MsgRoomClosed)
	require.Equal(t, CloseNormal, tb.players[0].conn.closeCode(t))
	require.Eventually(t, func() bool { _, ok := m.Get(tb.code); return !ok }, 2*time.Second, 10*time.Millisecond)
	_, err := m.Join(context.Background(), tb.code, JoinParams{Name: "x"}, "")
	require.ErrorIs(t, err, ErrRoomNotFound)
}

func TestErrorsAndUnknownTypes(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{})
	tb := newTable(t, m, CreateParams{}, 1, true)
	A := tb.players[0]
	ref := A.send("NO_SUCH_THING", nil)
	e := payload[ErrorPayload](t, A.conn.expect(t, MsgError))
	require.Equal(t, ErrCodeUnknownType, e.Code)
	require.Equal(t, ref, e.Ref)
	A.send("CHOOSE_QUESTION", ChooseIn{})
	e = payload[ErrorPayload](t, A.conn.expect(t, MsgError))
	require.Equal(t, ErrCodeBadState, e.Code)
	A.att.Deliver(Inbound{T: "CHOOSE_QUESTION", Seq: 9, P: json.RawMessage(`{"theme":"x"}`), TRecv: A.mono()})
	e = payload[ErrorPayload](t, A.conn.expect(t, MsgError))
	require.Equal(t, ErrCodeBadPayload, e.Code)
	A.send(InStart, nil)
	e = payload[ErrorPayload](t, A.conn.expect(t, MsgError))
	require.Equal(t, ErrCodeNotAllowed, e.Code)
	// a stale press (no arm) outside a question is just stale
	A.send(InPress, PressIn{ArmID: "x"})
	ack := payload[PressAckPayload](t, A.conn.expect(t, MsgPressAck))
	require.Equal(t, buzzer.StatusStale, ack.Status)
}

func TestConnQualityVariants(t *testing.T) {
	m := newTestManager(t, nil, RoomConfig{ConnQualityEveryMs: 60})
	tb := newTable(t, m, CreateParams{}, 1, true)
	staff := payload[ConnQualityPayload](t, tb.showman.conn.expect(t, MsgConnQuality))
	require.Len(t, staff.Players, 1)
	require.NotNil(t, staff.Players[0].Trust)
	pub := payload[ConnQualityPayload](t, tb.players[0].conn.expect(t, MsgConnQuality))
	require.Len(t, pub.Players, 1)
	require.Nil(t, pub.Players[0].Trust)
	require.Equal(t, buzzer.QualityGood, pub.Players[0].Quality)
}

func TestErrorIsMapping(t *testing.T) {
	require.Equal(t, ErrCodeNotAllowed, engineErrorCode(fmt.Errorf("x: %w", engine.ErrNotAllowed)))
	require.Equal(t, ErrCodeBadArgument, engineErrorCode(engine.ErrBadArgument))
	require.Equal(t, ErrCodeBadState, engineErrorCode(errors.New("other")))
}
