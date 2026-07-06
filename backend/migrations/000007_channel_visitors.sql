-- チャンネルの来訪者を永続化する。のべ人数（重複なし）とアバター一覧の正本。
-- アクティブ状態は Valkey 側で管理し、Valkey が消滅しても来訪者はここから復元できる。
CREATE TABLE IF NOT EXISTS channel_visitors (
  channel_id    BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  display_name  TEXT NOT NULL DEFAULT '',
  handle        TEXT,
  avatar_url    TEXT,
  provider      TEXT,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_post_at  TIMESTAMPTZ,
  PRIMARY KEY (channel_id, user_id)
);

-- アバター一覧の並び順（最後に投稿した人 → 最近来た人）。
CREATE INDEX IF NOT EXISTS idx_channel_visitors_order
  ON channel_visitors (channel_id, last_post_at DESC NULLS LAST, last_seen_at DESC);
