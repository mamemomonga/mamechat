package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/atproto"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/auth"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/chat"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/config"
	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/mastodon"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/misskey"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/ogp"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/realtime"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/tts"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/uploads"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/webpush"
)

type Server struct {
	cfg      config.Config
	pool     *pgxpool.Pool
	q        *db.Queries
	auth     *auth.Manager
	atproto  *atproto.Client
	mastodon *mastodon.Client
	misskey  *misskey.Client
	bus      chat.RealtimeBus
	valkey   *realtime.ValkeyBus
	hub      *chat.Hub
	tts      *tts.Enqueuer
	uploads  *uploads.Store
	ogp      *ogp.Fetcher
	push     *webpush.Sender // nil のとき Push 無効
	mux      *http.ServeMux
}

func New(cfg config.Config, pool *pgxpool.Pool, valkey *realtime.ValkeyBus) *Server {
	q := db.New(pool)
	authManager := auth.NewManager(cfg, pool)
	atprotoClient := atproto.NewClient(cfg, pool)
	mastodonClient := mastodon.NewClient(cfg, pool)
	misskeyClient := misskey.NewClient(cfg, pool)
	hub := chat.NewHub(valkey, valkey, q, cfg.ChannelSuspendGrace)
	var pushSender *webpush.Sender
	if cfg.PushEnabled() {
		sender, err := webpush.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
		if err != nil {
			slog.Warn("invalid VAPID keys; web push disabled", "error", err)
		} else {
			pushSender = sender
			slog.Info("web push enabled")
		}
	}
	var ttsEnqueuer *tts.Enqueuer
	if cfg.TTSEnabled {
		queue, err := tts.NewQueue(cfg.RedisURL)
		if err != nil {
			slog.Warn("create tts queue failed; tts enqueue disabled", "error", err)
		} else {
			ttsEnqueuer = tts.NewEnqueuer(tts.SettingsFromConfig(cfg), q, queue, valkey)
		}
	}
	s := &Server{
		cfg:      cfg,
		pool:     pool,
		q:        q,
		auth:     authManager,
		atproto:  atprotoClient,
		mastodon: mastodonClient,
		misskey:  misskeyClient,
		bus:      valkey,
		valkey:   valkey,
		hub:      hub,
		tts:      ttsEnqueuer,
		uploads:  uploads.New(cfg.UploadStorageDir, cfg.UploadVideoMaxSeconds),
		ogp:      ogp.NewFetcher(),
		push:     pushSender,
		mux:      http.NewServeMux(),
	}
	s.routes()
	atprotoClient.StartProfileSync(context.Background())
	mastodonClient.StartProfileSync(context.Background())
	misskeyClient.StartProfileSync(context.Background())
	s.startSuspendedChannelCleanup(context.Background())
	s.startExpiredMessageCleanup(context.Background())
	s.startUploadOrphanCleanup(context.Background())
	tts.StartGC(context.Background(), q, tts.SettingsFromConfig(cfg).GCInterval)
	return s
}

// startSuspendedChannelCleanup はサスペンド期間を過ぎたチャンネルを定期的に削除する。
func (s *Server) startSuspendedChannelCleanup(ctx context.Context) {
	retentionHours := int32(s.cfg.ChannelSuspendRetention / time.Hour)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := s.q.DeleteExpiredSuspendedChannels(ctx, retentionHours)
				if err != nil {
					slog.Warn("delete expired suspended channels failed", "error", err)
					continue
				}
				for _, slug := range deleted {
					slog.Info("deleted expired suspended channel", "channel", slug)
				}
			}
		}
	}()
}

// startExpiredMessageCleanup は各チャンネルの投稿寿命(post_ttl_hours)を過ぎた投稿を
// 定期的に削除し、紐づく画像ファイルも消す。
func (s *Server) startExpiredMessageCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				paths, err := s.q.DeleteExpiredMessages(ctx)
				if err != nil {
					slog.Warn("delete expired messages failed", "error", err)
					continue
				}
				if len(paths) == 0 {
					continue
				}
				for _, p := range paths {
					if p.Valid && p.String != "" {
						if err := s.uploads.Delete(p.String); err != nil {
							slog.Warn("delete expired message image failed", "path", p.String, "error", err)
						}
					}
				}
				slog.Info("deleted expired messages", "count", len(paths))
			}
		}
	}()
}

func (s *Server) Handler() http.Handler {
	return s.cors(s.mux)
}

func (s *Server) ListenAndServe() error {
	slog.Info("http server listening", "addr", s.cfg.HTTPAddr)
	return http.ListenAndServe(s.cfg.HTTPAddr, s.Handler())
}
