-- チャンネルごとの機能トグル（URLリンク化・画像アップロード）。既定はすべて有効。
ALTER TABLE channels
  ADD COLUMN IF NOT EXISTS url_linkify_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN IF NOT EXISTS image_upload_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- 投稿に添付する画像のメタデータは投稿内容（メッセージ行）に持たせる。
-- 画像ファイル本体は永続化ストレージにファイルとして保存し、image_path はその相対パス。
ALTER TABLE chat_messages
  ADD COLUMN IF NOT EXISTS image_path TEXT,
  ADD COLUMN IF NOT EXISTS image_width INTEGER,
  ADD COLUMN IF NOT EXISTS image_height INTEGER;
