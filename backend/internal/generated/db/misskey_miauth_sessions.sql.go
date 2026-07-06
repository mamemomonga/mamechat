package db

import (
	"context"
	"time"
)

const createMisskeyMiAuthSession = `-- name: CreateMisskeyMiAuthSession :one
INSERT INTO misskey_miauth_sessions (session, instance_url, expires_at)
VALUES ($1, $2, $3)
RETURNING session, instance_url, expires_at, created_at
`

type CreateMisskeyMiAuthSessionParams struct {
	Session     string    `json:"session"`
	InstanceUrl string    `json:"instance_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (q *Queries) CreateMisskeyMiAuthSession(ctx context.Context, arg CreateMisskeyMiAuthSessionParams) (MisskeyMiauthSession, error) {
	row := q.db.QueryRow(ctx, createMisskeyMiAuthSession, arg.Session, arg.InstanceUrl, arg.ExpiresAt)
	var i MisskeyMiauthSession
	err := row.Scan(&i.Session, &i.InstanceUrl, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

const getMisskeyMiAuthSession = `-- name: GetMisskeyMiAuthSession :one
SELECT session, instance_url, expires_at, created_at
FROM misskey_miauth_sessions
WHERE session = $1
`

func (q *Queries) GetMisskeyMiAuthSession(ctx context.Context, session string) (MisskeyMiauthSession, error) {
	row := q.db.QueryRow(ctx, getMisskeyMiAuthSession, session)
	var i MisskeyMiauthSession
	err := row.Scan(&i.Session, &i.InstanceUrl, &i.ExpiresAt, &i.CreatedAt)
	return i, err
}

const deleteMisskeyMiAuthSession = `-- name: DeleteMisskeyMiAuthSession :exec
DELETE FROM misskey_miauth_sessions
WHERE session = $1
`

func (q *Queries) DeleteMisskeyMiAuthSession(ctx context.Context, session string) error {
	_, err := q.db.Exec(ctx, deleteMisskeyMiAuthSession, session)
	return err
}

const deleteExpiredMisskeyMiAuthSessions = `-- name: DeleteExpiredMisskeyMiAuthSessions :exec
DELETE FROM misskey_miauth_sessions
WHERE expires_at < now()
`

func (q *Queries) DeleteExpiredMisskeyMiAuthSessions(ctx context.Context) error {
	_, err := q.db.Exec(ctx, deleteExpiredMisskeyMiAuthSessions)
	return err
}
