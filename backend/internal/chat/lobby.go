package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/mamemomonga/mamechat/backend/internal/auth"
)

// lobbyRefreshInterval はロビー（チャンネル一覧）プレゼンスの再構築・再配信間隔。
const lobbyRefreshInterval = 3 * time.Second

// LobbyClient はチャンネル一覧をリアルタイム購読する接続。投稿はせず、
// 全チャンネルのプレゼンス要約を受信するだけの読み取り専用クライアント。
type LobbyClient struct {
	Conn *websocket.Conn
	Send chan ServerMessage
}

func NewLobbyClient(conn *websocket.Conn) *LobbyClient {
	return &LobbyClient{
		Conn: conn,
		Send: make(chan ServerMessage, 8),
	}
}

func (c *LobbyClient) Enqueue(msg ServerMessage) bool {
	select {
	case c.Send <- msg:
		return true
	default:
		return false
	}
}

func (c *LobbyClient) WriteLoop(ctx context.Context) {
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
				slog.Warn("marshal lobby message failed", "error", err)
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

// lobbyState はロビー購読クライアントの集合と再配信ループの起動状態を保持する。
type lobbyState struct {
	mu      sync.Mutex
	clients map[*LobbyClient]struct{}
	running bool
}

// RegisterLobby はロビー購読を登録し、必要なら再配信ループを起動する。
// 登録直後にも一度配信して、初期表示を待たせない。
func (h *Hub) RegisterLobby(client *LobbyClient) {
	h.lobby.mu.Lock()
	if h.lobby.clients == nil {
		h.lobby.clients = make(map[*LobbyClient]struct{})
	}
	h.lobby.clients[client] = struct{}{}
	start := !h.lobby.running
	if start {
		h.lobby.running = true
	}
	h.lobby.mu.Unlock()

	if start {
		go h.runLobby()
	} else {
		go h.broadcastLobby()
	}
}

func (h *Hub) UnregisterLobby(client *LobbyClient) {
	h.lobby.mu.Lock()
	delete(h.lobby.clients, client)
	h.lobby.mu.Unlock()
	close(client.Send)
}

// runLobby はロビー購読者がいる間、一定間隔で全チャンネルのプレゼンスを再配信する。
func (h *Hub) runLobby() {
	ticker := time.NewTicker(lobbyRefreshInterval)
	defer ticker.Stop()
	for {
		h.lobby.mu.Lock()
		empty := len(h.lobby.clients) == 0
		if empty {
			h.lobby.running = false
		}
		h.lobby.mu.Unlock()
		if empty {
			return
		}
		h.broadcastLobby()
		<-ticker.C
	}
}

// broadcastLobby は全チャンネルのプレゼンス要約を組み立て、ロビー購読者へ一括配信する。
func (h *Hub) broadcastLobby() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	channels, err := h.q.ListChannels(ctx)
	if err != nil {
		slog.Warn("lobby list channels failed", "error", err)
		return
	}
	summaries := make([]LobbyChannel, 0, len(channels))
	for _, ch := range channels {
		var ownerID int64
		if ch.OwnerUserID.Valid {
			ownerID = ch.OwnerUserID.Int64
		}
		ps := h.buildPresence(ctx, ch.Slug, ch.ID, ownerID, "")
		summaries = append(summaries, LobbyChannel{
			ChannelSlug:     ch.Slug,
			Owner:           ps.Owner,
			Members:         ps.Members,
			ActiveCount:     ps.ActiveCount,
			TotalCount:      ps.TotalCount,
			SuspendDeadline: h.suspendDeadlineFor(ch.Slug),
		})
	}
	msg := LobbyPresence(summaries)

	h.lobby.mu.Lock()
	clients := make([]*LobbyClient, 0, len(h.lobby.clients))
	for c := range h.lobby.clients {
		clients = append(clients, c)
	}
	h.lobby.mu.Unlock()
	for _, c := range clients {
		c.Enqueue(msg)
	}
}

// LobbyWSHandler はチャンネル一覧のリアルタイムプレゼンスを配信するWebSocketハンドラ。
type LobbyWSHandler struct {
	Hub           *Hub
	Auth          *auth.Manager
	Version       string
	AllowedOrigin string
}

func (h *LobbyWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Auth.CurrentSession(r.Context(), r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{h.AllowedOrigin},
	})
	if err != nil {
		slog.Warn("lobby websocket accept failed", "error", err)
		return
	}
	conn.SetReadLimit(maxWSMessageBytes)

	client := NewLobbyClient(conn)
	h.Hub.RegisterLobby(client)
	defer h.Hub.UnregisterLobby(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go client.WriteLoop(ctx)
	// チャンネル一覧だけを開いている利用者にも、サーバ更新時のリロード判定を届ける。
	if h.Version != "" {
		_ = client.Enqueue(VersionMessage(h.Version))
	}

	// 読み取りループは切断検知と ping 応答のみ（購読のみで投稿はしない）。
	for {
		msgType, payload, err := conn.Read(ctx)
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		if msgType != websocket.MessageText {
			continue
		}
		var msg ClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Type == "ping" {
			_ = client.Enqueue(Pong())
		}
	}
}
