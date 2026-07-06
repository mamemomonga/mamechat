package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/access"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/auth"
	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/ttsautodict"
)

const maxWSMessageBytes = 4096

type WSHandler struct {
	Hub           *Hub
	Queries       *db.Queries
	Auth          *auth.Manager
	Bus           RealtimeBus
	TTS           TTSService
	Images        ImageStaging
	Version       string
	AllowedOrigin string
	MaxMessageLen int
}

type TTSService interface {
	EnqueueMessage(ctx context.Context, channelID int64, channelSlug string, messageID int64, speakerUUID string, body string) error
}

// ImageStaging はアップロード済み未投稿画像のステージング情報を取り出す。
type ImageStaging interface {
	TakeUpload(ctx context.Context, token string) ([]byte, bool, error)
}

// stagedImageInfo は投稿に添付する画像のメタ情報（Valkeyステージングと同形）。
type stagedImageInfo struct {
	ChannelID   int64  `json:"channelId"`
	UserID      int64  `json:"userId"`
	StoragePath string `json:"storagePath"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	session, err := h.Auth.CurrentSession(r.Context(), r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	channel, err := h.Queries.GetChannelBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		slog.Error("get websocket channel failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var ownerID int64
	if channel.OwnerUserID.Valid {
		ownerID = channel.OwnerUserID.Int64
	}
	// サスペンド中はチャンネルオーナー以外の入室を拒否する。
	if channel.SuspendedAt.Valid && session.User.ID != ownerID {
		http.Error(w, "channel is suspended", http.StatusForbidden)
		return
	}
	// 入室許可制御（ホワイト/ブラックリスト）。オーナー・管理者・ゴーストモードは常に入室可。
	if session.User.ID != ownerID && !auth.IsPrivileged(session.User.Role) && !session.User.GhostMode {
		// サーバ全体でホワイトリスト機能が無効なときは whitelist を none 扱いにする（未設定=無効）。
		whitelistEnabled := false
		if v, err := h.Queries.GetAppSetting(r.Context(), access.SettingWhitelistEnabled); err == nil {
			whitelistEnabled = access.SettingEnabled(v)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("get whitelist setting failed", "channel", slug, "error", err)
		}
		keys := access.UserKeys(session.User.Provider, session.User.Subject, session.User.Handle, session.User.ProfileURL)
		if !access.Allowed(access.EffectiveMode(channel.AccessMode, whitelistEnabled), access.ParseList(channel.AccessList), keys) {
			http.Error(w, "you are not allowed to join this channel", http.StatusForbidden)
			return
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{h.AllowedOrigin},
	})
	if err != nil {
		slog.Warn("websocket accept failed", "error", err)
		return
	}
	conn.SetReadLimit(h.readLimitBytes())

	// 一時停止（オーナー退出で凍結された営業残り時間）を接続スナップショットに含める。
	// 取得失敗時は未停止として扱う（致命ではない）。
	operatingPaused, err := h.Queries.GetChannelOperatingPause(r.Context(), slug)
	if err != nil {
		slog.Warn("get channel operating pause failed", "channel", slug, "error", err)
		operatingPaused = pgtype.Int4{}
	}

	client := NewClient(session.User, session.ID, channel.ID, slug, ownerID, channel.SuspendGraceSeconds, channel.SuspendedAt.Valid, channel.OperatingDeadline, channel.OperatingUnlimited, operatingPaused, conn)
	if err := h.Hub.Register(r.Context(), client); err != nil {
		slog.Error("register websocket client failed", "channel", slug, "error", err)
		_ = conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer h.Hub.Unregister(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go client.WriteLoop(ctx)
	// 接続直後にサーバのバージョンを通知する（クライアントは食い違えばリロードする）。
	if h.Version != "" {
		_ = client.Enqueue(VersionMessage(h.Version))
	}
	h.readLoop(ctx, client)
}

func (h *WSHandler) readLoop(ctx context.Context, client *Client) {
	defer func() {
		_ = client.Conn.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		msgType, payload, err := client.Conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageText {
			_ = client.Enqueue(ErrorMessage("text messages only"))
			continue
		}
		var msg ClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			_ = client.Enqueue(ErrorMessage("invalid json"))
			continue
		}
		switch msg.Type {
		case "ping":
			_ = client.Enqueue(Pong())
		case "presence.beacon":
			h.Hub.MarkSeen(client)
		case "chat.message":
			h.handleChatMessage(ctx, client, msg.Body, msg.ImageToken)
		case "reaction.toggle":
			h.handleReactionToggle(ctx, client, msg.MessageID, msg.Emoji)
		default:
			_ = client.Enqueue(ErrorMessage("unknown message type"))
		}
	}
}

func (h *WSHandler) handleChatMessage(ctx context.Context, client *Client, body string, imageToken string) {
	// 設定（読み上げ話者・ゴーストモード）の変更を再接続なしで反映するため、
	// 投稿のたびに最新状態をDBから読み直す。
	if speaker, ghost, err := h.Auth.LiveUserState(ctx, client.User.ID); err != nil {
		slog.Warn("refresh live user state failed", "user_id", client.User.ID, "error", err)
	} else {
		client.User.TTSVoicevoxSpeaker = speaker
		client.User.GhostMode = ghost
	}
	// ゴーストモード中は閲覧のみで書き込めない。
	if client.User.GhostMode {
		_ = client.Enqueue(ErrorMessage("ゴーストモード中は書き込めません"))
		return
	}
	body = strings.TrimSpace(body)
	if utf8.RuneCountInString(body) > h.messageMaxLen() {
		_ = client.Enqueue(ErrorMessage("message body is too long"))
		return
	}
	// 添付画像トークンがあればステージングから取り出して検証する。
	image, hasImage, ok := h.resolveImage(ctx, client, imageToken)
	if !ok {
		_ = client.Enqueue(ErrorMessage("image is no longer available"))
		return
	}
	// 本文・画像のいずれもない投稿は許可しない。
	if body == "" && !hasImage {
		_ = client.Enqueue(ErrorMessage("message body is required"))
		return
	}
	params := db.CreateChatMessageParams{
		ChannelID:                  client.ChannelID,
		UserID:                     client.User.ID,
		Body:                       body,
		UserDisplayName:            client.User.DisplayName,
		UserHandle:                 nullableText(client.User.Handle),
		UserAvatarUrl:              nullableText(client.User.AvatarURL),
		UserProvider:               nullableText(client.User.Provider),
		UserTtsVoicevoxSpeakerUuid: nullableText(userTTSSpeakerUUID(client.User)),
		UserTtsVoicevoxSpeakerName: nullableText(userTTSSpeakerName(client.User)),
		UserTtsVoicevoxSpeakerUrl:  nullableText(userTTSSpeakerURL(client.User)),
	}
	if hasImage {
		params.ImagePath = pgtype.Text{String: image.StoragePath, Valid: true}
		params.ImageWidth = pgtype.Int4{Int32: int32(image.Width), Valid: true}
		params.ImageHeight = pgtype.Int4{Int32: int32(image.Height), Valid: true}
	}
	dbMsg, err := h.Queries.CreateChatMessage(ctx, params)
	if err != nil {
		slog.Error("create chat message failed", "channel", client.ChannelSlug, "error", err)
		_ = client.Enqueue(ErrorMessage("failed to save message"))
		return
	}
	if body != "" {
		h.registerTTSAutoDictionary(ctx, client, body)
	}
	// 投稿者の最終投稿時刻とアクティブ状態を記録する（プレゼンスの並び順・在席判定用）。
	h.Hub.RecordPost(client)
	serverMsg := MessageFromDB(client.ChannelSlug, dbMsg)
	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.Bus.Publish(pubCtx, client.ChannelSlug, serverMsg); err != nil {
		slog.Error("publish chat message failed", "channel", client.ChannelSlug, "error", err)
		_ = client.Enqueue(ErrorMessage("failed to publish message"))
		return
	}
	if h.TTS != nil && body != "" && userTTSSpeakerUUID(client.User) != "" {
		ttsCtx, ttsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := h.TTS.EnqueueMessage(ttsCtx, client.ChannelID, client.ChannelSlug, dbMsg.ID, userTTSSpeakerUUID(client.User), body); err != nil {
			slog.Warn("enqueue tts message failed", "channel", client.ChannelSlug, "message_id", dbMsg.ID, "error", err)
		}
		ttsCancel()
	}
}

// handleReactionToggle は投稿への絵文字リアクションをトグルし、集計を全クライアントへ配信する。
func (h *WSHandler) handleReactionToggle(ctx context.Context, client *Client, messageID string, emoji string) {
	// ゴーストモード中は閲覧のみでリアクションできない。
	if _, ghost, err := h.Auth.LiveUserState(ctx, client.User.ID); err != nil {
		slog.Warn("refresh live user state failed", "user_id", client.User.ID, "error", err)
	} else {
		client.User.GhostMode = ghost
	}
	if client.User.GhostMode {
		_ = client.Enqueue(ErrorMessage("ゴーストモード中はリアクションできません"))
		return
	}
	emoji = strings.TrimSpace(emoji)
	if emoji == "" || len(emoji) > 32 || utf8.RuneCountInString(emoji) > 8 {
		_ = client.Enqueue(ErrorMessage("invalid emoji"))
		return
	}
	msgID, err := strconv.ParseInt(strings.TrimSpace(messageID), 10, 64)
	if err != nil {
		_ = client.Enqueue(ErrorMessage("invalid message id"))
		return
	}
	// 対象投稿が接続中チャンネルのものか検証する。
	dbMsg, err := h.Queries.GetChatMessageByID(ctx, msgID)
	if err != nil || dbMsg.ChannelID != client.ChannelID {
		_ = client.Enqueue(ErrorMessage("message not found"))
		return
	}
	// トグル: まず自分の同一リアクションを削除し、消えた行が無ければ追加する。
	removed, err := h.Queries.RemoveMessageReaction(ctx, db.RemoveMessageReactionParams{
		MessageID: msgID, UserID: client.User.ID, Emoji: emoji,
	})
	if err != nil {
		slog.Error("remove reaction failed", "error", err)
		_ = client.Enqueue(ErrorMessage("failed to update reaction"))
		return
	}
	if removed == 0 {
		if err := h.Queries.AddMessageReaction(ctx, db.AddMessageReactionParams{
			MessageID: msgID, UserID: client.User.ID, Emoji: emoji,
		}); err != nil {
			slog.Error("add reaction failed", "error", err)
			_ = client.Enqueue(ErrorMessage("failed to update reaction"))
			return
		}
	}
	rows, err := h.Queries.ListReactionsForMessage(ctx, msgID)
	if err != nil {
		slog.Error("list reactions failed", "error", err)
		return
	}
	groups := ReactionGroupsFromRows(rows)
	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.Bus.Publish(pubCtx, client.ChannelSlug, ReactionUpdate(client.ChannelSlug, messageID, groups)); err != nil {
		slog.Error("publish reaction failed", "channel", client.ChannelSlug, "error", err)
	}
}

// resolveImage はアップロードトークンを検証し、投稿に添付する画像情報を返す。
// トークンが空なら hasImage=false, ok=true。トークンが無効・期限切れ・他人/他チャンネルの
// ものなら ok=false。
func (h *WSHandler) resolveImage(ctx context.Context, client *Client, imageToken string) (stagedImageInfo, bool, bool) {
	imageToken = strings.TrimSpace(imageToken)
	if imageToken == "" || h.Images == nil {
		return stagedImageInfo{}, false, true
	}
	payload, found, err := h.Images.TakeUpload(ctx, imageToken)
	if err != nil {
		slog.Warn("take upload token failed", "channel", client.ChannelSlug, "error", err)
		return stagedImageInfo{}, false, false
	}
	if !found {
		return stagedImageInfo{}, false, false
	}
	var info stagedImageInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		slog.Warn("decode staged image failed", "channel", client.ChannelSlug, "error", err)
		return stagedImageInfo{}, false, false
	}
	// アップロードしたユーザー・チャンネルと一致しなければ拒否する。
	if info.UserID != client.User.ID || info.ChannelID != client.ChannelID || info.StoragePath == "" {
		slog.Warn("staged image ownership mismatch", "channel", client.ChannelSlug, "user", client.User.ID)
		return stagedImageInfo{}, false, false
	}
	return info, true, true
}

func (h *WSHandler) registerTTSAutoDictionary(ctx context.Context, client *Client, body string) {
	directives := ttsautodict.ExtractDirectives(body)
	if len(directives) == 0 {
		return
	}
	registerCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, directive := range directives {
		if _, err := h.Queries.UpsertTTSAutoDictionaryEntry(registerCtx, db.UpsertTTSAutoDictionaryEntryParams{
			TermKey:            ttsautodict.TermKey(directive.Term),
			Term:               directive.Term,
			Reading:            directive.Reading,
			RegisteredByUserID: pgtype.Int8{Int64: client.User.ID, Valid: true},
			RegisteredByHandle: nullableText(client.User.Handle),
		}); err != nil {
			slog.Warn("upsert tts auto dictionary entry failed", "term", directive.Term, "user_id", client.User.ID, "error", err)
		}
	}
}

func (h *WSHandler) messageMaxLen() int {
	if h.MaxMessageLen > 0 {
		return h.MaxMessageLen
	}
	return DefaultMessageBodyLen
}

func (h *WSHandler) readLimitBytes() int64 {
	limit := h.messageMaxLen()*4 + 1024
	if limit < maxWSMessageBytes {
		limit = maxWSMessageBytes
	}
	return int64(limit)
}

func userTTSSpeakerUUID(user auth.UserSnapshot) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.UUID
}

func userTTSSpeakerName(user auth.UserSnapshot) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.Name
}

func userTTSSpeakerURL(user auth.UserSnapshot) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.URL
}

func nullableText(v string) pgtype.Text {
	if strings.TrimSpace(v) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
