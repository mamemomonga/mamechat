-- ユーザーごとのDeck（複数カラム）レイアウトの保存先。1ユーザー1スロット、手動保存のみ。
-- 列の並びを表すJSON配列（[{"type":"list"} | {"type":"channel","slug":"..."}]）を格納する。NULL=未保存。
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS deck_layout JSONB;
