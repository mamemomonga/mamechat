-- name: UpsertChannelVisitorSeen :exec
INSERT INTO channel_visitors (
  channel_id, user_id, display_name, handle, avatar_url, provider,
  tts_voicevox_speaker_uuid, tts_voicevox_speaker_name, tts_voicevox_speaker_url
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (channel_id, user_id) DO UPDATE SET
  last_seen_at = now(),
  display_name = EXCLUDED.display_name,
  handle = EXCLUDED.handle,
  avatar_url = EXCLUDED.avatar_url,
  provider = EXCLUDED.provider,
  tts_voicevox_speaker_uuid = EXCLUDED.tts_voicevox_speaker_uuid,
  tts_voicevox_speaker_name = EXCLUDED.tts_voicevox_speaker_name,
  tts_voicevox_speaker_url = EXCLUDED.tts_voicevox_speaker_url;

-- name: MarkChannelVisitorPosted :exec
INSERT INTO channel_visitors (
  channel_id, user_id, display_name, handle, avatar_url, provider,
  tts_voicevox_speaker_uuid, tts_voicevox_speaker_name, tts_voicevox_speaker_url, last_post_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (channel_id, user_id) DO UPDATE SET
  last_seen_at = now(),
  last_post_at = now(),
  display_name = EXCLUDED.display_name,
  handle = EXCLUDED.handle,
  avatar_url = EXCLUDED.avatar_url,
  provider = EXCLUDED.provider,
  tts_voicevox_speaker_uuid = EXCLUDED.tts_voicevox_speaker_uuid,
  tts_voicevox_speaker_name = EXCLUDED.tts_voicevox_speaker_name,
  tts_voicevox_speaker_url = EXCLUDED.tts_voicevox_speaker_url;

-- name: CountChannelVisitors :one
SELECT COUNT(*)::int AS total
FROM channel_visitors
WHERE channel_id = $1;

-- name: ListChannelVisitors :many
SELECT user_id, display_name, handle, avatar_url, provider,
  tts_voicevox_speaker_uuid, tts_voicevox_speaker_name, tts_voicevox_speaker_url
FROM channel_visitors
WHERE channel_id = $1
ORDER BY last_post_at DESC NULLS LAST, last_seen_at DESC
LIMIT $2;

-- name: GetChannelVisitor :one
SELECT user_id, display_name, handle, avatar_url, provider,
  tts_voicevox_speaker_uuid, tts_voicevox_speaker_name, tts_voicevox_speaker_url
FROM channel_visitors
WHERE channel_id = $1 AND user_id = $2;
