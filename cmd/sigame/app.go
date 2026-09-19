package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/ai"
	"sigame/internal/clock"
	"sigame/internal/config"
	"sigame/internal/db"
	"sigame/internal/httpapi"
	"sigame/internal/media"
	"sigame/internal/packs"
	"sigame/internal/room"
	"sigame/internal/ws"
	"sigame/web"
)

// app is the assembled server: the HTTP handler (REST API, docs, media,
// WebSocket) and the collaborators run() and the tests need to drive and
// shut it down. The listener, signal handling and the periodic media GC
// stay in run().
type app struct {
	Handler http.Handler
	API     huma.API // for --print-openapi
	Rooms   *room.Manager
	DB      *sql.DB
	Store   *media.DiskStore
	FFProbe bool // ffprobe was found (media durations are known)
	// Close releases what buildApp opened (the database). Call
	// Rooms.Shutdown before it: rooms persist their state on shutdown.
	Close func()
}

// buildApp opens the database (and migrates it), the media store, the
// answer judge, the pack repository and the room manager, and mounts the
// REST API and the WebSocket transport on one router.
func buildApp(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*app, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	sqlDB, err := db.Open(ctx, filepath.Join(cfg.DataDir, "sigame.db"))
	if err != nil {
		return nil, err
	}
	closeDB := func() { _ = sqlDB.Close() }
	if err := db.Migrate(ctx, sqlDB); err != nil {
		closeDB()
		return nil, err
	}

	prober, ffprobeOK := newProber(cfg, logger)
	store, err := media.NewDiskStore(sqlDB, filepath.Join(cfg.DataDir, "media"), media.Limits{
		MaxImage: cfg.MaxImageMB << 20,
		MaxAudio: cfg.MaxAudioMB << 20,
		MaxVideo: cfg.MaxVideoMB << 20,
		MaxHTML:  cfg.MaxHTMLMB << 20,
		AllowSVG: cfg.AllowSVG,
	}, prober, logger)
	if err != nil {
		closeDB()
		return nil, err
	}

	judge, judgeStatus := newJudge(cfg, logger)
	packRepo := packs.NewRepo(sqlDB)
	clk := clock.NewReal()

	rooms := room.NewManager(room.ManagerDeps{
		DB:     sqlDB,
		Packs:  packRepo,
		Media:  store,
		Judge:  judge,
		Clock:  clk,
		Logger: logger,
		Cfg: room.RoomConfig{
			MaxRooms:           cfg.MaxRooms,
			MaxPlayers:         cfg.MaxPlayersPerRoom,
			RoomTTL:            cfg.RoomTTL,
			MediaFallbackMaxMs: 60_000,
			ConnQualityEveryMs: 5_000,
			ResumeBufferSize:   500,
			AIShowmanName:      "ИИ-ведущий",
			AITimeout:          cfg.AIJudgeTimeout,
		},
	})

	api, err := httpapi.New(httpapi.Deps{
		Cfg:       cfg,
		Packs:     packRepo,
		Media:     store,
		Rooms:     rooms,
		AI:        judge,
		AIStatus:  judgeStatus,
		Ping:      sqlDB.PingContext,
		FFProbe:   ffprobeOK,
		WebFS:     web.Dist(),
		Version:   version,
		Logger:    logger,
		StartedAt: time.Now(),
	})
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rooms.Shutdown(stopCtx)
		cancel()
		closeDB()
		return nil, err
	}
	ws.Mount(api.Router, &ws.Handler{
		Rooms: rooms,
		Cfg: ws.Config{
			MaxMessageBytes: cfg.WSMaxMessageBytes,
			MessagesPerSec:  cfg.WSMessagesPerSec,
			AllowedOrigins:  cfg.CORSOrigins,
		},
		Clock:  clk,
		Logger: logger,
	})

	return &app{
		Handler: api,
		API:     api.API,
		Rooms:   rooms,
		DB:      sqlDB,
		Store:   store,
		FFProbe: ffprobeOK,
		Close:   closeDB,
	}, nil
}

func newProber(cfg *config.Config, logger *slog.Logger) (media.Prober, bool) {
	path := cfg.FFProbePath
	if path == "" {
		p, ok := media.DetectFFProbe()
		if !ok {
			logger.Warn("ffprobe not found in PATH; media durations will be unknown")
			return media.NopProber{}, false
		}
		path = p
	}
	return &media.FFProbe{Path: path, Timeout: 10 * time.Second}, true
}

// newJudge builds the answer judge: fuzzy matching always, OpenRouter on top
// when an API key is configured.
func newJudge(cfg *config.Config, logger *slog.Logger) (ai.Judge, httpapi.AIStatusProvider) {
	fuzzy := ai.FuzzyJudge{}
	if !cfg.AIConfigured() {
		return &ai.Composite{Fuzzy: fuzzy, Logger: logger}, nil
	}
	or := ai.NewOpenRouter(ai.Config{
		APIKey:      cfg.OpenRouterAPIKey,
		Model:       cfg.OpenRouterModel,
		BaseURL:     cfg.OpenRouterBaseURL,
		Timeout:     cfg.AIJudgeTimeout,
		Temperature: cfg.AITemperature,
		MaxTokens:   cfg.AIMaxTokens,
		Referer:     cfg.PublicURL,
		// Reasoning models (tencent/hy*) never finish a json_schema-constrained
		// answer; SIGAME_AI_STRUCTURED=auto turns the schema off for them.
		StructuredOutputs: cfg.AIStructuredOutputs(),
		Logger:            logger,
	})
	return &ai.Composite{Fuzzy: fuzzy, AI: or, Cache: ai.NewCache(0), AITimeout: cfg.AIJudgeTimeout, Logger: logger}, or
}
