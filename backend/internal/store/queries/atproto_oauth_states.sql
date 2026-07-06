-- name: CreateAtprotoOAuthState :one
INSERT INTO atproto_oauth_states (
  state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, created_at, expires_at;

-- name: GetAtprotoOAuthState :one
SELECT state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, created_at, expires_at
FROM atproto_oauth_states
WHERE state = $1;

-- name: DeleteAtprotoOAuthState :exec
DELETE FROM atproto_oauth_states
WHERE state = $1;

-- name: DeleteExpiredAtprotoOAuthStates :exec
DELETE FROM atproto_oauth_states
WHERE expires_at < now();
