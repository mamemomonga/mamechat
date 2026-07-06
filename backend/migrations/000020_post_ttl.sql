-- 投稿の寿命（時間）。作成から post_ttl_hours 経過した投稿は自動削除される（画像も含む）。
-- 6 / 24 / 72(=3日) を想定。既定は24時間。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS post_ttl_hours INTEGER NOT NULL DEFAULT 24;
