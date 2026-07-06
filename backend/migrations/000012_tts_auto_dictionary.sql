CREATE TABLE IF NOT EXISTS tts_auto_dictionary_entries (
  term_key TEXT PRIMARY KEY,
  term TEXT NOT NULL,
  reading TEXT NOT NULL,
  registered_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  registered_by_handle TEXT,
  registered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tts_auto_dictionary_entries_registered_at_idx
  ON tts_auto_dictionary_entries (registered_at DESC);
