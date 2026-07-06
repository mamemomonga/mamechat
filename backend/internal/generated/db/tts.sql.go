package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const getTTSAsset = `-- name: GetTTSAsset :one
SELECT content_hash, file_path, file_size_bytes, duration_ms, text_preview, text_length,
       speaker_id, speaker_name, speaker_style_name, speed_scale, pitch_scale,
       intonation_scale, volume_scale, pre_phoneme_length, post_phoneme_length,
       voicevox_engine_version, format, codec, bitrate, channels, normalizer_version,
       splitter_version, use_count, created_at, last_used_at, marked_for_delete_at
FROM tts_assets
WHERE content_hash = $1
`

func (q *Queries) GetTTSAsset(ctx context.Context, contentHash string) (TTSAsset, error) {
	row := q.db.QueryRow(ctx, getTTSAsset, contentHash)
	var i TTSAsset
	err := row.Scan(
		&i.ContentHash, &i.FilePath, &i.FileSizeBytes, &i.DurationMs, &i.TextPreview, &i.TextLength,
		&i.SpeakerID, &i.SpeakerName, &i.SpeakerStyleName, &i.SpeedScale, &i.PitchScale,
		&i.IntonationScale, &i.VolumeScale, &i.PrePhonemeLength, &i.PostPhonemeLength,
		&i.VoicevoxEngineVersion, &i.Format, &i.Codec, &i.Bitrate, &i.Channels, &i.NormalizerVersion,
		&i.SplitterVersion, &i.UseCount, &i.CreatedAt, &i.LastUsedAt, &i.MarkedForDeleteAt,
	)
	return i, err
}

const touchTTSAsset = `-- name: TouchTTSAsset :exec
UPDATE tts_assets
SET use_count = use_count + 1,
    last_used_at = now()
WHERE content_hash = $1
`

func (q *Queries) TouchTTSAsset(ctx context.Context, contentHash string) error {
	_, err := q.db.Exec(ctx, touchTTSAsset, contentHash)
	return err
}

const upsertTTSAsset = `-- name: UpsertTTSAsset :one
INSERT INTO tts_assets (
  content_hash, file_path, file_size_bytes, duration_ms, text_preview, text_length,
  speaker_id, speaker_name, speaker_style_name, speed_scale, pitch_scale,
  intonation_scale, volume_scale, pre_phoneme_length, post_phoneme_length,
  voicevox_engine_version, format, codec, bitrate, channels, normalizer_version,
  splitter_version, use_count, created_at, last_used_at
)
VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11,
  $12, $13, $14, $15,
  $16, 'm4a', 'aac-lc', 48000, 1, $17,
  $18, 1, now(), now()
)
ON CONFLICT (content_hash)
DO UPDATE SET
  file_path = EXCLUDED.file_path,
  file_size_bytes = EXCLUDED.file_size_bytes,
  duration_ms = EXCLUDED.duration_ms,
  text_preview = EXCLUDED.text_preview,
  text_length = EXCLUDED.text_length,
  speaker_id = EXCLUDED.speaker_id,
  speaker_name = EXCLUDED.speaker_name,
  speaker_style_name = EXCLUDED.speaker_style_name,
  speed_scale = EXCLUDED.speed_scale,
  pitch_scale = EXCLUDED.pitch_scale,
  intonation_scale = EXCLUDED.intonation_scale,
  volume_scale = EXCLUDED.volume_scale,
  pre_phoneme_length = EXCLUDED.pre_phoneme_length,
  post_phoneme_length = EXCLUDED.post_phoneme_length,
  voicevox_engine_version = EXCLUDED.voicevox_engine_version,
  format = EXCLUDED.format,
  codec = EXCLUDED.codec,
  bitrate = EXCLUDED.bitrate,
  channels = EXCLUDED.channels,
  normalizer_version = EXCLUDED.normalizer_version,
  splitter_version = EXCLUDED.splitter_version,
  use_count = tts_assets.use_count + 1,
  last_used_at = now(),
  marked_for_delete_at = NULL
RETURNING content_hash, file_path, file_size_bytes, duration_ms, text_preview, text_length,
       speaker_id, speaker_name, speaker_style_name, speed_scale, pitch_scale,
       intonation_scale, volume_scale, pre_phoneme_length, post_phoneme_length,
       voicevox_engine_version, format, codec, bitrate, channels, normalizer_version,
       splitter_version, use_count, created_at, last_used_at, marked_for_delete_at
`

type UpsertTTSAssetParams struct {
	ContentHash           string      `json:"content_hash"`
	FilePath              string      `json:"file_path"`
	FileSizeBytes         int64       `json:"file_size_bytes"`
	DurationMs            pgtype.Int4 `json:"duration_ms"`
	TextPreview           pgtype.Text `json:"text_preview"`
	TextLength            int32       `json:"text_length"`
	SpeakerID             int32       `json:"speaker_id"`
	SpeakerName           string      `json:"speaker_name"`
	SpeakerStyleName      pgtype.Text `json:"speaker_style_name"`
	SpeedScale            float64     `json:"speed_scale"`
	PitchScale            float64     `json:"pitch_scale"`
	IntonationScale       float64     `json:"intonation_scale"`
	VolumeScale           float64     `json:"volume_scale"`
	PrePhonemeLength      float64     `json:"pre_phoneme_length"`
	PostPhonemeLength     float64     `json:"post_phoneme_length"`
	VoicevoxEngineVersion string      `json:"voicevox_engine_version"`
	NormalizerVersion     string      `json:"normalizer_version"`
	SplitterVersion       string      `json:"splitter_version"`
}

func (q *Queries) UpsertTTSAsset(ctx context.Context, arg UpsertTTSAssetParams) (TTSAsset, error) {
	row := q.db.QueryRow(ctx, upsertTTSAsset,
		arg.ContentHash, arg.FilePath, arg.FileSizeBytes, arg.DurationMs, arg.TextPreview, arg.TextLength,
		arg.SpeakerID, arg.SpeakerName, arg.SpeakerStyleName, arg.SpeedScale, arg.PitchScale,
		arg.IntonationScale, arg.VolumeScale, arg.PrePhonemeLength, arg.PostPhonemeLength,
		arg.VoicevoxEngineVersion, arg.NormalizerVersion, arg.SplitterVersion,
	)
	var i TTSAsset
	err := row.Scan(
		&i.ContentHash, &i.FilePath, &i.FileSizeBytes, &i.DurationMs, &i.TextPreview, &i.TextLength,
		&i.SpeakerID, &i.SpeakerName, &i.SpeakerStyleName, &i.SpeedScale, &i.PitchScale,
		&i.IntonationScale, &i.VolumeScale, &i.PrePhonemeLength, &i.PostPhonemeLength,
		&i.VoicevoxEngineVersion, &i.Format, &i.Codec, &i.Bitrate, &i.Channels, &i.NormalizerVersion,
		&i.SplitterVersion, &i.UseCount, &i.CreatedAt, &i.LastUsedAt, &i.MarkedForDeleteAt,
	)
	return i, err
}

const createTTSJob = `-- name: CreateTTSJob :exec
INSERT INTO tts_jobs (
  id, channel_id, message_id, content_hash, status, priority, speaker_id, text_preview, text_length
)
VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING
`

type CreateTTSJobParams struct {
	ID          string      `json:"id"`
	ChannelID   int64       `json:"channel_id"`
	MessageID   int64       `json:"message_id"`
	ContentHash string      `json:"content_hash"`
	Priority    int32       `json:"priority"`
	SpeakerID   int32       `json:"speaker_id"`
	TextPreview pgtype.Text `json:"text_preview"`
	TextLength  int32       `json:"text_length"`
}

func (q *Queries) CreateTTSJob(ctx context.Context, arg CreateTTSJobParams) error {
	_, err := q.db.Exec(ctx, createTTSJob, arg.ID, arg.ChannelID, arg.MessageID, arg.ContentHash, arg.Priority, arg.SpeakerID, arg.TextPreview, arg.TextLength)
	return err
}

func (q *Queries) MarkTTSJobProcessing(ctx context.Context, id string) error {
	_, err := q.db.Exec(ctx, `UPDATE tts_jobs SET status = 'processing', started_at = now() WHERE id = $1`, id)
	return err
}

func (q *Queries) GetTTSJobStatus(ctx context.Context, id string) (string, error) {
	row := q.db.QueryRow(ctx, `SELECT status FROM tts_jobs WHERE id = $1`, id)
	var status string
	err := row.Scan(&status)
	return status, err
}

func (q *Queries) MarkTTSJobReady(ctx context.Context, id string) error {
	_, err := q.db.Exec(ctx, `UPDATE tts_jobs SET status = 'ready', finished_at = now() WHERE id = $1`, id)
	return err
}

type MarkTTSJobFailedParams struct {
	ID           string `json:"id"`
	ErrorMessage string `json:"error_message"`
}

func (q *Queries) MarkTTSJobFailed(ctx context.Context, arg MarkTTSJobFailedParams) error {
	_, err := q.db.Exec(ctx, `UPDATE tts_jobs SET status = 'failed', error_message = $2, finished_at = now() WHERE id = $1`, arg.ID, arg.ErrorMessage)
	return err
}

const markOldQueuedTTSJobsSkipped = `-- name: MarkOldQueuedTTSJobsSkipped :many
WITH old_jobs AS (
  SELECT id
  FROM tts_jobs
  WHERE channel_id = $1
    AND status = 'queued'
    AND priority <= 0
  ORDER BY created_at DESC
  OFFSET $2
)
UPDATE tts_jobs
SET status = 'skipped',
    error_message = 'queue_overflow',
    finished_at = now()
WHERE id IN (SELECT id FROM old_jobs)
RETURNING message_id
`

type MarkOldQueuedTTSJobsSkippedParams struct {
	ChannelID int64 `json:"channel_id"`
	Offset    int32 `json:"offset"`
}

func (q *Queries) MarkOldQueuedTTSJobsSkipped(ctx context.Context, arg MarkOldQueuedTTSJobsSkippedParams) ([]int64, error) {
	rows, err := q.db.Query(ctx, markOldQueuedTTSJobsSkipped, arg.ChannelID, arg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	return items, rows.Err()
}

type CreateTTSMessagePartParams struct {
	ID          string      `json:"id"`
	ChannelID   int64       `json:"channel_id"`
	MessageID   int64       `json:"message_id"`
	ContentHash string      `json:"content_hash"`
	PartIndex   int32       `json:"part_index"`
	TextPreview pgtype.Text `json:"text_preview"`
	TextLength  int32       `json:"text_length"`
}

func (q *Queries) CreateTTSMessagePart(ctx context.Context, arg CreateTTSMessagePartParams) error {
	_, err := q.db.Exec(ctx, `INSERT INTO tts_message_parts (id, channel_id, message_id, content_hash, part_index, text_preview, text_length)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (message_id, part_index) DO NOTHING`, arg.ID, arg.ChannelID, arg.MessageID, arg.ContentHash, arg.PartIndex, arg.TextPreview, arg.TextLength)
	return err
}

type ListTTSGCCandidatesParams struct {
	OlderThanHours int32 `json:"older_than_hours"`
	UseCount       int64 `json:"use_count"`
	Limit          int32 `json:"limit"`
}

type ListTTSGCCandidatesRow struct {
	ContentHash string `json:"content_hash"`
	FilePath    string `json:"file_path"`
}

func (q *Queries) ListTTSGCCandidates(ctx context.Context, arg ListTTSGCCandidatesParams) ([]ListTTSGCCandidatesRow, error) {
	rows, err := q.db.Query(ctx, `SELECT content_hash, file_path
FROM tts_assets
WHERE last_used_at < now() - ($1::int * interval '1 hour')
  AND use_count < $2
  AND marked_for_delete_at IS NULL
ORDER BY last_used_at ASC
LIMIT $3`, arg.OlderThanHours, arg.UseCount, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListTTSGCCandidatesRow
	for rows.Next() {
		var i ListTTSGCCandidatesRow
		if err := rows.Scan(&i.ContentHash, &i.FilePath); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (q *Queries) MarkTTSAssetForDelete(ctx context.Context, contentHash string) error {
	_, err := q.db.Exec(ctx, `UPDATE tts_assets SET marked_for_delete_at = now() WHERE content_hash = $1`, contentHash)
	return err
}

func (q *Queries) DeleteTTSAsset(ctx context.Context, contentHash string) error {
	_, err := q.db.Exec(ctx, `DELETE FROM tts_assets WHERE content_hash = $1`, contentHash)
	return err
}
