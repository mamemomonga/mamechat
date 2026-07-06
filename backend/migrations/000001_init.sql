CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  display_name TEXT NOT NULL,
  handle TEXT,
  avatar_url TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  role TEXT NOT NULL DEFAULT 'user',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';

CREATE UNIQUE INDEX IF NOT EXISTS users_single_owner_idx
  ON users ((role))
  WHERE role = 'owner';

CREATE TABLE IF NOT EXISTS auth_identities (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  subject TEXT NOT NULL,
  instance_url TEXT,
  handle TEXT,
  display_name TEXT,
  avatar_url TEXT,
  profile_url TEXT,
  raw_profile JSONB,
  last_verified_at TIMESTAMPTZ,
  verification_expires_at TIMESTAMPTZ,
  last_profile_sync_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE TABLE IF NOT EXISTS atproto_oauth_states (
  state TEXT PRIMARY KEY,
  issuer TEXT NOT NULL,
  auth_server_issuer TEXT NOT NULL,
  authorization_endpoint TEXT NOT NULL,
  token_endpoint TEXT NOT NULL,
  pushed_authorization_request_endpoint TEXT NOT NULL,
  code_verifier TEXT NOT NULL,
  dpop_private_jwk JSONB NOT NULL,
  dpop_nonce TEXT,
  login_hint TEXT,
  expected_did TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE atproto_oauth_states
  ADD COLUMN IF NOT EXISTS expected_did TEXT;

CREATE INDEX IF NOT EXISTS atproto_oauth_states_expires_idx
  ON atproto_oauth_states (expires_at);

UPDATE users
SET role = 'owner'
WHERE role = 'user'
  AND id IN (
    SELECT user_id
    FROM auth_identities
    WHERE provider = 'admin' AND subject = 'admin'
  );

UPDATE auth_identities
SET provider = 'owner',
    subject = 'owner'
WHERE provider = 'admin' AND subject = 'admin';

CREATE TABLE IF NOT EXISTS sessions (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ,
  user_agent TEXT,
  ip_prefix TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS channels (
  id BIGSERIAL PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  description TEXT,
  owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
  suspended_at TIMESTAMPTZ,
  suspend_retention_hours INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_messages (
  id BIGSERIAL PRIMARY KEY,
  channel_id BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body TEXT NOT NULL,
  user_display_name TEXT NOT NULL,
  user_handle TEXT,
  user_avatar_url TEXT,
  user_provider TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS chat_messages_channel_created_idx
  ON chat_messages (channel_id, created_at DESC);

CREATE INDEX IF NOT EXISTS sessions_token_active_idx
  ON sessions (token_hash, expires_at)
  WHERE revoked_at IS NULL;
