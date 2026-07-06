-- operating_paused_remaining_seconds はオーナー退出中に凍結した営業残り時間（秒）。
-- 非NULL=一時停止中（このときは operating_deadline が NULL になる）。オーナーが戻ると
-- operating_deadline = now() + この秒数 で再開し、本カラムは NULL に戻す。再起動や全員退出を
-- またいでも一時停止状態を維持するためにDBへ永続化する。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS operating_paused_remaining_seconds INTEGER;
