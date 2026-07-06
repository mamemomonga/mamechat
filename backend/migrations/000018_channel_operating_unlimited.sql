-- operating_unlimited は「時間制限なし」チャンネル。true のとき営業終了予定時刻を持たず、
-- カウントダウン・残り時間設定・オーナー退出による自動閉店を行わない（開店/閉店のみ）。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS operating_unlimited BOOLEAN NOT NULL DEFAULT false;
