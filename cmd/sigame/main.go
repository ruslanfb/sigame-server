// Command sigame runs the SIGame server: REST API (OpenAPI), WebSocket game
// transport, media serving and the SQLite-backed pack library.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"sigame/internal/ai"
	"sigame/internal/clock"
	"sigame/internal/config"
	"sigame/internal/db"
	"sigame/internal/httpapi"
	"sigame/internal/lan"
	"sigame/internal/media"
	"sigame/internal/packs"
	"sigame/internal/room"
	"sigame/internal/ws"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	printOpenAPI := flag.Bool("print-openapi", false, "print the OpenAPI spec as JSON and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(context.Background(), *printOpenAPI); err != nil {
		fmt.Fprintln(os.Stderr, "sigame:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, printOpenAPI bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("data dir: %w", err)
	}
	sqlDB, err := db.Open(ctx, filepath.Join(cfg.DataDir, "sigame.db"))
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := db.Migrate(ctx, sqlDB); err != nil {
		return err
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
		return err
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
		Version:   version,
		Logger:    logger,
		StartedAt: time.Now(),
	})
	if err != nil {
		return err
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

	if printOpenAPI {
		b, err := json.MarshalIndent(api.API.OpenAPI(), "", "  ")
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// ConnContext exposes the raw TCP connection to the WebSocket handler
		// so the buzzer can read the kernel's RTT estimate (TCP_INFO).
		ConnContext: ws.ConnContext,
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Addr, err)
	}
	addrs, _ := lan.Addresses()
	urls := lan.JoinURLs(ln.Addr().String(), cfg.PublicURL, addrs)
	fmt.Print(lan.Banner(urls))
	logger.Info("server started", "version", version, "addr", ln.Addr().String(), "dataDir", cfg.DataDir,
		"ffprobe", ffprobeOK, "ai", cfg.AIConfigured(), "model", cfg.OpenRouterModel)

	// Periodic media garbage collection.
	gcCtx, gcCancel := context.WithCancel(ctx)
	defer gcCancel()
	go mediaGC(gcCtx, store, cfg.MediaGCGrace, logger)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-sigCtx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Rooms first: they notify clients (ROOM_CLOSED, close 1001) and persist
	// state; then the HTTP server drains.
	rooms.Shutdown(shutdownCtx)
	return srv.Shutdown(shutdownCtx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
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
		Logger:      logger,
	})
	return &ai.Composite{Fuzzy: fuzzy, AI: or, Cache: ai.NewCache(0), AITimeout: cfg.AIJudgeTimeout, Logger: logger}, or
}

func mediaGC(ctx context.Context, store *media.DiskStore, grace time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.Sweep(ctx, grace)
			if err != nil {
				logger.Warn("media gc failed", "err", err)
			} else if n > 0 {
				logger.Info("media gc", "deleted", n)
			}
		}
	}
}
