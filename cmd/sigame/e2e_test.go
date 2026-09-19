package main

// End-to-end test of the assembled server: REST (pack import, room
// creation, joins, host endpoints) plus four real WebSocket clients playing
// two questions of testdata/siq/SIGameTestEn.siq through the buzzer, a
// reconnect and the host closing the room. The clients behave like the
// browser client described in docs/protocol.md §6: SYNC burst, ARM_ACK,
// stamped presses on their own clocks.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"sigame/internal/buzzer"
	"sigame/internal/config"
	"sigame/internal/engine"
	"sigame/internal/httpapi"
	"sigame/internal/packs"
	"sigame/internal/room"
	"sigame/internal/ws"
)

const (
	e2eWait     = 5 * time.Second
	e2ePackPath = "../../testdata/siq/SIGameTestEn.siq"
	e2eRecent   = 10 // message types remembered per client for timeout diagnostics
)

// ---------------------------------------------------------------------------
// server harness
// ---------------------------------------------------------------------------

// newE2EConfig builds the server configuration through the real loader:
// a fresh data directory, no AI key (fuzzy judge only), quiet logs.
func newE2EConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("SIGAME_CONFIG", "")
	t.Setenv("SIGAME_DATA_DIR", t.TempDir())
	t.Setenv("SIGAME_ADDR", "127.0.0.1:0")
	t.Setenv("SIGAME_LOG_LEVEL", "warn")
	t.Setenv("SIGAME_LOG_FORMAT", "text")
	t.Setenv("OPENROUTER_API_KEY", "")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.False(t, cfg.AIConfigured(), "the e2e game must run without an AI judge")
	return cfg
}

// newE2EApp wires the server exactly like run() does (buildApp) and
// registers its shutdown.
func newE2EApp(t *testing.T) *app {
	t.Helper()
	cfg := newE2EConfig(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	a, err := buildApp(context.Background(), cfg, logger)
	require.NoError(t, err)
	t.Cleanup(a.Close)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), e2eWait)
		defer cancel()
		a.Rooms.Shutdown(ctx)
	})
	return a
}

type e2eServer struct {
	t   *testing.T
	app *app
	srv *httptest.Server
	hc  *http.Client
}

func newE2EServer(t *testing.T) *e2eServer {
	t.Helper()
	a := newE2EApp(t)
	srv := httptest.NewUnstartedServer(a.Handler)
	srv.Config.ConnContext = ws.ConnContext // kernel RTT anchor for the buzzer
	srv.Start()
	t.Cleanup(srv.Close)
	return &e2eServer{t: t, app: a, srv: srv, hc: srv.Client()}
}

// rest performs a JSON request against /api/v1 and decodes the body into
// out (when non-nil). hostToken is sent as X-Host-Token when non-empty.
func (s *e2eServer) rest(method, path string, body any, hostToken string, wantStatus int, out any) {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(s.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.srv.URL+"/api/v1"+path, rd)
	require.NoError(s.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if hostToken != "" {
		req.Header.Set("X-Host-Token", hostToken)
	}
	resp, err := s.hc.Do(req)
	require.NoError(s.t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(s.t, err)
	require.Equalf(s.t, wantStatus, resp.StatusCode, "%s %s → %s", method, path, raw)
	if out != nil {
		require.NoError(s.t, json.Unmarshal(raw, out), "%s %s → %s", method, path, raw)
	}
}

// importPack uploads a .siq through POST /packs/import and returns the pack id.
func (s *e2eServer) importPack(path string) string {
	s.t.Helper()
	f, err := os.Open(path)
	require.NoError(s.t, err)
	defer f.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filepath.Base(path))
	require.NoError(s.t, err)
	_, err = io.Copy(part, f)
	require.NoError(s.t, err)
	require.NoError(s.t, mw.Close())

	req, err := http.NewRequest(http.MethodPost, s.srv.URL+"/api/v1/packs/import", &buf)
	require.NoError(s.t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := s.hc.Do(req)
	require.NoError(s.t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(s.t, err)
	require.Equalf(s.t, http.StatusCreated, resp.StatusCode, "import: %s", raw)
	var res httpapi.ImportResult
	require.NoError(s.t, json.Unmarshal(raw, &res), "%s", raw)
	require.NotNil(s.t, res.Pack)
	require.NotEmpty(s.t, res.Pack.ID)
	require.NotEmpty(s.t, res.Pack.Rounds)
	return res.Pack.ID
}

func (s *e2eServer) join(code, name string, role engine.Role, hostToken string) httpapi.JoinResponse {
	s.t.Helper()
	var out httpapi.JoinResponse
	s.rest(http.MethodPost, "/rooms/"+code+"/join", httpapi.JoinRequest{Name: name, Role: role}, hostToken, http.StatusOK, &out)
	require.Equal(s.t, code, out.RoomCode)
	require.Equal(s.t, role, out.Role)
	require.NotEmpty(s.t, out.PersonID)
	require.NotEmpty(s.t, out.SessionToken)
	require.True(s.t, strings.HasPrefix(out.WsURL, "/ws?room="+code+"&token="), out.WsURL)
	return out
}

// ---------------------------------------------------------------------------
// WebSocket client
// ---------------------------------------------------------------------------

// envelope is a received frame stamped with the client clock at read time
// (the c4 of the SYNC schedule and recvLocal of ARM_ACK).
type envelope struct {
	ws.Envelope
	at float64
}

// client is one WebSocket participant. A reader goroutine feeds ch; the
// test goroutine consumes it with waitFor/collect. Its "performance.now()"
// is time.Since(base) shifted by an arbitrary offset so that every client
// has its own clock origin, as browsers do.
type client struct {
	t       *testing.T
	name    string
	id      string
	sess    httpapi.JoinResponse
	conn    *websocket.Conn
	ch      chan envelope
	readErr error // set by the reader before ch is closed

	seq              int64
	base             time.Time
	offset           float64
	plan             room.SyncPlan
	syncSeq, prevSeq int64
	prevC4           float64

	mu     sync.Mutex
	recent []string
}

func (s *e2eServer) dial(sess httpapi.JoinResponse, name string, clockOffset float64) *client {
	s.t.Helper()
	url := "ws" + strings.TrimPrefix(s.srv.URL, "http") + sess.WsURL
	ctx, cancel := context.WithTimeout(context.Background(), e2eWait)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(s.t, err)
	conn.SetReadLimit(1 << 20)
	c := &client{t: s.t, name: name, id: sess.PersonID, sess: sess, conn: conn,
		ch: make(chan envelope, 4096), base: time.Now(), offset: clockOffset}
	s.t.Cleanup(func() { _ = conn.CloseNow() })
	go c.readLoop()
	return c
}

func (c *client) readLoop() {
	defer close(c.ch)
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.readErr = err
			return
		}
		at := c.local()
		e, err := ws.Decode(data, 1<<20)
		if err != nil {
			c.readErr = err
			return
		}
		c.mu.Lock()
		c.recent = append(c.recent, e.T)
		if len(c.recent) > e2eRecent {
			c.recent = c.recent[len(c.recent)-e2eRecent:]
		}
		c.mu.Unlock()
		c.ch <- envelope{Envelope: e, at: at}
	}
}

func (c *client) recentTypes() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.recent, ",")
}

// local is the client's monotonic clock in ms (performance.now()).
func (c *client) local() float64 {
	return float64(time.Since(c.base).Nanoseconds())/1e6 + c.offset
}

func (c *client) write(typ string, payload any) error {
	c.seq++
	b, err := ws.Encode(typ, c.seq, payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eWait)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, b)
}

func (c *client) send(typ string, payload any) {
	c.t.Helper()
	require.NoError(c.t, c.write(typ, payload), "%s: send %s", c.name, typ)
}

// recv returns the next envelope of the given type, discarding the others.
func (c *client) recv(typ string, timeout time.Duration) (envelope, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-c.ch:
			if !ok {
				return envelope{}, fmt.Errorf("%s: socket closed (%v) while waiting for %s; recent: [%s]", c.name, c.readErr, typ, c.recentTypes())
			}
			if e.T == typ {
				return e, nil
			}
		case <-timer.C:
			return envelope{}, fmt.Errorf("%s: timeout waiting for %s; recent: [%s]", c.name, typ, c.recentTypes())
		}
	}
}

// waitFor is recv with the e2e timeout; a miss fails the test with the last
// received message types.
func (c *client) waitFor(typ string) json.RawMessage {
	c.t.Helper()
	e, err := c.recv(typ, e2eWait)
	require.NoError(c.t, err)
	return e.P
}

// collect reads everything up to and including the first envelope of the
// given type. Use it where the relative order of the intermediate messages
// is not part of the contract.
func (c *client) collect(until string) []envelope {
	c.t.Helper()
	timer := time.NewTimer(e2eWait)
	defer timer.Stop()
	var out []envelope
	for {
		select {
		case e, ok := <-c.ch:
			if !ok {
				c.t.Fatalf("%s: socket closed (%v) while collecting up to %s; recent: [%s]", c.name, c.readErr, until, c.recentTypes())
			}
			out = append(out, e)
			if e.T == until {
				return out
			}
		case <-timer.C:
			c.t.Fatalf("%s: timeout collecting up to %s; recent: [%s]", c.name, until, c.recentTypes())
		}
	}
}

// waitClosed drains the socket until the server closes it and returns the
// close status.
func (c *client) waitClosed() websocket.StatusCode {
	c.t.Helper()
	timer := time.NewTimer(e2eWait)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-c.ch:
			if !ok {
				return websocket.CloseStatus(c.readErr)
			}
		case <-timer.C:
			c.t.Fatalf("%s: socket not closed by the server; recent: [%s]", c.name, c.recentTypes())
		}
	}
}

func find(envs []envelope, typ string) (json.RawMessage, bool) {
	for _, e := range envs {
		if e.T == typ {
			return e.P, true
		}
	}
	return nil, false
}

func findAll(envs []envelope, typ string) []json.RawMessage {
	var out []json.RawMessage
	for _, e := range envs {
		if e.T == typ {
			out = append(out, e.P)
		}
	}
	return out
}

func mustFind(t *testing.T, envs []envelope, typ string) json.RawMessage {
	t.Helper()
	raw, ok := find(envs, typ)
	if !ok {
		types := make([]string, 0, len(envs))
		for _, e := range envs {
			types = append(types, e.T)
		}
		t.Fatalf("%s not among the collected messages [%s]", typ, strings.Join(types, ","))
	}
	return raw
}

func decodeAs[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v), "payload %s", raw)
	return v
}

// hello consumes WELCOME + SNAPSHOT and checks the identity the server sees.
func (c *client) hello(code string, role engine.Role, isHost bool) room.SnapshotPayload {
	c.t.Helper()
	w := decodeAs[room.WelcomePayload](c.t, c.waitFor(room.MsgWelcome))
	require.Equal(c.t, c.id, w.PersonID)
	require.Equal(c.t, c.name, w.Name)
	require.Equal(c.t, role, w.Role)
	require.Equal(c.t, isHost, w.IsHost)
	require.Equal(c.t, code, w.RoomCode)
	require.Equal(c.t, room.ShowmanHuman, w.Showman)
	require.Greater(c.t, w.SyncPlan.Burst, 0)
	require.Greater(c.t, w.SyncPlan.IntervalMs, int64(0))
	require.Equal(c.t, buzzer.ModeAnchoredHybrid, w.BuzzerSettings.Mode)
	c.plan = w.SyncPlan
	snap := decodeAs[room.SnapshotPayload](c.t, c.waitFor(room.MsgSnapshot))
	require.Equal(c.t, code, snap.Room.Code)
	return snap
}

// syncBurst runs the connect-time SYNC schedule of docs/protocol.md §6.1:
// plan.Burst samples plan.IntervalMs apart, c4 of ACK n riding in SYNC n+1.
// It returns errors instead of failing so that bursts can run concurrently.
func (c *client) syncBurst() (room.SyncAckPayload, error) {
	var last room.SyncAckPayload
	for i := 0; i < c.plan.Burst; i++ {
		if i > 0 {
			time.Sleep(time.Duration(c.plan.IntervalMs) * time.Millisecond)
		}
		c.syncSeq++
		if err := c.write(room.InSync, room.SyncIn{Seq: c.syncSeq, C1: c.local(), PrevSeq: c.prevSeq, PrevC4: c.prevC4}); err != nil {
			return last, err
		}
		e, err := c.recv(room.MsgSyncAck, e2eWait)
		if err != nil {
			return last, err
		}
		if err := json.Unmarshal(e.P, &last); err != nil {
			return last, fmt.Errorf("%s: SYNC_ACK payload: %w", c.name, err)
		}
		if last.Seq != c.syncSeq {
			return last, fmt.Errorf("%s: SYNC_ACK seq %d, want %d", c.name, last.Seq, c.syncSeq)
		}
		c.prevSeq, c.prevC4 = last.Seq, e.at
	}
	return last, nil
}

// waitArm receives BUTTON_ARM and acknowledges it at once, like a browser
// client does before scheduling the light.
func (c *client) waitArm() buzzer.ArmMsg {
	c.t.Helper()
	e, err := c.recv(room.MsgButtonArm, e2eWait)
	require.NoError(c.t, err)
	return c.ackArm(e)
}

func (c *client) ackArm(e envelope) buzzer.ArmMsg {
	c.t.Helper()
	require.Equal(c.t, room.MsgButtonArm, e.T)
	arm := decodeAs[buzzer.ArmMsg](c.t, e.P)
	require.NotEmpty(c.t, arm.ArmID)
	c.send(room.InArmAck, room.ArmAckIn{ArmID: arm.ArmID, RecvLocal: e.at})
	return arm
}

// press emulates an honest player: the light goes on at armAtLocal on the
// client's own clock (or on receipt when that instant has passed) and the
// press is stamped reactionMs later.
func (c *client) press(arm buzzer.ArmMsg, reactionMs float64) {
	c.t.Helper()
	lit := arm.ArmAtLocal
	if arm.Mode != "scheduled" || lit < c.local() {
		lit = c.local()
	}
	pressLocal := lit + reactionMs
	if d := pressLocal - c.local(); d > 0 {
		time.Sleep(time.Duration(d * float64(time.Millisecond)))
	}
	c.send(room.InPress, room.PressIn{ArmID: arm.ArmID, Seq: 1, PressLocal: pressLocal, LitLocal: lit, Src: "pointer"})
}

func scoreMap(entries []engine.ScoreEntry) map[string]int {
	out := make(map[string]int, len(entries))
	for _, e := range entries {
		out[e.PersonID] = e.Score
	}
	return out
}

func personConnected(persons []room.PersonView, id string) (bool, bool) {
	for _, p := range persons {
		if p.ID == id {
			return p.Connected, true
		}
	}
	return false, false
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestPrintOpenAPI(t *testing.T) {
	a := newE2EApp(t)
	b, err := json.Marshal(a.API.OpenAPI())
	require.NoError(t, err)
	spec := string(b)
	require.Contains(t, spec, `"listRooms"`)
	require.Contains(t, spec, `"importSiq"`)
	require.Contains(t, spec, `"joinRoom"`)
}

func TestE2EGame(t *testing.T) {
	started := time.Now()
	s := newE2EServer(t)

	// --- REST: pack, room, joins --------------------------------------------
	packID := s.importPack(e2ePackPath)

	// SIGame defaults except a faster reading speed and a shorter reflection
	// pause so that the test stays well under a minute; the rules/times
	// objects must be complete (huma rejects partial ones).
	rules := engine.DefaultRules()
	rules.ReadingSpeed = 100
	times := engine.DefaultTimeSettings()
	times.ReflectionMs = 500
	var created httpapi.CreateRoomResponse
	s.rest(http.MethodPost, "/rooms", httpapi.CreateRoomRequest{
		PackID: packID, Name: "e2e", Showman: room.ShowmanHuman, BuzzerPreset: buzzer.PresetLANWired,
		Rules: &rules, Times: &times,
	}, "", http.StatusCreated, &created)
	code := created.Room.Code
	hostToken := created.HostToken
	require.Len(t, code, 5)
	require.NotEmpty(t, hostToken)
	require.Equal(t, room.StatusLobby, created.Room.Status)
	require.Equal(t, buzzer.NetLAN, created.Room.Buzzer.NetProfile)

	smSess := s.join(code, "Ведущий", engine.RoleShowman, hostToken)
	require.True(t, smSess.IsHost)
	anyaSess := s.join(code, "Аня", engine.RolePlayer, "")
	borisSess := s.join(code, "Борис", engine.RolePlayer, "")
	veraSess := s.join(code, "Вера", engine.RolePlayer, "")
	require.False(t, anyaSess.IsHost)

	// --- WebSocket: connect, WELCOME + SNAPSHOT ----------------------------
	sm := s.dial(smSess, "Ведущий", 5_000)
	anya := s.dial(anyaSess, "Аня", 120_000)
	boris := s.dial(borisSess, "Борис", 987_654)
	vera := s.dial(veraSess, "Вера", 42_000)
	players := []*client{anya, boris, vera}
	everyone := []*client{sm, anya, boris, vera}

	snap := sm.hello(code, engine.RoleShowman, true)
	require.Nil(t, snap.Game, "lobby snapshot has no game")
	require.Equal(t, int64(0), snap.LastSeq)
	for _, p := range players {
		snap := p.hello(code, engine.RolePlayer, false)
		require.Nil(t, snap.Game)
		require.Equal(t, "idle", snap.Buzzer.State)
	}

	// --- SYNC bursts (concurrently, as real clients would) -----------------
	acks := make([]room.SyncAckPayload, len(players))
	errs := make([]error, len(players))
	var wg sync.WaitGroup
	for i, p := range players {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acks[i], errs[i] = p.syncBurst()
		}()
	}
	wg.Wait()
	for i, p := range players {
		require.NoError(t, errs[i])
		m := acks[i].Model
		require.GreaterOrEqual(t, m.Samples, 5, "%s: samples", p.name)
		require.Contains(t, []buzzer.Quality{buzzer.QualityGood, buzzer.QualityFair}, m.Quality, "%s: model %+v", p.name, m)
		// serverTime = performance.now() + offsetMs (protocol.md §4): the send
		// stamp c1 mapped through the model lands within a half round trip of
		// the server receive stamp s2.
		require.InDelta(t, acks[i].S2, acks[i].C1+m.OffsetMs, 50, "%s: clock model offset", p.name)
	}

	// --- START: round-start chooser tie → showman picks -------------------
	sm.send(room.InStart, nil)
	startEnvs := sm.collect(string(engine.EvAskSelectPlayer))
	rs := decodeAs[engine.RoundStartPayload](t, mustFind(t, startEnvs, string(engine.EvRoundStart)))
	require.Equal(t, 0, rs.Index)
	require.Equal(t, []string{"Questions of Different Types", "Question Content", "Additional"}, rs.Themes)
	table := decodeAs[engine.TablePayload](t, mustFind(t, startEnvs, string(engine.EvTable)))
	require.Len(t, table.Themes, 3)
	require.Equal(t, 100, table.Themes[1].Questions[0].Price)
	require.Equal(t, 100, table.Themes[0].Questions[0].Price)
	ask := decodeAs[engine.AskSelectPlayerPayload](t, startEnvs[len(startEnvs)-1].P)
	require.Equal(t, "chooser", ask.Reason)
	require.Equal(t, sm.id, ask.DeciderID)
	require.Len(t, ask.Candidates, 3, "three players at 0 tie for the first choice")
	sm.send("SELECT_PLAYER", room.PersonIn{PersonID: ask.Candidates[0]})

	var chooser *client
	for _, p := range players {
		if p.id == ask.Candidates[0] {
			chooser = p
		}
	}
	require.NotNil(t, chooser)
	askChoose := decodeAs[engine.AskChoosePayload](t, chooser.waitFor(string(engine.EvAskChoose)))
	require.Equal(t, chooser.id, askChoose.PersonID)

	// --- Question 1: "Question Content" / 100, Аня beats Борис ------------
	const q1Right = "This is a regular answer"
	chooser.send("CHOOSE_QUESTION", room.ChooseIn{Theme: 1, Q: 0})
	for _, p := range everyone {
		qs := decodeAs[engine.QuestionStartPayload](t, p.waitFor(string(engine.EvQuestionStart)))
		require.Equal(t, 1, qs.ThemeIndex, p.name)
		require.Equal(t, 0, qs.QuestionIndex, p.name)
		require.Equal(t, "Question Content", qs.Theme, p.name)
		require.Equal(t, 100, qs.Price, p.name)
		require.Equal(t, packs.QSimple, qs.Type, p.name)
		content := decodeAs[engine.ContentPayload](t, p.waitFor(string(engine.EvContent)))
		require.Equal(t, "question", content.Phase, p.name)
		require.NotEmpty(t, content.Items, p.name)
		require.Equal(t, "This is regular text", content.Items[0].Text, p.name)
	}
	armA := anya.waitArm()
	armB := boris.waitArm()
	armV := vera.waitArm()
	require.Equal(t, armA.ArmID, armB.ArmID)
	require.Equal(t, armA.ArmID, armV.ArmID)
	require.Equal(t, "scheduled", armA.Mode, "a synced LAN client is armed on schedule")
	require.Equal(t, "scheduled", armB.Mode)
	require.Greater(t, armA.DeadlineAt, armA.ArmAt)
	require.Equal(t, created.Room.Buzzer.FalseStartLockoutMs, armA.LockoutMs)

	anya.press(armA, 180)
	boris.press(armB, 260)

	ackA := decodeAs[room.PressAckPayload](t, anya.waitFor(room.MsgPressAck))
	require.Equal(t, buzzer.StatusPending, ackA.Status, "Аня: %s", ackA.Reason)
	require.InDelta(t, 180, ackA.ReactionMs, 100)
	resA := decodeAs[room.ButtonResultPublic](t, anya.waitFor(room.MsgButtonResult))
	require.Equal(t, armA.ArmID, resA.ArmID)
	require.Equal(t, buzzer.KindWinner, resA.Kind)
	require.Equal(t, anya.id, resA.WinnerID)
	require.Greater(t, resA.ReactionMs, 0.0, "the public result carries the receiver's own reaction")

	// Борис: pending (inside the fairness window), late (after it closed) or
	// stale/noArm (the engine already consumed the result and the room dropped
	// the arm); his BUTTON_RESULT may precede the PRESS_ACK of a late press.
	bEnvs := boris.collect(room.MsgButtonResult)
	rawAckB, ok := find(bEnvs, room.MsgPressAck)
	if !ok {
		rawAckB = boris.waitFor(room.MsgPressAck)
	}
	ackB := decodeAs[room.PressAckPayload](t, rawAckB)
	require.Contains(t, []string{buzzer.StatusPending, buzzer.StatusLate, buzzer.StatusStale}, ackB.Status, "Борис: %s", ackB.Reason)
	resB := decodeAs[room.ButtonResultPublic](t, bEnvs[len(bEnvs)-1].P)
	require.Equal(t, anya.id, resB.WinnerID)

	resSM := decodeAs[buzzer.Result](t, sm.waitFor(room.MsgButtonResult))
	require.Equal(t, anya.id, resSM.WinnerID)
	require.Equal(t, "single", resSM.Rule)
	require.NotEmpty(t, resSM.Ranking)
	require.Equal(t, anya.id, resSM.Ranking[0].PlayerID)
	require.NotEmpty(t, resSM.Seed)

	askAns := decodeAs[engine.AskAnswerPayload](t, anya.waitFor(string(engine.EvAskAnswer)))
	require.Equal(t, anya.id, askAns.PersonID)
	require.False(t, askAns.Oral)
	anya.send("ANSWER", room.AnswerIn{Text: q1Right})

	smEnvs := sm.collect(string(engine.EvAskValidate))
	_, hasAudit := find(smEnvs, room.MsgButtonAudit)
	require.True(t, hasAudit, "the host receives BUTTON_AUDIT after the arm resolves")
	askVal := decodeAs[engine.AskValidatePayload](t, smEnvs[len(smEnvs)-1].P)
	require.Equal(t, anya.id, askVal.PersonID)
	require.Equal(t, q1Right, askVal.Answer)
	require.Contains(t, askVal.Rights, q1Right)
	sm.send("VALIDATE", room.ValidateIn{PersonID: anya.id, Right: true})

	for _, p := range everyone {
		envs := p.collect(string(engine.EvQuestionEnd))
		val := decodeAs[engine.ValidationPayload](t, mustFind(t, envs, string(engine.EvValidation)))
		require.True(t, val.Right, p.name)
		require.Equal(t, anya.id, val.PersonID, p.name)
		require.Equal(t, "showman", val.Source, p.name)
		sc := decodeAs[engine.PersonScorePayload](t, mustFind(t, envs, string(engine.EvPersonScore)))
		require.Equal(t, anya.id, sc.PersonID, p.name)
		require.Equal(t, 100, sc.Delta, p.name)
		require.Equal(t, 100, sc.Score, p.name)
		ra := decodeAs[engine.RightAnswerPayload](t, mustFind(t, envs, string(engine.EvRightAnswer)))
		require.Equal(t, q1Right, ra.Text, p.name)
		sums := decodeAs[engine.SumsPayload](t, p.waitFor(string(engine.EvSums)))
		require.Equal(t, map[string]int{anya.id: 100, boris.id: 0, vera.id: 0}, scoreMap(sums.Scores), p.name)
	}

	// --- Question 2: "Questions of Different Types" / 100 ------------------
	// Борис wins the button, answers wrong (-100); the button re-arms for
	// Аня and Вера only; Вера answers right (+100).
	const q2Right = "This is the correct answer"
	askChoose = decodeAs[engine.AskChoosePayload](t, anya.waitFor(string(engine.EvAskChoose)))
	require.Equal(t, anya.id, askChoose.PersonID, "the right answerer chooses next")
	anya.send("CHOOSE_QUESTION", room.ChooseIn{Theme: 0, Q: 0})
	for _, p := range everyone {
		qs := decodeAs[engine.QuestionStartPayload](t, p.waitFor(string(engine.EvQuestionStart)))
		require.Equal(t, 0, qs.ThemeIndex, p.name)
		require.Equal(t, 0, qs.QuestionIndex, p.name)
		require.Equal(t, packs.QSimple, qs.Type, p.name)
		require.Equal(t, 100, qs.Price, p.name)
	}
	armA2 := anya.waitArm()
	armB2 := boris.waitArm()
	armV2 := vera.waitArm()
	require.Equal(t, armB2.ArmID, armA2.ArmID)
	require.Equal(t, armB2.ArmID, armV2.ArmID)
	require.NotEqual(t, armA.ArmID, armB2.ArmID)

	boris.press(armB2, 180)
	ackB2 := decodeAs[room.PressAckPayload](t, boris.waitFor(room.MsgPressAck))
	require.Equal(t, buzzer.StatusPending, ackB2.Status, "Борис: %s", ackB2.Reason)
	resB2 := decodeAs[room.ButtonResultPublic](t, boris.waitFor(room.MsgButtonResult))
	require.Equal(t, boris.id, resB2.WinnerID)
	askAnsB := decodeAs[engine.AskAnswerPayload](t, boris.waitFor(string(engine.EvAskAnswer)))
	require.Equal(t, boris.id, askAnsB.PersonID)
	boris.send("ANSWER", room.AnswerIn{Text: "nonsense"})

	askValB := decodeAs[engine.AskValidatePayload](t, sm.waitFor(string(engine.EvAskValidate)))
	require.Equal(t, boris.id, askValB.PersonID)
	require.Equal(t, "nonsense", askValB.Answer)
	require.Contains(t, askValB.Rights, q2Right)
	sm.send("VALIDATE", room.ValidateIn{PersonID: boris.id, Right: false})

	// Аня and Вера see the wrong verdict, then get a fresh arm.
	var armA3, armV3 buzzer.ArmMsg
	for _, p := range []*client{anya, vera} {
		envs := p.collect(room.MsgButtonArm)
		val := decodeAs[engine.ValidationPayload](t, mustFind(t, envs, string(engine.EvValidation)))
		require.False(t, val.Right, p.name)
		require.Equal(t, boris.id, val.PersonID, p.name)
		sc := decodeAs[engine.PersonScorePayload](t, mustFind(t, envs, string(engine.EvPersonScore)))
		require.Equal(t, boris.id, sc.PersonID, p.name)
		require.Equal(t, -100, sc.Delta, p.name)
		require.Equal(t, -100, sc.Score, p.name)
		arm := p.ackArm(envs[len(envs)-1])
		if p == anya {
			armA3 = arm
		} else {
			armV3 = arm
		}
	}
	require.Equal(t, armA3.ArmID, armV3.ArmID)
	require.NotEqual(t, armB2.ArmID, armV3.ArmID, "a re-arm is a new arm")

	vera.press(armV3, 200)
	ackV := decodeAs[room.PressAckPayload](t, vera.waitFor(room.MsgPressAck))
	require.Equal(t, buzzer.StatusPending, ackV.Status, "Вера: %s", ackV.Reason)
	resV := decodeAs[room.ButtonResultPublic](t, vera.waitFor(room.MsgButtonResult))
	require.Equal(t, vera.id, resV.WinnerID)
	askAnsV := decodeAs[engine.AskAnswerPayload](t, vera.waitFor(string(engine.EvAskAnswer)))
	require.Equal(t, vera.id, askAnsV.PersonID)
	vera.send("ANSWER", room.AnswerIn{Text: q2Right})

	askValV := decodeAs[engine.AskValidatePayload](t, sm.waitFor(string(engine.EvAskValidate)))
	require.Equal(t, vera.id, askValV.PersonID)
	require.Equal(t, q2Right, askValV.Answer)
	sm.send("VALIDATE", room.ValidateIn{PersonID: vera.id, Right: true})

	for _, p := range everyone {
		envs := p.collect(string(engine.EvQuestionEnd))
		vals := findAll(envs, string(engine.EvValidation))
		require.NotEmpty(t, vals, p.name)
		last := decodeAs[engine.ValidationPayload](t, vals[len(vals)-1])
		require.True(t, last.Right, p.name)
		require.Equal(t, vera.id, last.PersonID, p.name)
		scores := findAll(envs, string(engine.EvPersonScore))
		require.NotEmpty(t, scores, p.name)
		sc := decodeAs[engine.PersonScorePayload](t, scores[len(scores)-1])
		require.Equal(t, vera.id, sc.PersonID, p.name)
		require.Equal(t, 100, sc.Delta, p.name)
		require.Equal(t, 100, sc.Score, p.name)
		if p == boris {
			// He answered wrong: no second chance on this question.
			_, rearmed := find(envs, room.MsgButtonArm)
			require.False(t, rearmed, "Борис must not be re-armed after a wrong answer")
			require.Len(t, vals, 2, "Борис sees his own verdict and Вера's")
			first := decodeAs[engine.ValidationPayload](t, vals[0])
			require.False(t, first.Right)
			require.Equal(t, boris.id, first.PersonID)
		}
		sums := decodeAs[engine.SumsPayload](t, p.waitFor(string(engine.EvSums)))
		require.Equal(t, map[string]int{anya.id: 100, boris.id: -100, vera.id: 100}, scoreMap(sums.Scores), p.name)
	}

	// --- Resume: Вера drops, re-joins by name, reconnects ------------------
	require.NoError(t, vera.conn.Close(websocket.StatusNormalClosure, "network drop"))
	for {
		persons := decodeAs[room.RoomPersonsPayload](t, sm.waitFor(room.MsgRoomPersons))
		connected, found := personConnected(persons.Persons, vera.id)
		require.True(t, found, "Вера keeps her seat while disconnected")
		if !connected {
			break
		}
	}
	veraSess2 := s.join(code, "Вера", engine.RolePlayer, "")
	require.Equal(t, vera.id, veraSess2.PersonID, "re-joining by name resumes the same person")
	require.NotEqual(t, veraSess.SessionToken, veraSess2.SessionToken)
	vera2 := s.dial(veraSess2, "Вера", 300_000)
	snap2 := vera2.hello(code, engine.RolePlayer, false)
	require.NotNil(t, snap2.Game, "mid-game snapshot carries the game")
	require.Equal(t, vera.id, snap2.Game.PersonID)
	require.Equal(t, engine.RolePlayer, snap2.Game.Role)
	require.Equal(t, 100, scoreMap(snap2.Game.Scores)[vera.id], "score preserved across the reconnect")
	require.Equal(t, room.StatusPlaying, snap2.Room.Status)
	viewConnected, _ := personConnected(snap2.Room.Players, vera.id)
	require.True(t, viewConnected)
	for _, pv := range snap2.Room.Players {
		if pv.ID == vera.id {
			require.Equal(t, 100, pv.Score)
		}
	}
	require.Greater(t, snap2.LastSeq, int64(0), "lastSeq of the previous connection")
	require.Equal(t, "idle", snap2.Buzzer.State)
	vera2.send(room.InHello, room.HelloIn{LastSeq: snap2.LastSeq})
	resume := decodeAs[room.ResumePayload](t, vera2.waitFor(room.MsgResume))
	require.True(t, resume.Covered)
	require.Equal(t, 0, resume.Count, "nothing to replay when lastSeq is current")
	everyone = []*client{sm, anya, boris, vera2}

	// --- Host REST during the game ----------------------------------------
	var view httpapi.RoomView
	s.rest(http.MethodGet, "/rooms/"+code, nil, "", http.StatusOK, &view)
	require.Equal(t, room.StatusPlaying, view.Status)
	require.Len(t, view.Players, 4)

	s.rest(http.MethodGet, "/rooms/"+code+"/buzz-log", nil, "", http.StatusUnauthorized, nil)
	var buzzLog httpapi.BuzzLog
	s.rest(http.MethodGet, "/rooms/"+code+"/buzz-log", nil, hostToken, http.StatusOK, &buzzLog)
	require.GreaterOrEqual(t, len(buzzLog.Arms), 2, "two questions, one re-arm")
	first := buzzLog.Arms[0]
	require.Equal(t, armA.ArmID, first.ArmID)
	require.Equal(t, anya.id, first.Result.WinnerID)
	require.NotEmpty(t, first.Result.Ranking)
	require.Equal(t, anya.id, first.Result.Ranking[0].PlayerID)
	require.NotEmpty(t, first.Audit)
	require.Greater(t, first.ResolvedAt, int64(0))
	for _, arm := range buzzLog.Arms {
		require.NotEmpty(t, arm.QuestionID)
		require.Equal(t, buzzer.KindWinner, arm.Result.Kind)
		require.NotEmpty(t, arm.Result.Ranking, arm.ArmID)
	}

	// --- Pause, then the host closes the room -------------------------------
	sm.send("PAUSE", room.PauseIn{On: true})
	for _, p := range everyone {
		ps := decodeAs[engine.PausePayload](t, p.waitFor(string(engine.EvPause)))
		require.True(t, ps.On, p.name)
	}

	s.rest(http.MethodDelete, "/rooms/"+code, nil, hostToken, http.StatusNoContent, nil)
	for _, p := range everyone {
		closed := decodeAs[room.RoomClosedPayload](t, p.waitFor(room.MsgRoomClosed))
		require.Equal(t, "host", closed.Reason, p.name)
		require.Equal(t, websocket.StatusNormalClosure, p.waitClosed(), p.name)
	}
	require.Eventually(t, func() bool {
		resp, err := s.hc.Get(s.srv.URL + "/api/v1/rooms/" + code)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusNotFound
	}, e2eWait, 50*time.Millisecond, "the closed room disappears")

	t.Logf("e2e game flow: %s", time.Since(started).Round(time.Millisecond))
}
