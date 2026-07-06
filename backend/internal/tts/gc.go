package tts

import (
	"context"
	"log/slog"
	"os"
	"time"

	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

func StartGC(ctx context.Context, q *db.Queries, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runGC(ctx, q)
			}
		}
	}()
}

func runGC(ctx context.Context, q *db.Queries) {
	candidates, err := q.ListTTSGCCandidates(ctx, db.ListTTSGCCandidatesParams{
		OlderThanHours: 24 * 7,
		UseCount:       3,
		Limit:          100,
	})
	if err != nil {
		slog.Warn("list tts gc candidates failed", "error", err)
		return
	}
	for _, c := range candidates {
		if err := q.MarkTTSAssetForDelete(ctx, c.ContentHash); err != nil {
			slog.Warn("mark tts asset for delete failed", "content_hash", c.ContentHash, "error", err)
			continue
		}
		if err := os.Remove(c.FilePath); err != nil && !os.IsNotExist(err) {
			slog.Warn("remove tts asset failed", "content_hash", c.ContentHash, "file_path", c.FilePath, "error", err)
			continue
		}
		if err := q.DeleteTTSAsset(ctx, c.ContentHash); err != nil {
			slog.Warn("delete tts asset row failed", "content_hash", c.ContentHash, "error", err)
		}
	}
}
