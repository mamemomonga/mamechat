package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const createUser = `-- name: CreateUser :one
INSERT INTO users (display_name, handle, avatar_url)
VALUES ($1, $2, $3)
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at
`

type CreateUserParams struct {
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
}

func (q *Queries) CreateUser(ctx context.Context, arg CreateUserParams) (User, error) {
	row := q.db.QueryRow(ctx, createUser, arg.DisplayName, arg.Handle, arg.AvatarUrl)
	var i User
	err := row.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const createUserWithRole = `-- name: CreateUserWithRole :one
INSERT INTO users (display_name, handle, avatar_url, role)
VALUES ($1, $2, $3, $4)
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at
`

type CreateUserWithRoleParams struct {
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	Role        string      `json:"role"`
}

func (q *Queries) CreateUserWithRole(ctx context.Context, arg CreateUserWithRoleParams) (User, error) {
	row := q.db.QueryRow(ctx, createUserWithRole, arg.DisplayName, arg.Handle, arg.AvatarUrl, arg.Role)
	var i User
	err := row.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const getUserByID = `-- name: GetUserByID :one
SELECT id, display_name, handle, avatar_url, status, role, created_at, updated_at
FROM users
WHERE id = $1
`

func (q *Queries) GetUserByID(ctx context.Context, id int64) (User, error) {
	row := q.db.QueryRow(ctx, getUserByID, id)
	var i User
	err := row.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const createAuthIdentity = `-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (
  user_id, provider, subject, instance_url, handle, display_name, avatar_url, profile_url,
  raw_profile, last_verified_at, last_profile_sync_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
RETURNING id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
`

type CreateAuthIdentityParams struct {
	UserID      int64       `json:"user_id"`
	Provider    string      `json:"provider"`
	Subject     string      `json:"subject"`
	InstanceUrl pgtype.Text `json:"instance_url"`
	Handle      pgtype.Text `json:"handle"`
	DisplayName pgtype.Text `json:"display_name"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	ProfileUrl  pgtype.Text `json:"profile_url"`
	RawProfile  []byte      `json:"raw_profile"`
}

func (q *Queries) CreateAuthIdentity(ctx context.Context, arg CreateAuthIdentityParams) (AuthIdentity, error) {
	row := q.db.QueryRow(ctx, createAuthIdentity,
		arg.UserID, arg.Provider, arg.Subject, arg.InstanceUrl, arg.Handle, arg.DisplayName,
		arg.AvatarUrl, arg.ProfileUrl, arg.RawProfile,
	)
	var i AuthIdentity
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
		&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
		&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const getAuthIdentityByProviderSubject = `-- name: GetAuthIdentityByProviderSubject :one
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = $1 AND subject = $2
`

type GetAuthIdentityByProviderSubjectParams struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

func (q *Queries) GetAuthIdentityByProviderSubject(ctx context.Context, arg GetAuthIdentityByProviderSubjectParams) (AuthIdentity, error) {
	row := q.db.QueryRow(ctx, getAuthIdentityByProviderSubject, arg.Provider, arg.Subject)
	var i AuthIdentity
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
		&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
		&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const getPrimaryIdentityForUser = `-- name: GetPrimaryIdentityForUser :one
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE user_id = $1
ORDER BY created_at ASC
LIMIT 1
`

func (q *Queries) GetPrimaryIdentityForUser(ctx context.Context, userID int64) (AuthIdentity, error) {
	row := q.db.QueryRow(ctx, getPrimaryIdentityForUser, userID)
	var i AuthIdentity
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
		&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
		&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const updateUserProfile = `-- name: UpdateUserProfile :one
UPDATE users
SET display_name = $2,
    handle = $3,
    avatar_url = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, display_name, handle, avatar_url, status, role, created_at, updated_at
`

type UpdateUserProfileParams struct {
	ID          int64       `json:"id"`
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
}

func (q *Queries) UpdateUserProfile(ctx context.Context, arg UpdateUserProfileParams) (User, error) {
	row := q.db.QueryRow(ctx, updateUserProfile, arg.ID, arg.DisplayName, arg.Handle, arg.AvatarUrl)
	var i User
	err := row.Scan(&i.ID, &i.DisplayName, &i.Handle, &i.AvatarUrl, &i.Status, &i.Role, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const updateAuthIdentityProfileByProviderSubject = `-- name: UpdateAuthIdentityProfileByProviderSubject :one
UPDATE auth_identities
SET user_id = $3,
    handle = $4,
    display_name = $5,
    avatar_url = $6,
    profile_url = $7,
    raw_profile = $8,
    last_verified_at = CASE WHEN $9 THEN now() ELSE last_verified_at END,
    last_profile_sync_at = now(),
    updated_at = now()
WHERE provider = $1 AND subject = $2
RETURNING id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
`

type UpdateAuthIdentityProfileByProviderSubjectParams struct {
	Provider    string      `json:"provider"`
	Subject     string      `json:"subject"`
	UserID      int64       `json:"user_id"`
	Handle      pgtype.Text `json:"handle"`
	DisplayName pgtype.Text `json:"display_name"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	ProfileUrl  pgtype.Text `json:"profile_url"`
	RawProfile  []byte      `json:"raw_profile"`
	Verified    bool        `json:"verified"`
}

func (q *Queries) UpdateAuthIdentityProfileByProviderSubject(ctx context.Context, arg UpdateAuthIdentityProfileByProviderSubjectParams) (AuthIdentity, error) {
	row := q.db.QueryRow(ctx, updateAuthIdentityProfileByProviderSubject,
		arg.Provider, arg.Subject, arg.UserID, arg.Handle, arg.DisplayName,
		arg.AvatarUrl, arg.ProfileUrl, arg.RawProfile, arg.Verified,
	)
	var i AuthIdentity
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
		&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
		&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

const listStaleMisskeyIdentities = `-- name: ListStaleMisskeyIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'misskey'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2
`

type ListStaleMisskeyIdentitiesParams struct {
	StaleAfterHours int32 `json:"stale_after_hours"`
	Limit           int32 `json:"limit"`
}

func (q *Queries) ListStaleMisskeyIdentities(ctx context.Context, arg ListStaleMisskeyIdentitiesParams) ([]AuthIdentity, error) {
	rows, err := q.db.Query(ctx, listStaleMisskeyIdentities, arg.StaleAfterHours, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthIdentity{}
	for rows.Next() {
		var i AuthIdentity
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
			&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
			&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listStaleMastodonIdentities = `-- name: ListStaleMastodonIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'mastodon'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2
`

type ListStaleMastodonIdentitiesParams struct {
	StaleAfterHours int32 `json:"stale_after_hours"`
	Limit           int32 `json:"limit"`
}

func (q *Queries) ListStaleMastodonIdentities(ctx context.Context, arg ListStaleMastodonIdentitiesParams) ([]AuthIdentity, error) {
	rows, err := q.db.Query(ctx, listStaleMastodonIdentities, arg.StaleAfterHours, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthIdentity{}
	for rows.Next() {
		var i AuthIdentity
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
			&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
			&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listStaleAtprotoIdentities = `-- name: ListStaleAtprotoIdentities :many
SELECT id, user_id, provider, subject, instance_url, handle, display_name, avatar_url,
  profile_url, COALESCE(raw_profile, '{}'::jsonb) AS raw_profile, last_verified_at, verification_expires_at, last_profile_sync_at,
  created_at, updated_at
FROM auth_identities
WHERE provider = 'atproto'
  AND (last_profile_sync_at IS NULL OR last_profile_sync_at < now() - ($1::int * interval '1 hour'))
ORDER BY COALESCE(last_profile_sync_at, created_at) ASC
LIMIT $2
`

type ListStaleAtprotoIdentitiesParams struct {
	StaleAfterHours int32 `json:"stale_after_hours"`
	Limit           int32 `json:"limit"`
}

func (q *Queries) ListStaleAtprotoIdentities(ctx context.Context, arg ListStaleAtprotoIdentitiesParams) ([]AuthIdentity, error) {
	rows, err := q.db.Query(ctx, listStaleAtprotoIdentities, arg.StaleAfterHours, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AuthIdentity{}
	for rows.Next() {
		var i AuthIdentity
		if err := rows.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.InstanceUrl, &i.Handle,
			&i.DisplayName, &i.AvatarUrl, &i.ProfileUrl, &i.RawProfile, &i.LastVerifiedAt,
			&i.VerificationExpiresAt, &i.LastProfileSyncAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
