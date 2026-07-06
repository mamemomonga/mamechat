package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mamemomonga/mamechat/backend/internal/config"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
	"github.com/mamemomonga/mamechat/backend/internal/realtime"
	"github.com/mamemomonga/mamechat/backend/internal/store"
	"github.com/mamemomonga/mamechat/backend/internal/tts"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pool, err := store.Open(startCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	if err := store.Migrate(startCtx, pool); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	bus, err := realtime.NewValkeyBus(cfg.RedisURL, cfg.ActiveWindow)
	if err != nil {
		log.Fatalf("create valkey bus: %v", err)
	}
	defer func() {
		if err := bus.Close(); err != nil {
			slog.Warn("close valkey bus failed", "error", err)
		}
	}()

	queue, err := tts.NewQueue(cfg.RedisURL)
	if err != nil {
		log.Fatalf("create tts queue: %v", err)
	}
	defer func() {
		if err := queue.Close(); err != nil {
			slog.Warn("close tts queue failed", "error", err)
		}
	}()
	if err := queue.Ping(startCtx); err != nil {
		log.Fatalf("ping valkey: %v", err)
	}

	settings := tts.SettingsFromConfig(cfg)
	tts.StartGC(ctx, db.New(pool), settings.GCInterval)
	worker := tts.NewWorker(settings, db.New(pool), queue, bus)
	slog.Info("tts worker started", "voicevox_urls", settings.VoicevoxURLs, "concurrency", settings.WorkerConcurrency)
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("tts worker: %v", err)
	}
}
