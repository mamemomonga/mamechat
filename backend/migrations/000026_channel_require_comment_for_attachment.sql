-- require_comment_for_attachment が true のチャンネルでは、コメント（本文テキスト）なしで
-- 画像やURLだけを投稿することを禁止する。デフォルトは無効。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS require_comment_for_attachment BOOLEAN NOT NULL DEFAULT false;
