package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/auth"
)

const (
	writeTimeout  = 5 * time.Second
	sendQueueSize = 32
)

type Client struct {
	User                auth.UserSnapshot
	SessionID           int64
	ChannelID           int64
	ChannelSlug         string
	ChannelOwnerID      int64
	SuspendGraceSeconds pgtype.Int4        // チャンネルのサスペンド猶予（null=既定/負値=無期限/0以上=秒）
	ChannelSuspended    bool               // 接続時点でチャンネルが準備中か
	OperatingDeadline   pgtype.Timestamptz // 接続時点の営業終了予定時刻（再起動後のカウントダウン復元用）
	OperatingUnlimited  bool               // 時間制限なしチャンネル（自動閉店・カウントダウンをしない）
	OperatingPaused     pgtype.Int4        // 接続時点の一時停止残り秒数（非NULL=オーナー退出で凍結中）
	Conn                *websocket.Conn
	Send                chan ServerMessage
}

func NewClient(user auth.UserSnapshot, sessionID int64, channelID int64, channelSlug string, channelOwnerID int64, suspendGraceSeconds pgtype.Int4, channelSuspended bool, operatingDeadline pgtype.Timestamptz, operatingUnlimited bool, operatingPaused pgtype.Int4, conn *websocket.Conn) *Client {
	return &Client{
		User:                user,
		SessionID:           sessionID,
		ChannelID:           channelID,
		ChannelSlug:         channelSlug,
		ChannelOwnerID:      channelOwnerID,
		SuspendGraceSeconds: suspendGraceSeconds,
		ChannelSuspended:    channelSuspended,
		OperatingDeadline:   operatingDeadline,
		OperatingUnlimited:  operatingUnlimited,
		OperatingPaused:     operatingPaused,
		Conn:                conn,
		Send:                make(chan ServerMessage, sendQueueSize),
	}
}

func (c *Client) WriteLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-c.Send:
			if !ok {
				return
			}
			payload, err := json.Marshal(msg)
			if err != nil {
				slog.Warn("marshal websocket message failed", "error", err)
				continue
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err = c.Conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) Enqueue(msg ServerMessage) bool {
	select {
	case c.Send <- msg:
		return true
	default:
		return false
	}
}
