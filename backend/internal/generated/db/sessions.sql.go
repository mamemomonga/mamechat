package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const createSession = `-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent, ip_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at
`

type CreateSessionParams struct {
	UserID    int64       `json:"user_id"`
	TokenHash string      `json:"token_hash"`
	ExpiresAt time.Time   `json:"expires_at"`
	UserAgent pgtype.Text `json:"user_agent"`
	IpPrefix  pgtype.Text `json:"ip_prefix"`
}

func (q *Queries) CreateSession(ctx context.Context, arg CreateSessionParams) (Session, error) {
	row := q.db.QueryRow(ctx, createSession, arg.UserID, arg.TokenHash, arg.ExpiresAt, arg.UserAgent, arg.IpPrefix)
	var i Session
	err := row.Scan(&i.ID, &i.UserID, &i.TokenHash, &i.ExpiresAt, &i.LastSeenAt, &i.UserAgent, &i.IpPrefix, &i.CreatedAt, &i.RevokedAt)
	return i, err
}

const getSessionByTokenHash = `-- name: GetSessionByTokenHash :one
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now()
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at
`

func (q *Queries) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	row := q.db.QueryRow(ctx, getSessionByTokenHash, tokenHash)
	var i Session
	err := row.Scan(&i.ID, &i.UserID, &i.TokenHash, &i.ExpiresAt, &i.LastSeenAt, &i.UserAgent, &i.IpPrefix, &i.CreatedAt, &i.RevokedAt)
	return i, err
}

const revokeSession = `-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL
`

func (q *Queries) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := q.db.Exec(ctx, revokeSession, tokenHash)
	return err
}

const revokeAllSessions = `-- name: RevokeAllSessions :exec
UPDATE sessions
SET revoked_at = now()
WHERE revoked_at IS NULL
`

func (q *Queries) RevokeAllSessions(ctx context.Context) error {
	_, err := q.db.Exec(ctx, revokeAllSessions)
	return err
}

const revokeSessionsForUser = `-- name: RevokeSessionsForUser :exec
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL
`

func (q *Queries) RevokeSessionsForUser(ctx context.Context, userID int64) error {
	_, err := q.db.Exec(ctx, revokeSessionsForUser, userID)
	return err
}
