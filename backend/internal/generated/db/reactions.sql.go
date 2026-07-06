package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const removeMessageReaction = `-- name: RemoveMessageReaction :execrows
DELETE FROM message_reactions
WHERE message_id = $1 AND user_id = $2 AND emoji = $3
`

type RemoveMessageReactionParams struct {
	MessageID int64  `json:"message_id"`
	UserID    int64  `json:"user_id"`
	Emoji     string `json:"emoji"`
}

func (q *Queries) RemoveMessageReaction(ctx context.Context, arg RemoveMessageReactionParams) (int64, error) {
	result, err := q.db.Exec(ctx, removeMessageReaction, arg.MessageID, arg.UserID, arg.Emoji)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const addMessageReaction = `-- name: AddMessageReaction :exec
INSERT INTO message_reactions (message_id, user_id, emoji)
VALUES ($1, $2, $3)
ON CONFLICT (message_id, user_id, emoji) DO NOTHING
`

type AddMessageReactionParams struct {
	MessageID int64  `json:"message_id"`
	UserID    int64  `json:"user_id"`
	Emoji     string `json:"emoji"`
}

func (q *Queries) AddMessageReaction(ctx context.Context, arg AddMessageReactionParams) error {
	_, err := q.db.Exec(ctx, addMessageReaction, arg.MessageID, arg.UserID, arg.Emoji)
	return err
}

const listReactionsForMessage = `-- name: ListReactionsForMessage :many
SELECT r.message_id, r.emoji, r.user_id, u.handle AS user_handle, u.display_name AS user_display_name
FROM message_reactions r
JOIN users u ON u.id = r.user_id
WHERE r.message_id = $1
ORDER BY r.created_at ASC
`

type ListReactionsForMessageRow struct {
	MessageID       int64       `json:"message_id"`
	Emoji           string      `json:"emoji"`
	UserID          int64       `json:"user_id"`
	UserHandle      pgtype.Text `json:"user_handle"`
	UserDisplayName string      `json:"user_display_name"`
}

func (q *Queries) ListReactionsForMessage(ctx context.Context, messageID int64) ([]ListReactionsForMessageRow, error) {
	rows, err := q.db.Query(ctx, listReactionsForMessage, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListReactionsForMessageRow
	for rows.Next() {
		var i ListReactionsForMessageRow
		if err := rows.Scan(&i.MessageID, &i.Emoji, &i.UserID, &i.UserHandle, &i.UserDisplayName); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

const listReactionsForMessages = `-- name: ListReactionsForMessages :many
SELECT r.message_id, r.emoji, r.user_id, u.handle AS user_handle, u.display_name AS user_display_name
FROM message_reactions r
JOIN users u ON u.id = r.user_id
WHERE r.message_id = ANY($1::bigint[])
ORDER BY r.message_id ASC, r.created_at ASC
`

func (q *Queries) ListReactionsForMessages(ctx context.Context, messageIds []int64) ([]ListReactionsForMessageRow, error) {
	rows, err := q.db.Query(ctx, listReactionsForMessages, messageIds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListReactionsForMessageRow
	for rows.Next() {
		var i ListReactionsForMessageRow
		if err := rows.Scan(&i.MessageID, &i.Emoji, &i.UserID, &i.UserHandle, &i.UserDisplayName); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
