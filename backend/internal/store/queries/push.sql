-- name: UpsertPushSubscription :exec
-- 同じendpointが既にあれば鍵と所有ユーザーを更新する（デバイス移行・再購読に対応）。
INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
VALUES ($1, $2, $3, $4)
ON CONFLICT (endpoint) DO UPDATE
SET user_id = EXCLUDED.user_id,
    p256dh = EXCLUDED.p256dh,
    auth = EXCLUDED.auth;

-- name: DeletePushSubscriptionByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: ListPushSubscriptionsByUser :many
SELECT id, user_id, endpoint, p256dh, auth, created_at
FROM push_subscriptions
WHERE user_id = $1;

-- name: CountPushSubscriptionsByUser :one
SELECT count(*) FROM push_subscriptions WHERE user_id = $1;

-- name: SetChannelNotificationOptin :exec
INSERT INTO channel_notification_optins (channel_id, user_id)
VALUES ($1, $2)
ON CONFLICT (channel_id, user_id) DO NOTHING;

-- name: DeleteChannelNotificationOptin :exec
DELETE FROM channel_notification_optins WHERE channel_id = $1 AND user_id = $2;

-- name: IsChannelNotificationOptedIn :one
SELECT EXISTS (
  SELECT 1 FROM channel_notification_optins WHERE channel_id = $1 AND user_id = $2
);

-- name: ListPushSubscriptionsForChannelOptins :many
-- あるチャンネルの通知をオンにしているユーザー全員のPush購読を返す（営業開始通知の送信対象）。
SELECT ps.id, ps.user_id, ps.endpoint, ps.p256dh, ps.auth
FROM channel_notification_optins o
JOIN push_subscriptions ps ON ps.user_id = o.user_id
WHERE o.channel_id = $1;
