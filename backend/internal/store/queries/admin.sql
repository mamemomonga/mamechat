-- name: GetAdminStats :one
SELECT
  (SELECT count(*) FROM users) AS users_count,
  (SELECT count(*) FROM channels) AS channels_count,
  (SELECT count(*) FROM chat_messages) AS chat_messages_count,
  (SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND expires_at > now()) AS active_sessions_count;

-- name: ListActiveSessions :many
SELECT
  s.id,
  s.user_id,
  u.display_name AS user_display_name,
  u.handle AS user_handle,
  s.expires_at,
  s.last_seen_at,
  s.user_agent,
  s.ip_prefix,
  s.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.revoked_at IS NULL
  AND s.expires_at > now()
ORDER BY COALESCE(s.last_seen_at, s.created_at) DESC;

-- name: RevokeSessionByID :one
UPDATE sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at;

-- name: ListAdminUsers :many
SELECT
  u.id,
  u.display_name,
  u.handle,
  u.avatar_url,
  u.status,
  u.role,
  u.created_at,
  u.updated_at,
  COALESCE(ai.provider, '') AS provider,
  COALESCE(ai.subject, '') AS subject
FROM users u
LEFT JOIN LATERAL (
  SELECT provider, subject
  FROM auth_identities
  WHERE user_id = u.id
  ORDER BY created_at ASC
  LIMIT 1
) ai ON true
ORDER BY u.created_at DESC;

-- name: UpdateUser :one
UPDATE users
SET display_name = $2,
    handle = $3,
    avatar_url = $4,
    status = $5,
    role = $6,
    updated_at = now()
WHERE id = $1
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = $1;

-- name: DeleteChannelBySlug :exec
DELETE FROM channels
WHERE slug = $1;
