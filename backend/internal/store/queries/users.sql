-- name: CreateUser :one
INSERT INTO users (display_name, handle, avatar_url)
VALUES ($1, $2, $3)
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at;

-- name: CreateUserWithRole :one
INSERT INTO users (display_name, handle, avatar_url, role)
VALUES ($1, $2, $3, $4)
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, display_name, handle, avatar_url, status, role, created_at, updated_at
FROM users
WHERE id = $1;

-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (
  user_id, provider, subject, instance_url, handle, display_name, avatar_url, profile_url,
  raw_profile, last_verified_at, last_profile_sync_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
RETURNING id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at;

-- name: GetAuthIdentityByProviderSubject :one
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = $1 AND subject = $2;

-- name: GetPrimaryIdentityForUser :one
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE user_id = $1
ORDER BY created_at ASC
LIMIT 1;

-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2,
    handle = $3,
    avatar_url = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at;

-- name: UpdateAuthIdentityProfileByProviderSubject :one
UPDATE auth_identities
SET user_id = $3,
    handle = $4,
    display_name = $5,
    avatar_url = $6,
    profile_url = $7,
    raw_profile = $8,
    last_verified_at = CASE WHEN $9 THEN now() ELSE last_verified_at END,
    last_profile_sync_at = now(),
    updated_at = now()
WHERE provider = $1 AND subject = $2
RETURNING id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at;

-- name: ListStaleMisskeyIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'misskey'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2;

-- name: ListStaleMastodonIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'mastodon'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2;

-- name: ListStaleAtprotoIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'atproto'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2;
