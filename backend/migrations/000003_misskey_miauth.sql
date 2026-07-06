CREATE TABLE IF NOT EXISTS misskey_miauth_sessions (
  session TEXT PRIMARY KEY,
  instance_url TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
