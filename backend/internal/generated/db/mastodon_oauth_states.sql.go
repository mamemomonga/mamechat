package db

import (
	"context"
	"time"
)

const createMastodonOAuthState = `-- name: CreateMastodonOAuthState :one
INSERT INTO mastodon_oauth_states (state, instance_url, code_verifier, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING state, instance_url, code_verifier, expires_at, created_at
`

type CreateMastodonOAuthStateParams struct {
	State        string    `json:"state"`
	InstanceUrl  string    `json:"instance_url"`
	CodeVerifier string    `json:"code_verifier"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (q *Queries) CreateMastodonOAuthState(ctx context.Context, arg CreateMastodonOAuthStateParams) (MastodonOauthState, error) {
	row := q.db.QueryRow(ctx, createMastodonOAuthState, arg.State, arg.InstanceUrl, arg.CodeVerifier, arg.ExpiresAt)
	var i MastodonOauthState
	err := row.Scan(&i.State, &i.InstanceUrl, &i.CodeVerifier, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

const getMastodonOAuthState = `-- name: GetMastodonOAuthState :one
SELECT state, instance_url, code_verifier, expires_at, created_at
FROM mastodon_oauth_states
WHERE state = $1
`

func (q *Queries) GetMastodonOAuthState(ctx context.Context, state string) (MastodonOauthState, error) {
	row := q.db.QueryRow(ctx, getMastodonOAuthState, state)
	var i MastodonOauthState
	err := row.Scan(&i.State, &i.InstanceUrl, &i.CodeVerifier, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

const deleteMastodonOAuthState = `-- name: DeleteMastodonOAuthState :exec
DELETE FROM mastodon_oauth_states
WHERE state = $1
`

func (q *Queries) DeleteMastodonOAuthState(ctx context.Context, state string) error {
	_, err := q.db.Exec(ctx, deleteMastodonOAuthState, state)
	return err
}

const deleteExpiredMastodonOAuthStates = `-- name: DeleteExpiredMastodonOAuthStates :exec
DELETE FROM mastodon_oauth_states
WHERE expires_at < now()
`

func (q *Queries) DeleteExpiredMastodonOAuthStates(ctx context.Context) error {
	_, err := q.db.Exec(ctx, deleteExpiredMastodonOAuthStates)
	return err
}
