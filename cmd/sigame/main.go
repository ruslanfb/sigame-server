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
	"syscall"
	"time"

	"sigame/internal/config"
	"sigame/internal/lan"
	"sigame/internal/media"
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

	a, err := buildApp(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer a.Close()

	if printOpenAPI {
		b, err := json.MarshalIndent(a.API.OpenAPI(), "", "  ")
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.Handler,
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
		"ffprobe", a.FFProbe, "ai", cfg.AIConfigured(), "model", cfg.OpenRouterModel)

	// Periodic media garbage collection.
	gcCtx, gcCancel := context.WithCancel(ctx)
	defer gcCancel()
	go mediaGC(gcCtx, a.Store, cfg.MediaGCGrace, logger)

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
	a.Rooms.Shutdown(shutdownCtx)
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
