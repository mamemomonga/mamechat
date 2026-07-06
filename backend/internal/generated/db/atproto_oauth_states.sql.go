package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const createAtprotoOAuthState = `-- name: CreateAtprotoOAuthState :one
INSERT INTO atproto_oauth_states (
  state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, created_at, expires_at
`

type CreateAtprotoOAuthStateParams struct {
	State                              string      `json:"state"`
	Issuer                             string      `json:"issuer"`
	AuthServerIssuer                   string      `json:"auth_server_issuer"`
	AuthorizationEndpoint              string      `json:"authorization_endpoint"`
	TokenEndpoint                      string      `json:"token_endpoint"`
	PushedAuthorizationRequestEndpoint string      `json:"pushed_authorization_request_endpoint"`
	CodeVerifier                       string      `json:"code_verifier"`
	DpopPrivateJwk                     []byte      `json:"dpop_private_jwk"`
	DpopNonce                          pgtype.Text `json:"dpop_nonce"`
	LoginHint                          pgtype.Text `json:"login_hint"`
	ExpectedDid                        pgtype.Text `json:"expected_did"`
	ExpiresAt                          time.Time   `json:"expires_at"`
}

func (q *Queries) CreateAtprotoOAuthState(ctx context.Context, arg CreateAtprotoOAuthStateParams) (AtprotoOauthState, error) {
	row := q.db.QueryRow(ctx, createAtprotoOAuthState,
		arg.State, arg.Issuer, arg.AuthServerIssuer, arg.AuthorizationEndpoint,
		arg.TokenEndpoint, arg.PushedAuthorizationRequestEndpoint, arg.CodeVerifier,
		arg.DpopPrivateJwk, arg.DpopNonce, arg.LoginHint, arg.ExpectedDid, arg.ExpiresAt,
	)
	var i AtprotoOauthState
	err := row.Scan(&i.State, &i.Issuer, &i.AuthServerIssuer, &i.AuthorizationEndpoint,
		&i.TokenEndpoint, &i.PushedAuthorizationRequestEndpoint, &i.CodeVerifier,
		&i.DpopPrivateJwk, &i.DpopNonce, &i.LoginHint, &i.ExpectedDid, &i.CreatedAt, &i.ExpiresAt)
	return i, err
}

const getAtprotoOAuthState = `-- name: GetAtprotoOAuthState :one
SELECT state, issuer, auth_server_issuer, authorization_endpoint, token_endpoint,
  pushed_authorization_request_endpoint, code_verifier, dpop_private_jwk,
  dpop_nonce, login_hint, expected_did, created_at, expires_at
FROM atproto_oauth_states
WHERE state = $1
`

func (q *Queries) GetAtprotoOAuthState(ctx context.Context, state string) (AtprotoOauthState, error) {
	row := q.db.QueryRow(ctx, getAtprotoOAuthState, state)
	var i AtprotoOauthState
	err := row.Scan(&i.State, &i.Issuer, &i.AuthServerIssuer, &i.AuthorizationEndpoint,
		&i.TokenEndpoint, &i.PushedAuthorizationRequestEndpoint, &i.CodeVerifier,
		&i.DpopPrivateJwk, &i.DpopNonce, &i.LoginHint, &i.ExpectedDid, &i.CreatedAt, &i.ExpiresAt)
	return i, err
}

const deleteAtprotoOAuthState = `-- name: DeleteAtprotoOAuthState :exec
DELETE FROM atproto_oauth_states
WHERE state = $1
`

func (q *Queries) DeleteAtprotoOAuthState(ctx context.Context, state string) error {
	_, err := q.db.Exec(ctx, deleteAtprotoOAuthState, state)
	return err
}

const deleteExpiredAtprotoOAuthStates = `-- name: DeleteExpiredAtprotoOAuthStates :exec
DELETE FROM atproto_oauth_states
WHERE expires_at < now()
`

func (q *Queries) DeleteExpiredAtprotoOAuthStates(ctx context.Context) error {
	_, err := q.db.Exec(ctx, deleteExpiredAtprotoOAuthStates)
	return err
}
