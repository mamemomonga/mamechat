-- サービス全体の設定を格納するキー・バリューストア（1行1設定）。
-- 管理画面のサーバ設定から編集する。現状のキー:
--   server_overview   … サーバ概要（Markdown）
--   header_image_path … サービスヘッダ画像の相対パス（uploads ストア内）。未設定=画像なし。
CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
