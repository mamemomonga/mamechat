package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mamemomonga/mamechat/backend/internal/config"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
	"github.com/mamemomonga/mamechat/backend/internal/voicevox"
)

type UserSnapshot struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"displayName"`
	Handle      string `json:"handle,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	Provider    string `json:"provider,omitempty"`
	// Subject はプロバイダ内で安定した一意ID（atprotoならDID、fediなら instance:accountID）。
	// 入室許可制御の照合に使う。クライアントには返さない。
	Subject string `json:"-"`
	Role    string `json:"role"`
	// Status はユーザーの状態（"active" / "suspended"）。停止ユーザーはフロントで
	// 「現在ご利用になれません」ページへ誘導するため、/api/me で返す。
	Status string `json:"status"`
	// GhostMode 有効時はどのチャンネルにも入室できるが書き込みはできない。
	GhostMode          bool                        `json:"ghostMode"`
	ProfileURL         string                      `json:"profileUrl,omitempty"`
	TTSVoicevoxSpeaker *TTSVoicevoxSpeakerSnapshot `json:"ttsVoicevoxSpeaker,omitempty"`
}

type TTSVoicevoxSpeakerSnapshot struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type SessionSnapshot struct {
	ID   int64
	User UserSnapshot
}

type Manager struct {
	cfg  config.Config
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewManager(cfg config.Config, pool *pgxpool.Pool) *Manager {
	return &Manager{cfg: cfg, pool: pool, q: db.New(pool)}
}

func (m *Manager) CreateSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(m.cfg.SessionTTL)
	_, err = m.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: hashToken(token),
		ExpiresAt: expiresAt,
		UserAgent: text(r.UserAgent()),
		IpPrefix:  text(ipPrefix(clientIP(r))),
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(m.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.cfg.SecureSessionCookie,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (m *Manager) CurrentUser(ctx context.Context, r *http.Request) (UserSnapshot, error) {
	session, err := m.CurrentSession(ctx, r)
	if err != nil {
		return UserSnapshot{}, err
	}
	return session.User, nil
}

func (m *Manager) CurrentSession(ctx context.Context, r *http.Request) (SessionSnapshot, error) {
	return m.currentSession(ctx, r, false)
}

// CurrentSessionAllowInactive は停止（suspended）ユーザーでもセッションが有効なら
// スナップショットを返す。停止ユーザーを「現在ご利用になれません」ページへ誘導するため、
// /api/me のみで使う。通常のAPIは CurrentSession（停止ユーザーを拒否）を使うこと。
func (m *Manager) CurrentSessionAllowInactive(ctx context.Context, r *http.Request) (SessionSnapshot, error) {
	return m.currentSession(ctx, r, true)
}

func (m *Manager) currentSession(ctx context.Context, r *http.Request, allowInactive bool) (SessionSnapshot, error) {
	cookie, err := r.Cookie(m.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return SessionSnapshot{}, pgx.ErrNoRows
	}
	session, err := m.q.GetSessionByTokenHash(ctx, hashToken(cookie.Value))
	if err != nil {
		return SessionSnapshot{}, err
	}
	user, err := m.q.GetUserByID(ctx, session.UserID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if !allowInactive && user.Status != "active" {
		return SessionSnapshot{}, pgx.ErrNoRows
	}
	provider := ""
	subject := ""
	profileURL := ""
	identity, err := m.q.GetPrimaryIdentityForUser(ctx, user.ID)
	if err == nil {
		provider = identity.Provider
		subject = identity.Subject
		if identity.ProfileUrl.Valid {
			profileURL = identity.ProfileUrl.String
		}
	} else if err != pgx.ErrNoRows {
		return SessionSnapshot{}, err
	}
	ttsSpeaker, ghostMode, err := m.ensureUserSettings(ctx, user.ID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	snapshot := SnapshotFromDB(user, provider, subject, profileURL, ttsSpeaker)
	snapshot.GhostMode = ghostMode
	return SessionSnapshot{ID: session.ID, User: snapshot}, nil
}

func (m *Manager) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(m.cfg.SessionCookieName)
	if err == nil && cookie.Value != "" {
		if err := m.q.RevokeSession(ctx, hashToken(cookie.Value)); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cfg.SecureSessionCookie,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ensureUserSettings はユーザー設定（読み上げキャラクター・ゴーストモード）を取得し、
// 行が無ければランダムキャラクターで作成する。読み上げ話者とゴーストモードを返す。
// LiveUserState は最新の読み上げ話者とゴーストモードをDBから取得する。
// WebSocket接続中に設定が変更されても、再接続（リロード）なしに投稿へ反映するため、
// 投稿時にこれで最新状態を読み直す。
func (m *Manager) LiveUserState(ctx context.Context, userID int64) (*TTSVoicevoxSpeakerSnapshot, bool, error) {
	return m.ensureUserSettings(ctx, userID)
}

func (m *Manager) ensureUserSettings(ctx context.Context, userID int64) (*TTSVoicevoxSpeakerSnapshot, bool, error) {
	settings, err := m.q.GetUserSettings(ctx, userID)
	if err == nil {
		if !settings.TtsVoicevoxSpeakerUuid.Valid {
			return nil, settings.GhostMode, nil
		}
		if character, ok := voicevox.CharacterByUUID(settings.TtsVoicevoxSpeakerUuid.String); ok {
			return ttsSpeakerSnapshot(character), settings.GhostMode, nil
		}
		return nil, settings.GhostMode, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, false, err
	}
	character := voicevox.RandomCharacter()
	created, err := m.q.UpsertUserSettingsTTSVoicevoxSpeaker(ctx, db.UpsertUserSettingsTTSVoicevoxSpeakerParams{
		UserID:                 userID,
		TtsVoicevoxSpeakerUuid: pgtype.Text{String: character.UUID, Valid: true},
	})
	if err != nil {
		return nil, false, err
	}
	return ttsSpeakerSnapshot(character), created.GhostMode, nil
}

func SnapshotFromDB(user db.User, provider string, subject string, profileURL string, ttsSpeaker *TTSVoicevoxSpeakerSnapshot) UserSnapshot {
	return UserSnapshot{
		ID:                 user.ID,
		DisplayName:        user.DisplayName,
		Handle:             textValue(user.Handle),
		AvatarURL:          textValue(user.AvatarUrl),
		Provider:           provider,
		Subject:            subject,
		Role:               user.Role,
		Status:             user.Status,
		ProfileURL:         profileURL,
		TTSVoicevoxSpeaker: ttsSpeaker,
	}
}

func ttsSpeakerSnapshot(character voicevox.Character) *TTSVoicevoxSpeakerSnapshot {
	if character.UUID == "" {
		return nil
	}
	return &TTSVoicevoxSpeakerSnapshot{
		UUID: character.UUID,
		Name: character.Name,
		URL:  character.URL,
	}
}

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func text(v string) pgtype.Text {
	if strings.TrimSpace(v) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// clientIP はリバースプロキシ（nginx）経由でも実クライアントIPを返す。
// nginx が realip で付与する X-Real-IP を優先し、無ければ X-Forwarded-For の先頭、
// それも無ければ接続元(RemoteAddr)を使う。到達経路はプロキシのみである前提。
func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipPrefix(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IPv4(v4[0], v4[1], v4[2], 0).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
