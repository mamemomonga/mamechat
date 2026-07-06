-- チャンネルごとのオーナー退出後サスペンドまでの猶予（秒）。
-- NULL=環境変数の既定値、負値=無期限（サスペンドしない）、0以上=その秒数。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS suspend_grace_seconds INTEGER;
