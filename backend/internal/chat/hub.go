package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

// プレゼンスのアバター一覧に出す来訪者の最大件数。数字(のべ人数)は全体を示す。
const presenceVisitorLimit int32 = 50

// ErrNotOperating は営業中でないチャンネルに営業延長を要求したときに返す。
var ErrNotOperating = errors.New("channel is not operating")

// PresenceStore はアクティブ状態（揮発的）の保存先。実体は Valkey。
// 来訪者の正本はDBにあり、ここが消えてもDBから復元できる。
type PresenceStore interface {
	MarkActive(ctx context.Context, channelSlug, userID string) error
	RemoveActive(ctx context.Context, channelSlug, userID string) error
	ClearActive(ctx context.Context, channelSlug string) error
	ActiveUserIDs(ctx context.Context, channelSlug string) (map[string]struct{}, error)
}

type Hub struct {
	bus           RealtimeBus
	presence      PresenceStore
	q             *db.Queries
	grace         time.Duration // オーナー離席からサスペンドするまでの既定猶予（graceEnabled=false のとき無効）
	graceEnabled  bool          // 既定猶予が有効か。false=既定は無期限（離席で自動閉店しない）
	nodeID        string
	mu            sync.Mutex
	channels      map[string]*localChannel
	suspendTimers map[string]*pendingSuspend
	lobby         lobbyState
}

// pendingSuspend は準備中（サスペンド）への移行予約（カウントダウン）を表す。
// deadline は営業の終了予定時刻、またはオーナー離席猶予のうち早い方。
type pendingSuspend struct {
	timer    *time.Timer
	deadline time.Time
}

type RealtimeBus interface {
	Publish(ctx context.Context, channelSlug string, msg ServerMessage) error
	Subscribe(ctx context.Context, channelSlug string) (<-chan ServerMessage, func() error, error)
}

// localChannel はこのノードが購読中のチャンネルと、その接続クライアントを表す。
// 来訪者・アクティブ状態は DB / Valkey が正本なので、メモリには保持しない。
type localChannel struct {
	slug           string
	channelID      int64
	ownerUserID    int64
	grace          time.Duration // このチャンネルのサスペンド猶予（resolveGraceで解決済み）
	suspendEnabled bool          // false=無期限（オーナー退出後もサスペンドしない）
	clients        map[*Client]struct{}
	cancel         context.CancelFunc
	unsubscribe    func() error
	// suspended はチャンネルが準備中（サスペンド中）か。営業状態の判定に使う。
	suspended bool
	// operatingDeadline は営業の終了予定時刻（ゼロ値=営業の予定なし）。
	// 到達すると自動で準備中へ移行する。サーバ再起動後もDBから復元する。
	operatingDeadline time.Time
	// operatingUnlimited は時間制限なしチャンネル。true のとき終了予定時刻も離席猶予も
	// 無視し、自動で準備中へ移行しない（開店/閉店のみ）。
	operatingUnlimited bool
	// ownerAbsentSince はオーナーが不在になった時刻（ゼロ=在席/未追跡）。自動閉店カウントダウンの
	// 基点として使い、再評価のたびに締切がずれない（＝ぶれない）ようにする。
	ownerAbsentSince time.Time
	// operatingPausedRemaining は一時停止中に凍結した営業残り時間（0=停止していない）。
	// オーナー退出中は operatingDeadline を止めてこの残り時間を保持し、復帰時に now+残りで再開する。
	// DBの operating_paused_remaining_seconds を正本とし、メモリにも保持する。
	operatingPausedRemaining time.Duration
}

// resolveGrace はチャンネルの suspend_grace_seconds を有効な猶予に変換する。
// null=環境変数の既定（既定が0なら無期限）、負値=無期限（サスペンドしない）、0以上=その秒数。
func (h *Hub) resolveGrace(s pgtype.Int4) (time.Duration, bool) {
	if !s.Valid {
		return h.grace, h.graceEnabled
	}
	if s.Int32 < 0 {
		return 0, false
	}
	return time.Duration(s.Int32) * time.Second, true
}

func NewHub(bus RealtimeBus, presence PresenceStore, q *db.Queries, grace time.Duration) *Hub {
	return &Hub{
		bus:           bus,
		presence:      presence,
		q:             q,
		grace:         grace,
		graceEnabled:  grace > 0,
		nodeID:        newNodeID(),
		channels:      make(map[string]*localChannel),
		suspendTimers: make(map[string]*pendingSuspend),
	}
}

func (h *Hub) Register(ctx context.Context, client *Client) error {
	h.mu.Lock()
	// 同じ slug が別の channel_id で作り直された（削除→同名で再作成）場合、メモリ上の
	// 旧エントリは古い channel_id を握ったままなので、プレゼンスが旧チャンネルの
	// （CASCADE削除済みの）来訪者を参照して空になる。古いエントリを破棄して作り直す。
	if stale := h.channels[client.ChannelSlug]; stale != nil && stale.channelID != client.ChannelID {
		delete(h.channels, client.ChannelSlug)
		staleClients := make([]*Client, 0, len(stale.clients))
		for c := range stale.clients {
			staleClients = append(staleClients, c)
		}
		stale.cancel()
		if stale.unsubscribe != nil {
			if err := stale.unsubscribe(); err != nil {
				slog.Warn("unsubscribe stale channel failed", "channel", client.ChannelSlug, "error", err)
			}
		}
		if ps, ok := h.suspendTimers[client.ChannelSlug]; ok {
			ps.timer.Stop()
			delete(h.suspendTimers, client.ChannelSlug)
		}
		h.mu.Unlock()
		for _, c := range staleClients {
			_ = c.Conn.Close(websocket.StatusGoingAway, "channel recreated")
		}
		// 旧 channel_id のアクティブ集合を消し、のべ人数/アクティブ数の水増しを防ぐ。
		h.clearActive(client.ChannelSlug)
		h.mu.Lock()
	}
	ch := h.channels[client.ChannelSlug]
	if ch == nil {
		chCtx, cancel := context.WithCancel(context.Background())
		msgCh, unsubscribe, err := h.bus.Subscribe(chCtx, client.ChannelSlug)
		if err != nil {
			cancel()
			h.mu.Unlock()
			return err
		}
		ch = &localChannel{
			slug:        client.ChannelSlug,
			channelID:   client.ChannelID,
			ownerUserID: client.ChannelOwnerID,
			clients:     make(map[*Client]struct{}),
			cancel:      cancel,
			unsubscribe: unsubscribe,
			// チャンネルが再びアクティブになった時点の営業状態をDBから復元する。
			// これによりサーバ再起動後もカウントダウンが正しく続く。
			suspended:          client.ChannelSuspended,
			operatingUnlimited: client.OperatingUnlimited,
		}
		if client.OperatingDeadline.Valid {
			ch.operatingDeadline = client.OperatingDeadline.Time
		}
		// 一時停止中（オーナー退出で凍結）ならDBから残り時間を復元する。この間 operating_deadline は
		// NULL のため上の復元ではゼロ値のまま。オーナー在席が確認できれば evaluateSuspend で再開される。
		if client.OperatingPaused.Valid && client.OperatingPaused.Int32 > 0 {
			ch.operatingPausedRemaining = time.Duration(client.OperatingPaused.Int32) * time.Second
		}
		h.channels[client.ChannelSlug] = ch
		go h.runChannel(chCtx, client.ChannelSlug, msgCh)
	}
	ch.clients[client] = struct{}{}
	// 接続のたびに猶予設定を反映する（管理者が変更した場合に追従する）。
	ch.grace, ch.suspendEnabled = h.resolveGrace(client.SuspendGraceSeconds)
	h.mu.Unlock()

	// 来訪をDBに記録し、アクティブ集合(Valkey)に登録する。
	h.markVisitorSeen(client)
	h.broadcastPresence(client.ChannelSlug)
	h.publishRefresh(client.ChannelSlug)
	// オーナーが戻ればサスペンド解除、非オーナーのみならサスペンドを評価する。
	go h.evaluateSuspend(client.ChannelSlug)
	return nil
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	ch := h.channels[client.ChannelSlug]
	if ch == nil {
		h.mu.Unlock()
		return
	}
	delete(ch.clients, client)
	close(client.Send)
	// このノードに同一ユーザーの接続がまだ残っていれば、アクティブ解除しない。
	userStillLocal := false
	for c := range ch.clients {
		if c.User.ID == client.User.ID {
			userStillLocal = true
			break
		}
	}
	empty := len(ch.clients) == 0
	ownerID := ch.ownerUserID
	grace := ch.grace
	suspendEnabled := ch.suspendEnabled
	suspended := ch.suspended
	operatingUnlimited := ch.operatingUnlimited
	operatingDeadline := ch.operatingDeadline
	ownerAbsentSince := ch.ownerAbsentSince
	alreadyPaused := ch.operatingPausedRemaining > 0
	slug := client.ChannelSlug
	if empty {
		ch.cancel()
		if ch.unsubscribe != nil {
			if err := ch.unsubscribe(); err != nil {
				slog.Warn("unsubscribe failed", "channel", slug, "error", err)
			}
		}
		delete(h.channels, slug)
	}
	h.mu.Unlock()

	if !userStillLocal {
		// 切断を即時に反映する（アクティブ集合から除外）。
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := h.presence.RemoveActive(ctx, slug, strconv.FormatInt(client.User.ID, 10)); err != nil {
			slog.Warn("remove active failed", "channel", slug, "error", err)
		}
		cancel()
	}
	h.broadcastPresence(slug)
	h.publishRefresh(slug)
	if empty {
		// 全員退室＝オーナーも不在。チャンネルはメモリから消えるため、捕捉した状態から
		// 自動閉店タイマーだけを残す（他ノードにオーナーがいれば、そのノードが一時停止通知を
		// 受けて評価し直し、営業を再開して自己修復する）。
		if ownerID != 0 && !suspended && !operatingUnlimited {
			now := time.Now()
			remaining := max(operatingDeadline.Sub(now), 0)
			// 離席タイマー（猶予）が残り営業時間より大きい場合は自動閉店を動かさない
			// （残時間を延ばさない）。その場合は下の「通常営業」分岐で本来の終了予定時刻を使う。
			awayTimerActive := suspendEnabled && !operatingDeadline.IsZero() && grace <= remaining
			if suspendEnabled && (awayTimerActive || alreadyPaused) {
				// 自動閉店あり：営業時間を凍結し、自動閉店（猶予）カウントダウンを残す。
				anchor := ownerAbsentSince
				if anchor.IsZero() {
					anchor = now
				}
				if awayTimerActive && !alreadyPaused && !operatingDeadline.IsZero() {
					// このノードでオーナーが退出した瞬間（まだ凍結していない）なら凍結する。
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					if _, err := h.q.PauseChannelOperating(ctx, slug); err != nil {
						slog.Warn("pause channel operating failed", "channel", slug, "error", err)
					} else {
						secs := int32(remaining / time.Second)
						if err := h.bus.Publish(ctx, slug, ChannelOperatingPaused(slug, anchor.Add(grace), secs)); err != nil {
							slog.Warn("publish channel paused failed", "channel", slug, "error", err)
						}
					}
					cancel()
				}
				h.setSuspendTimer(slug, anchor.Add(grace))
			} else if !operatingDeadline.IsZero() {
				// 自動閉店なし、または離席タイマーが残時間より大きい：
				// 営業終了予定時刻まで通常営業（一時停止しない）。
				h.setSuspendTimer(slug, operatingDeadline)
			}
		}
	} else {
		go h.evaluateSuspend(slug)
	}
}

func (h *Hub) DisconnectSession(sessionID int64) {
	h.mu.Lock()
	var clients []*Client
	for _, ch := range h.channels {
		for client := range ch.clients {
			if client.SessionID == sessionID {
				clients = append(clients, client)
			}
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		_ = client.Conn.Close(websocket.StatusPolicyViolation, "session revoked")
	}
}

// DisconnectUser は指定ユーザーの全WebSocket接続を切断する。
// ユーザー停止（suspended化）時に、開いているセッションを即時切断するために使う。
func (h *Hub) DisconnectUser(userID int64) {
	h.mu.Lock()
	var clients []*Client
	for _, ch := range h.channels {
		for client := range ch.clients {
			if client.User.ID == userID {
				clients = append(clients, client)
			}
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		_ = client.Conn.Close(websocket.StatusPolicyViolation, "session revoked")
	}
}

// DisconnectAll は全WebSocket接続を切断する（全セッションリセット時に使う）。
func (h *Hub) DisconnectAll() {
	h.mu.Lock()
	var clients []*Client
	for _, ch := range h.channels {
		for client := range ch.clients {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		_ = client.Conn.Close(websocket.StatusPolicyViolation, "session revoked")
	}
}

// MarkSeen はビーコン受信時にアクティブ集合と来訪者の最終確認時刻を更新する。
// 配信は10秒間隔のティッカーに任せる（毎ビーコンでの再配信を避ける）。
func (h *Hub) MarkSeen(client *Client) {
	h.markVisitorSeen(client)
}

// RecordPost は投稿時に投稿者のアクティブ状態と最終投稿時刻を更新する。
// プレゼンスの再配信は chat.message のバス配信を受けて各ノードが行う。
func (h *Hub) RecordPost(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	user := UserFromSnapshot(client.User)
	if err := h.presence.MarkActive(ctx, client.ChannelSlug, user.ID); err != nil {
		slog.Warn("mark active (post) failed", "channel", client.ChannelSlug, "error", err)
	}
	if err := h.q.MarkChannelVisitorPosted(ctx, db.MarkChannelVisitorPostedParams{
		ChannelID:              client.ChannelID,
		UserID:                 client.User.ID,
		DisplayName:            user.DisplayName,
		Handle:                 nullableText(user.Handle),
		AvatarUrl:              nullableText(user.AvatarURL),
		Provider:               nullableText(user.Provider),
		TtsVoicevoxSpeakerUuid: nullableText(ttsSpeakerUUID(user)),
		TtsVoicevoxSpeakerName: nullableText(ttsSpeakerName(user)),
		TtsVoicevoxSpeakerUrl:  nullableText(ttsSpeakerURL(user)),
	}); err != nil {
		slog.Warn("mark visitor posted failed", "channel", client.ChannelSlug, "error", err)
	}
}

// markVisitorSeen は来訪者をDBに記録し、アクティブ集合に登録する。
func (h *Hub) markVisitorSeen(client *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	user := UserFromSnapshot(client.User)
	if err := h.presence.MarkActive(ctx, client.ChannelSlug, user.ID); err != nil {
		slog.Warn("mark active failed", "channel", client.ChannelSlug, "error", err)
	}
	if err := h.q.UpsertChannelVisitorSeen(ctx, db.UpsertChannelVisitorSeenParams{
		ChannelID:              client.ChannelID,
		UserID:                 client.User.ID,
		DisplayName:            user.DisplayName,
		Handle:                 nullableText(user.Handle),
		AvatarUrl:              nullableText(user.AvatarURL),
		Provider:               nullableText(user.Provider),
		TtsVoicevoxSpeakerUuid: nullableText(ttsSpeakerUUID(user)),
		TtsVoicevoxSpeakerName: nullableText(ttsSpeakerName(user)),
		TtsVoicevoxSpeakerUrl:  nullableText(ttsSpeakerURL(user)),
	}); err != nil {
		slog.Warn("upsert visitor failed", "channel", client.ChannelSlug, "error", err)
	}
}

func (h *Hub) runChannel(ctx context.Context, channelSlug string, ch <-chan ServerMessage) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 定期再配信でアクティブ期限切れ（非アクティブ化）を反映する。
			h.broadcastPresence(channelSlug)
			h.evaluateSuspend(channelSlug)
		case msg, ok := <-ch:
			if !ok {
				return
			}
			switch msg.Type {
			case "channel.presence.refresh":
				// 他ノードからの再構築要請。クライアントには配信しない。
				h.broadcastPresence(channelSlug)
			case "channel.kicked":
				// チャンネル削除の通知。このノードの在室者へ通知して退出させる（切断でエントリも消える）。
				h.kickAll(channelSlug)
			case "channel.suspended":
				// 準備中への移行確定。各ノードの営業状態を更新してカウントダウンを止め、
				// 通知を配信して在室中の非オーナーを切断する。
				h.setChannelOperating(channelSlug, true, time.Time{})
				h.broadcast(channelSlug, msg)
				h.kickNonOwners(channelSlug)
			case "channel.resumed":
				// 営業再開（終了予定時刻なし。主に旧データ向け）。
				h.setChannelOperating(channelSlug, false, time.Time{})
				h.broadcast(channelSlug, msg)
			case "channel.operating":
				// 「営業中」ボタンによる営業開始／延長、またはオーナー復帰による営業再開。
				// 終了予定時刻を設定し、クライアントへ通知してカウントダウンを開始する。
				// SuspendDeadline が空（時間制限なしの開店）ならカウントダウンしない。
				var deadline time.Time
				if msg.SuspendDeadline != "" {
					deadline = parseTime(msg.SuspendDeadline)
				}
				h.setChannelOperating(channelSlug, false, deadline)
				h.broadcast(channelSlug, msg)
			case "channel.operating.paused":
				// オーナー退出による営業時間の一時停止。各ノードで終了予定時刻を止めて
				// 凍結残り時間を保持し、自動閉店カウントダウンへ切り替える。
				h.setChannelPaused(channelSlug, time.Duration(msg.PausedRemainingSeconds)*time.Second)
				h.broadcast(channelSlug, msg)
			case "channel.operating.mode":
				// 「時間制限なし」設定の変更。各ノードのフラグを更新してタイマーを貼り直し、
				// クライアントへも通知する。
				if msg.OperatingUnlimited != nil {
					h.setChannelUnlimited(channelSlug, *msg.OperatingUnlimited)
				}
				h.broadcast(channelSlug, msg)
			case "chat.message":
				h.broadcast(channelSlug, msg)
				// 投稿で並び順（最後に投稿した人が先頭）が変わるため再配信する。
				h.broadcastPresence(channelSlug)
			default:
				h.broadcast(channelSlug, msg)
			}
		}
	}
}

func (h *Hub) broadcast(channelSlug string, msg ServerMessage) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	var slow []*Client
	if ch != nil {
		for client := range ch.clients {
			if !client.Enqueue(msg) {
				slow = append(slow, client)
			}
		}
	}
	h.mu.Unlock()
	h.disconnectSlow(channelSlug, slow)
}

// broadcastPresence は Valkey(アクティブ集合)とDB(来訪者)から最新のプレゼンスを組み立て、
// suspendDeadlineFor は準備中へ移行する予定時刻（営業終了 or オーナー離席の自動閉店）を
// このノードの suspendTimers から返す。予約がなければ空文字。ロビー要約のカウントダウン用。
func (h *Hub) suspendDeadlineFor(channelSlug string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ps, ok := h.suspendTimers[channelSlug]; ok {
		return formatTime(ps.deadline)
	}
	return ""
}

// このノードの在室クライアントへ配信する。
func (h *Hub) broadcastPresence(channelSlug string) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	if ch == nil {
		h.mu.Unlock()
		return
	}
	channelID := ch.channelID
	ownerID := ch.ownerUserID
	suspendDeadline := ""
	if ps, ok := h.suspendTimers[channelSlug]; ok {
		suspendDeadline = formatTime(ps.deadline)
	}
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	msg := h.buildPresence(ctx, channelSlug, channelID, ownerID, suspendDeadline)
	cancel()

	h.mu.Lock()
	ch = h.channels[channelSlug]
	var slow []*Client
	if ch != nil {
		for client := range ch.clients {
			if !client.Enqueue(msg) {
				slow = append(slow, client)
			}
		}
	}
	h.mu.Unlock()
	h.disconnectSlow(channelSlug, slow)
}

// buildPresence はアクティブ集合と来訪者一覧からプレゼンスメッセージを構築する。
// 先頭はオーナー、その後にアクティブ数/のべ人数、来訪者アバターが続く。
func (h *Hub) buildPresence(ctx context.Context, channelSlug string, channelID, ownerID int64, suspendDeadline string) ServerMessage {
	active, err := h.presence.ActiveUserIDs(ctx, channelSlug)
	if err != nil {
		slog.Warn("active users failed", "channel", channelSlug, "error", err)
		active = map[string]struct{}{}
	}
	visitors, err := h.q.ListChannelVisitors(ctx, db.ListChannelVisitorsParams{
		ChannelID: channelID,
		Limit:     presenceVisitorLimit,
	})
	if err != nil {
		slog.Warn("list visitors failed", "channel", channelSlug, "error", err)
	}
	total, err := h.q.CountChannelVisitors(ctx, channelID)
	if err != nil {
		slog.Warn("count visitors failed", "channel", channelSlug, "error", err)
	}

	var owner *PresenceMember
	members := make([]PresenceMember, 0, len(visitors))
	for _, v := range visitors {
		idStr := strconv.FormatInt(v.UserID, 10)
		_, isActive := active[idStr]
		m := PresenceMember{
			User:   visitorUser(v.UserID, v.DisplayName, v.Handle, v.AvatarUrl, v.Provider, v.TtsVoicevoxSpeakerUuid, v.TtsVoicevoxSpeakerName, v.TtsVoicevoxSpeakerUrl),
			Active: isActive,
		}
		if ownerID != 0 && v.UserID == ownerID {
			m.IsOwner = true
			ownerCopy := m
			owner = &ownerCopy
		} else {
			members = append(members, m)
		}
	}
	// 表示順を4群にする：アクティブ＆投稿済み → アクティブ → 非アクティブ＆投稿済み → 非アクティブ。
	// 入力は last_post_at DESC（投稿済みが先）で並んでいるため、アクティブを前へ安定ソートすれば
	// 各群内の投稿順が保たれてこの並びになる。
	sort.SliceStable(members, func(i, j int) bool {
		return members[i].Active && !members[j].Active
	})

	// オーナーが上限件数に入らなかった場合は個別取得する。
	if owner == nil && ownerID != 0 {
		if ov, err := h.q.GetChannelVisitor(ctx, db.GetChannelVisitorParams{ChannelID: channelID, UserID: ownerID}); err == nil {
			_, isActive := active[strconv.FormatInt(ownerID, 10)]
			owner = &PresenceMember{
				User:    visitorUser(ov.UserID, ov.DisplayName, ov.Handle, ov.AvatarUrl, ov.Provider, ov.TtsVoicevoxSpeakerUuid, ov.TtsVoicevoxSpeakerName, ov.TtsVoicevoxSpeakerUrl),
				Active:  isActive,
				IsOwner: true,
			}
		}
	}

	return Presence(channelSlug, owner, members, len(active), int(total), suspendDeadline)
}

func visitorUser(userID int64, displayName string, handle, avatar, provider, ttsUUID, ttsName, ttsURL pgtype.Text) MessageUser {
	return MessageUser{
		ID:                 strconv.FormatInt(userID, 10),
		DisplayName:        displayName,
		Handle:             textValue(handle),
		AvatarURL:          textValue(avatar),
		Provider:           textValue(provider),
		TTSVoicevoxSpeaker: ttsVoicevoxSpeakerInfo(ttsUUID, ttsName, ttsURL),
	}
}

func ttsSpeakerUUID(user MessageUser) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.UUID
}

func ttsSpeakerName(user MessageUser) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.Name
}

func ttsSpeakerURL(user MessageUser) string {
	if user.TTSVoicevoxSpeaker == nil {
		return ""
	}
	return user.TTSVoicevoxSpeaker.URL
}

func (h *Hub) publishRefresh(channelSlug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.bus.Publish(ctx, channelSlug, PresenceRefresh(channelSlug)); err != nil {
		slog.Warn("publish presence refresh failed", "channel", channelSlug, "error", err)
	}
}

// clearActive はチャンネルのアクティブ集合(Valkey)を丸ごと消す。
func (h *Hub) clearActive(channelSlug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.presence.ClearActive(ctx, channelSlug); err != nil {
		slog.Warn("clear active failed", "channel", channelSlug, "error", err)
	}
}

// DropChannel はチャンネル削除時に、このノードのメモリ状態・在室接続・アクティブ集合を
// 即座に破棄する。在室クライアントへは channel.kicked を通知してから切断する。
// 他ノードには別途 channel.kicked をバス配信し、runChannel 側で同様に切断させる。
func (h *Hub) DropChannel(channelSlug string) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	var clients []*Client
	if ch != nil {
		delete(h.channels, channelSlug)
		for c := range ch.clients {
			clients = append(clients, c)
		}
		ch.cancel()
		if ch.unsubscribe != nil {
			if err := ch.unsubscribe(); err != nil {
				slog.Warn("unsubscribe dropped channel failed", "channel", channelSlug, "error", err)
			}
		}
	}
	if ps, ok := h.suspendTimers[channelSlug]; ok {
		ps.timer.Stop()
		delete(h.suspendTimers, channelSlug)
	}
	h.mu.Unlock()

	for _, client := range clients {
		conn := client.Conn
		if client.Enqueue(ChannelKicked(channelSlug)) {
			time.AfterFunc(2*time.Second, func() {
				_ = conn.Close(websocket.StatusGoingAway, "channel deleted")
			})
		} else {
			_ = conn.Close(websocket.StatusGoingAway, "channel deleted")
		}
	}
	h.clearActive(channelSlug)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Now()
	}
	return t
}

// setChannelOperating は各ノードのローカルな営業状態（準備中フラグ・営業終了予定時刻）を
// 更新し、サスペンドタイマーを貼り直す。バスメッセージ受信時に呼ぶ。
func (h *Hub) setChannelOperating(channelSlug string, suspended bool, operatingDeadline time.Time) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	if ch != nil {
		ch.suspended = suspended
		ch.operatingDeadline = operatingDeadline
		// 終了予定時刻が入る＝営業（再開含む）。一時停止を解除する。
		if !operatingDeadline.IsZero() {
			ch.operatingPausedRemaining = 0
			ch.ownerAbsentSince = time.Time{}
		}
		if suspended {
			ch.operatingPausedRemaining = 0
			ch.ownerAbsentSince = time.Time{}
		}
	}
	h.mu.Unlock()
	// 在席状況を見てタイマーを再評価する（準備中ならタイマー解除、営業中なら終了予定時刻で予約）。
	h.evaluateSuspend(channelSlug)
}

// setChannelPaused は他ノードからの一時停止通知を各ノードのローカル状態へ反映する。
// 営業終了予定時刻を止め、凍結した営業残り時間を保持する（自動閉店カウントダウンへ切り替わる）。
func (h *Hub) setChannelPaused(channelSlug string, remaining time.Duration) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	if ch != nil && !ch.suspended && !ch.operatingUnlimited {
		ch.operatingDeadline = time.Time{}
		if remaining > 0 {
			ch.operatingPausedRemaining = remaining
		}
	}
	h.mu.Unlock()
	h.evaluateSuspend(channelSlug)
}

// setChannelUnlimited は各ノードの「時間制限なし」フラグを更新し、タイマーを貼り直す。
// 有効化した場合は終了予定時刻も消してカウントダウンを止める。
func (h *Hub) setChannelUnlimited(channelSlug string, unlimited bool) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	if ch != nil {
		ch.operatingUnlimited = unlimited
		if unlimited {
			ch.operatingDeadline = time.Time{}
		}
	}
	h.mu.Unlock()
	h.evaluateSuspend(channelSlug)
}

// computeSuspendDeadlineLocked は現在の営業状態とオーナー在席から、準備中へ移行すべき時刻を返す。
// ゼロ値は「予約不要」を意味する。h.mu を保持した状態で呼ぶこと。
func (h *Hub) computeSuspendDeadlineLocked(ch *localChannel, ownerPresent bool, now time.Time) time.Time {
	if ch.suspended {
		return time.Time{} // 既に準備中：カウントダウンしない
	}
	if ch.operatingUnlimited {
		return time.Time{} // 時間制限なし：終了予定時刻も離席猶予も無視（自動閉店しない）
	}
	deadline := ch.operatingDeadline // 営業の終了予定時刻（ゼロ=予定なし・一時停止中）
	// オーナー不在なら自動閉店（猶予）でも準備中へ。営業終了予定より早ければそちらを優先する。
	// 締切の基点は「不在になった時刻(ownerAbsentSince)」に固定し、再評価のたびに後ろへ延びない。
	if !ownerPresent && ch.suspendEnabled {
		anchor := ch.ownerAbsentSince
		if anchor.IsZero() {
			anchor = now
		}
		g := anchor.Add(ch.grace)
		if deadline.IsZero() || g.Before(deadline) {
			deadline = g
		}
	}
	return deadline
}

// evaluateSuspend はオーナーの在席状況と営業終了予定時刻からサスペンド予約を貼り直す。
// あわせて、オーナー退出で営業時間を一時停止し、復帰で残り時間から再開する（自動閉店機能）。
// オーナーの在席は、このノードの接続（即時）と、Valkeyのアクティブ集合（クロスノード）で判定する。
func (h *Hub) evaluateSuspend(channelSlug string) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	if ch == nil {
		h.mu.Unlock()
		return
	}
	ownerID := ch.ownerUserID
	if ownerID == 0 {
		h.mu.Unlock()
		return // オーナー不明（owner_user_id が NULL）は制御対象外
	}
	ownerPresent := false
	for c := range ch.clients {
		if c.User.ID == ownerID {
			ownerPresent = true
			break
		}
	}
	h.mu.Unlock()

	if !ownerPresent {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		active, err := h.presence.ActiveUserIDs(ctx, channelSlug)
		cancel()
		if err == nil {
			_, ownerPresent = active[strconv.FormatInt(ownerID, 10)]
		}
	}

	h.mu.Lock()
	ch = h.channels[channelSlug]
	if ch == nil {
		h.mu.Unlock()
		return
	}
	now := time.Now()
	// オーナーの在席状況が変わったら、営業時間の一時停止／再開を判定する。
	// pauseRemaining>0 のときはDBへ一時停止を永続化し、resumeDeadline!=0 のときは再開を反映する。
	var resumeDeadline time.Time
	var pauseAutoClose time.Time
	var pauseRemaining time.Duration
	pausable := !ch.suspended && !ch.operatingUnlimited && ch.suspendEnabled
	if ownerPresent {
		ch.ownerAbsentSince = time.Time{}
		if ch.operatingPausedRemaining > 0 {
			// オーナー復帰：凍結した残り時間で営業を再開する。
			resumeDeadline = now.Add(ch.operatingPausedRemaining)
			ch.operatingDeadline = resumeDeadline
			ch.operatingPausedRemaining = 0
		}
	} else if pausable {
		if ch.ownerAbsentSince.IsZero() {
			ch.ownerAbsentSince = now
		}
		if ch.operatingPausedRemaining == 0 && !ch.operatingDeadline.IsZero() {
			remaining := max(ch.operatingDeadline.Sub(now), 0)
			// 離席タイマー（猶予）が残り営業時間より大きい＝自動閉店時刻が本来の営業終了より
			// 後ろになる（残時間が延びてしまう）場合は、離席タイマーを動かさない。
			// 営業残り時間を凍結せず、本来の営業終了予定時刻まで通常どおり営業する。
			if ch.grace <= remaining {
				// オーナー退出：営業残り時間を凍結し、自動閉店カウントダウンへ切り替える。
				pauseRemaining = remaining
				ch.operatingPausedRemaining = pauseRemaining
				ch.operatingDeadline = time.Time{}
				pauseAutoClose = ch.ownerAbsentSince.Add(ch.grace)
			}
		}
	}
	deadline := h.computeSuspendDeadlineLocked(ch, ownerPresent, now)
	h.mu.Unlock()

	// 一時停止／再開はDBを正本として永続化し、全ノードへ配信する（再起動・全員退出をまたぐ）。
	if !pauseAutoClose.IsZero() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if _, err := h.q.PauseChannelOperating(ctx, channelSlug); err != nil {
			slog.Warn("pause channel operating failed", "channel", channelSlug, "error", err)
		} else {
			secs := int32(pauseRemaining / time.Second)
			if err := h.bus.Publish(ctx, channelSlug, ChannelOperatingPaused(channelSlug, pauseAutoClose, secs)); err != nil {
				slog.Warn("publish channel paused failed", "channel", channelSlug, "error", err)
			}
		}
		cancel()
	}
	if !resumeDeadline.IsZero() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		dbDeadline, err := h.q.ResumeChannelOperating(ctx, channelSlug)
		if err != nil {
			slog.Warn("resume channel operating failed", "channel", channelSlug, "error", err)
		} else {
			// DBが確定した終了予定時刻を正本として配信する（ノード間のズレを防ぐ）。
			if dbDeadline.Valid {
				resumeDeadline = dbDeadline.Time
			}
			if err := h.bus.Publish(ctx, channelSlug, ChannelOperating(channelSlug, resumeDeadline)); err != nil {
				slog.Warn("publish channel resume failed", "channel", channelSlug, "error", err)
			}
		}
		cancel()
	}

	h.setSuspendTimer(channelSlug, deadline)
}

// setSuspendTimer は準備中への移行予約を指定時刻に貼り直す（ゼロ値なら予約を解除する）。
// 既存予約と同じ時刻なら何もしない（不要な再配信を避ける）。
func (h *Hub) setSuspendTimer(channelSlug string, deadline time.Time) {
	h.mu.Lock()
	prev, had := h.suspendTimers[channelSlug]
	if deadline.IsZero() {
		if had {
			prev.timer.Stop()
			delete(h.suspendTimers, channelSlug)
		}
		h.mu.Unlock()
		if had {
			h.broadcastPresence(channelSlug)
		}
		return
	}
	if had && prev.deadline.Equal(deadline) {
		h.mu.Unlock()
		return // 変化なし
	}
	if had {
		prev.timer.Stop()
		delete(h.suspendTimers, channelSlug)
	}
	d := max(time.Until(deadline), 0)
	h.suspendTimers[channelSlug] = &pendingSuspend{
		deadline: deadline,
		timer: time.AfterFunc(d, func() {
			h.mu.Lock()
			delete(h.suspendTimers, channelSlug)
			h.mu.Unlock()
			h.suspendChannel(channelSlug)
		}),
	}
	h.mu.Unlock()
	// カウントダウン開始／更新を即時反映する（プレゼンスに終了予定時刻を含めて配信）。
	h.broadcastPresence(channelSlug)
}

// suspendChannel は条件付き更新で準備中にし、遷移した時だけ全ノードへ通知する。
func (h *Hub) suspendChannel(channelSlug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	affected, err := h.q.SuspendChannelIfActive(ctx, channelSlug)
	if err != nil {
		slog.Warn("suspend channel failed", "channel", channelSlug, "error", err)
		return
	}
	if affected == 0 {
		return // 既に準備中
	}
	if err := h.bus.Publish(ctx, channelSlug, ChannelSuspended(channelSlug)); err != nil {
		slog.Warn("publish channel suspended failed", "channel", channelSlug, "error", err)
	}
}

// RequestStartOperating は「営業中」ボタンによる営業開始を行う。
// 準備中なら営業中に戻し、now+dur を終了予定時刻として全ノードへ通知する。
func (h *Hub) RequestStartOperating(ctx context.Context, channelSlug string, dur time.Duration) error {
	deadline := time.Now().Add(dur)
	if _, err := h.q.StartChannelOperating(ctx, db.StartChannelOperatingParams{
		Slug:              channelSlug,
		OperatingDeadline: pgtype.Timestamptz{Time: deadline, Valid: true},
	}); err != nil {
		return err
	}
	return h.bus.Publish(ctx, channelSlug, ChannelOperating(channelSlug, deadline))
}

// RequestOpenChannel は「時間制限なし」チャンネルの開店を行う。
// 準備中なら営業中に戻し、終了予定時刻は設定しない（カウントダウンしない）。
func (h *Hub) RequestOpenChannel(ctx context.Context, channelSlug string) error {
	if _, err := h.q.StartChannelOperating(ctx, db.StartChannelOperatingParams{
		Slug:              channelSlug,
		OperatingDeadline: pgtype.Timestamptz{Valid: false},
	}); err != nil {
		return err
	}
	return h.bus.Publish(ctx, channelSlug, ChannelOperating(channelSlug, time.Time{}))
}

// RequestSetOperatingDuration は現在時刻からdur後を終了予定時刻として設定する。
// 準備中なら営業開始、営業中なら残り時間の変更として扱う。
func (h *Hub) RequestSetOperatingDuration(ctx context.Context, channelSlug string, dur time.Duration) error {
	channel, err := h.q.GetChannelBySlug(ctx, channelSlug)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(dur)
	if channel.SuspendedAt.Valid {
		if _, err := h.q.StartChannelOperating(ctx, db.StartChannelOperatingParams{
			Slug:              channelSlug,
			OperatingDeadline: pgtype.Timestamptz{Time: deadline, Valid: true},
		}); err != nil {
			return err
		}
	} else {
		affected, err := h.q.SetChannelOperatingDeadline(ctx, db.SetChannelOperatingDeadlineParams{
			Slug:              channelSlug,
			OperatingDeadline: pgtype.Timestamptz{Time: deadline, Valid: true},
		})
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrNotOperating
		}
	}
	return h.bus.Publish(ctx, channelSlug, ChannelOperating(channelSlug, deadline))
}

// SetOperatingUnlimited は「時間制限なし」設定を更新し、全ノード・クライアントへ反映する。
func (h *Hub) SetOperatingUnlimited(ctx context.Context, channelSlug string, unlimited bool) error {
	if err := h.q.SetChannelOperatingUnlimited(ctx, db.SetChannelOperatingUnlimitedParams{
		Slug:               channelSlug,
		OperatingUnlimited: unlimited,
	}); err != nil {
		return err
	}
	return h.bus.Publish(ctx, channelSlug, ChannelOperatingMode(channelSlug, unlimited))
}

// RequestExtendOperating は営業中の終了予定時刻を dur だけ延長する。
// 現在の終了予定時刻はDBを正本として読み、そこへ加算する（複数ノードでもズレない）。
func (h *Hub) RequestExtendOperating(ctx context.Context, channelSlug string, dur time.Duration) error {
	channel, err := h.q.GetChannelBySlug(ctx, channelSlug)
	if err != nil {
		return err
	}
	if channel.SuspendedAt.Valid {
		return ErrNotOperating // 準備中は延長できない
	}
	base := time.Now()
	if channel.OperatingDeadline.Valid && channel.OperatingDeadline.Time.After(base) {
		base = channel.OperatingDeadline.Time
	}
	deadline := base.Add(dur)
	affected, err := h.q.SetChannelOperatingDeadline(ctx, db.SetChannelOperatingDeadlineParams{
		Slug:              channelSlug,
		OperatingDeadline: pgtype.Timestamptz{Time: deadline, Valid: true},
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotOperating // 営業中でなくなっていた
	}
	return h.bus.Publish(ctx, channelSlug, ChannelOperating(channelSlug, deadline))
}

// RequestSuspendNow はオーナー操作で即座に準備中へ移行する（営業の途中終了）。
func (h *Hub) RequestSuspendNow(ctx context.Context, channelSlug string) error {
	h.suspendChannel(channelSlug)
	return nil
}

// kickAll はチャンネル削除時に、このノードの在室者を全員切断する。
// 切断で各クライアントの Unregister が走り、空になればローカルのチャンネル状態も消える。
func (h *Hub) kickAll(channelSlug string) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	var kicked []*Client
	if ch != nil {
		for client := range ch.clients {
			kicked = append(kicked, client)
		}
	}
	h.mu.Unlock()

	for _, client := range kicked {
		conn := client.Conn
		if client.Enqueue(ChannelKicked(channelSlug)) {
			time.AfterFunc(2*time.Second, func() {
				_ = conn.Close(websocket.StatusGoingAway, "channel deleted")
			})
		} else {
			_ = conn.Close(websocket.StatusGoingAway, "channel deleted")
		}
	}
}

// kickNonOwners はサスペンド確定時に在室中の非オーナーを切断する。
func (h *Hub) kickNonOwners(channelSlug string) {
	h.mu.Lock()
	ch := h.channels[channelSlug]
	var kicked []*Client
	if ch != nil {
		for client := range ch.clients {
			if client.User.ID != ch.ownerUserID {
				kicked = append(kicked, client)
			}
		}
	}
	h.mu.Unlock()

	for _, client := range kicked {
		conn := client.Conn
		if client.Enqueue(ChannelKicked(channelSlug)) {
			// キック通知を送ってからフォールバックで切断する。
			time.AfterFunc(2*time.Second, func() {
				_ = conn.Close(websocket.StatusPolicyViolation, "channel suspended")
			})
		} else {
			_ = conn.Close(websocket.StatusPolicyViolation, "channel suspended")
		}
	}
}

func (h *Hub) disconnectSlow(channelSlug string, slow []*Client) {
	for _, client := range slow {
		slog.Warn("disconnecting slow websocket client", "channel", channelSlug)
		_ = client.Conn.Close(websocket.StatusPolicyViolation, "slow client")
	}
}

func newNodeID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
