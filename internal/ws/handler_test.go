package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"sigame/internal/buzzer"
	"sigame/internal/clock"
	"sigame/internal/db"
	"sigame/internal/engine"
	"sigame/internal/packs"
	"sigame/internal/room"
)

// ---------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	rooms  *room.Manager
	clock  clock.Clock
	code   string
	host   string
	packID string
}

func newHarness(t *testing.T, perSec int) *harness {
	t.Helper()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Migrate(ctx, sqlDB))
	repo := packs.NewRepo(sqlDB)
	pack := tinyPack()
	require.NoError(t, repo.Create(ctx, pack))

	clk := clock.NewReal()
	rooms := room.NewManager(room.ManagerDeps{DB: sqlDB, Packs: repo, Clock: clk,
		Cfg: room.RoomConfig{SweepEvery: time.Hour, ConnQualityEveryMs: 60_000}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rooms.Shutdown(ctx)
	})

	r := chi.NewRouter()
	Mount(r, &Handler{Rooms: rooms, Clock: clk, Cfg: Config{MaxMessageBytes: 4096, MessagesPerSec: perSec, WriteTimeout: 2 * time.Second, OutboundBuffer: 64}})
	srv := httptest.NewUnstartedServer(r)
	srv.Config.ConnContext = ConnContext
	srv.Start()
	t.Cleanup(srv.Close)

	times := engine.DefaultTimeSettings()
	times.ButtonPressingMs = 2500
	times.ReflectionMs = 100
	rules := engine.DefaultRules()
	rules.ReadingSpeed = 2000
	bz, _ := buzzer.Preset(buzzer.PresetLANWired)
	bz.ArmJitterMinMs, bz.ArmJitterMaxMs = 50, 120
	bz.PressWindowMs = 800
	bz.MaxCollectMs = 100
	info, hostTok, err := rooms.Create(ctx, room.CreateParams{PackID: pack.ID, Name: "ws", Rules: rules, Times: times, Buzzer: bz})
	require.NoError(t, err)

	// The room row was persisted.
	var status string
	require.NoError(t, sqlDB.QueryRowContext(ctx, `SELECT status FROM rooms WHERE code = ?`, info.Code).Scan(&status))
	require.Equal(t, "lobby", status)
	return &harness{t: t, srv: srv, rooms: rooms, clock: clk, code: info.Code, host: hostTok, packID: pack.ID}
}

func tinyPack() *packs.Pack {
	q := func(price int, right string) packs.Question {
		return packs.Question{Price: price, Type: packs.QSimple, Right: []string{right},
			Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Q?"}}}}
	}
	return &packs.Pack{Name: "tiny", Rounds: []packs.Round{{Name: "R1", Type: packs.RoundStandard, Themes: []packs.Theme{
		{Name: "T1", Questions: []packs.Question{q(100, "a1"), q(200, "a2")}},
	}}}}
}

func (h *harness) wsURL(code, token string) string {
	return "ws" + strings.TrimPrefix(h.srv.URL, "http") + "/ws?room=" + code + "&token=" + token
}

// wsClient is a real coder/websocket client.
type wsClient struct {
	t       *testing.T
	h       *harness
	c       *websocket.Conn
	sess    room.Session
	seq     int64
	lastSeq int64
	pending []Envelope
	offset  float64
	syncSeq int64
	prevSeq int64
	prevC4  float64
}

func (h *harness) join(name string, role engine.Role, tok string) room.Session {
	s, err := h.rooms.Join(context.Background(), h.code, room.JoinParams{Name: name, Role: role}, tok)
	require.NoError(h.t, err)
	return s
}

func (h *harness) dial(sess room.Session) *wsClient {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, h.wsURL(h.code, sess.Token), nil)
	require.NoError(h.t, err)
	c.SetReadLimit(1 << 20)
	cl := &wsClient{t: h.t, h: h, c: c, sess: sess, offset: 777_000}
	h.t.Cleanup(func() { _ = c.CloseNow() })
	return cl
}

func (cl *wsClient) send(typ string, p any) int64 {
	cl.seq++
	b, err := Encode(typ, cl.seq, p)
	require.NoError(cl.t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(cl.t, cl.c.Write(ctx, websocket.MessageText, b))
	return cl.seq
}

func (cl *wsClient) sendRaw(b []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(cl.t, cl.c.Write(ctx, websocket.MessageText, b))
}

// read returns the next envelope, checking seq monotonicity.
func (cl *wsClient) read(timeout time.Duration) (Envelope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, data, err := cl.c.Read(ctx)
	if err != nil {
		return Envelope{}, err
	}
	e, err := Decode(data, 1<<20)
	require.NoError(cl.t, err)
	require.Equal(cl.t, cl.lastSeq+1, e.Seq, "server seq must be monotonic (%s)", e.T)
	cl.lastSeq = e.Seq
	return e, nil
}

func (cl *wsClient) expect(typ string) Envelope {
	cl.t.Helper()
	for i, e := range cl.pending {
		if e.T == typ {
			cl.pending = append(cl.pending[:i], cl.pending[i+1:]...)
			return e
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e, err := cl.read(time.Until(deadline))
		if err != nil {
			cl.t.Fatalf("waiting for %s: %v", typ, err)
		}
		if e.T == typ {
			return e
		}
		cl.pending = append(cl.pending, e)
	}
	cl.t.Fatalf("timeout waiting for %s", typ)
	return Envelope{}
}

func (cl *wsClient) local() float64 { return cl.h.clock.Mono() - cl.offset }

func (cl *wsClient) syncBurst(n int) room.SyncAckPayload {
	var last room.SyncAckPayload
	for i := 0; i < n; i++ {
		cl.syncSeq++
		cl.send(room.InSync, room.SyncIn{Seq: cl.syncSeq, C1: cl.local(), PrevSeq: cl.prevSeq, PrevC4: cl.prevC4})
		ack, err := DecodePayload[room.SyncAckPayload](cl.expect(room.MsgSyncAck))
		require.NoError(cl.t, err)
		cl.prevSeq, cl.prevC4 = ack.Seq, cl.local()
		last = ack
		time.Sleep(2 * time.Millisecond)
	}
	return last
}

func (cl *wsClient) press(arm buzzer.ArmMsg, reaction float64) {
	lightAt := arm.ArmAt
	if arm.Mode != "scheduled" || arm.ArmAtLocal+cl.offset < cl.h.clock.Mono() {
		lightAt = cl.h.clock.Mono()
	}
	litLocal := lightAt - cl.offset
	if d := lightAt + reaction - cl.h.clock.Mono(); d > 0 {
		time.Sleep(time.Duration(d * float64(time.Millisecond)))
	}
	cl.send(room.InPress, room.PressIn{ArmID: arm.ArmID, Seq: 1, PressLocal: litLocal + reaction, LitLocal: litLocal, Src: "pointer"})
}

func decodeP[T any](t *testing.T, e Envelope) T {
	t.Helper()
	v, err := DecodePayload[T](e)
	require.NoError(t, err)
	return v
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestHandlerRejectsBadRequests(t *testing.T) {
	h := newHarness(t, 40)
	resp, err := http.Get(h.srv.URL + "/ws")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, h.wsURL(h.code, "bogus"), nil)
	require.NoError(t, err)
	defer c.CloseNow()
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	e, err := Decode(data, 4096)
	require.NoError(t, err)
	require.Equal(t, "ERROR", e.T)
	_, _, err = c.Read(ctx)
	require.Error(t, err)
	require.Equal(t, websocket.StatusCode(room.CloseBadToken), websocket.CloseStatus(err))
}

func TestHandlerWelcomeSyncAndRateLimit(t *testing.T) {
	h := newHarness(t, 10)
	sess := h.join("A", engine.RolePlayer, h.host)
	cl := h.dial(sess)
	w := decodeP[room.WelcomePayload](t, cl.expect(room.MsgWelcome))
	require.Equal(t, sess.PersonID, w.PersonID)
	require.True(t, w.IsHost)
	snap := decodeP[room.SnapshotPayload](t, cl.expect(room.MsgSnapshot))
	require.Nil(t, snap.Game)
	require.Equal(t, h.code, snap.Room.Code)

	ack := cl.syncBurst(buzzer.SyncBurstCount + 1)
	require.GreaterOrEqual(t, ack.Model.Samples, 5)
	require.Equal(t, buzzer.QualityGood, ack.Model.Quality)
	require.InDelta(t, cl.offset, ack.Model.OffsetMs, 5)
	require.Less(t, ack.Model.RttRefMs, 30.0)

	// The WS ping anchor is measured by the room's anchor loop within ~1 s.
	require.Eventually(t, func() bool {
		cl.syncSeq++
		cl.send(room.InSync, room.SyncIn{Seq: cl.syncSeq, C1: cl.local(), PrevSeq: cl.prevSeq, PrevC4: cl.prevC4})
		a := decodeP[room.SyncAckPayload](t, cl.expect(room.MsgSyncAck))
		cl.prevSeq, cl.prevC4 = a.Seq, cl.local()
		return a.Model.RttWsMs > 0
	}, 4*time.Second, 200*time.Millisecond)

	// Rate limit: 10/s with burst 20 → a flood gets ERROR rateLimited
	// (30 messages = 10 strikes, below the 20 that close the socket).
	for i := 0; i < 30; i++ {
		cl.send(room.InVisible, nil)
	}
	e := cl.expect("ERROR")
	p := decodeP[ErrorPayload](t, e)
	require.Equal(t, "rateLimited", p.Code)

	// Bad envelope → ERROR badPayload, connection stays open (the remaining
	// rateLimited errors of the flood are skipped).
	time.Sleep(1100 * time.Millisecond)
	cl.pending = nil
	cl.sendRaw([]byte(`{"t":"lower"}`))
	for {
		p = decodeP[ErrorPayload](t, cl.expect("ERROR"))
		if p.Code != "rateLimited" {
			break
		}
	}
	require.Equal(t, "badPayload", p.Code)
	cl.send(room.InChat, room.ChatIn{Text: "still alive"})
	chat := decodeP[room.ChatPayload](t, cl.expect(room.MsgChat))
	require.Equal(t, "still alive", chat.Text)
}

func TestHandlerRateLimitCloses(t *testing.T) {
	h := newHarness(t, 10)
	cl := h.dial(h.join("A", engine.RolePlayer, ""))
	cl.expect(room.MsgSnapshot)
	for i := 0; i < 60; i++ {
		cl.send(room.InVisible, nil)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := cl.read(time.Until(deadline))
		if err != nil {
			require.Equal(t, websocket.StatusCode(room.ClosePolicy), websocket.CloseStatus(err))
			return
		}
	}
	t.Fatal("socket was not closed after 20 consecutive rate-limit strikes")
}

func TestHandlerSessionReplaced(t *testing.T) {
	h := newHarness(t, 40)
	sess := h.join("A", engine.RolePlayer, "")
	first := h.dial(sess)
	first.expect(room.MsgSnapshot)
	second := h.dial(sess)
	second.expect(room.MsgSnapshot)
	first.expect(room.MsgSessionReplaced)
	_, err := first.read(3 * time.Second)
	require.Error(t, err)
	require.Equal(t, websocket.StatusCode(room.CloseSessionReplaced), websocket.CloseStatus(err))
	_ = first
}

func TestHandlerMiniGame(t *testing.T) {
	before := runtime.NumGoroutine()
	h := newHarness(t, 100)
	sm := h.dial(h.join("SM", engine.RoleShowman, h.host))
	a := h.dial(h.join("A", engine.RolePlayer, ""))
	b := h.dial(h.join("B", engine.RolePlayer, ""))
	for _, c := range []*wsClient{sm, a, b} {
		c.expect(room.MsgSnapshot)
	}
	a.syncBurst(buzzer.SyncBurstCount + 1)
	b.syncBurst(buzzer.SyncBurstCount + 1)

	sm.send(room.InStart, nil)
	ask := decodeP[engine.AskSelectPlayerPayload](t, sm.expect(string(engine.EvAskSelectPlayer)))
	require.Len(t, ask.Candidates, 2)
	sm.send("SELECT_PLAYER", room.PersonIn{PersonID: a.sess.PersonID})
	a.expect(string(engine.EvAskChoose))
	a.send("CHOOSE_QUESTION", room.ChooseIn{Theme: 0, Q: 0})
	armA := decodeP[buzzer.ArmMsg](t, a.expect(room.MsgButtonArm))
	armB := decodeP[buzzer.ArmMsg](t, b.expect(room.MsgButtonArm))
	require.Equal(t, armA.ArmID, armB.ArmID)
	require.Equal(t, "scheduled", armA.Mode)
	a.send(room.InArmAck, room.ArmAckIn{ArmID: armA.ArmID, RecvLocal: a.local()})

	b.press(armB, 200)
	ackB := decodeP[room.PressAckPayload](t, b.expect(room.MsgPressAck))
	require.Equal(t, buzzer.StatusPending, ackB.Status, ackB.Reason)
	a.press(armA, 320)
	a.expect(room.MsgPressAck)

	res := decodeP[buzzer.Result](t, sm.expect(room.MsgButtonResult))
	require.Equal(t, b.sess.PersonID, res.WinnerID)
	pub := decodeP[room.ButtonResultPublic](t, a.expect(room.MsgButtonResult))
	require.Equal(t, b.sess.PersonID, pub.WinnerID)
	b.expect(string(engine.EvAskAnswer))
	b.send("ANSWER", room.AnswerIn{Text: "a1"})
	sm.expect(string(engine.EvAskValidate))
	sm.send("VALIDATE", room.ValidateIn{Right: true})
	val := decodeP[engine.ValidationPayload](t, a.expect(string(engine.EvValidation)))
	require.True(t, val.Right)
	a.expect(string(engine.EvQuestionEnd))

	info, ok := h.rooms.Get(h.code)
	require.True(t, ok)
	require.Equal(t, room.StatusPlaying, info.Status)

	// Shutdown closes the sockets with 1001 and leaves no goroutines behind.
	for _, c := range []*wsClient{sm, a, b} {
		_ = c.c.CloseNow()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.rooms.Shutdown(ctx)
	h.srv.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > before+2 {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutine leak: %d before, %d after\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

func TestEnvelopePayloadRawMessage(t *testing.T) {
	// Send passes json.RawMessage payloads through untouched.
	raw := json.RawMessage(`{"a":1}`)
	b, err := Encode("X", 3, raw)
	require.NoError(t, err)
	require.JSONEq(t, `{"t":"X","seq":3,"p":{"a":1}}`, string(b))
	b, err = Encode("Y", 4, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"t":"Y","seq":4}`, string(b))
	_ = fmt.Sprint()
}
