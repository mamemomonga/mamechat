CREATE TABLE IF NOT EXISTS mastodon_app_registrations (
  id BIGSERIAL PRIMARY KEY,
  instance_url TEXT NOT NULL UNIQUE,
  client_id TEXT NOT NULL,
  client_secret TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mastodon_oauth_states (
  state TEXT PRIMARY KEY,
  instance_url TEXT NOT NULL,
  code_verifier TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
