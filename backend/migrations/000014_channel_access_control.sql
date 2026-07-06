-- チャンネルの入室許可制御（ホワイトリスト/ブラックリスト）。
-- access_mode: 'none'（制限なし・既定）, 'whitelist'（許可リストのみ入室可）, 'blacklist'（拒否リストは入室不可）
-- access_list: 入室判定に使うエントリ（ハンドルまたはプロフィールURLの文字列）の配列。
ALTER TABLE channels
  ADD COLUMN IF NOT EXISTS access_mode TEXT NOT NULL DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS access_list JSONB NOT NULL DEFAULT '[]'::jsonb;
