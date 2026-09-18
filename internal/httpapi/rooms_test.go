package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sigame/internal/ai"
	"sigame/internal/buzzer"
	"sigame/internal/clock"
	"sigame/internal/engine"
	"sigame/internal/packs"
	"sigame/internal/room"
)

// newRoomEnv builds a test server with a room manager on the env's database.
func newRoomEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	e := newTestEnv(t, opts...)
	mgr := room.NewManager(room.ManagerDeps{
		DB:     e.db,
		Packs:  e.repo,
		Media:  e.store,
		Clock:  clock.NewReal(),
		Logger: slog.New(slog.DiscardHandler),
		Cfg:    room.RoomConfig{MaxRooms: 5, MaxPlayers: 12, RoomTTL: time.Hour, SweepEvery: time.Hour},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mgr.Shutdown(ctx)
	})
	e.srv.deps.Rooms = mgr
	return e
}

// createRoom posts body and returns the created room.
func (e *testEnv) createRoom(t *testing.T, body map[string]any) CreateRoomResponse {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/api/v1/rooms", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out CreateRoomResponse
	decodeJSON(t, rec, &out)
	return out
}

// join posts a join request and returns the recorder.
func (e *testEnv) join(t *testing.T, code string, body map[string]any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/v1/rooms/"+code+"/join", body, headers...)
}

// mustJoin joins and requires success.
func (e *testEnv) mustJoin(t *testing.T, code string, body map[string]any, headers ...string) JoinResponse {
	t.Helper()
	rec := e.join(t, code, body, headers...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out JoinResponse
	decodeJSON(t, rec, &out)
	return out
}

// getRoom fetches the room view.
func (e *testEnv) getRoom(t *testing.T, code string) RoomView {
	t.Helper()
	rec := e.do(t, http.MethodGet, "/api/v1/rooms/"+code, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out RoomView
	decodeJSON(t, rec, &out)
	return out
}

// fakeRoomConn is a room.Conn that swallows every envelope.
type fakeRoomConn struct {
	mu   sync.Mutex
	seq  int64
	sent []string
}

func (c *fakeRoomConn) Send(t string, _ json.RawMessage) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.sent = append(c.sent, t)
	return c.seq, nil
}

func (c *fakeRoomConn) Close(int, string) {}

func (c *fakeRoomConn) Info() room.ConnInfo { return room.ConnInfo{RemoteAddr: "test"} }

func findPerson(t *testing.T, v RoomView, id string) room.PersonView {
	t.Helper()
	for _, p := range v.Players {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("person %s not in room %s", id, v.Code)
	return room.PersonView{}
}

func TestRoomsCreateGetList(t *testing.T) {
	e := newRoomEnv(t)
	pack := e.createPack(t, samplePack("Quiz night"))

	rec := e.do(t, http.MethodPost, "/api/v1/rooms", map[string]any{"packId": pack.ID})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created CreateRoomResponse
	decodeJSON(t, rec, &created)
	code := created.Room.Code
	require.Len(t, code, 5)
	require.Equal(t, "/api/v1/rooms/"+code, rec.Header().Get("Location"))
	require.NotEmpty(t, created.HostToken)
	require.True(t, strings.HasSuffix(created.JoinURL, "/?room="+code), created.JoinURL)

	r := created.Room
	require.NotEmpty(t, r.ID)
	require.Equal(t, "Quiz night", r.Name)
	require.Equal(t, pack.ID, r.PackID)
	require.Equal(t, "Quiz night", r.PackName)
	require.Equal(t, room.StatusLobby, r.Status)
	require.Equal(t, room.ShowmanHuman, r.Showman)
	require.False(t, r.HasPassword)
	require.Empty(t, r.Players)
	require.Equal(t, 12, r.MaxPlayers)
	require.True(t, r.AllowViewers)
	require.EqualValues(t, 8000, r.HybridConfirmMs)
	require.Equal(t, room.JoinAny, r.JoinMode)
	require.Equal(t, engine.DefaultRules(), r.Rules)
	require.Equal(t, engine.DefaultTimeSettings(), r.Times)
	require.Equal(t, buzzer.DefaultSettings(), r.Buzzer)
	require.Equal(t, buzzer.NetWiFi, r.Buzzer.NetProfile)
	require.NotZero(t, r.CreatedAt)

	// Get, also with a lower-case code.
	got := e.getRoom(t, code)
	require.Equal(t, r.ID, got.ID)
	got = e.getRoom(t, strings.ToLower(code))
	require.Equal(t, code, got.Code)

	// List.
	rec = e.do(t, http.MethodGet, "/api/v1/rooms", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list RoomList
	decodeJSON(t, rec, &list)
	require.Len(t, list.Items, 1)
	require.Equal(t, code, list.Items[0].Code)

	// Unknown code.
	rec = e.do(t, http.MethodGet, "/api/v1/rooms/ZZZZZ", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusNotFound, decodeProblem(t, rec).Status)

	// Explicit options are reflected.
	rules := engine.DefaultRules()
	rules.Mode = engine.ModeSimple
	rules.Oral = true
	times := engine.DefaultTimeSettings()
	times.AnsweringMs = 10_000
	lan, _ := buzzer.Preset(buzzer.PresetLANWired)
	custom := e.createRoom(t, map[string]any{
		"packId": pack.ID, "name": "  Finals  ", "password": "pw", "buzzerPreset": "lanWired",
		"rules": rules, "times": times, "maxPlayers": 3, "allowViewers": false, "hybridConfirmMs": 5000, "language": "ru",
	})
	require.Equal(t, "Finals", custom.Room.Name)
	require.True(t, custom.Room.HasPassword)
	require.Equal(t, lan, custom.Room.Buzzer)
	require.Equal(t, rules, custom.Room.Rules)
	require.Equal(t, times, custom.Room.Times)
	require.Equal(t, 3, custom.Room.MaxPlayers)
	require.False(t, custom.Room.AllowViewers)
	require.EqualValues(t, 5000, custom.Room.HybridConfirmMs)
	require.Equal(t, "ru", custom.Room.Language)

	// Explicit settings override the preset entirely.
	explicit := buzzer.DefaultSettings()
	explicit.Mode = buzzer.ModeServerArrival
	explicit.ShowPing = false
	withBuzzer := e.createRoom(t, map[string]any{"packId": pack.ID, "buzzerPreset": "tournament", "buzzer": explicit})
	require.Equal(t, explicit, withBuzzer.Room.Buzzer)

	// Newest first.
	rec = e.do(t, http.MethodGet, "/api/v1/rooms", nil)
	decodeJSON(t, rec, &list)
	require.Len(t, list.Items, 3)
	require.Equal(t, withBuzzer.Room.Code, list.Items[0].Code)
}

func TestRoomsCreateValidation(t *testing.T) {
	e := newRoomEnv(t)
	pack := e.createPack(t, samplePack("Quiz"))
	empty := e.createPack(t, packs.Pack{Name: "Empty"})
	bad := buzzer.DefaultSettings()
	bad.MinHumanMs = 10

	cases := []struct {
		name     string
		body     any
		location string
		value    any
	}{
		{"missing packId", map[string]any{}, "body", nil},
		{"unknown pack", map[string]any{"packId": "0193a6f0-0000-7000-8000-000000000000"}, "body.packId", "packNotFound"},
		{"pack without rounds", map[string]any{"packId": empty.ID}, "body", "invalidParams"},
		{"ai showman without judge", map[string]any{"packId": pack.ID, "showman": "ai"}, "body.showman", "aiNotConfigured"},
		{"hybrid showman without judge", map[string]any{"packId": pack.ID, "showman": "hybrid"}, "body.showman", "aiNotConfigured"},
		{"unknown showman", map[string]any{"packId": pack.ID, "showman": "robot"}, "body.showman", nil},
		{"unknown preset", map[string]any{"packId": pack.ID, "buzzerPreset": "turbo"}, "body.buzzerPreset", nil},
		{"unsupported preset", map[string]any{"packId": pack.ID, "buzzerPreset": "noRace"}, "body.buzzerPreset", "unsupportedBuzzerMode"},
		{"invalid buzzer", map[string]any{"packId": pack.ID, "buzzer": bad}, "body.buzzer", "invalidBuzzerSettings"},
		{"partial buzzer", map[string]any{"packId": pack.ID, "buzzer": map[string]any{"mode": "anchoredHybrid"}}, "body.buzzer", nil},
		{"partial rules", map[string]any{"packId": pack.ID, "rules": map[string]any{"mode": "simple"}}, "body.rules", nil},
		{"too many players", map[string]any{"packId": pack.ID, "maxPlayers": 13}, "body.maxPlayers", nil},
		{"name too long", map[string]any{"packId": pack.ID, "name": strings.Repeat("x", 81)}, "body.name", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.do(t, http.MethodPost, "/api/v1/rooms", tc.body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			p := decodeProblem(t, rec)
			require.NotEmpty(t, p.Errors, rec.Body.String())
			locations := map[string]any{}
			for _, d := range p.Errors {
				locations[d.Location] = d.Value
			}
			require.Contains(t, locations, tc.location, rec.Body.String())
			if tc.value != nil {
				require.Equal(t, tc.value, locations[tc.location])
			}
		})
	}

	// The whole room limit (MaxRooms 5) yields 503.
	for i := 0; i < 5; i++ {
		e.createRoom(t, map[string]any{"packId": pack.ID})
	}
	rec := e.do(t, http.MethodPost, "/api/v1/rooms", map[string]any{"packId": pack.ID})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

	// Without a manager every room operation answers 503.
	plain := newTestEnv(t)
	rec = plain.do(t, http.MethodPost, "/api/v1/rooms", map[string]any{"packId": pack.ID})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	rec = plain.do(t, http.MethodGet, "/api/v1/rooms", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

func TestRoomsCreateWithAIShowman(t *testing.T) {
	judge := ai.JudgeFunc(func(_ context.Context, _ ai.Request) (ai.Verdict, error) {
		return ai.Verdict{Right: true, Factor: 1, Source: ai.SourceAI}, nil
	})
	e := newRoomEnv(t, withDeps(func(d *Deps) { d.AI = judge; d.Cfg.OpenRouterAPIKey = "test-key" }))
	pack := e.createPack(t, samplePack("Quiz"))

	created := e.createRoom(t, map[string]any{"packId": pack.ID, "showman": "ai"})
	require.Equal(t, room.ShowmanAI, created.Room.Showman)
	require.Len(t, created.Room.Players, 1)
	require.Equal(t, room.AIShowmanID, created.Room.Players[0].ID)
	require.Equal(t, engine.RoleShowman, created.Room.Players[0].Role)
	require.True(t, created.Room.Players[0].Connected)

	// Nobody can take the showman seat of an AI room.
	rec := e.join(t, created.Room.Code, map[string]any{"name": "Sam", "role": "showman"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "badRole", decodeProblem(t, rec).Errors[0].Value)

	hybrid := e.createRoom(t, map[string]any{"packId": pack.ID, "showman": "hybrid"})
	require.Equal(t, room.ShowmanHybrid, hybrid.Room.Showman)
	require.Empty(t, hybrid.Room.Players)

	// A status provider reporting a configured key is enough as well.
	e2 := newRoomEnv(t, withDeps(func(d *Deps) { d.AIStatus = &fakeProvider{status: ai.StatusInfo{Configured: true}} }))
	pack2 := e2.createPack(t, samplePack("Quiz"))
	e2.createRoom(t, map[string]any{"packId": pack2.ID, "showman": "ai"})
	e3 := newRoomEnv(t, withDeps(func(d *Deps) { d.AIStatus = &fakeProvider{} }))
	pack3 := e3.createPack(t, samplePack("Quiz"))
	rec = e3.do(t, http.MethodPost, "/api/v1/rooms", map[string]any{"packId": pack3.ID, "showman": "ai"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestRoomsJoinAndLeave(t *testing.T) {
	e := newRoomEnv(t)
	pack := e.createPack(t, samplePack("Quiz"))
	created := e.createRoom(t, map[string]any{"packId": pack.ID, "password": "s3cret"})
	code := created.Room.Code

	// Wrong / missing password.
	rec := e.join(t, code, map[string]any{"name": "Alice"})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	p := decodeProblem(t, rec)
	require.Equal(t, "badPassword", p.Errors[0].Value)
	require.Equal(t, "body.password", p.Errors[0].Location)
	rec = e.join(t, code, map[string]any{"name": "Alice", "password": "nope"})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// Player.
	alice := e.mustJoin(t, code, map[string]any{"name": "Alice", "password": "s3cret"})
	require.NotEmpty(t, alice.SessionToken)
	require.NotEmpty(t, alice.PersonID)
	require.Equal(t, engine.RolePlayer, alice.Role)
	require.False(t, alice.IsHost)
	require.Equal(t, code, alice.RoomCode)
	require.Equal(t, "/ws?room="+code+"&token="+alice.SessionToken, alice.WsURL)

	// The host token bypasses the password and grants host rights once.
	host := e.mustJoin(t, code, map[string]any{"name": "Host", "role": "showman"}, "X-Host-Token", created.HostToken)
	require.True(t, host.IsHost)
	require.Equal(t, engine.RoleShowman, host.Role)
	second := e.mustJoin(t, code, map[string]any{"name": "Bob"}, "X-Host-Token", created.HostToken)
	require.False(t, second.IsHost)

	// Wrong host token.
	rec = e.join(t, code, map[string]any{"name": "Eve"}, "X-Host-Token", "not-the-token")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, "invalidHostToken", decodeProblem(t, rec).Errors[0].Value)

	// Duplicate name (case-insensitive): 409 for another role, and for the
	// same role while the person is connected; a disconnected person of the
	// same name and role is resumed instead (new token, same personId).
	rec = e.join(t, code, map[string]any{"name": "alice", "role": "viewer", "password": "s3cret"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, "nameTaken", decodeProblem(t, rec).Errors[0].Value)
	resumed := e.mustJoin(t, code, map[string]any{"name": "alice", "password": "s3cret"})
	require.Equal(t, alice.PersonID, resumed.PersonID)
	require.NotEqual(t, alice.SessionToken, resumed.SessionToken)
	alice = resumed
	conn := &fakeRoomConn{}
	att, err := e.srv.deps.Rooms.Attach(code, alice.SessionToken, conn)
	require.NoError(t, err)
	require.True(t, findPerson(t, e.getRoom(t, code), alice.PersonID).Connected)
	rec = e.join(t, code, map[string]any{"name": "Alice", "password": "s3cret"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, "nameTaken", decodeProblem(t, rec).Errors[0].Value)
	att.Detach()

	// Showman seat is taken → 422.
	rec = e.join(t, code, map[string]any{"name": "Sam", "role": "showman", "password": "s3cret"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "badRole", decodeProblem(t, rec).Errors[0].Value)

	// Schema validation: unknown role, empty name.
	rec = e.join(t, code, map[string]any{"name": "Dan", "role": "dancer", "password": "s3cret"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.join(t, code, map[string]any{"name": "", "password": "s3cret"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// Unknown room.
	rec = e.join(t, "ZZZZZ", map[string]any{"name": "Alice"})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// The room view lists everyone with role/connected/score/host.
	view := e.getRoom(t, code)
	require.Len(t, view.Players, 3)
	pa := findPerson(t, view, alice.PersonID)
	require.Equal(t, "Alice", pa.Name)
	require.Equal(t, engine.RolePlayer, pa.Role)
	require.False(t, pa.Connected)
	require.Equal(t, 0, pa.Score)
	require.False(t, pa.IsHost)
	require.True(t, findPerson(t, view, host.PersonID).IsHost)

	// Leave.
	rec = e.do(t, http.MethodPost, "/api/v1/rooms/"+code+"/leave", map[string]any{"sessionToken": alice.SessionToken})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Len(t, e.getRoom(t, code).Players, 2)
	rec = e.do(t, http.MethodPost, "/api/v1/rooms/"+code+"/leave", map[string]any{"sessionToken": alice.SessionToken})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, "badSession", decodeProblem(t, rec).Errors[0].Value)
	rec = e.do(t, http.MethodPost, "/api/v1/rooms/"+code+"/leave", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, "/api/v1/rooms/ZZZZZ/leave", map[string]any{"sessionToken": "x"})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// Alice can come back under the same name.
	e.mustJoin(t, code, map[string]any{"name": "Alice", "password": "s3cret"})
}

func TestRoomsJoinLimits(t *testing.T) {
	e := newRoomEnv(t)
	pack := e.createPack(t, samplePack("Quiz"))
	created := e.createRoom(t, map[string]any{"packId": pack.ID, "maxPlayers": 1, "allowViewers": false})
	code := created.Room.Code

	e.mustJoin(t, code, map[string]any{"name": "Bob"})

	rec := e.join(t, code, map[string]any{"name": "Carol"})
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Equal(t, "roomFull", decodeProblem(t, rec).Errors[0].Value)

	rec = e.join(t, code, map[string]any{"name": "Dave", "role": "viewer"})
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "badRole", decodeProblem(t, rec).Errors[0].Value)

	// The host may still enter as a viewer.
	hostViewer := e.mustJoin(t, code, map[string]any{"name": "Host", "role": "viewer"}, "X-Host-Token", created.HostToken)
	require.True(t, hostViewer.IsHost)

	// joinMode closed → 423 for everyone without the host token.
	rec = e.do(t, http.MethodPatch, "/api/v1/rooms/"+code+"/settings", map[string]any{"joinMode": "closed"},
		"X-Host-Token", created.HostToken)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = e.join(t, code, map[string]any{"name": "Erin", "role": "showman"})
	require.Equal(t, http.StatusLocked, rec.Code, rec.Body.String())
	require.Equal(t, "joinClosed", decodeProblem(t, rec).Errors[0].Value)
}

func TestRoomsHostEndpoints(t *testing.T) {
	e := newRoomEnv(t)
	pack := e.createPack(t, samplePack("Quiz"))
	created := e.createRoom(t, map[string]any{"packId": pack.ID})
	code := created.Room.Code
	base := "/api/v1/rooms/" + code
	host := e.mustJoin(t, code, map[string]any{"name": "Host", "role": "showman"}, "X-Host-Token", created.HostToken)
	alice := e.mustJoin(t, code, map[string]any{"name": "Alice"})

	hostOps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, base + "/settings", map[string]any{"name": "x"}},
		{http.MethodPost, base + "/kick", map[string]any{"personId": alice.PersonID}},
		{http.MethodPost, base + "/unban", map[string]any{"personId": alice.PersonID}},
		{http.MethodPost, base + "/transfer-host", map[string]any{"personId": alice.PersonID}},
		{http.MethodGet, base + "/buzz-log", nil},
		{http.MethodDelete, base, nil},
	}
	for _, op := range hostOps {
		rec := e.do(t, op.method, op.path, op.body)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s: %s", op.method, op.path, rec.Body.String())
		p := decodeProblem(t, rec)
		require.Equal(t, "header.X-Host-Token", p.Errors[0].Location)

		rec = e.do(t, op.method, op.path, op.body, "X-Host-Token", "wrong")
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", op.method, op.path, rec.Body.String())
		require.Equal(t, "invalidHostToken", decodeProblem(t, rec).Errors[0].Value)

		rec = e.do(t, op.method, "/api/v1/rooms/ZZZZZ"+strings.TrimPrefix(op.path, base), op.body, "X-Host-Token", created.HostToken)
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s: %s", op.method, op.path, rec.Body.String())
	}
	tok := []string{"X-Host-Token", created.HostToken}

	// Settings: rename, password, buzzer preset, rules patch.
	rec := e.do(t, http.MethodPatch, base+"/settings",
		map[string]any{"name": "Finals", "password": "pw", "buzzerPreset": "lanWired", "rules": map[string]any{"oral": true, "readingSpeed": 15}}, tok...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view RoomView
	decodeJSON(t, rec, &view)
	lan, _ := buzzer.Preset(buzzer.PresetLANWired)
	require.Equal(t, "Finals", view.Name)
	require.True(t, view.HasPassword)
	require.Equal(t, lan, view.Buzzer)
	require.True(t, view.Rules.Oral)
	require.Equal(t, 15, view.Rules.ReadingSpeed)
	require.Equal(t, engine.ModeClassic, view.Rules.Mode)
	require.Equal(t, view, e.getRoom(t, code))

	// Explicit buzzer settings win over the preset and are validated.
	explicit := buzzer.DefaultSettings()
	explicit.TieBreak = buzzer.TieBreakRotate
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"buzzer": explicit, "buzzerPreset": "tournament"}, tok...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	decodeJSON(t, rec, &view)
	require.Equal(t, explicit, view.Buzzer)
	bad := explicit
	bad.MaxCollectMs = 5
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"buzzer": bad}, tok...)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "body.buzzer", decodeProblem(t, rec).Errors[0].Location)
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"buzzerPreset": "noRace"}, tok...)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "body.buzzerPreset", decodeProblem(t, rec).Errors[0].Location)
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"buzzerPreset": "turbo"}, tok...)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"joinMode": "sometimes"}, tok...)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"name": "   "}, tok...)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "invalidParams", decodeProblem(t, rec).Errors[0].Value)
	// The AI showman mode needs a judge even when patched in.
	rec = e.do(t, http.MethodPatch, base+"/settings", map[string]any{"showman": "ai"}, tok...)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String(), "a human showman is present")

	// Kick / ban / unban.
	rec = e.do(t, http.MethodPost, base+"/kick", map[string]any{"personId": "nobody"}, tok...)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, "personNotFound", decodeProblem(t, rec).Errors[0].Value)
	rec = e.do(t, http.MethodPost, base+"/kick", map[string]any{"personId": host.PersonID}, tok...)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, base+"/kick", map[string]any{"personId": alice.PersonID, "ban": true}, tok...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Len(t, e.getRoom(t, code).Players, 1)
	rec = e.join(t, code, map[string]any{"name": "ALICE", "password": "pw"})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Equal(t, "banned", decodeProblem(t, rec).Errors[0].Value)
	rec = e.do(t, http.MethodPost, base+"/unban", map[string]any{"personId": "nobody"}, tok...)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, base+"/unban", map[string]any{"personId": alice.PersonID}, tok...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	alice = e.mustJoin(t, code, map[string]any{"name": "Alice", "password": "pw"})

	// Transfer host.
	rec = e.do(t, http.MethodPost, base+"/transfer-host", map[string]any{"personId": "nobody"}, tok...)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodPost, base+"/transfer-host", map[string]any{"personId": alice.PersonID}, tok...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	view = e.getRoom(t, code)
	require.True(t, findPerson(t, view, alice.PersonID).IsHost)
	require.False(t, findPerson(t, view, host.PersonID).IsHost)

	// Buzz log: empty in the lobby.
	rec = e.do(t, http.MethodGet, base+"/buzz-log", nil, tok...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var log BuzzLog
	decodeJSON(t, rec, &log)
	require.NotNil(t, log.Arms)
	require.Empty(t, log.Arms)
	require.Contains(t, rec.Body.String(), `"arms":[]`)
	rec = e.do(t, http.MethodGet, base+"/buzz-log?questionId=q1", nil, tok...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Close: 204, then the code stops resolving.
	rec = e.do(t, http.MethodDelete, base, nil, tok...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Eventually(t, func() bool {
		return e.do(t, http.MethodGet, base, nil).Code == http.StatusNotFound
	}, 3*time.Second, 10*time.Millisecond)
	rec = e.do(t, http.MethodDelete, base, nil, tok...)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = e.do(t, http.MethodGet, "/api/v1/rooms", nil)
	var list RoomList
	decodeJSON(t, rec, &list)
	require.Empty(t, list.Items)
}

func TestBuzzerPresets(t *testing.T) {
	e := newTestEnv(t)

	rec := e.do(t, http.MethodGet, "/api/v1/buzzer-presets", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list BuzzerPresetList
	decodeJSON(t, rec, &list)
	require.Equal(t, buzzer.PresetWiFiParty, list.Default)
	require.Len(t, list.Presets, 5)
	names := make([]string, 0, len(list.Presets))
	for _, p := range list.Presets {
		names = append(names, p.Name)
		want, ok := buzzer.Preset(p.Name)
		require.True(t, ok, p.Name)
		require.Equal(t, want, p.Settings, p.Name)
		require.NotEmpty(t, p.Title, p.Name)
		require.NotEmpty(t, p.Description, p.Name)
		require.Equal(t, p.Name != buzzer.PresetNoRace, p.Supported, p.Name)
	}
	require.Equal(t, buzzer.PresetNames, names)

	rec = e.do(t, http.MethodGet, "/api/v1/buzzer-presets/tournament", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var one BuzzerPreset
	decodeJSON(t, rec, &one)
	require.Equal(t, buzzer.PresetTournament, one.Name)
	require.True(t, one.Settings.StrictTrust)
	require.True(t, one.Supported)

	rec = e.do(t, http.MethodGet, "/api/v1/buzzer-presets/turbo", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusNotFound, decodeProblem(t, rec).Status)
}

func TestRoomsOpenAPI(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(t, http.MethodGet, "/openapi.json", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	for _, id := range []string{"listRooms", "createRoom", "getRoom", "joinRoom", "leaveRoom", "updateRoomSettings",
		"kickPerson", "unbanPerson", "transferHost", "closeRoom", "getBuzzLog", "listBuzzerPresets", "getBuzzerPreset"} {
		require.Contains(t, body, `"operationId":"`+id+`"`, id)
	}
	require.Contains(t, body, `"RoomView"`)
	require.Contains(t, body, `"X-Host-Token"`)
	require.Contains(t, body, "hostToken")
}
