CREATE TABLE IF NOT EXISTS user_settings (
  user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  tts_voicevox_speaker_uuid TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE chat_messages
  ADD COLUMN IF NOT EXISTS user_tts_voicevox_speaker_uuid TEXT,
  ADD COLUMN IF NOT EXISTS user_tts_voicevox_speaker_name TEXT,
  ADD COLUMN IF NOT EXISTS user_tts_voicevox_speaker_url TEXT;

ALTER TABLE channel_visitors
  ADD COLUMN IF NOT EXISTS tts_voicevox_speaker_uuid TEXT,
  ADD COLUMN IF NOT EXISTS tts_voicevox_speaker_name TEXT,
  ADD COLUMN IF NOT EXISTS tts_voicevox_speaker_url TEXT;
