package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mamemomonga/mamechat/backend/internal/chat"
)

// アクティブ判定の既定有効期間。ビーコン間隔を十分に上回る値にし、1回の取りこぼしで
// 非アクティブ化しないようにする。期限切れのエントリは読み取り時に掃除される。
const defaultActivePresenceWindow = 30 * time.Second

type ValkeyBus struct {
	client *redis.Client
	// activeWindow はアクティブ判定の有効期間（ACTIVE_WINDOW_SECONDS で調整）。
	activeWindow time.Duration
}

// NewValkeyBus は Valkey/Redis 接続を作る。activeWindow<=0 のときは既定値を使う。
func NewValkeyBus(redisURL string, activeWindow time.Duration) (*ValkeyBus, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	if activeWindow <= 0 {
		activeWindow = defaultActivePresenceWindow
	}
	return &ValkeyBus{client: redis.NewClient(opt), activeWindow: activeWindow}, nil
}

func (b *ValkeyBus) Ping(ctx context.Context) error {
	return b.client.Ping(ctx).Err()
}

func (b *ValkeyBus) Close() error {
	return b.client.Close()
}

func (b *ValkeyBus) Publish(ctx context.Context, channelSlug string, msg chat.ServerMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := b.client.Publish(ctx, pubsubName(channelSlug), payload).Err(); err != nil {
		return fmt.Errorf("publish channel %s: %w", channelSlug, err)
	}
	return nil
}

func (b *ValkeyBus) Subscribe(ctx context.Context, channelSlug string) (<-chan chat.ServerMessage, func() error, error) {
	pubsub := b.client.Subscribe(ctx, pubsubName(channelSlug))
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, fmt.Errorf("subscribe channel %s: %w", channelSlug, err)
	}

	out := make(chan chat.ServerMessage, 32)
	go func() {
		defer close(out)
		defer func() {
			if err := pubsub.Close(); err != nil {
				slog.Warn("valkey pubsub close failed", "channel", channelSlug, "error", err)
			}
		}()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var serverMsg chat.ServerMessage
				if err := json.Unmarshal([]byte(msg.Payload), &serverMsg); err != nil {
					slog.Warn("invalid pubsub payload", "channel", channelSlug, "error", err)
					continue
				}
				select {
				case out <- serverMsg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, pubsub.Close, nil
}

func pubsubName(channelSlug string) string {
	return "chat:channel:" + channelSlug
}

// ── アクティブ状態（プレゼンス）の保存 ───────────────────────────────────
// チャンネルごとに sorted set でアクティブなユーザーを保持する。
// member=userID, score=最終確認のUNIX秒。来訪者の正本はDBにあり、ここは揮発的な
// アクティブ状態のみを持つ（Valkeyが消えても来訪者はDBから復元できる）。

func presenceKey(channelSlug string) string {
	return "presence:active:" + channelSlug
}

// MarkActive はユーザーをアクティブとして記録する（接続・ビーコン・投稿時）。
func (b *ValkeyBus) MarkActive(ctx context.Context, channelSlug, userID string) error {
	key := presenceKey(channelSlug)
	pipe := b.client.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(time.Now().Unix()), Member: userID})
	// アイドルなチャンネルのキーは自然に消えるようにTTLを更新する。
	pipe.Expire(ctx, key, 2*b.activeWindow)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("mark active %s: %w", channelSlug, err)
	}
	return nil
}

// RemoveActive はユーザーをアクティブ集合から外す（切断時の即時反映用）。
func (b *ValkeyBus) RemoveActive(ctx context.Context, channelSlug, userID string) error {
	if err := b.client.ZRem(ctx, presenceKey(channelSlug), userID).Err(); err != nil {
		return fmt.Errorf("remove active %s: %w", channelSlug, err)
	}
	return nil
}

// ClearActive はチャンネルのアクティブ集合を丸ごと消す（チャンネル削除・slug再作成時）。
// 削除された旧チャンネルのアクティブ状態が同じ slug の新チャンネルに残らないようにする。
func (b *ValkeyBus) ClearActive(ctx context.Context, channelSlug string) error {
	if err := b.client.Del(ctx, presenceKey(channelSlug)).Err(); err != nil {
		return fmt.Errorf("clear active %s: %w", channelSlug, err)
	}
	return nil
}

// ── チャンネル並び順（最終投稿順）のキャッシュ ──────────────────────────
// 並び順はリアルタイムでなくてよいので、最終投稿順に並べたslug配列をTTL付きで
// キャッシュする。サスペンド状態は別途DBから都度取得して反映する。

const channelOrderKey = "channels:order"

// GetChannelOrder はキャッシュ済みの並び順（slug配列）を返す。未キャッシュなら ok=false。
func (b *ValkeyBus) GetChannelOrder(ctx context.Context) ([]string, bool, error) {
	s, err := b.client.Get(ctx, channelOrderKey).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var slugs []string
	if err := json.Unmarshal([]byte(s), &slugs); err != nil {
		return nil, false, err
	}
	return slugs, true, nil
}

// SetChannelOrder は並び順をTTL付きでキャッシュする。
func (b *ValkeyBus) SetChannelOrder(ctx context.Context, slugs []string, ttl time.Duration) error {
	payload, err := json.Marshal(slugs)
	if err != nil {
		return err
	}
	return b.client.Set(ctx, channelOrderKey, payload, ttl).Err()
}

// ── OGP（リンクプレビュー）キャッシュ ─────────────────────────────────────

func ogpKey(hash string) string {
	return "ogp:" + hash
}

// GetOGP はキャッシュ済みのOGP JSONを返す。未キャッシュなら ok=false。
func (b *ValkeyBus) GetOGP(ctx context.Context, hash string) ([]byte, bool, error) {
	s, err := b.client.Get(ctx, ogpKey(hash)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(s), true, nil
}

// SetOGP はOGP JSONをTTL付きでキャッシュする。
func (b *ValkeyBus) SetOGP(ctx context.Context, hash string, payload []byte, ttl time.Duration) error {
	return b.client.Set(ctx, ogpKey(hash), payload, ttl).Err()
}

// ── 画像アップロードのステージング ───────────────────────────────────────
// アップロード済みだが未投稿の画像トークンを、TTL付きで一時保存する。
// 投稿時にトークンを取り出して検証し、メッセージに添付する。

func uploadTokenKey(token string) string {
	return "upload:img:" + token
}

// StageUpload はアップロード済み画像のメタJSONをトークン付きで一時保存する。
func (b *ValkeyBus) StageUpload(ctx context.Context, token string, payload []byte, ttl time.Duration) error {
	return b.client.Set(ctx, uploadTokenKey(token), payload, ttl).Err()
}

// TakeUpload はトークンに対応するステージング情報を取り出して削除する（消費）。
// 存在しなければ ok=false。
func (b *ValkeyBus) TakeUpload(ctx context.Context, token string) ([]byte, bool, error) {
	s, err := b.client.GetDel(ctx, uploadTokenKey(token)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(s), true, nil
}

// ActiveUserIDs は有効期間内にアクティブなユーザーID集合を返す。期限切れは掃除する。
func (b *ValkeyBus) ActiveUserIDs(ctx context.Context, channelSlug string) (map[string]struct{}, error) {
	key := presenceKey(channelSlug)
	cutoff := time.Now().Add(-b.activeWindow).Unix()
	// 期限切れエントリを掃除する（クラッシュ等でRemoveActiveされ損ねた分）。
	if err := b.client.ZRemRangeByScore(ctx, key, "-inf", "("+strconv.FormatInt(cutoff, 10)).Err(); err != nil {
		slog.Warn("presence cleanup failed", "channel", channelSlug, "error", err)
	}
	ids, err := b.client.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(cutoff, 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("active users %s: %w", channelSlug, err)
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}
