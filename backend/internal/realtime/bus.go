package realtime

import (
	"context"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/chat"
)

type Bus interface {
	Publish(ctx context.Context, channelSlug string, msg chat.ServerMessage) error
	Subscribe(ctx context.Context, channelSlug string) (<-chan chat.ServerMessage, func() error, error)
}
