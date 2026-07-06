package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const createChannel = `-- name: CreateChannel :one
INSERT INTO channels (slug, title, description, owner_user_id, suspended_at)
VALUES ($1, $2, $3, $4, now())
RETURNING id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list
`

type CreateChannelParams struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description pgtype.Text `json:"description"`
	OwnerUserID pgtype.Int8 `json:"owner_user_id"`
}

func (q *Queries) CreateChannel(ctx context.Context, arg CreateChannelParams) (Channel, error) {
	row := q.db.QueryRow(ctx, createChannel, arg.Slug, arg.Title, arg.Description, arg.OwnerUserID)
	var i Channel
	err := row.Scan(&i.ID, &i.Slug, &i.Title, &i.Description, &i.OwnerUserID, &i.SuspendedAt, &i.SuspendRetentionHours, &i.SuspendGraceSeconds, &i.OperatingDeadline, &i.OperatingUnlimited, &i.PostTtlHours, &i.CreatedAt, &i.UpdatedAt, &i.UrlLinkifyEnabled, &i.ImageUploadEnabled, &i.AccessMode, &i.AccessList)
	return i, err
}

const listChannels = `-- name: ListChannels :many
SELECT id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list
FROM channels
ORDER BY created_at ASC
`

func (q *Queries) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := q.db.Query(ctx, listChannels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Channel
	for rows.Next() {
		var i Channel
		if err := rows.Scan(&i.ID, &i.Slug, &i.Title, &i.Description, &i.OwnerUserID, &i.SuspendedAt, &i.SuspendRetentionHours, &i.SuspendGraceSeconds, &i.OperatingDeadline, &i.OperatingUnlimited, &i.PostTtlHours, &i.CreatedAt, &i.UpdatedAt, &i.UrlLinkifyEnabled, &i.ImageUploadEnabled, &i.AccessMode, &i.AccessList); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const getChannelBySlug = `-- name: GetChannelBySlug :one
SELECT id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list
FROM channels
WHERE slug = $1
`

func (q *Queries) GetChannelBySlug(ctx context.Context, slug string) (Channel, error) {
	row := q.db.QueryRow(ctx, getChannelBySlug, slug)
	var i Channel
	err := row.Scan(&i.ID, &i.Slug, &i.Title, &i.Description, &i.OwnerUserID, &i.SuspendedAt, &i.SuspendRetentionHours, &i.SuspendGraceSeconds, &i.OperatingDeadline, &i.OperatingUnlimited, &i.PostTtlHours, &i.CreatedAt, &i.UpdatedAt, &i.UrlLinkifyEnabled, &i.ImageUploadEnabled, &i.AccessMode, &i.AccessList)
	return i, err
}

const getChannelOGPBySlug = `-- name: GetChannelOGPBySlug :one
SELECT
  c.slug,
  c.title,
  c.description,
  COALESCE(u.display_name, '') AS owner_display_name,
  u.avatar_url AS owner_avatar_url
FROM channels c
LEFT JOIN users u ON u.id = c.owner_user_id
WHERE c.slug = $1
`

type GetChannelOGPBySlugRow struct {
	Slug             string      `json:"slug"`
	Title            string      `json:"title"`
	Description      pgtype.Text `json:"description"`
	OwnerDisplayName string      `json:"owner_display_name"`
	OwnerAvatarUrl   pgtype.Text `json:"owner_avatar_url"`
}

func (q *Queries) GetChannelOGPBySlug(ctx context.Context, slug string) (GetChannelOGPBySlugRow, error) {
	row := q.db.QueryRow(ctx, getChannelOGPBySlug, slug)
	var i GetChannelOGPBySlugRow
	err := row.Scan(
		&i.Slug,
		&i.Title,
		&i.Description,
		&i.OwnerDisplayName,
		&i.OwnerAvatarUrl,
	)
	return i, err
}

const suspendChannelIfActive = `-- name: SuspendChannelIfActive :execrows
UPDATE channels
SET suspended_at = now(), operating_deadline = NULL,
    operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL
`

func (q *Queries) SuspendChannelIfActive(ctx context.Context, slug string) (int64, error) {
	result, err := q.db.Exec(ctx, suspendChannelIfActive, slug)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const resumeChannelIfSuspended = `-- name: ResumeChannelIfSuspended :execrows
UPDATE channels
SET suspended_at = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NOT NULL
`

func (q *Queries) ResumeChannelIfSuspended(ctx context.Context, slug string) (int64, error) {
	result, err := q.db.Exec(ctx, resumeChannelIfSuspended, slug)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const startChannelOperating = `-- name: StartChannelOperating :execrows
UPDATE channels
SET suspended_at = NULL, operating_deadline = $2,
    operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1
`

type StartChannelOperatingParams struct {
	Slug              string             `json:"slug"`
	OperatingDeadline pgtype.Timestamptz `json:"operating_deadline"`
}

func (q *Queries) StartChannelOperating(ctx context.Context, arg StartChannelOperatingParams) (int64, error) {
	result, err := q.db.Exec(ctx, startChannelOperating, arg.Slug, arg.OperatingDeadline)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const setChannelOperatingDeadline = `-- name: SetChannelOperatingDeadline :execrows
UPDATE channels
SET operating_deadline = $2, operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL
`

type SetChannelOperatingDeadlineParams struct {
	Slug              string             `json:"slug"`
	OperatingDeadline pgtype.Timestamptz `json:"operating_deadline"`
}

func (q *Queries) SetChannelOperatingDeadline(ctx context.Context, arg SetChannelOperatingDeadlineParams) (int64, error) {
	result, err := q.db.Exec(ctx, setChannelOperatingDeadline, arg.Slug, arg.OperatingDeadline)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const pauseChannelOperating = `-- name: PauseChannelOperating :execrows
UPDATE channels
SET operating_paused_remaining_seconds =
      GREATEST(0, CEIL(EXTRACT(EPOCH FROM (operating_deadline - now()))))::int,
    operating_deadline = NULL,
    updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL AND operating_deadline IS NOT NULL
  AND operating_unlimited = false AND operating_paused_remaining_seconds IS NULL
`

// PauseChannelOperating はオーナー退出時に営業残り時間を凍結する。
func (q *Queries) PauseChannelOperating(ctx context.Context, slug string) (int64, error) {
	result, err := q.db.Exec(ctx, pauseChannelOperating, slug)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const resumeChannelOperating = `-- name: ResumeChannelOperating :one
UPDATE channels
SET operating_deadline = now() + (operating_paused_remaining_seconds * interval '1 second'),
    operating_paused_remaining_seconds = NULL,
    updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL AND operating_paused_remaining_seconds IS NOT NULL
RETURNING operating_deadline
`

// ResumeChannelOperating はオーナー復帰時に凍結した残り時間で営業を再開する。
func (q *Queries) ResumeChannelOperating(ctx context.Context, slug string) (pgtype.Timestamptz, error) {
	row := q.db.QueryRow(ctx, resumeChannelOperating, slug)
	var operating_deadline pgtype.Timestamptz
	err := row.Scan(&operating_deadline)
	return operating_deadline, err
}

const getChannelOperatingPause = `-- name: GetChannelOperatingPause :one
SELECT operating_paused_remaining_seconds FROM channels WHERE slug = $1
`

// GetChannelOperatingPause は接続時に一時停止の残り秒数を取得する（NULL=一時停止なし）。
func (q *Queries) GetChannelOperatingPause(ctx context.Context, slug string) (pgtype.Int4, error) {
	row := q.db.QueryRow(ctx, getChannelOperatingPause, slug)
	var operating_paused_remaining_seconds pgtype.Int4
	err := row.Scan(&operating_paused_remaining_seconds)
	return operating_paused_remaining_seconds, err
}

const setChannelSuspendRetention = `-- name: SetChannelSuspendRetention :exec
UPDATE channels
SET suspend_retention_hours = $2, updated_at = now()
WHERE slug = $1
`

type SetChannelSuspendRetentionParams struct {
	Slug                  string      `json:"slug"`
	SuspendRetentionHours pgtype.Int4 `json:"suspend_retention_hours"`
}

func (q *Queries) SetChannelSuspendRetention(ctx context.Context, arg SetChannelSuspendRetentionParams) error {
	_, err := q.db.Exec(ctx, setChannelSuspendRetention, arg.Slug, arg.SuspendRetentionHours)
	return err
}

const setChannelSuspendGrace = `-- name: SetChannelSuspendGrace :exec
UPDATE channels
SET suspend_grace_seconds = $2, updated_at = now()
WHERE slug = $1
`

type SetChannelSuspendGraceParams struct {
	Slug                string      `json:"slug"`
	SuspendGraceSeconds pgtype.Int4 `json:"suspend_grace_seconds"`
}

func (q *Queries) SetChannelSuspendGrace(ctx context.Context, arg SetChannelSuspendGraceParams) error {
	_, err := q.db.Exec(ctx, setChannelSuspendGrace, arg.Slug, arg.SuspendGraceSeconds)
	return err
}

const setChannelPostTTL = `-- name: SetChannelPostTTL :exec
UPDATE channels
SET post_ttl_hours = $2, updated_at = now()
WHERE slug = $1
`

type SetChannelPostTTLParams struct {
	Slug         string `json:"slug"`
	PostTtlHours int32  `json:"post_ttl_hours"`
}

func (q *Queries) SetChannelPostTTL(ctx context.Context, arg SetChannelPostTTLParams) error {
	_, err := q.db.Exec(ctx, setChannelPostTTL, arg.Slug, arg.PostTtlHours)
	return err
}

const setChannelOperatingUnlimited = `-- name: SetChannelOperatingUnlimited :exec
UPDATE channels
SET operating_unlimited = $2,
    operating_deadline = CASE WHEN $2 THEN NULL ELSE operating_deadline END,
    operating_paused_remaining_seconds =
      CASE WHEN $2 THEN NULL ELSE operating_paused_remaining_seconds END,
    updated_at = now()
WHERE slug = $1
`

type SetChannelOperatingUnlimitedParams struct {
	Slug               string `json:"slug"`
	OperatingUnlimited bool   `json:"operating_unlimited"`
}

func (q *Queries) SetChannelOperatingUnlimited(ctx context.Context, arg SetChannelOperatingUnlimitedParams) error {
	_, err := q.db.Exec(ctx, setChannelOperatingUnlimited, arg.Slug, arg.OperatingUnlimited)
	return err
}

const setChannelFeatures = `-- name: SetChannelFeatures :exec
UPDATE channels
SET url_linkify_enabled = $2, image_upload_enabled = $3, updated_at = now()
WHERE slug = $1
`

type SetChannelFeaturesParams struct {
	Slug               string `json:"slug"`
	UrlLinkifyEnabled  bool   `json:"url_linkify_enabled"`
	ImageUploadEnabled bool   `json:"image_upload_enabled"`
}

func (q *Queries) SetChannelFeatures(ctx context.Context, arg SetChannelFeaturesParams) error {
	_, err := q.db.Exec(ctx, setChannelFeatures, arg.Slug, arg.UrlLinkifyEnabled, arg.ImageUploadEnabled)
	return err
}

const setChannelProfile = `-- name: SetChannelProfile :exec
UPDATE channels
SET title = $2, description = $3, updated_at = now()
WHERE slug = $1
`

type SetChannelProfileParams struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Description pgtype.Text `json:"description"`
}

func (q *Queries) SetChannelProfile(ctx context.Context, arg SetChannelProfileParams) error {
	_, err := q.db.Exec(ctx, setChannelProfile, arg.Slug, arg.Title, arg.Description)
	return err
}

const setChannelAccess = `-- name: SetChannelAccess :exec
UPDATE channels
SET access_mode = $2, access_list = $3, updated_at = now()
WHERE slug = $1
`

type SetChannelAccessParams struct {
	Slug       string `json:"slug"`
	AccessMode string `json:"access_mode"`
	AccessList []byte `json:"access_list"`
}

func (q *Queries) SetChannelAccess(ctx context.Context, arg SetChannelAccessParams) error {
	_, err := q.db.Exec(ctx, setChannelAccess, arg.Slug, arg.AccessMode, arg.AccessList)
	return err
}

const countChannelsOwnedByUser = `-- name: CountChannelsOwnedByUser :one
SELECT count(*) FROM channels WHERE owner_user_id = $1
`

func (q *Queries) CountChannelsOwnedByUser(ctx context.Context, ownerUserID pgtype.Int8) (int64, error) {
	row := q.db.QueryRow(ctx, countChannelsOwnedByUser, ownerUserID)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const listChannelImagePaths = `-- name: ListChannelImagePaths :many
SELECT image_path
FROM chat_messages
WHERE channel_id = $1 AND image_path IS NOT NULL AND image_path <> ''
`

func (q *Queries) ListChannelImagePaths(ctx context.Context, channelID int64) ([]pgtype.Text, error) {
	rows, err := q.db.Query(ctx, listChannelImagePaths, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []pgtype.Text
	for rows.Next() {
		var imagePath pgtype.Text
		if err := rows.Scan(&imagePath); err != nil {
			return nil, err
		}
		items = append(items, imagePath)
	}
	return items, rows.Err()
}

const listAllImagePaths = `-- name: ListAllImagePaths :many
SELECT image_path
FROM chat_messages
WHERE image_path IS NOT NULL AND image_path <> ''
`

func (q *Queries) ListAllImagePaths(ctx context.Context) ([]pgtype.Text, error) {
	rows, err := q.db.Query(ctx, listAllImagePaths)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []pgtype.Text
	for rows.Next() {
		var imagePath pgtype.Text
		if err := rows.Scan(&imagePath); err != nil {
			return nil, err
		}
		items = append(items, imagePath)
	}
	return items, rows.Err()
}

const deleteExpiredSuspendedChannels = `-- name: DeleteExpiredSuspendedChannels :many
DELETE FROM channels
WHERE suspended_at IS NOT NULL
  AND COALESCE(suspend_retention_hours, $1::int) >= 0
  AND suspended_at < now() - (COALESCE(suspend_retention_hours, $1::int) * interval '1 hour')
RETURNING slug
`

func (q *Queries) DeleteExpiredSuspendedChannels(ctx context.Context, defaultRetentionHours int32) ([]string, error) {
	rows, err := q.db.Query(ctx, deleteExpiredSuspendedChannels, defaultRetentionHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		items = append(items, slug)
	}
	return items, rows.Err()
}

const deleteChatMessagesByChannelID = `-- name: DeleteChatMessagesByChannelID :exec
DELETE FROM chat_messages
WHERE channel_id = $1
`

func (q *Queries) DeleteChatMessagesByChannelID(ctx context.Context, channelID int64) error {
	_, err := q.db.Exec(ctx, deleteChatMessagesByChannelID, channelID)
	return err
}

const listChannelLastPostTimes = `-- name: ListChannelLastPostTimes :many
SELECT c.slug, MAX(m.created_at)::timestamptz AS last_post_at
FROM channels c
LEFT JOIN chat_messages m ON m.channel_id = c.id
GROUP BY c.slug
`

type ListChannelLastPostTimesRow struct {
	Slug       string             `json:"slug"`
	LastPostAt pgtype.Timestamptz `json:"last_post_at"`
}

func (q *Queries) ListChannelLastPostTimes(ctx context.Context) ([]ListChannelLastPostTimesRow, error) {
	rows, err := q.db.Query(ctx, listChannelLastPostTimes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListChannelLastPostTimesRow
	for rows.Next() {
		var i ListChannelLastPostTimesRow
		if err := rows.Scan(&i.Slug, &i.LastPostAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
