package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const getAdminStats = `-- name: GetAdminStats :one
SELECT
  (SELECT count(*) FROM users) AS users_count,
  (SELECT count(*) FROM channels) AS channels_count,
  (SELECT count(*) FROM chat_messages) AS chat_messages_count,
  (SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND expires_at > now()) AS active_sessions_count
`

type GetAdminStatsRow struct {
	UsersCount          int64 `json:"users_count"`
	ChannelsCount       int64 `json:"channels_count"`
	ChatMessagesCount   int64 `json:"chat_messages_count"`
	ActiveSessionsCount int64 `json:"active_sessions_count"`
}

func (q *Queries) GetAdminStats(ctx context.Context) (GetAdminStatsRow, error) {
	row := q.db.QueryRow(ctx, getAdminStats)
	var i GetAdminStatsRow
	err := row.Scan(&i.UsersCount, &i.ChannelsCount, &i.ChatMessagesCount, &i.ActiveSessionsCount)
	return i, err
}

const listActiveSessions = `-- name: ListActiveSessions :many
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
ORDER BY COALESCE(s.last_seen_at, s.created_at) DESC
`

type ListActiveSessionsRow struct {
	ID              int64              `json:"id"`
	UserID          int64              `json:"user_id"`
	UserDisplayName string             `json:"user_display_name"`
	UserHandle      pgtype.Text        `json:"user_handle"`
	ExpiresAt       time.Time          `json:"expires_at"`
	LastSeenAt      pgtype.Timestamptz `json:"last_seen_at"`
	UserAgent       pgtype.Text        `json:"user_agent"`
	IpPrefix        pgtype.Text        `json:"ip_prefix"`
	CreatedAt       time.Time          `json:"created_at"`
}

func (q *Queries) ListActiveSessions(ctx context.Context) ([]ListActiveSessionsRow, error) {
	rows, err := q.db.Query(ctx, listActiveSessions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListActiveSessionsRow
	for rows.Next() {
		var i ListActiveSessionsRow
		if err := rows.Scan(&i.ID, &i.UserID, &i.UserDisplayName, &i.UserHandle, &i.ExpiresAt, &i.LastSeenAt, &i.UserAgent, &i.IpPrefix, &i.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const revokeSessionByID = `-- name: RevokeSessionByID :one
UPDATE sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL
RETURNING id, user_id, token_hash, expires_at, last_seen_at, user_agent, ip_prefix, created_at, revoked_at
`

func (q *Queries) RevokeSessionByID(ctx context.Context, id int64) (Session, error) {
	row := q.db.QueryRow(ctx, revokeSessionByID, id)
	var i Session
	err := row.Scan(&i.ID, &i.UserID, &i.TokenHash, &i.ExpiresAt, &i.LastSeenAt, &i.UserAgent, &i.IpPrefix, &i.CreatedAt, &i.RevokedAt)
	return i, err
}

const listAdminUsers = `-- name: ListAdminUsers :many
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
ORDER BY u.created_at DESC
`

type ListAdminUsersRow struct {
	ID          int64       `json:"id"`
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	Status      string      `json:"status"`
	Role        string      `json:"role"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Provider    string      `json:"provider"`
	Subject     string      `json:"subject"`
}

func (q *Queries) ListAdminUsers(ctx context.Context) ([]ListAdminUsersRow, error) {
	rows, err := q.db.Query(ctx, listAdminUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListAdminUsersRow
	for rows.Next() {
		var i ListAdminUsersRow
		if err := rows.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt, &i.Provider, &i.Subject); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const updateUser = `-- name: UpdateUser :one
UPDATE users
SET display_name = $2,
    handle = $3,
    avatar_url = $4,
    status = $5,
    role = $6,
    updated_at = now()
WHERE id = $1
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at
`

type UpdateUserParams struct {
	ID          int64       `json:"id"`
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	Status      string      `json:"status"`
	Role        string      `json:"role"`
}

func (q *Queries) UpdateUser(ctx context.Context, arg UpdateUserParams) (User, error) {
	row := q.db.QueryRow(ctx, updateUser, arg.ID, arg.DisplayName, arg.Handle, arg.AvatarUrl, arg.Status, arg.Role)
	var i User
	err := row.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const deleteUser = `-- name: DeleteUser :exec
DELETE FROM users
WHERE id = $1
`

func (q *Queries) DeleteUser(ctx context.Context, id int64) error {
	_, err := q.db.Exec(ctx, deleteUser, id)
	return err
}

const deleteChannelBySlug = `-- name: DeleteChannelBySlug :exec
DELETE FROM channels
WHERE slug = $1
`

func (q *Queries) DeleteChannelBySlug(ctx context.Context, slug string) error {
	_, err := q.db.Exec(ctx, deleteChannelBySlug, slug)
	return err
}
