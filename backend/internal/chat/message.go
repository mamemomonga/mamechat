package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mamemomonga/mamechat/backend/internal/auth"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

const DefaultMessageBodyLen = 400

type ClientMessage struct {
	Type       string `json:"type"`
	Body       string `json:"body,omitempty"`
	ImageToken string `json:"imageToken,omitempty"`
	// リアクション（reaction.toggle）用。
	MessageID string `json:"messageId,omitempty"`
	Emoji     string `json:"emoji,omitempty"`
}

type ServerMessage struct {
	Type            string           `json:"type"`
	ID              string           `json:"id,omitempty"`
	ChannelID       string           `json:"channelId,omitempty"`
	ChannelSlug     string           `json:"channelSlug,omitempty"`
	User            *MessageUser     `json:"user,omitempty"`
	Body            string           `json:"body,omitempty"`
	CreatedAt       string           `json:"createdAt,omitempty"`
	Message         string           `json:"message,omitempty"`
	NodeID          string           `json:"nodeId,omitempty"`
	Owner           *PresenceMember  `json:"owner,omitempty"`
	Members         []PresenceMember `json:"members,omitempty"`
	ActiveCount     int              `json:"activeCount,omitempty"`
	TotalCount      int              `json:"totalCount,omitempty"`
	SuspendDeadline string           `json:"suspendDeadline,omitempty"`
	// PausedRemainingSeconds は一時停止（オーナー退出で凍結）した営業残り秒数
	// （channel.operating.paused で全ノードへ配信）。
	PausedRemainingSeconds int32 `json:"pausedRemainingSeconds,omitempty"`
	// OperatingUnlimited は「時間制限なし」の現在値（channel.operating.mode で配信）。
	OperatingUnlimited *bool          `json:"operatingUnlimited,omitempty"`
	MessageID          string         `json:"messageId,omitempty"`
	PartCount          int            `json:"partCount,omitempty"`
	PartIndex          *int           `json:"partIndex,omitempty"`
	ContentHash        string         `json:"contentHash,omitempty"`
	AudioURL           string         `json:"audioUrl,omitempty"`
	MimeType           string         `json:"mimeType,omitempty"`
	Codec              string         `json:"codec,omitempty"`
	DurationMs         int32          `json:"durationMs,omitempty"`
	Parts              []TTSPart      `json:"parts,omitempty"`
	Reason             string         `json:"reason,omitempty"`
	ImageURL           string         `json:"imageUrl,omitempty"`
	ImageWidth         int32          `json:"imageWidth,omitempty"`
	ImageHeight        int32          `json:"imageHeight,omitempty"`
	MediaKind          string         `json:"mediaKind,omitempty"`
	Version            string         `json:"version,omitempty"`
	Channels           []LobbyChannel `json:"channels,omitempty"`
	// Reactions は投稿のリアクション集計（chat.message に同梱、または chat.reaction 更新で配信）。
	Reactions []ReactionGroup `json:"reactions,omitempty"`
}

// ReactionUser はあるリアクションを付けたユーザー（長押しの「誰が」表示用）。
type ReactionUser struct {
	ID          string `json:"userId"`
	Handle      string `json:"handle,omitempty"`
	DisplayName string `json:"displayName"`
}

// ReactionGroup は1つの絵文字ごとのリアクション集計。
type ReactionGroup struct {
	Emoji string         `json:"emoji"`
	Count int            `json:"count"`
	Users []ReactionUser `json:"users"`
}

// ReactionUpdate は1投稿のリアクション集計更新を全ノード・全クライアントへ配信する。
func ReactionUpdate(channelSlug, messageID string, groups []ReactionGroup) ServerMessage {
	return ServerMessage{
		Type:        "chat.reaction",
		MessageID:   messageID,
		ChannelSlug: channelSlug,
		Reactions:   groups,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ReactionGroupsFromRows は1投稿分のリアクション行を絵文字ごとに集計する（初出順）。
func ReactionGroupsFromRows(rows []db.ListReactionsForMessageRow) []ReactionGroup {
	order := make([]string, 0)
	byEmoji := make(map[string]*ReactionGroup)
	for _, r := range rows {
		g := byEmoji[r.Emoji]
		if g == nil {
			g = &ReactionGroup{Emoji: r.Emoji}
			byEmoji[r.Emoji] = g
			order = append(order, r.Emoji)
		}
		g.Count++
		g.Users = append(g.Users, ReactionUser{
			ID:          strconv.FormatInt(r.UserID, 10),
			Handle:      textValue(r.UserHandle),
			DisplayName: r.UserDisplayName,
		})
	}
	groups := make([]ReactionGroup, 0, len(order))
	for _, e := range order {
		groups = append(groups, *byEmoji[e])
	}
	return groups
}

// ReactionGroupsByMessage は複数投稿分のリアクション行を messageID ごとに集計する。
func ReactionGroupsByMessage(rows []db.ListReactionsForMessageRow) map[int64][]ReactionGroup {
	byMsg := make(map[int64][]db.ListReactionsForMessageRow)
	for _, r := range rows {
		byMsg[r.MessageID] = append(byMsg[r.MessageID], r)
	}
	out := make(map[int64][]ReactionGroup, len(byMsg))
	for id, rs := range byMsg {
		out[id] = ReactionGroupsFromRows(rs)
	}
	return out
}

// LobbyChannel はロビー（チャンネル一覧）向けの1チャンネル分のプレゼンス要約。
type LobbyChannel struct {
	ChannelSlug string           `json:"channelSlug"`
	Owner       *PresenceMember  `json:"owner,omitempty"`
	Members     []PresenceMember `json:"members,omitempty"`
	ActiveCount int              `json:"activeCount,omitempty"`
	TotalCount  int              `json:"totalCount,omitempty"`
	// SuspendDeadline は準備中へ移行する予定時刻（営業終了 or オーナー離席の自動閉店の早い方）。
	// 空=カウントダウンなし。一覧のカウントダウン表示に使う。
	SuspendDeadline string `json:"suspendDeadline,omitempty"`
}

type MessageUser struct {
	ID                 string                  `json:"id"`
	DisplayName        string                  `json:"displayName"`
	Handle             string                  `json:"handle,omitempty"`
	AvatarURL          string                  `json:"avatarUrl,omitempty"`
	Provider           string                  `json:"provider,omitempty"`
	TTSVoicevoxSpeaker *TTSVoicevoxSpeakerInfo `json:"ttsVoicevoxSpeaker,omitempty"`
}

type TTSVoicevoxSpeakerInfo struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type PresenceMember struct {
	User    MessageUser `json:"user"`
	Active  bool        `json:"active"`
	IsOwner bool        `json:"isOwner,omitempty"`
}

type TTSPart struct {
	PartIndex   int    `json:"partIndex"`
	ContentHash string `json:"contentHash"`
	AudioURL    string `json:"audioUrl"`
	MimeType    string `json:"mimeType"`
	Codec       string `json:"codec,omitempty"`
	DurationMs  int32  `json:"durationMs,omitempty"`
}

func MessageFromDB(channelSlug string, m db.ChatMessage) ServerMessage {
	msg := ServerMessage{
		Type:        "chat.message",
		ID:          strconv.FormatInt(m.ID, 10),
		ChannelID:   strconv.FormatInt(m.ChannelID, 10),
		ChannelSlug: channelSlug,
		User: &MessageUser{
			ID:                 strconv.FormatInt(m.UserID, 10),
			DisplayName:        m.UserDisplayName,
			Handle:             textValue(m.UserHandle),
			AvatarURL:          textValue(m.UserAvatarUrl),
			Provider:           textValue(m.UserProvider),
			TTSVoicevoxSpeaker: ttsVoicevoxSpeakerInfo(m.UserTtsVoicevoxSpeakerUuid, m.UserTtsVoicevoxSpeakerName, m.UserTtsVoicevoxSpeakerUrl),
		},
		Body:      m.Body,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if m.ImagePath.Valid && m.ImagePath.String != "" {
		msg.ImageURL = "/api/uploads/" + m.ImagePath.String
		// 拡張子で画像/動画を判別する（.mp4 は音声なしループ動画）。
		if strings.HasSuffix(m.ImagePath.String, ".mp4") {
			msg.MediaKind = "video"
		} else {
			msg.MediaKind = "image"
		}
		if m.ImageWidth.Valid {
			msg.ImageWidth = m.ImageWidth.Int32
		}
		if m.ImageHeight.Valid {
			msg.ImageHeight = m.ImageHeight.Int32
		}
	}
	return msg
}

func ErrorMessage(message string) ServerMessage {
	return ServerMessage{Type: "error", Message: message}
}

// LobbyPresence はチャンネル一覧（ロビー）向けに全チャンネルのプレゼンス要約をまとめて配信する。
func LobbyPresence(channels []LobbyChannel) ServerMessage {
	return ServerMessage{
		Type:      "lobby.presence",
		Channels:  channels,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func MessageDeleted(messageID string, channelID string, channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "chat.message.deleted",
		ID:          messageID,
		ChannelID:   channelID,
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func Pong() ServerMessage {
	return ServerMessage{Type: "pong", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

// VersionMessage はサーバ側のアプリバージョンをクライアントへ通知する。
// クライアントは自身に埋め込まれたバージョンと食い違えばリロードする。
func VersionMessage(version string) ServerMessage {
	return ServerMessage{
		Type:      "version",
		Version:   version,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func ChannelSuspended(channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "channel.suspended",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func ChannelResumed(channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "channel.resumed",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ChannelOperating は「営業中」ボタンによる営業開始／延長を全ノードへ通知する。
// SuspendDeadline に営業の終了予定時刻を載せ、各ノードはカウントダウンを開始する。
// deadline がゼロ値（時間制限なしの開店）のときは SuspendDeadline を空にする。
func ChannelOperating(channelSlug string, deadline time.Time) ServerMessage {
	msg := ServerMessage{
		Type:        "channel.operating",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	if !deadline.IsZero() {
		msg.SuspendDeadline = deadline.UTC().Format(time.RFC3339Nano)
	}
	return msg
}

// ChannelOperatingPaused はオーナー退出による営業時間の一時停止を全ノードへ通知する。
// SuspendDeadline に自動閉店（猶予）の締切、PausedRemainingSeconds に凍結した営業残り秒数を載せる。
// 各ノードは営業終了予定時刻を止め、自動閉店カウントダウンへ切り替える。
func ChannelOperatingPaused(channelSlug string, autoCloseDeadline time.Time, remainingSeconds int32) ServerMessage {
	msg := ServerMessage{
		Type:                   "channel.operating.paused",
		ChannelSlug:            channelSlug,
		PausedRemainingSeconds: remainingSeconds,
		CreatedAt:              time.Now().UTC().Format(time.RFC3339Nano),
	}
	if !autoCloseDeadline.IsZero() {
		msg.SuspendDeadline = autoCloseDeadline.UTC().Format(time.RFC3339Nano)
	}
	return msg
}

// ChannelOperatingMode は「時間制限なし」設定の変更を全ノード・クライアントへ通知する。
func ChannelOperatingMode(channelSlug string, unlimited bool) ServerMessage {
	return ServerMessage{
		Type:               "channel.operating.mode",
		ChannelSlug:        channelSlug,
		OperatingUnlimited: &unlimited,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func ChannelKicked(channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "channel.kicked",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func ChannelCleared(channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "channel.cleared",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// PresenceRefresh は他ノードへ「プレゼンスを再構築して再配信せよ」と促す軽量な合図。
// 各ノードが Valkey(アクティブ集合)とDB(来訪者)から最新状態を組み立て直す。
func PresenceRefresh(channelSlug string) ServerMessage {
	return ServerMessage{
		Type:        "channel.presence.refresh",
		ChannelSlug: channelSlug,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func Presence(channelSlug string, owner *PresenceMember, members []PresenceMember, activeCount int, totalCount int, suspendDeadline string) ServerMessage {
	return ServerMessage{
		Type:            "channel.presence",
		ChannelSlug:     channelSlug,
		Owner:           owner,
		Members:         members,
		ActiveCount:     activeCount,
		TotalCount:      totalCount,
		SuspendDeadline: suspendDeadline,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TTSQueued(messageID string, partCount int) ServerMessage {
	return ServerMessage{
		Type:      "tts_queued",
		MessageID: messageID,
		PartCount: partCount,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TTSPartReady(messageID string, partIndex int, contentHash string, durationMs int32) ServerMessage {
	return ServerMessage{
		Type:        "tts_part_ready",
		MessageID:   messageID,
		PartIndex:   &partIndex,
		ContentHash: contentHash,
		AudioURL:    "/api/tts/" + contentHash + ".m4a",
		MimeType:    "audio/mp4",
		Codec:       "aac-lc",
		DurationMs:  durationMs,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TTSReady(messageID string, parts []TTSPart) ServerMessage {
	return ServerMessage{
		Type:      "tts_ready",
		MessageID: messageID,
		Parts:     parts,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TTSSkipped(messageID string, reason string) ServerMessage {
	return ServerMessage{
		Type:      "tts_skipped",
		MessageID: messageID,
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TTSError(messageID string, reason string) ServerMessage {
	return ServerMessage{
		Type:      "tts_error",
		MessageID: messageID,
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func UserFromSnapshot(user auth.UserSnapshot) MessageUser {
	var speaker *TTSVoicevoxSpeakerInfo
	if user.TTSVoicevoxSpeaker != nil {
		speaker = &TTSVoicevoxSpeakerInfo{
			UUID: user.TTSVoicevoxSpeaker.UUID,
			Name: user.TTSVoicevoxSpeaker.Name,
			URL:  user.TTSVoicevoxSpeaker.URL,
		}
	}
	return MessageUser{
		ID:                 strconv.FormatInt(user.ID, 10),
		DisplayName:        user.DisplayName,
		Handle:             user.Handle,
		AvatarURL:          user.AvatarURL,
		Provider:           user.Provider,
		TTSVoicevoxSpeaker: speaker,
	}
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func ttsVoicevoxSpeakerInfo(uuid, name, url pgtype.Text) *TTSVoicevoxSpeakerInfo {
	if !uuid.Valid || !name.Valid {
		return nil
	}
	return &TTSVoicevoxSpeakerInfo{
		UUID: uuid.String,
		Name: name.String,
		URL:  textValue(url),
	}
}
