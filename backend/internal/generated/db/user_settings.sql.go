package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const getUserSettings = `-- name: GetUserSettings :one
SELECT user_id, tts_voicevox_speaker_uuid, created_at, updated_at, ghost_mode
FROM user_settings
WHERE user_id = $1
`

func (q *Queries) GetUserSettings(ctx context.Context, userID int64) (UserSetting, error) {
	row := q.db.QueryRow(ctx, getUserSettings, userID)
	var i UserSetting
	err := row.Scan(&i.UserID, &i.TtsVoicevoxSpeakerUuid, &i.CreatedAt, &i.UpdatedAt, &i.GhostMode)
	return i, err
}

const getUserDeckLayout = `-- name: GetUserDeckLayout :one
SELECT deck_layout FROM user_settings WHERE user_id = $1
`

func (q *Queries) GetUserDeckLayout(ctx context.Context, userID int64) ([]byte, error) {
	row := q.db.QueryRow(ctx, getUserDeckLayout, userID)
	var deckLayout []byte
	err := row.Scan(&deckLayout)
	return deckLayout, err
}

const setUserDeckLayout = `-- name: SetUserDeckLayout :exec
INSERT INTO user_settings (user_id, deck_layout)
VALUES ($1, $2)
ON CONFLICT (user_id)
DO UPDATE SET deck_layout = EXCLUDED.deck_layout, updated_at = now()
`

type SetUserDeckLayoutParams struct {
	UserID     int64  `json:"user_id"`
	DeckLayout []byte `json:"deck_layout"`
}

func (q *Queries) SetUserDeckLayout(ctx context.Context, arg SetUserDeckLayoutParams) error {
	_, err := q.db.Exec(ctx, setUserDeckLayout, arg.UserID, arg.DeckLayout)
	return err
}

const setUserSettingsGhostMode = `-- name: SetUserSettingsGhostMode :exec
UPDATE user_settings
SET ghost_mode = $2, updated_at = now()
WHERE user_id = $1
`

type SetUserSettingsGhostModeParams struct {
	UserID    int64 `json:"user_id"`
	GhostMode bool  `json:"ghost_mode"`
}

func (q *Queries) SetUserSettingsGhostMode(ctx context.Context, arg SetUserSettingsGhostModeParams) error {
	_, err := q.db.Exec(ctx, setUserSettingsGhostMode, arg.UserID, arg.GhostMode)
	return err
}

const upsertUserSettingsTTSVoicevoxSpeaker = `-- name: UpsertUserSettingsTTSVoicevoxSpeaker :one
INSERT INTO user_settings (user_id, tts_voicevox_speaker_uuid)
VALUES ($1, $2)
ON CONFLICT (user_id)
DO UPDATE SET
  tts_voicevox_speaker_uuid = EXCLUDED.tts_voicevox_speaker_uuid,
  updated_at = now()
RETURNING user_id, tts_voicevox_speaker_uuid, created_at, updated_at, ghost_mode
`

type UpsertUserSettingsTTSVoicevoxSpeakerParams struct {
	UserID                 int64       `json:"user_id"`
	TtsVoicevoxSpeakerUuid pgtype.Text `json:"tts_voicevox_speaker_uuid"`
}

func (q *Queries) UpsertUserSettingsTTSVoicevoxSpeaker(ctx context.Context, arg UpsertUserSettingsTTSVoicevoxSpeakerParams) (UserSetting, error) {
	row := q.db.QueryRow(ctx, upsertUserSettingsTTSVoicevoxSpeaker, arg.UserID, arg.TtsVoicevoxSpeakerUuid)
	var i UserSetting
	err := row.Scan(&i.UserID, &i.TtsVoicevoxSpeakerUuid, &i.CreatedAt, &i.UpdatedAt, &i.GhostMode)
	return i, err
}
