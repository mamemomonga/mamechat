-- name: GetUserSettings :one
SELECT user_id, tts_voicevox_speaker_uuid, created_at, updated_at, ghost_mode
FROM user_settings
WHERE user_id = $1;

-- name: SetUserSettingsGhostMode :exec
UPDATE user_settings
SET ghost_mode = $2, updated_at = now()
WHERE user_id = $1;

-- name: GetUserDeckLayout :one
SELECT deck_layout FROM user_settings WHERE user_id = $1;

-- name: SetUserDeckLayout :exec
INSERT INTO user_settings (user_id, deck_layout)
VALUES ($1, $2)
ON CONFLICT (user_id)
DO UPDATE SET deck_layout = EXCLUDED.deck_layout, updated_at = now();

-- name: UpsertUserSettingsTTSVoicevoxSpeaker :one
INSERT INTO user_settings (user_id, tts_voicevox_speaker_uuid)
VALUES ($1, sqlc.narg('tts_voicevox_speaker_uuid'))
ON CONFLICT (user_id)
DO UPDATE SET
  tts_voicevox_speaker_uuid = EXCLUDED.tts_voicevox_speaker_uuid,
  updated_at = now()
RETURNING user_id, tts_voicevox_speaker_uuid, created_at, updated_at, ghost_mode;
