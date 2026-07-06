-- name: CreateMastodonOAuthState :one
INSERT INTO mastodon_oauth_states (state, instance_url, code_verifier, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING state, instance_url, code_verifier, expires_at, created_at;

-- name: GetMastodonOAuthState :one
SELECT state, instance_url, code_verifier, expires_at, created_at
FROM mastodon_oauth_states
WHERE state = $1;

-- name: DeleteMastodonOAuthState :exec
DELETE FROM mastodon_oauth_states
WHERE state = $1;

-- name: DeleteExpiredMastodonOAuthStates :exec
DELETE FROM mastodon_oauth_states
WHERE expires_at < now();
