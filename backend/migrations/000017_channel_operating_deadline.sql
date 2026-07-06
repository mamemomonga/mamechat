-- 営業セッションの終了予定時刻。
-- NULL=営業していない（準備中）。値あり=その時刻まで営業中で、到達すると自動で準備中へ移行する。
-- サーバ再起動後もカウントダウンを復元できるよう、メモリではなくDBに保持する。
-- 新規チャンネルは「準備中」で開始する（CreateChannel が suspended_at = now() を設定する）。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS operating_deadline TIMESTAMPTZ;
