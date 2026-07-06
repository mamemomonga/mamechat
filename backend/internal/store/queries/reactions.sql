-- name: RemoveMessageReaction :execrows
DELETE FROM message_reactions
WHERE message_id = $1 AND user_id = $2 AND emoji = $3;

-- name: AddMessageReaction :exec
INSERT INTO message_reactions (message_id, user_id, emoji)
VALUES ($1, $2, $3)
ON CONFLICT (message_id, user_id, emoji) DO NOTHING;

-- name: ListReactionsForMessage :many
SELECT r.message_id, r.emoji, r.user_id, u.handle AS user_handle, u.display_name AS user_display_name
FROM message_reactions r
JOIN users u ON u.id = r.user_id
WHERE r.message_id = $1
ORDER BY r.created_at ASC;

-- name: ListReactionsForMessages :many
SELECT r.message_id, r.emoji, r.user_id, u.handle AS user_handle, u.display_name AS user_display_name
FROM message_reactions r
JOIN users u ON u.id = r.user_id
WHERE r.message_id = ANY($1::bigint[])
ORDER BY r.message_id ASC, r.created_at ASC;
