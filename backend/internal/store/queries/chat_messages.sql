-- name: CreateChatMessage :one
INSERT INTO chat_messages (
  channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url,
  image_path, image_width, image_height
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height;

-- name: ListRecentChatMessagesByChannel :many
SELECT id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height
FROM chat_messages
WHERE channel_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListChatMessagesAfterID :many
SELECT id, channel_id, user_id, body, user_display_name, user_handle, user_avatar_url, user_provider,
  user_tts_voicevox_speaker_uuid, user_tts_voicevox_speaker_name, user_tts_voicevox_speaker_url, created_at,
  image_path, image_width, image_height
FROM chat_messages
WHERE channel_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: GetChatMessageByID :one
SELECT id, channel_id, user_id
FROM chat_messages
WHERE id = $1;

-- name: GetChatMessageForTTS :one
SELECT channel_id, body, user_tts_voicevox_speaker_uuid
FROM chat_messages
WHERE id = $1;

-- name: DeleteChatMessageByID :one
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
  channels.slug AS channel_slug;

-- name: DeleteExpiredMessages :many
-- 各チャンネルの post_ttl_hours を過ぎた投稿を削除し、画像パスを返す（ファイル削除用）。
DELETE FROM chat_messages m
USING channels c
WHERE m.channel_id = c.id
  AND m.created_at < now() - (c.post_ttl_hours * interval '1 hour')
RETURNING m.image_path;
