-- name: CreateChannel :one
-- 新規チャンネルは「準備中」（suspended_at = now()）で作成する。
INSERT INTO channels (slug, title, description, owner_user_id, suspended_at)
VALUES ($1, $2, $3, $4, now())
RETURNING id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list;

-- name: ListChannels :many
SELECT id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list
FROM channels
ORDER BY created_at ASC;

-- name: GetChannelBySlug :one
SELECT id, slug, title, description, owner_user_id, suspended_at, suspend_retention_hours, suspend_grace_seconds, operating_deadline, operating_unlimited, post_ttl_hours, created_at, updated_at, url_linkify_enabled, image_upload_enabled, access_mode, access_list
FROM channels
WHERE slug = $1;

-- name: GetChannelOGPBySlug :one
SELECT
  c.slug,
  c.title,
  c.description,
  COALESCE(u.display_name, '') AS owner_display_name,
  u.avatar_url AS owner_avatar_url
FROM channels c
LEFT JOIN users u ON u.id = c.owner_user_id
WHERE c.slug = $1;

-- name: SuspendChannelIfActive :execrows
UPDATE channels
SET suspended_at = now(), operating_deadline = NULL,
    operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL;

-- name: ResumeChannelIfSuspended :execrows
UPDATE channels
SET suspended_at = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NOT NULL;

-- name: StartChannelOperating :execrows
-- 「営業中」ボタンで営業を開始する。準備中なら営業中に戻し、終了予定時刻を設定する。
UPDATE channels
SET suspended_at = NULL, operating_deadline = $2,
    operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1;

-- name: SetChannelOperatingDeadline :execrows
-- 営業中の延長（終了予定時刻の付け替え）。営業中（未サスペンド）のときだけ更新する。
-- 延長時は一時停止も解除する（凍結中に延長したら通常営業へ戻す）。
UPDATE channels
SET operating_deadline = $2, operating_paused_remaining_seconds = NULL, updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL;

-- name: PauseChannelOperating :execrows
-- オーナー退出時に営業残り時間を凍結する。operating_deadline を NULL にし、残り秒数を保存する。
-- 営業中（未サスペンド・終了予定あり・時間制限なしでない・未凍結）のときだけ行う。
UPDATE channels
SET operating_paused_remaining_seconds =
      GREATEST(0, CEIL(EXTRACT(EPOCH FROM (operating_deadline - now()))))::int,
    operating_deadline = NULL,
    updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL AND operating_deadline IS NOT NULL
  AND operating_unlimited = false AND operating_paused_remaining_seconds IS NULL;

-- name: ResumeChannelOperating :one
-- オーナー復帰時に凍結した営業残り時間で再開する。now()+残り秒数 を終了予定時刻に据え直す。
UPDATE channels
SET operating_deadline = now() + (operating_paused_remaining_seconds * interval '1 second'),
    operating_paused_remaining_seconds = NULL,
    updated_at = now()
WHERE slug = $1 AND suspended_at IS NULL AND operating_paused_remaining_seconds IS NOT NULL
RETURNING operating_deadline;

-- name: GetChannelOperatingPause :one
-- 接続時に一時停止の残り秒数を取得する（NULL=一時停止していない）。
SELECT operating_paused_remaining_seconds FROM channels WHERE slug = $1;

-- name: SetChannelSuspendRetention :exec
UPDATE channels
SET suspend_retention_hours = $2, updated_at = now()
WHERE slug = $1;

-- name: SetChannelSuspendGrace :exec
UPDATE channels
SET suspend_grace_seconds = $2, updated_at = now()
WHERE slug = $1;

-- name: SetChannelPostTTL :exec
UPDATE channels
SET post_ttl_hours = $2, updated_at = now()
WHERE slug = $1;

-- name: SetChannelOperatingUnlimited :exec
-- 「時間制限なし」の切り替え。有効化するときは既存の営業終了予定時刻・一時停止を消して
-- カウントダウンを止める。
UPDATE channels
SET operating_unlimited = $2,
    operating_deadline = CASE WHEN $2 THEN NULL ELSE operating_deadline END,
    operating_paused_remaining_seconds =
      CASE WHEN $2 THEN NULL ELSE operating_paused_remaining_seconds END,
    updated_at = now()
WHERE slug = $1;

-- name: SetChannelFeatures :exec
UPDATE channels
SET url_linkify_enabled = $2, image_upload_enabled = $3, updated_at = now()
WHERE slug = $1;

-- name: SetChannelProfile :exec
UPDATE channels
SET title = $2, description = $3, updated_at = now()
WHERE slug = $1;

-- name: SetChannelAccess :exec
UPDATE channels
SET access_mode = $2, access_list = $3, updated_at = now()
WHERE slug = $1;

-- name: CountChannelsOwnedByUser :one
SELECT count(*) FROM channels WHERE owner_user_id = $1;

-- name: ListChannelImagePaths :many
SELECT image_path
FROM chat_messages
WHERE channel_id = $1 AND image_path IS NOT NULL AND image_path <> '';

-- name: ListAllImagePaths :many
SELECT image_path
FROM chat_messages
WHERE image_path IS NOT NULL AND image_path <> '';

-- name: DeleteExpiredSuspendedChannels :many
DELETE FROM channels
WHERE suspended_at IS NOT NULL
  AND COALESCE(suspend_retention_hours, $1::int) >= 0
  AND suspended_at < now() - (COALESCE(suspend_retention_hours, $1::int) * interval '1 hour')
RETURNING slug;

-- name: DeleteChatMessagesByChannelID :exec
DELETE FROM chat_messages
WHERE channel_id = $1;

-- name: ListChannelLastPostTimes :many
SELECT c.slug, MAX(m.created_at)::timestamptz AS last_post_at
FROM channels c
LEFT JOIN chat_messages m ON m.channel_id = c.id
GROUP BY c.slug;
