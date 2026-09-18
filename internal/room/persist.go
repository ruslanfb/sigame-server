package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"sigame/internal/buzzer"
	"sigame/internal/engine"
)

// persistTimeout bounds every SQL statement issued by a room.
const persistTimeout = 3 * time.Second

// storedSettings is the JSON document kept in rooms.settings.
type storedSettings struct {
	Name            string              `json:"name"`
	HasPassword     bool                `json:"hasPassword"`
	Rules           engine.Rules        `json:"rules"`
	Times           engine.TimeSettings `json:"times"`
	Buzzer          buzzer.Settings     `json:"buzzer"`
	Showman         ShowmanMode         `json:"showman"`
	HybridConfirmMs int64               `json:"hybridConfirmMs"`
	MaxPlayers      int                 `json:"maxPlayers"`
	AllowViewers    bool                `json:"allowViewers"`
	Language        string              `json:"language,omitempty"`
	JoinMode        string              `json:"joinMode"`
}

// gameResultDoc is the JSON document kept in game_results.doc.
type gameResultDoc struct {
	RoomCode   string              `json:"roomCode"`
	RoomName   string              `json:"roomName"`
	PackID     string              `json:"packId"`
	PackName   string              `json:"packName"`
	Showman    ShowmanMode         `json:"showman"`
	Players    []PersonView        `json:"players"`
	Scores     []engine.ScoreEntry `json:"scores"`
	WinnerID   string              `json:"winnerId"`
	WinnerName string              `json:"winnerName,omitempty"`
	Statistics engine.Statistics   `json:"statistics"`
	Rules      engine.Rules        `json:"rules"`
	Times      engine.TimeSettings `json:"times"`
	Buzzer     buzzer.Settings     `json:"buzzer"`
	StartedAt  int64               `json:"startedAt"`
	FinishedAt int64               `json:"finishedAt"`
}

func (r *Room) settingsDoc() storedSettings {
	return storedSettings{Name: r.name, HasPassword: r.password != "", Rules: r.rules, Times: r.times, Buzzer: r.bz,
		Showman: r.showman, HybridConfirmMs: r.hybridConfirmMs, MaxPlayers: r.maxPlayers, AllowViewers: r.allowViewers,
		Language: r.language, JoinMode: r.joinMode}
}

func insertRoom(ctx context.Context, sqlDB *sql.DB, r *Room) error {
	if sqlDB == nil {
		return nil
	}
	doc, err := json.Marshal(r.settingsDoc())
	if err != nil {
		return fmt.Errorf("room: encode settings: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, persistTimeout)
	defer cancel()
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO rooms (id, code, pack_id, name, settings, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.id, r.code, r.packID, r.name, string(doc), r.status, r.createdAt, r.updatedAt)
	if err != nil {
		return fmt.Errorf("room: insert: %w", err)
	}
	return nil
}

// persistRoom updates status/settings; errors are logged, never fatal.
func (r *Room) persistRoom() {
	if r.m.deps.DB == nil {
		return
	}
	doc, err := json.Marshal(r.settingsDoc())
	if err != nil {
		r.log.Warn("room: encode settings", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if _, err := r.m.deps.DB.ExecContext(ctx, `UPDATE rooms SET name = ?, settings = ?, status = ?, updated_at = ? WHERE id = ?`,
		r.name, string(doc), r.status, r.updatedAt, r.id); err != nil {
		r.log.Warn("room: persist", "err", err)
	}
}

func (r *Room) persistResult(p engine.GameEndPayload) {
	if r.m.deps.DB == nil {
		return
	}
	now := r.wallMs()
	doc := gameResultDoc{RoomCode: r.code, RoomName: r.name, PackID: r.packID, PackName: r.pack.Name, Showman: r.showman,
		Players: r.personViews(), Scores: p.Scores, WinnerID: p.WinnerID, Statistics: p.Statistics, Rules: r.rules, Times: r.times,
		Buzzer: r.bz, StartedAt: r.startedAt, FinishedAt: now}
	if w := r.persons[p.WinnerID]; w != nil {
		doc.WinnerName = w.Name
	}
	b, err := json.Marshal(doc)
	if err != nil {
		r.log.Warn("room: encode result", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if _, err := r.m.deps.DB.ExecContext(ctx, `INSERT INTO game_results (id, room_id, pack_id, finished_at, doc) VALUES (?, ?, ?, ?, ?)`,
		newID(), r.id, r.packID, now, string(b)); err != nil {
		r.log.Warn("room: persist result", "err", err)
	}
}
