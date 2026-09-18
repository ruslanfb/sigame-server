package room

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"sigame/internal/ai"
	"sigame/internal/buzzer"
	"sigame/internal/clock"
	"sigame/internal/engine"
	"sigame/internal/media"
	"sigame/internal/packs"
)

// Defaults.
const (
	defaultMaxRooms         = 50
	defaultMaxPlayers       = 12
	defaultRoomTTL          = 6 * time.Hour
	defaultMediaFallbackMs  = 60_000
	defaultConnQualityMs    = int64(buzzer.ConnQualityIntervalMs)
	defaultResumeBufferSize = 500
	defaultAIShowmanName    = "AI Showman"
	defaultAITimeout        = 15 * time.Second
	defaultHybridConfirmMs  = 8000
	defaultSweepEvery       = time.Minute
	codeAlphabet            = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength              = 5
)

// RoomConfig are the operator limits.
type RoomConfig struct {
	MaxRooms           int
	MaxPlayers         int           // ≤ 12
	RoomTTL            time.Duration // idle rooms are closed after this
	MediaFallbackMaxMs int64         // mediaFallback timer when media duration is unknown
	ConnQualityEveryMs int64         // CONN_QUALITY broadcast interval
	ResumeBufferSize   int           // outbound envelopes kept per person for resume
	AIShowmanName      string        // display name of the AI showman pseudo-person
	AITimeout          time.Duration // bound on one judge call
	// FuzzyUncertainAsWrong: an uncertain automatic verdict counts as a wrong
	// answer with the normal penalty; false (default) rejects it without penalty.
	FuzzyUncertainAsWrong bool
	SweepEvery            time.Duration // TTL sweeper period (default 1 min)
}

// PackSource loads packs (implemented by *packs.Repo).
type PackSource interface {
	Get(ctx context.Context, id string) (*packs.Pack, error)
}

// ManagerDeps wires the manager.
type ManagerDeps struct {
	DB     *sql.DB     // rooms / game_results tables; nil = no persistence
	Packs  PackSource  // pack loader
	Media  media.Store // only Stat is used (media durations); may be nil
	Judge  ai.Judge    // answer judge for ai/hybrid rooms; nil = fuzzy only
	Clock  clock.Clock
	Logger *slog.Logger
	Cfg    RoomConfig
}

// CreateParams describe a new room.
type CreateParams struct {
	PackID          string              `json:"packId"`
	Name            string              `json:"name"`
	Password        string              `json:"password,omitempty"`
	Rules           engine.Rules        `json:"rules"`
	Times           engine.TimeSettings `json:"times"`
	Buzzer          buzzer.Settings     `json:"buzzer"`
	Showman         ShowmanMode         `json:"showman"`
	HybridConfirmMs int64               `json:"hybridConfirmMs"`
	MaxPlayers      int                 `json:"maxPlayers"`
	AllowViewers    bool                `json:"allowViewers"`
	Language        string              `json:"language,omitempty"`
}

// PersonView is a person as listed publicly.
type PersonView struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Role      engine.Role `json:"role" enum:"showman,player,viewer"`
	Connected bool        `json:"connected"`
	Score     int         `json:"score"`
	IsHost    bool        `json:"isHost"`
}

// Info is the public projection of a room.
type Info struct {
	ID              string              `json:"id" doc:"UUIDv7"`
	Code            string              `json:"code" doc:"5-character join code"`
	Name            string              `json:"name"`
	PackID          string              `json:"packId"`
	PackName        string              `json:"packName"`
	Status          string              `json:"status" enum:"lobby,playing,finished,closed"`
	Showman         ShowmanMode         `json:"showman" enum:"human,ai,hybrid"`
	HasPassword     bool                `json:"hasPassword"`
	Players         []PersonView        `json:"players" doc:"Everyone in the room (showman, players, viewers)"`
	Viewers         int                 `json:"viewers"`
	MaxPlayers      int                 `json:"maxPlayers"`
	AllowViewers    bool                `json:"allowViewers"`
	Language        string              `json:"language,omitempty"`
	HybridConfirmMs int64               `json:"hybridConfirmMs" doc:"hybrid mode: the AI verdict applies after this delay without a human verdict"`
	CreatedAt       int64               `json:"createdAt" doc:"Unix ms UTC"`
	UpdatedAt       int64               `json:"updatedAt" doc:"Unix ms UTC"`
	Rules           engine.Rules        `json:"rules"`
	Times           engine.TimeSettings `json:"times"`
	Buzzer          buzzer.Settings     `json:"buzzer"`
	JoinMode        string              `json:"joinMode" enum:"any,viewersOnly,closed"`
}

// JoinParams describe a join request.
type JoinParams struct {
	Name     string      `json:"name" minLength:"1" maxLength:"64"`
	Role     engine.Role `json:"role" enum:"showman,player,viewer" doc:"Default player"`
	Password string      `json:"password,omitempty"`
}

// SettingsPatch changes room settings (nil = untouched).
type SettingsPatch struct {
	Rules    *engine.RulesPatch   `json:"rules,omitempty" doc:"Applied via SET_OPTIONS when the game is running"`
	Times    *engine.TimeSettings `json:"times,omitempty" doc:"Lobby only"`
	Buzzer   *buzzer.Settings     `json:"buzzer,omitempty"`
	Showman  *ShowmanMode         `json:"showman,omitempty" doc:"Lobby only"`
	JoinMode *string              `json:"joinMode,omitempty" enum:"any,viewersOnly,closed"`
	Name     *string              `json:"name,omitempty"`
	Password *string              `json:"password,omitempty" doc:"Empty string removes the password"`
}

// Manager owns every room of the server.
type Manager struct {
	deps    ManagerDeps
	log     *slog.Logger
	mu      sync.Mutex
	rooms   map[string]*Room
	wg      sync.WaitGroup // room loops, anchor loops, judge calls
	stopC   chan struct{}
	stopped atomic.Bool
	fuzzy   ai.Judge
}

// NewManager creates a manager and starts the TTL sweeper.
func NewManager(deps ManagerDeps) *Manager {
	if deps.Clock == nil {
		deps.Clock = clock.NewReal()
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	c := &deps.Cfg
	if c.MaxRooms <= 0 {
		c.MaxRooms = defaultMaxRooms
	}
	if c.MaxPlayers <= 0 || c.MaxPlayers > engine.MaxPlayers {
		c.MaxPlayers = defaultMaxPlayers
	}
	if c.RoomTTL <= 0 {
		c.RoomTTL = defaultRoomTTL
	}
	if c.MediaFallbackMaxMs <= 0 {
		c.MediaFallbackMaxMs = defaultMediaFallbackMs
	}
	if c.ConnQualityEveryMs <= 0 {
		c.ConnQualityEveryMs = defaultConnQualityMs
	}
	if c.ResumeBufferSize <= 0 {
		c.ResumeBufferSize = defaultResumeBufferSize
	}
	if c.AIShowmanName == "" {
		c.AIShowmanName = defaultAIShowmanName
	}
	if c.AITimeout <= 0 {
		c.AITimeout = defaultAITimeout
	}
	if c.SweepEvery <= 0 {
		c.SweepEvery = defaultSweepEvery
	}
	m := &Manager{deps: deps, log: deps.Logger, rooms: map[string]*Room{}, stopC: make(chan struct{}), fuzzy: ai.FuzzyJudge{}}
	m.wg.Add(1)
	go m.sweeper()
	return m
}

func (m *Manager) judge() ai.Judge {
	if m.deps.Judge != nil {
		return m.deps.Judge
	}
	return m.fuzzy
}

func (m *Manager) sweeper() {
	defer m.wg.Done()
	t := time.NewTicker(m.deps.Cfg.SweepEvery)
	defer t.Stop()
	for {
		select {
		case <-m.stopC:
			return
		case <-t.C:
			m.sweep()
		}
	}
}

func (m *Manager) sweep() {
	cutoff := m.deps.Clock.Now().Add(-m.deps.Cfg.RoomTTL).UnixMilli()
	for _, r := range m.snapshot() {
		if r.lastActivity.Load() < cutoff {
			r.post(func() { r.closeRoom("ttl") })
		}
	}
}

func (m *Manager) snapshot() []*Room {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	return out
}

func (m *Manager) lookup(code string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return r, nil
}

func (m *Manager) remove(code string) {
	m.mu.Lock()
	delete(m.rooms, code)
	m.mu.Unlock()
}

func (m *Manager) newCode() string {
	b := make([]byte, codeLength)
	for {
		if _, err := rand.Read(b); err != nil {
			panic("room: crypto/rand: " + err.Error())
		}
		for i := range b {
			b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
		}
		code := string(b)
		if _, taken := m.rooms[code]; !taken {
			return code
		}
	}
}

// Create makes a room in the lobby state. The returned host token grants
// host rights to the first joiner presenting it (Join) and authorises the
// host REST calls.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Info, string, error) {
	if m.stopped.Load() {
		return Info{}, "", ErrRoomClosed
	}
	if p.Rules.Mode == "" {
		p.Rules = engine.DefaultRules()
	}
	if p.Times == (engine.TimeSettings{}) {
		p.Times = engine.DefaultTimeSettings()
	}
	if p.Buzzer.Mode == "" {
		p.Buzzer = buzzer.DefaultSettings()
	}
	if p.Showman == "" {
		p.Showman = ShowmanHuman
	}
	switch p.Showman {
	case ShowmanHuman, ShowmanAI, ShowmanHybrid:
	default:
		return Info{}, "", fmt.Errorf("%w: unknown showman mode %q", ErrInvalidParams, p.Showman)
	}
	if p.HybridConfirmMs <= 0 {
		p.HybridConfirmMs = defaultHybridConfirmMs
	}
	if p.MaxPlayers <= 0 || p.MaxPlayers > m.deps.Cfg.MaxPlayers {
		p.MaxPlayers = m.deps.Cfg.MaxPlayers
	}
	p.Name = strings.TrimSpace(p.Name)
	if utf8.RuneCountInString(p.Name) > 100 {
		return Info{}, "", fmt.Errorf("%w: name too long", ErrInvalidParams)
	}
	if err := checkBuzzer(p.Buzzer); err != nil {
		return Info{}, "", err
	}
	if m.deps.Packs == nil {
		return Info{}, "", fmt.Errorf("%w: no pack source", ErrInvalidParams)
	}
	pack, err := m.deps.Packs.Get(ctx, p.PackID)
	if err != nil {
		if errors.Is(err, packs.ErrNotFound) {
			return Info{}, "", ErrPackNotFound
		}
		return Info{}, "", fmt.Errorf("room: load pack: %w", err)
	}
	if len(pack.Rounds) == 0 {
		return Info{}, "", fmt.Errorf("%w: pack has no rounds", ErrInvalidParams)
	}
	if p.Name == "" {
		p.Name = pack.Name
	}
	hostToken := newToken()
	now := m.deps.Clock.Now().UnixMilli()

	m.mu.Lock()
	if len(m.rooms) >= m.deps.Cfg.MaxRooms {
		m.mu.Unlock()
		return Info{}, "", ErrTooManyRooms
	}
	r := &Room{m: m, id: newID(), code: m.newCode(), createdAt: now, updatedAt: now,
		mailbox: make(chan func(), mailboxSize), done: make(chan struct{}), stopped: make(chan struct{}),
		name: p.Name, password: p.Password, hostHash: hashToken(hostToken), packID: pack.ID, pack: pack,
		rules: p.Rules, times: p.Times, bz: p.Buzzer, showman: p.Showman, hybridConfirmMs: p.HybridConfirmMs,
		maxPlayers: p.MaxPlayers, allowViewers: p.AllowViewers, language: p.Language, joinMode: JoinAny, status: StatusLobby,
		persons: map[string]*person{}, bannedNames: map[string]bool{}, bannedIDs: map[string]string{},
		timers: map[string]*time.Timer{}, lastWinnerSeat: -1}
	r.log = m.log.With("room", r.code)
	r.lastActivity.Store(now)
	r.syncAIShowman()
	r.publishInfo()
	m.rooms[r.code] = r
	m.mu.Unlock()

	if err := insertRoom(ctx, m.deps.DB, r); err != nil {
		m.remove(r.code)
		return Info{}, "", err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		r.run()
	}()
	r.post(r.startQualityTicker)
	return r.info(), hostToken, nil
}

// Get returns the public projection of a room.
func (m *Manager) Get(code string) (Info, bool) {
	r, err := m.lookup(code)
	if err != nil {
		return Info{}, false
	}
	return r.info(), true
}

// List returns every open room, newest first.
func (m *Manager) List() []Info {
	rooms := m.snapshot()
	out := make([]Info, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, r.info())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Join adds a person (or re-issues a session to a disconnected one).
func (m *Manager) Join(ctx context.Context, code string, p JoinParams, hostToken string) (Session, error) {
	r, err := m.lookup(code)
	if err != nil {
		return Session{}, err
	}
	var s Session
	err = r.call(ctx, func() error {
		var e error
		s, e = r.join(p, hostToken)
		return e
	})
	return s, err
}

// Leave removes a person; a seated player keeps the seat (rejoin by name).
func (m *Manager) Leave(code, sessionToken string) error {
	r, err := m.lookup(code)
	if err != nil {
		return err
	}
	return r.call(context.Background(), func() error { return r.leave(sessionToken) })
}

func (m *Manager) hostCall(code, hostToken string, fn func(r *Room) error) error {
	r, err := m.lookup(code)
	if err != nil {
		return err
	}
	if hashToken(hostToken) != r.hostHash {
		return ErrInvalidHostToken
	}
	return r.call(context.Background(), func() error { return fn(r) })
}

// UpdateSettings patches room settings (host token).
func (m *Manager) UpdateSettings(code, hostToken string, patch SettingsPatch) (Info, error) {
	var info Info
	err := m.hostCall(code, hostToken, func(r *Room) error {
		if err := r.updateSettings(patch); err != nil {
			return err
		}
		info = r.info()
		return nil
	})
	return info, err
}

// Kick removes a person; ban also blocks the name.
func (m *Manager) Kick(code, hostToken, personID string, ban bool) error {
	return m.hostCall(code, hostToken, func(r *Room) error { return r.kick(personID, ban) })
}

// Unban lifts a ban.
func (m *Manager) Unban(code, hostToken, personID string) error {
	return m.hostCall(code, hostToken, func(r *Room) error {
		name, ok := r.bannedIDs[personID]
		if !ok {
			return ErrPersonNotFound
		}
		delete(r.bannedIDs, personID)
		delete(r.bannedNames, name)
		return nil
	})
}

// TransferHost makes another person the host.
func (m *Manager) TransferHost(code, hostToken, personID string) error {
	return m.hostCall(code, hostToken, func(r *Room) error { return r.transferHost(personID) })
}

// BuzzLog returns the arm records of a room (all, or one question).
func (m *Manager) BuzzLog(code, hostToken, questionID string) ([]ArmRecord, error) {
	var out []ArmRecord
	err := m.hostCall(code, hostToken, func(r *Room) error {
		out = r.buzzLog(questionID)
		return nil
	})
	return out, err
}

// Close closes a room (host token).
func (m *Manager) Close(code, hostToken string) error {
	r, err := m.lookup(code)
	if err != nil {
		return err
	}
	if hashToken(hostToken) != r.hostHash {
		return ErrInvalidHostToken
	}
	if !r.post(func() { r.closeRoom("host") }) {
		return ErrRoomClosed
	}
	return nil
}

// Attach registers a live connection for a session.
func (m *Manager) Attach(code, sessionToken string, c Conn) (*Attachment, error) {
	r, err := m.lookup(code)
	if err != nil {
		return nil, err
	}
	var att *Attachment
	err = r.call(context.Background(), func() error {
		var e error
		att, e = r.attach(sessionToken, c)
		return e
	})
	return att, err
}

// Shutdown closes every room (ROOM_CLOSED + close 1001) and waits for the
// actors, anchor loops and judge calls to finish or ctx to expire.
func (m *Manager) Shutdown(ctx context.Context) {
	if m.stopped.Swap(true) {
		return
	}
	close(m.stopC)
	for _, r := range m.snapshot() {
		r.post(func() { r.closeRoom("shutdown") })
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
