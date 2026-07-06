package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const createChatMessage = `-- name: CreateChatMessage :one
INSERT INTO chat_messages (
  channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url,
  image_path, image_width, image_height
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height
`

type CreateChatMessageParams struct {
	ChannelID                  int64       `json:"channel_id"`
	UserID                     int64       `json:"user_id"`
	Body                       string      `json:"body"`
	UserDisplayName            string      `json:"user_display_name"`
	UserHandle                 pgtype.Text `json:"user_handle"`
	UserAvatarUrl              pgtype.Text `json:"user_avatar_url"`
	UserProvider               pgtype.Text `json:"user_provider"`
	UserTtsVoicevoxSpeakerUuid pgtype.Text `json:"user_tts_voicevox_speaker_uuid"`
	UserTtsVoicevoxSpeakerName pgtype.Text `json:"user_tts_voicevox_speaker_name"`
	UserTtsVoicevoxSpeakerUrl  pgtype.Text `json:"user_tts_voicevox_speaker_url"`
	ImagePath                  pgtype.Text `json:"image_path"`
	ImageWidth                 pgtype.Int4 `json:"image_width"`
	ImageHeight                pgtype.Int4 `json:"image_height"`
}

func (q *Queries) CreateChatMessage(ctx context.Context, arg CreateChatMessageParams) (ChatMessage, error) {
	row := q.db.QueryRow(ctx, createChatMessage,
		arg.ChannelID,
		arg.UserID,
		arg.Body,
		arg.UserDisplayName,
		arg.UserHandle,
		arg.UserAvatarUrl,
		arg.UserProvider,
		arg.UserTtsVoicevoxSpeakerUuid,
		arg.UserTtsVoicevoxSpeakerName,
		arg.UserTtsVoicevoxSpeakerUrl,
		arg.ImagePath,
		arg.ImageWidth,
		arg.ImageHeight,
	)
	var i ChatMessage
	err := row.Scan(
		&i.ID,
		&i.ChannelID,
		&i.UserID,
		&i.Body,
		&i.UserDisplayName,
		&i.UserHandle,
		&i.UserAvatarUrl,
		&i.UserProvider,
		&i.UserTtsVoicevoxSpeakerUuid,
		&i.UserTtsVoicevoxSpeakerName,
		&i.UserTtsVoicevoxSpeakerUrl,
		&i.CreatedAt,
		&i.ImagePath,
		&i.ImageWidth,
		&i.ImageHeight,
	)
	return i, err
}

const listChatMessagesAfterID = `-- name: ListChatMessagesAfterID :many
SELECT id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height
FROM chat_messages
WHERE channel_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3
`

type ListChatMessagesAfterIDParams struct {
	ChannelID int64 `json:"channel_id"`
	ID        int64 `json:"id"`
	Limit     int32 `json:"limit"`
}

func (q *Queries) ListChatMessagesAfterID(ctx context.Context, arg ListChatMessagesAfterIDParams) ([]ChatMessage, error) {
	rows, err := q.db.Query(ctx, listChatMessagesAfterID, arg.ChannelID, arg.ID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ChatMessage
	for rows.Next() {
		var i ChatMessage
		if err := rows.Scan(
			&i.ID,
			&i.ChannelID,
			&i.UserID,
			&i.Body,
			&i.UserDisplayName,
			&i.UserHandle,
			&i.UserAvatarUrl,
			&i.UserProvider,
			&i.UserTtsVoicevoxSpeakerUuid,
			&i.UserTtsVoicevoxSpeakerName,
			&i.UserTtsVoicevoxSpeakerUrl,
			&i.CreatedAt,
			&i.ImagePath,
			&i.ImageWidth,
			&i.ImageHeight,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const listRecentChatMessagesByChannel = `-- name: ListRecentChatMessagesByChannel :many
SELECT id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height
FROM chat_messages
WHERE channel_id = $1
ORDER BY created_at DESC
LIMIT $2
`

type ListRecentChatMessagesByChannelParams struct {
	ChannelID int64 `json:"channel_id"`
	Limit     int32 `json:"limit"`
}

func (q *Queries) ListRecentChatMessagesByChannel(ctx context.Context, arg ListRecentChatMessagesByChannelParams) ([]ChatMessage, error) {
	rows, err := q.db.Query(ctx, listRecentChatMessagesByChannel, arg.ChannelID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ChatMessage
	for rows.Next() {
		var i ChatMessage
		if err := rows.Scan(
			&i.ID,
			&i.ChannelID,
			&i.UserID,
			&i.Body,
			&i.UserDisplayName,
			&i.UserHandle,
			&i.UserAvatarUrl,
			&i.UserProvider,
			&i.UserTtsVoicevoxSpeakerUuid,
			&i.UserTtsVoicevoxSpeakerName,
			&i.UserTtsVoicevoxSpeakerUrl,
			&i.CreatedAt,
			&i.ImagePath,
			&i.ImageWidth,
			&i.ImageHeight,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const getChatMessageByID = `-- name: GetChatMessageByID :one
SELECT id, channel_id, user_id
FROM chat_messages
WHERE id = $1
`

type GetChatMessageByIDRow struct {
	ID        int64 `json:"id"`
	ChannelID int64 `json:"channel_id"`
	UserID    int64 `json:"user_id"`
}

func (q *Queries) GetChatMessageByID(ctx context.Context, id int64) (GetChatMessageByIDRow, error) {
	row := q.db.QueryRow(ctx, getChatMessageByID, id)
	var i GetChatMessageByIDRow
	err := row.Scan(&i.ID, &i.ChannelID, &i.UserID)
	return i, err
}

const getChatMessageForTTS = `-- name: GetChatMessageForTTS :one
SELECT channel_id, body, user_tts_voicevox_speaker_uuid
FROM chat_messages
WHERE id = $1
`

type GetChatMessageForTTSRow struct {
	ChannelID                  int64       `json:"channel_id"`
	Body                       string      `json:"body"`
	UserTtsVoicevoxSpeakerUuid pgtype.Text `json:"user_tts_voicevox_speaker_uuid"`
}

func (q *Queries) GetChatMessageForTTS(ctx context.Context, id int64) (GetChatMessageForTTSRow, error) {
	row := q.db.QueryRow(ctx, getChatMessageForTTS, id)
	var i GetChatMessageForTTSRow
	err := row.Scan(&i.ChannelID, &i.Body, &i.UserTtsVoicevoxSpeakerUuid)
	return i, err
}

const deleteChatMessageByID = `-- name: DeleteChatMessageByID :one
DELETE FROM chat_messages
USING channels
WHERE chat_messages.id = $1
  AND channels.id = chat_messages.channel_id
RETURNING
  chat_messages.id,
  chat_messages.channel_id,
  chat_messages.user_id,
  chat_messages.body,
  chat_messages.user_display_name,
  chat_messages.user_handle,
  chat_messages.user_avatar_url,
  chat_messages.user_provider,
  chat_messages.user_tts_voicevox_speaker_uuid,
  chat_messages.user_tts_voicevox_speaker_name,
  chat_messages.user_tts_voicevox_speaker_url,
  chat_messages.created_at,
  chat_messages.image_path,
  chat_messages.image_width,
  chat_messages.image_height,
  channels.slug AS channel_slug
`

type DeleteChatMessageByIDRow struct {
	ID                         int64       `json:"id"`
	ChannelID                  int64       `json:"channel_id"`
	UserID                     int64       `json:"user_id"`
	Body                       string      `json:"body"`
	UserDisplayName            string      `json:"user_display_name"`
	UserHandle                 pgtype.Text `json:"user_handle"`
	UserAvatarUrl              pgtype.Text `json:"user_avatar_url"`
	UserProvider               pgtype.Text `json:"user_provider"`
	UserTtsVoicevoxSpeakerUuid pgtype.Text `json:"user_tts_voicevox_speaker_uuid"`
	UserTtsVoicevoxSpeakerName pgtype.Text `json:"user_tts_voicevox_speaker_name"`
	UserTtsVoicevoxSpeakerUrl  pgtype.Text `json:"user_tts_voicevox_speaker_url"`
	CreatedAt                  time.Time   `json:"created_at"`
	ImagePath                  pgtype.Text `json:"image_path"`
	ImageWidth                 pgtype.Int4 `json:"image_width"`
	ImageHeight                pgtype.Int4 `json:"image_height"`
	ChannelSlug                string      `json:"channel_slug"`
}

func (q *Queries) DeleteChatMessageByID(ctx context.Context, id int64) (DeleteChatMessageByIDRow, error) {
	row := q.db.QueryRow(ctx, deleteChatMessageByID, id)
	var i DeleteChatMessageByIDRow
	err := row.Scan(
		&i.ID,
		&i.ChannelID,
		&i.UserID,
		&i.Body,
		&i.UserDisplayName,
		&i.UserHandle,
		&i.UserAvatarUrl,
		&i.UserProvider,
		&i.UserTtsVoicevoxSpeakerUuid,
		&i.UserTtsVoicevoxSpeakerName,
		&i.UserTtsVoicevoxSpeakerUrl,
		&i.CreatedAt,
		&i.ImagePath,
		&i.ImageWidth,
		&i.ImageHeight,
		&i.ChannelSlug,
	)
	return i, err
}

const deleteExpiredMessages = `-- name: DeleteExpiredMessages :many
DELETE FROM chat_messages m
USING channels c
WHERE m.channel_id = c.id
  AND m.created_at < now() - (c.post_ttl_hours * interval '1 hour')
RETURNING m.image_path
`

func (q *Queries) DeleteExpiredMessages(ctx context.Context) ([]pgtype.Text, error) {
	rows, err := q.db.Query(ctx, deleteExpiredMessages)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []pgtype.Text
	for rows.Next() {
		var image_path pgtype.Text
		if err := rows.Scan(&image_path); err != nil {
			return nil, err
		}
		items = append(items, image_path)
	}
	return items, rows.Err()
}
