-- no_consecutive_posts が true のチャンネルでは、投稿者は自分以外の誰かが投稿するまで
-- 続けて投稿できない（連続投稿の禁止）。デフォルトは無効。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS no_consecutive_posts BOOLEAN NOT NULL DEFAULT false;
