CREATE TABLE IF NOT EXISTS tts_assets (
  content_hash TEXT PRIMARY KEY,
  file_path TEXT NOT NULL,
  file_size_bytes BIGINT NOT NULL,
  duration_ms INTEGER,
  text_preview TEXT,
  text_length INTEGER NOT NULL,
  speaker_id INTEGER NOT NULL,
  speaker_name TEXT NOT NULL,
  speaker_style_name TEXT,
  speed_scale DOUBLE PRECISION NOT NULL,
  pitch_scale DOUBLE PRECISION NOT NULL,
  intonation_scale DOUBLE PRECISION NOT NULL,
  volume_scale DOUBLE PRECISION NOT NULL,
  pre_phoneme_length DOUBLE PRECISION NOT NULL,
  post_phoneme_length DOUBLE PRECISION NOT NULL,
  voicevox_engine_version TEXT NOT NULL,
  format TEXT NOT NULL,
  codec TEXT NOT NULL,
  bitrate INTEGER NOT NULL,
  channels INTEGER NOT NULL,
  normalizer_version TEXT NOT NULL,
  splitter_version TEXT NOT NULL,
  use_count BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  marked_for_delete_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tts_jobs (
  id TEXT PRIMARY KEY,
  channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  message_id BIGINT NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  speaker_id INTEGER NOT NULL,
  text_preview TEXT,
  text_length INTEGER NOT NULL,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS tts_jobs_channel_status_created_idx
  ON tts_jobs (channel_id, status, created_at);

CREATE INDEX IF NOT EXISTS tts_jobs_content_hash_idx
  ON tts_jobs (content_hash);

CREATE TABLE IF NOT EXISTS tts_message_parts (
  id TEXT PRIMARY KEY,
  channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  message_id BIGINT NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
  content_hash TEXT NOT NULL REFERENCES tts_assets(content_hash),
  part_index INTEGER NOT NULL,
  text_preview TEXT,
  text_length INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (message_id, part_index)
);
