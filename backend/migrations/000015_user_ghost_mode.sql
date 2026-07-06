-- ゴーストモード（管理者・オーナー向け）。有効時はどのチャンネルにも入室できるが
-- 書き込みはできない。既定は無効。
ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS ghost_mode BOOLEAN NOT NULL DEFAULT FALSE;
