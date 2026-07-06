-- name: GetTTSAsset :one
SELECT content_hash, file_path, file_size_bytes, duration_ms, text_preview, text_length,
       speaker_id, speaker_name, speaker_style_name, speed_scale, pitch_scale,
       intonation_scale, volume_scale, pre_phoneme_length, post_phoneme_length,
       voicevox_engine_version, format, codec, bitrate, channels, normalizer_version,
       splitter_version, use_count, created_at, last_used_at, marked_for_delete_at
FROM tts_assets
WHERE content_hash = $1;

-- name: TouchTTSAsset :exec
UPDATE tts_assets
SET use_count = use_count + 1,
    last_used_at = now()
WHERE content_hash = $1;

-- name: UpsertTTSAsset :one
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
       splitter_version, use_count, created_at, last_used_at, marked_for_delete_at;

-- name: CreateTTSJob :exec
INSERT INTO tts_jobs (
  id, channel_id, message_id, content_hash, status, priority, speaker_id, text_preview, text_length
)
VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING;

-- name: MarkTTSJobProcessing :exec
UPDATE tts_jobs
SET status = 'processing',
    started_at = now()
WHERE id = $1;

-- name: GetTTSJobStatus :one
SELECT status
FROM tts_jobs
WHERE id = $1;

-- name: MarkTTSJobReady :exec
UPDATE tts_jobs
SET status = 'ready',
    finished_at = now()
WHERE id = $1;

-- name: MarkTTSJobFailed :exec
UPDATE tts_jobs
SET status = 'failed',
    error_message = $2,
    finished_at = now()
WHERE id = $1;

-- name: MarkOldQueuedTTSJobsSkipped :many
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
RETURNING message_id;

-- name: CreateTTSMessagePart :exec
INSERT INTO tts_message_parts (
  id, channel_id, message_id, content_hash, part_index, text_preview, text_length
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (message_id, part_index) DO NOTHING;

-- name: ListTTSGCCandidates :many
SELECT content_hash, file_path
FROM tts_assets
WHERE last_used_at < now() - ($1::int * interval '1 hour')
  AND use_count < $2
  AND marked_for_delete_at IS NULL
ORDER BY last_used_at ASC
LIMIT $3;

-- name: MarkTTSAssetForDelete :exec
UPDATE tts_assets
SET marked_for_delete_at = now()
WHERE content_hash = $1;

-- name: DeleteTTSAsset :exec
DELETE FROM tts_assets
WHERE content_hash = $1;
