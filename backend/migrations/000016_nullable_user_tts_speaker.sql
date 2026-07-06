-- tts_voicevox_speaker_uuid が NULL の場合、そのユーザーの投稿は読み上げない。
-- 既存ユーザーの値は維持し、新規・明示設定では「使用しない」を表せるようにする。
ALTER TABLE user_settings
  ALTER COLUMN tts_voicevox_speaker_uuid DROP NOT NULL;
