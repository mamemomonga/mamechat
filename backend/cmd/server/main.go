package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/mamemomonga/mamechat/backend/internal/auth"
	"github.com/mamemomonga/mamechat/backend/internal/config"
	"github.com/mamemomonga/mamechat/backend/internal/httpserver"
	"github.com/mamemomonga/mamechat/backend/internal/realtime"
	"github.com/mamemomonga/mamechat/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	ownerUser, err := auth.EnsureOwnerUser(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("ensure owner user: %v", err)
	}
	slog.Info("owner user ready", "user_id", ownerUser.ID, "handle", ownerUser.Handle)

	bus, err := realtime.NewValkeyBus(cfg.RedisURL, cfg.ActiveWindow)
	if err != nil {
		log.Fatalf("create valkey bus: %v", err)
	}
	defer func() {
		if err := bus.Close(); err != nil {
			slog.Warn("close valkey client failed", "error", err)
		}
	}()
	if err := bus.Ping(ctx); err != nil {
		log.Fatalf("ping valkey: %v", err)
	}

	server := httpserver.New(cfg, pool, bus)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("http server: %v", err)
	}
}
