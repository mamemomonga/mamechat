-- name: CreateMisskeyMiAuthSession :one
INSERT INTO misskey_miauth_sessions (session, instance_url, expires_at)
VALUES ($1, $2, $3)
RETURNING session, instance_url, expires_at, created_at;

-- name: GetMisskeyMiAuthSession :one
SELECT session, instance_url, expires_at, created_at
FROM misskey_miauth_sessions
WHERE session = $1;

-- name: DeleteMisskeyMiAuthSession :exec
DELETE FROM misskey_miauth_sessions
WHERE session = $1;

-- name: DeleteExpiredMisskeyMiAuthSessions :exec
DELETE FROM misskey_miauth_sessions
WHERE expires_at < now();
