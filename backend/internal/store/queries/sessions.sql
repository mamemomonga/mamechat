-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent, ip_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at;

-- name: GetSessionByTokenHash :one
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now()
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessions :exec
UPDATE sessions
SET revoked_at = now()
WHERE revoked_at IS NULL;

-- name: RevokeSessionsForUser :exec
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;
