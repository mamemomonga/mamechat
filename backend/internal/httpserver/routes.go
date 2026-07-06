package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/access"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/atproto"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/auth"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/chat"
	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/mastodon"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/misskey"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/store"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/tts"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/ttsautodict"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/voicevox"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{6,48}[a-z0-9]$`)
var ttsAssetPattern = regexp.MustCompile(`^[a-f0-9]{64}\.m4a$`)
var ttsAutoDictionaryReadingPattern = regexp.MustCompile(`^[\p{Katakana}ー・]{1,64}$`)

const (
	maxChannelTitleLen       = 50
	maxChannelDescriptionLen = 200
	maxTTSAutoDictionaryLen  = 64
	maxTTSAutoDictionaryBulk = 5000
)

// validateChannelText はチャンネルのタイトル・説明の文字数を検証する。
// 問題があればエラーメッセージを返す（問題なければ空文字）。
func validateChannelText(title, description string) string {
	if utf8.RuneCountInString(title) > maxChannelTitleLen {
		return "title must be 50 characters or fewer"
	}
	if utf8.RuneCountInString(description) > maxChannelDescriptionLen {
		return "description must be 200 characters or fewer"
	}
	return ""
}

func validateTTSAutoDictionaryEntry(term, reading string) (string, string, string, string) {
	term = strings.TrimSpace(term)
	reading = strings.TrimSpace(reading)
	if term == "" {
		return "", "", "", "term is required"
	}
	if reading == "" {
		return "", "", "", "reading is required"
	}
	if strings.ContainsAny(term, "\r\n") {
		return "", "", "", "term must be a single line"
	}
	if utf8.RuneCountInString(term) > maxTTSAutoDictionaryLen {
		return "", "", "", "term must be 64 characters or fewer"
	}
	if !ttsAutoDictionaryReadingPattern.MatchString(reading) {
		return "", "", "", "reading must be 1-64 katakana characters"
	}
	return term, reading, ttsautodict.TermKey(term), ""
}

var errOwnerMustStayActive = errors.New("owner must stay active")
var errOwnerRoleLocked = errors.New("owner role locked")

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /channels/{slug}", s.channelOGP)
	s.mux.HandleFunc("GET /api/config", s.publicConfig)
	s.mux.HandleFunc("GET /api/service-info", s.serviceInfo)
	s.mux.HandleFunc("GET /oauth/atproto/client-metadata.json", s.atprotoClientMetadata)
	s.mux.HandleFunc("POST /api/auth/atproto/start", s.atprotoLoginStart)
	s.mux.HandleFunc("GET /api/auth/atproto/callback", s.atprotoCallback)
	s.mux.HandleFunc("POST /api/auth/mastodon/start", s.mastodonLoginStart)
	s.mux.HandleFunc("GET /api/auth/mastodon/callback", s.mastodonCallback)
	s.mux.HandleFunc("POST /api/auth/misskey/start", s.misskeyLoginStart)
	s.mux.HandleFunc("GET /api/auth/misskey/callback", s.misskeyCallback)
	s.mux.HandleFunc("POST /api/owner/login", s.ownerLogin)
	s.mux.HandleFunc("GET /api/admin/stats", s.adminStats)
	s.mux.HandleFunc("GET /api/admin/sessions", s.adminSessions)
	s.mux.HandleFunc("POST /api/admin/sessions/{sessionID}/revoke", s.adminRevokeSession)
	s.mux.HandleFunc("POST /api/admin/sessions/revoke-all", s.adminRevokeAllSessions)
	s.mux.HandleFunc("GET /api/admin/users", s.adminUsers)
	s.mux.HandleFunc("PUT /api/admin/users/{userID}", s.adminUpdateUser)
	s.mux.HandleFunc("DELETE /api/admin/users/{userID}", s.adminDeleteUser)
	s.mux.HandleFunc("GET /api/admin/tts-dictionary", s.adminListTTSAutoDictionary)
	s.mux.HandleFunc("POST /api/admin/tts-dictionary", s.adminUpsertTTSAutoDictionary)
	s.mux.HandleFunc("DELETE /api/admin/tts-dictionary", s.adminDeleteTTSAutoDictionary)
	s.mux.HandleFunc("POST /api/admin/tts-dictionary/import", s.adminImportTTSAutoDictionary)
	s.mux.HandleFunc("POST /api/admin/channels", s.adminCreateChannel)
	s.mux.HandleFunc("DELETE /api/admin/channels/{slug}", s.adminDeleteChannel)
	s.mux.HandleFunc("PUT /api/admin/channels/{slug}/suspend", s.adminSetChannelSuspendRetention)
	s.mux.HandleFunc("PUT /api/admin/channels/{slug}/grace", s.adminSetChannelSuspendGrace)
	s.mux.HandleFunc("PUT /api/admin/channels/{slug}/operating-unlimited", s.adminSetChannelOperatingUnlimited)
	s.mux.HandleFunc("DELETE /api/admin/messages/{messageID}", s.adminDeleteMessage)
	s.mux.HandleFunc("GET /api/admin/service-settings", s.adminServiceSettings)
	s.mux.HandleFunc("PUT /api/admin/service-settings/overview", s.adminUpdateServiceOverview)
	s.mux.HandleFunc("PUT /api/admin/service-settings/whitelist", s.adminUpdateWhitelistEnabled)
	s.mux.HandleFunc("POST /api/admin/service-settings/header", s.adminUploadServiceHeader)
	s.mux.HandleFunc("DELETE /api/admin/service-settings/header", s.adminDeleteServiceHeader)
	s.mux.HandleFunc("POST /api/logout", s.logout)
	s.mux.HandleFunc("GET /api/me", s.me)
	s.mux.HandleFunc("GET /api/me/deck", s.getDeckLayout)
	s.mux.HandleFunc("PUT /api/me/deck", s.saveDeckLayout)
	s.mux.HandleFunc("PUT /api/account", s.updateAccount)
	s.mux.HandleFunc("GET /api/settings/voicevox-speakers", s.listVoicevoxSpeakers)
	s.mux.HandleFunc("PUT /api/settings/voicevox-speaker", s.updateVoicevoxSpeaker)
	s.mux.HandleFunc("POST /api/settings/voicevox-speaker/preview", s.previewVoicevoxSpeaker)
	s.mux.HandleFunc("PUT /api/settings/ghost-mode", s.updateGhostMode)
	s.mux.HandleFunc("GET /api/channels", s.listChannels)
	s.mux.HandleFunc("POST /api/channels", s.createChannel)
	s.mux.HandleFunc("GET /api/channels/{slug}", s.getChannel)
	s.mux.HandleFunc("GET /api/channels/{slug}/messages", s.listMessages)
	s.mux.HandleFunc("DELETE /api/channels/{slug}/messages", s.clearChannelMessages)
	s.mux.HandleFunc("DELETE /api/channels/{slug}/messages/{messageID}", s.deleteChannelMessage)
	s.mux.HandleFunc("GET /api/channels/{slug}/messages/{messageID}/tts", s.getMessageTTS)
	s.mux.HandleFunc("PUT /api/channels/{slug}/settings", s.updateChannelSettings)
	s.mux.HandleFunc("PUT /api/channels/{slug}/suspend-retention", s.setChannelSuspendRetentionByOwner)
	s.mux.HandleFunc("PUT /api/channels/{slug}/suspend-grace", s.setChannelSuspendGraceByOwner)
	s.mux.HandleFunc("POST /api/channels/{slug}/operating", s.startOperatingByOwner)
	s.mux.HandleFunc("POST /api/channels/{slug}/operating/open", s.openChannelByOwner)
	s.mux.HandleFunc("POST /api/channels/{slug}/operating/duration", s.setOperatingDurationByOwner)
	s.mux.HandleFunc("POST /api/channels/{slug}/operating/extend", s.extendOperatingByOwner)
	s.mux.HandleFunc("POST /api/channels/{slug}/rest", s.suspendNowByOwner)
	s.mux.HandleFunc("PUT /api/channels/{slug}/notify", s.setChannelNotify)
	s.mux.HandleFunc("POST /api/push/subscribe", s.subscribePush)
	s.mux.HandleFunc("POST /api/push/unsubscribe", s.unsubscribePush)
	s.mux.HandleFunc("POST /api/push/test", s.testPush)
	s.mux.HandleFunc("POST /api/channels/{slug}/access/resolve", s.resolveAccessEntryHandler)
	s.mux.HandleFunc("POST /api/channels/{slug}/images", s.uploadChannelImage)
	s.mux.HandleFunc("DELETE /api/channels/{slug}", s.deleteChannel)
	s.mux.HandleFunc("GET /api/tts/{asset}", s.getTTSAsset)
	s.mux.HandleFunc("GET /api/uploads/{path...}", s.serveUpload)
	s.mux.HandleFunc("GET /api/og", s.getLinkPreview)
	s.mux.Handle("GET /ws/channels/{slug}", &chat.WSHandler{
		Hub:           s.hub,
		Queries:       s.q,
		Auth:          s.auth,
		Bus:           s.bus,
		TTS:           s.tts,
		Images:        s.valkey,
		Version:       s.cfg.Version,
		AllowedOrigin: s.cfg.CORSAllowedOrigin,
		MaxMessageLen: s.cfg.MessageMaxLength,
	})
	s.mux.Handle("GET /ws/lobby", &chat.LobbyWSHandler{
		Hub:           s.hub,
		Auth:          s.auth,
		Version:       s.cfg.Version,
		AllowedOrigin: s.cfg.CORSAllowedOrigin,
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"messageMaxLength":  s.cfg.MessageMaxLength,
		"serviceName":       s.cfg.ServiceName,
		"activePollSeconds": s.cfg.ActivePollSeconds,
		"beaconSeconds":     s.cfg.BeaconSeconds,
		// Push が有効なときのみ公開鍵を返す。フロントはこれで購読可否を判断する。
		"pushEnabled":    s.push != nil,
		"vapidPublicKey": s.cfg.VAPIDPublicKey,
		// ホワイトリスト機能のサーバ全体 有効/無効。フロントは入室許可UI・バッヂの出し分けに使う。
		"whitelistEnabled": s.whitelistEnabled(r.Context()),
	})
}

func (s *Server) getTTSAsset(w http.ResponseWriter, r *http.Request) {
	if _, err := s.auth.CurrentUser(r.Context(), r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	assetName := r.PathValue("asset")
	if !ttsAssetPattern.MatchString(assetName) {
		writeError(w, http.StatusNotFound, "tts asset not found")
		return
	}
	contentHash := strings.TrimSuffix(assetName, ".m4a")
	asset, err := s.q.GetTTSAsset(r.Context(), contentHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tts asset not found")
			return
		}
		slog.Error("get tts asset failed", "content_hash", contentHash, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load tts asset")
		return
	}
	info, err := os.Stat(asset.FilePath)
	if err != nil {
		slog.Warn("tts asset file missing", "content_hash", contentHash, "file_path", asset.FilePath, "error", err)
		writeError(w, http.StatusNotFound, "tts asset not found")
		return
	}
	if info.Size() == 0 {
		slog.Warn("tts asset file is empty", "content_hash", contentHash, "file_path", asset.FilePath)
		if err := s.q.DeleteTTSAsset(r.Context(), contentHash); err != nil {
			slog.Warn("delete empty tts asset row failed", "content_hash", contentHash, "error", err)
		}
		writeError(w, http.StatusNotFound, "tts asset not found")
		return
	}
	if err := s.q.TouchTTSAsset(r.Context(), contentHash); err != nil {
		slog.Warn("touch tts asset failed", "content_hash", contentHash, "error", err)
	}
	w.Header().Set("Content-Type", "audio/mp4")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, asset.FilePath)
}

func (s *Server) atprotoClientMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.atproto.ClientMetadata())
}

func (s *Server) atprotoLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	authorizationURL, err := s.atproto.StartOAuth(r.Context(), req.Identifier)
	if err != nil {
		if errors.Is(err, atproto.ErrInvalidOAuthConfig) {
			writeError(w, http.StatusServiceUnavailable, "atproto oauth is not configured")
			return
		}
		slog.Error("start atproto oauth failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to start atproto oauth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": authorizationURL})
}

func (s *Server) atprotoCallback(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	user, err := s.atproto.CompleteOAuth(r.Context(), values.Get("state"), values.Get("iss"), values.Get("code"))
	if err != nil {
		slog.Warn("atproto oauth callback failed", "error", err)
		s.redirectLoginError(w, r, s.cfg.AtprotoRedirectURL, "atproto")
		return
	}
	if err := s.auth.CreateSession(r.Context(), w, r, user.ID); err != nil {
		slog.Error("atproto session creation failed", "user_id", user.ID, "error", err)
		s.redirectLoginError(w, r, s.cfg.AtprotoRedirectURL, "session")
		return
	}
	http.Redirect(w, r, s.cfg.AtprotoRedirectURL, http.StatusSeeOther)
}

func (s *Server) mastodonLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceURL string `json:"instanceUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	authorizationURL, err := s.mastodon.StartOAuth(r.Context(), req.InstanceURL)
	if err != nil {
		if errors.Is(err, mastodon.ErrInvalidInstance) {
			writeError(w, http.StatusBadRequest, "invalid mastodon instance url")
			return
		}
		slog.Error("start mastodon oauth failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to start mastodon oauth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": authorizationURL})
}

func (s *Server) mastodonCallback(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	user, err := s.mastodon.CompleteOAuth(r.Context(), values.Get("state"), values.Get("code"))
	if err != nil {
		slog.Warn("mastodon oauth callback failed", "error", err)
		s.redirectLoginError(w, r, s.cfg.MastodonRedirectURL, "mastodon")
		return
	}
	if err := s.auth.CreateSession(r.Context(), w, r, user.ID); err != nil {
		slog.Error("mastodon session creation failed", "user_id", user.ID, "error", err)
		s.redirectLoginError(w, r, s.cfg.MastodonRedirectURL, "session")
		return
	}
	http.Redirect(w, r, s.cfg.MastodonRedirectURL, http.StatusSeeOther)
}

func (s *Server) misskeyLoginStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceURL string `json:"instanceUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	authorizationURL, err := s.misskey.StartMiAuth(r.Context(), req.InstanceURL)
	if err != nil {
		if errors.Is(err, misskey.ErrInvalidInstance) {
			writeError(w, http.StatusBadRequest, "invalid misskey instance url")
			return
		}
		slog.Error("start misskey miauth failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to start misskey miauth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": authorizationURL})
}

func (s *Server) misskeyCallback(w http.ResponseWriter, r *http.Request) {
	session := r.URL.Query().Get("session")
	user, err := s.misskey.CompleteMiAuth(r.Context(), session)
	if err != nil {
		slog.Warn("misskey miauth callback failed", "error", err)
		s.redirectLoginError(w, r, s.cfg.MisskeyRedirectURL, "misskey")
		return
	}
	if err := s.auth.CreateSession(r.Context(), w, r, user.ID); err != nil {
		slog.Error("misskey session creation failed", "user_id", user.ID, "error", err)
		s.redirectLoginError(w, r, s.cfg.MisskeyRedirectURL, "session")
		return
	}
	http.Redirect(w, r, s.cfg.MisskeyRedirectURL, http.StatusSeeOther)
}

func (s *Server) ownerLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, ok, err := auth.AuthenticateOwner(r.Context(), s.pool, s.cfg, req.Password)
	if err != nil {
		slog.Error("owner login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to login")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	if err := s.auth.CreateSession(r.Context(), w, r, user.ID); err != nil {
		slog.Error("owner login session creation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": apiUser(user)})
}

func (s *Server) redirectLoginError(w http.ResponseWriter, r *http.Request, redirectBase, code string) {
	parsed, err := url.Parse(redirectBase + "/login")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to login")
		return
	}
	values := parsed.Query()
	values.Set("error", code)
	parsed.RawQuery = values.Encode()
	http.Redirect(w, r, parsed.String(), http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(r.Context(), w, r); err != nil {
		slog.Error("logout failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to logout")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	// 停止ユーザーも「現在ご利用になれません」ページへ誘導するため、状態付きで返す
	// （通常APIは CurrentSession が停止ユーザーを拒否する）。
	session, err := s.auth.CurrentSessionAllowInactive(r.Context(), r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	user := session.User
	out := apiUser(user)
	// 停止ユーザーには管理判定を行わない（余計なクエリを避ける）。
	if user.Status == "active" {
		out.CanManage = s.userCanManage(r.Context(), user)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": out})
}

// userCanManage は「管理」機能（ゴーストモード）を使えるユーザーか判定する。
// 管理者/オーナー権限を持つか、チャンネルを1つ以上所有していれば true。
func (s *Server) userCanManage(ctx context.Context, user auth.UserSnapshot) bool {
	if auth.IsPrivileged(user.Role) {
		return true
	}
	count, err := s.q.CountChannelsOwnedByUser(ctx, pgtype.Int8{Int64: user.ID, Valid: true})
	if err != nil {
		slog.Warn("count owned channels failed", "user_id", user.ID, "error", err)
		return false
	}
	return count > 0
}

// updateGhostMode は管理者/オーナー（または1つ以上チャンネルを所有するユーザー）が
// ゴーストモードのオン/オフを切り替える。
func (s *Server) updateGhostMode(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.userCanManage(r.Context(), session.User) {
		writeError(w, http.StatusForbidden, "管理者・オーナーのみ利用できます")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.q.SetUserSettingsGhostMode(r.Context(), db.SetUserSettingsGhostModeParams{
		UserID:    session.User.ID,
		GhostMode: req.Enabled,
	}); err != nil {
		slog.Error("set ghost mode failed", "user_id", session.User.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "ゴーストモードの設定に失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ghostMode": req.Enabled})
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
		AvatarURL   string `json:"avatarUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	updated, err := s.q.UpdateUserProfile(r.Context(), db.UpdateUserProfileParams{
		ID:          session.User.ID,
		DisplayName: displayName,
		Handle:      nullableText(session.User.Handle),
		AvatarUrl:   nullableText(strings.TrimSpace(req.AvatarURL)),
	})
	if err != nil {
		slog.Error("update account failed", "user_id", session.User.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userResponse{
		ID:                 strconv.FormatInt(updated.ID, 10),
		DisplayName:        updated.DisplayName,
		Handle:             textValue(updated.Handle),
		AvatarURL:          textValue(updated.AvatarUrl),
		Provider:           session.User.Provider,
		Role:               updated.Role,
		TTSVoicevoxSpeaker: apiTTSSpeaker(session.User.TTSVoicevoxSpeaker),
	}})
}

func (s *Server) listVoicevoxSpeakers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.auth.CurrentUser(r.Context(), r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"speakers": voicevox.Characters})
}

func (s *Server) updateVoicevoxSpeaker(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		SpeakerUUID string `json:"speakerUuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	speakerUUID := strings.TrimSpace(req.SpeakerUUID)
	if speakerUUID == "" {
		if _, err := s.q.UpsertUserSettingsTTSVoicevoxSpeaker(r.Context(), db.UpsertUserSettingsTTSVoicevoxSpeakerParams{
			UserID: session.User.ID,
		}); err != nil {
			slog.Error("disable voicevox speaker failed", "user_id", session.User.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update speaker")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"speaker": nil})
		return
	}
	character, ok := voicevox.CharacterByUUID(speakerUUID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown speaker")
		return
	}
	if _, err := s.q.UpsertUserSettingsTTSVoicevoxSpeaker(r.Context(), db.UpsertUserSettingsTTSVoicevoxSpeakerParams{
		UserID:                 session.User.ID,
		TtsVoicevoxSpeakerUuid: pgtype.Text{String: character.UUID, Valid: true},
	}); err != nil {
		slog.Error("update voicevox speaker failed", "user_id", session.User.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update speaker")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"speaker": apiTTSSpeaker(&auth.TTSVoicevoxSpeakerSnapshot{
		UUID: character.UUID,
		Name: character.Name,
		URL:  character.URL,
	})})
}

// previewVoicevoxSpeaker は指定キャラクターの声で試聴用の固定文を合成し、その音声URLを返す。
func (s *Server) previewVoicevoxSpeaker(w http.ResponseWriter, r *http.Request) {
	if _, err := s.auth.CurrentUser(r.Context(), r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.tts == nil {
		writeError(w, http.StatusServiceUnavailable, "tts disabled")
		return
	}
	var req struct {
		SpeakerUUID string `json:"speakerUuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	character, ok := voicevox.CharacterByUUID(strings.TrimSpace(req.SpeakerUUID))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown speaker")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	hash, err := s.tts.SpeakerPreview(ctx, character.UUID)
	if err != nil {
		slog.Error("voicevox speaker preview failed", "speaker_uuid", character.UUID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to synthesize preview")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audioUrl": "/api/tts/" + hash + ".m4a"})
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	stats, err := s.q.GetAdminStats(r.Context())
	if err != nil {
		slog.Error("get admin stats failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": statsResponse{
		UsersCount:          stats.UsersCount,
		ChannelsCount:       stats.ChannelsCount,
		ChatMessagesCount:   stats.ChatMessagesCount,
		ActiveSessionsCount: stats.ActiveSessionsCount,
	}})
}

func (s *Server) adminSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	sessions, err := s.q.ListActiveSessions(r.Context())
	if err != nil {
		slog.Error("list admin sessions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	out := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, apiSession(session))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) adminRevokeSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	sessionID, err := strconv.ParseInt(r.PathValue("sessionID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	session, err := s.q.RevokeSessionByID(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		slog.Error("revoke session failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to revoke session")
		return
	}
	s.hub.DisconnectSession(session.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminRevokeAllSessions は全ブラウザセッションを失効し、全WebSocketを切断する。
// 実行者自身のセッションも失効するため、実行後は再ログインが必要になる。
func (s *Server) adminRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.q.RevokeAllSessions(r.Context()); err != nil {
		slog.Error("revoke all sessions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	s.hub.DisconnectAll()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	users, err := s.q.ListAdminUsers(r.Context())
	if err != nil {
		slog.Error("list admin users failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	out := make([]adminUserResponse, 0, len(users))
	for _, user := range users {
		out = append(out, apiAdminUser(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
		Handle      string `json:"handle"`
		AvatarURL   string `json:"avatarUrl"`
		Status      string `json:"status"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	status := strings.TrimSpace(req.Status)
	if displayName == "" {
		writeError(w, http.StatusBadRequest, "displayName is required")
		return
	}
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "suspended" {
		writeError(w, http.StatusBadRequest, "status must be active or suspended")
		return
	}
	role := normalizeRole(req.Role)
	if role == "" {
		role = auth.UserRole
	}
	if role != auth.UserRole && role != auth.AdminRole && role != auth.OwnerRole {
		writeError(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	var identity db.AuthIdentity
	var updated db.User
	var statusChanged bool
	if err := store.WithTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		current, err := q.GetUserByID(r.Context(), userID)
		if err != nil {
			return err
		}
		statusChanged = current.Status != status
		identity, err = q.GetPrimaryIdentityForUser(r.Context(), userID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current.Role == auth.OwnerRole {
			if status != "active" {
				return errOwnerMustStayActive
			}
			if role != auth.OwnerRole {
				return errOwnerRoleLocked
			}
		} else if role == auth.OwnerRole {
			return errOwnerRoleLocked
		}
		updated, err = q.UpdateUser(r.Context(), db.UpdateUserParams{
			ID:          userID,
			DisplayName: displayName,
			Handle:      nullableText(req.Handle),
			AvatarUrl:   nullableText(req.AvatarURL),
			Status:      status,
			Role:        role,
		})
		return err
	}); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "user not found")
		case errors.Is(err, errOwnerMustStayActive):
			writeError(w, http.StatusBadRequest, "owner user must stay active")
		case errors.Is(err, errOwnerRoleLocked):
			writeError(w, http.StatusBadRequest, "owner role cannot be changed")
		default:
			slog.Error("update user failed", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update user")
		}
		return
	}
	// ユーザーの状態が切り替えられたら、当該ユーザーの全セッションを失効し、
	// 開いているWebSocketも切断する。停止ユーザーは再ログインで停止状態が反映され、
	// 復帰ユーザーは再ログインで通常状態に戻る（このセッションが消えるまで前の状態が続く）。
	if statusChanged {
		if err := s.q.RevokeSessionsForUser(r.Context(), userID); err != nil {
			slog.Error("revoke sessions for user failed", "user_id", userID, "error", err)
		}
		s.hub.DisconnectUser(userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userResponse{
		ID:          strconv.FormatInt(updated.ID, 10),
		DisplayName: updated.DisplayName,
		Handle:      textValue(updated.Handle),
		AvatarURL:   textValue(updated.AvatarUrl),
		Provider:    identity.Provider,
		Role:        updated.Role,
	}})
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := s.q.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("get delete user failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	if user.Role == auth.OwnerRole {
		writeError(w, http.StatusBadRequest, "owner user cannot be deleted")
		return
	}
	if err := s.q.DeleteUser(r.Context(), userID); err != nil {
		slog.Error("delete user failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminListTTSAutoDictionary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	rows, err := s.q.ListTTSAutoDictionaryEntries(r.Context())
	if err != nil {
		slog.Error("list tts auto dictionary failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tts dictionary")
		return
	}
	out := make([]ttsAutoDictionaryEntryResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiTTSAutoDictionaryEntry(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func (s *Server) adminUpsertTTSAutoDictionary(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		Term    string `json:"term"`
		Reading string `json:"reading"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	term, reading, termKey, msg := validateTTSAutoDictionaryEntry(req.Term, req.Reading)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	row, err := s.q.UpsertTTSAutoDictionaryEntry(r.Context(), db.UpsertTTSAutoDictionaryEntryParams{
		TermKey:            termKey,
		Term:               term,
		Reading:            reading,
		RegisteredByUserID: pgtype.Int8{Int64: session.User.ID, Valid: true},
		RegisteredByHandle: nullableText(session.User.Handle),
	})
	if err != nil {
		slog.Error("upsert tts auto dictionary failed", "term", term, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save tts dictionary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": apiTTSAutoDictionaryEntry(row)})
}

func (s *Server) adminDeleteTTSAutoDictionary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	termKey := strings.TrimSpace(r.URL.Query().Get("termKey"))
	if termKey == "" {
		writeError(w, http.StatusBadRequest, "termKey is required")
		return
	}
	tag, err := s.q.DeleteTTSAutoDictionaryEntry(r.Context(), termKey)
	if err != nil {
		slog.Error("delete tts auto dictionary failed", "term_key", termKey, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete tts dictionary")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "tts dictionary entry not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminImportTTSAutoDictionary(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		Entries []struct {
			Term    string `json:"term"`
			Reading string `json:"reading"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Entries) > maxTTSAutoDictionaryBulk {
		writeError(w, http.StatusBadRequest, "too many dictionary entries")
		return
	}
	type cleanedEntry struct {
		term    string
		reading string
		termKey string
	}
	cleaned := make([]cleanedEntry, 0, len(req.Entries))
	for i, entry := range req.Entries {
		term, reading, termKey, msg := validateTTSAutoDictionaryEntry(entry.Term, entry.Reading)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "entry "+strconv.Itoa(i+1)+": "+msg)
			return
		}
		cleaned = append(cleaned, cleanedEntry{term: term, reading: reading, termKey: termKey})
	}
	out := make([]ttsAutoDictionaryEntryResponse, 0, len(cleaned))
	if err := store.WithTx(r.Context(), s.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		for _, entry := range cleaned {
			row, err := q.UpsertTTSAutoDictionaryEntry(r.Context(), db.UpsertTTSAutoDictionaryEntryParams{
				TermKey:            entry.termKey,
				Term:               entry.term,
				Reading:            entry.reading,
				RegisteredByUserID: pgtype.Int8{Int64: session.User.ID, Valid: true},
				RegisteredByHandle: nullableText(session.User.Handle),
			})
			if err != nil {
				return err
			}
			out = append(out, apiTTSAutoDictionaryEntry(row))
		}
		return nil
	}); err != nil {
		slog.Error("import tts auto dictionary failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to import tts dictionary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "imported": len(out)})
}

func (s *Server) adminCreateChannel(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	slug := normalizeSlug(req.Slug)
	title := strings.TrimSpace(req.Title)
	description := strings.TrimSpace(req.Description)
	if !slugPattern.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "slug must be 8-50 lowercase letters, numbers, or hyphens")
		return
	}
	if msg := validateChannelText(title, description); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if title == "" {
		title = slug
	}
	channel, err := s.q.CreateChannel(r.Context(), db.CreateChannelParams{
		Slug:        slug,
		Title:       title,
		Description: nullableText(description),
		OwnerUserID: pgtype.Int8{Int64: session.User.ID, Valid: true},
	})
	if err != nil {
		slog.Error("admin create channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": apiChannel(channel)})
}

func (s *Server) adminDeleteChannel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	slug := r.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "channel slug is required")
		return
	}
	if _, err := s.q.GetChannelBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get delete channel failed", "channel", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	if err := s.q.DeleteChannelBySlug(r.Context(), slug); err != nil {
		slog.Error("delete channel failed", "channel", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	messageID, err := strconv.ParseInt(r.PathValue("messageID"), 10, 64)
	if err != nil || messageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	deleted, err := s.q.DeleteChatMessageByID(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		slog.Error("delete message failed", "message_id", messageID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	s.afterMessageDeleted(r.Context(), deleted)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// deleteChannelMessage はチャンネルページからの投稿削除。投稿者本人・チャンネルオーナー・
// 管理者のみが削除できる。
func (s *Server) deleteChannelMessage(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	messageID, err := strconv.ParseInt(r.PathValue("messageID"), 10, 64)
	if err != nil || messageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for message delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	message, err := s.q.GetChatMessageByID(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		slog.Error("get message for delete failed", "message_id", messageID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	if message.ChannelID != channel.ID {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	// 投稿者本人・チャンネルオーナー・管理者のみ削除できる。
	isAuthor := message.UserID == session.User.ID
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isAuthor && !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "you cannot delete this message")
		return
	}
	deleted, err := s.q.DeleteChatMessageByID(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		slog.Error("delete message failed", "message_id", messageID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	s.afterMessageDeleted(r.Context(), deleted)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// getMessageTTS は「ここから読み上げる」用に、1メッセージ分の読み上げ音声を同期生成して
// 再生用URLを返す。バス配信ではなく要求者だけが再生する。読み上げ不可なら空で返す。
func (s *Server) getMessageTTS(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.tts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"parts": []any{}})
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for message tts failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read message")
		return
	}
	// 入室許可制御に従う（閲覧できないチャンネルの本文は読み上げさせない）。
	if !channelAccessAllowed(channel, session.User, s.whitelistEnabled(r.Context())) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	messageID, err := strconv.ParseInt(r.PathValue("messageID"), 10, 64)
	if err != nil || messageID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid message id")
		return
	}
	info, err := s.q.GetChatMessageForTTS(r.Context(), messageID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		slog.Error("get message for tts failed", "message_id", messageID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read message")
		return
	}
	if info.ChannelID != channel.ID {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}
	parts, err := s.tts.SynthesizeMessageParts(r.Context(), textValue(info.UserTtsVoicevoxSpeakerUuid), info.Body)
	if err != nil {
		// 合成失敗は致命的にせず空で返す（遡り読み上げの途中で止めない）。
		slog.Warn("synthesize message tts failed", "message_id", messageID, "error", err)
		parts = nil
	}
	if parts == nil {
		parts = []tts.SynthPart{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": parts})
}

// afterMessageDeleted は投稿削除後の後処理（添付画像の削除と削除通知の配信）を行う。
func (s *Server) afterMessageDeleted(ctx context.Context, deleted db.DeleteChatMessageByIDRow) {
	if deleted.ImagePath.Valid {
		if err := s.uploads.Delete(deleted.ImagePath.String); err != nil {
			slog.Warn("delete message image file failed", "path", deleted.ImagePath.String, "error", err)
		}
	}
	msg := chat.MessageDeleted(
		strconv.FormatInt(deleted.ID, 10),
		strconv.FormatInt(deleted.ChannelID, 10),
		deleted.ChannelSlug,
	)
	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.bus.Publish(pubCtx, deleted.ChannelSlug, msg); err != nil {
		slog.Error("publish deleted message failed", "message_id", deleted.ID, "channel", deleted.ChannelSlug, "error", err)
	}
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.CurrentUser(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channels, err := s.q.ListChannels(r.Context())
	if err != nil {
		slog.Error("list channels failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	// 入室許可制御（ホワイト/ブラックリスト）で入室不可のチャンネルは一覧から隠す。
	whitelistEnabled := s.whitelistEnabled(r.Context())
	visible := channels[:0]
	for _, ch := range channels {
		if channelAccessAllowed(ch, user, whitelistEnabled) {
			visible = append(visible, ch)
		}
	}
	channels = visible
	// サスペンド状態はDBから都度取得（リアルタイム）。並び順は最終投稿順で、
	// 計算コストを避けるためValkeyにキャッシュする（リアルタイム不要）。
	rank := s.channelOrderRank(r.Context())
	sort.SliceStable(channels, func(i, j int) bool {
		si := channels[i].SuspendedAt.Valid
		sj := channels[j].SuspendedAt.Valid
		if si != sj {
			return !si // サスペンド中は必ず後ろ
		}
		ri := rankOf(rank, channels[i].Slug)
		rj := rankOf(rank, channels[j].Slug)
		if ri != rj {
			return ri < rj // 最終投稿が新しい順（キャッシュ順）
		}
		return channels[i].CreatedAt.After(channels[j].CreatedAt)
	})
	out := make([]channelResponse, 0, len(channels))
	for _, ch := range channels {
		out = append(out, apiChannel(ch))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

const channelOrderTTL = 15 * time.Second

// channelOrderRank はキャッシュ済みの並び順（slug→順位）を返す。未キャッシュなら
// DBから最終投稿時刻を集計して並びを作りキャッシュする（最大15秒で更新）。
func (s *Server) channelOrderRank(ctx context.Context) map[string]int {
	order, ok, err := s.valkey.GetChannelOrder(ctx)
	if err != nil {
		slog.Warn("get channel order cache failed", "error", err)
	}
	if !ok {
		times, err := s.q.ListChannelLastPostTimes(ctx)
		if err != nil {
			slog.Warn("list channel last post times failed", "error", err)
			return map[string]int{}
		}
		order = sortSlugsByLastPost(times)
		if err := s.valkey.SetChannelOrder(ctx, order, channelOrderTTL); err != nil {
			slog.Warn("set channel order cache failed", "error", err)
		}
	}
	rank := make(map[string]int, len(order))
	for i, slug := range order {
		rank[slug] = i
	}
	return rank
}

func rankOf(rank map[string]int, slug string) int {
	if r, ok := rank[slug]; ok {
		return r
	}
	return len(rank) + 1 // キャッシュに無い（新規チャンネル等）→末尾
}

// sortSlugsByLastPost は最終投稿が新しい順にslugを並べる（投稿なしは末尾、同点はslug順）。
func sortSlugsByLastPost(times []db.ListChannelLastPostTimesRow) []string {
	sorted := make([]db.ListChannelLastPostTimesRow, len(times))
	copy(sorted, times)
	sort.SliceStable(sorted, func(i, j int) bool {
		vi := sorted[i].LastPostAt.Valid
		vj := sorted[j].LastPostAt.Valid
		if vi != vj {
			return vi // 投稿ありを先に
		}
		if vi && vj {
			return sorted[i].LastPostAt.Time.After(sorted[j].LastPostAt.Time)
		}
		return sorted[i].Slug < sorted[j].Slug
	})
	slugs := make([]string, len(sorted))
	for i := range sorted {
		slugs[i] = sorted[i].Slug
	}
	return slugs
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.CurrentUser(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	slug := normalizeSlug(req.Slug)
	title := strings.TrimSpace(req.Title)
	description := strings.TrimSpace(req.Description)
	if !slugPattern.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "slug must be 8-50 lowercase letters, numbers, or hyphens")
		return
	}
	if msg := validateChannelText(title, description); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if title == "" {
		title = slug
	}
	channel, err := s.q.CreateChannel(r.Context(), db.CreateChannelParams{
		Slug:        slug,
		Title:       title,
		Description: nullableText(description),
		OwnerUserID: pgtype.Int8{Int64: user.ID, Valid: true},
	})
	if err != nil {
		slog.Error("create channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": apiChannel(channel)})
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.CurrentUser(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get channel")
		return
	}
	// 入室不可ユーザーにはチャンネルの存在自体を隠す（一覧非表示と整合）。
	if !channelAccessAllowed(channel, user, s.whitelistEnabled(r.Context())) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	// オーナー/管理者には許可リストの中身も返す（設定画面で編集するため）。
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == user.ID
	var resp channelResponse
	if isOwner || auth.IsPrivileged(user.Role) {
		resp = apiChannelForOwner(channel)
	} else {
		resp = apiChannel(channel)
	}
	// このユーザーが営業開始通知をオンにしているかを反映する（Push有効時のみ意味がある）。
	if s.push != nil {
		optedIn, err := s.q.IsChannelNotificationOptedIn(r.Context(), db.IsChannelNotificationOptedInParams{
			ChannelID: channel.ID,
			UserID:    user.ID,
		})
		if err != nil {
			slog.Warn("check notify optin failed", "channel", channel.Slug, "error", err)
		}
		resp.NotifyEnabled = optedIn
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": resp})
}

// attachReactions は表示用メッセージ配列に、各投稿のリアクション集計を付与する。
func (s *Server) attachReactions(ctx context.Context, msgs []chat.ServerMessage) {
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		if id, err := strconv.ParseInt(m.ID, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := s.q.ListReactionsForMessages(ctx, ids)
	if err != nil {
		slog.Warn("list reactions for messages failed", "error", err)
		return
	}
	byMsg := chat.ReactionGroupsByMessage(rows)
	for i := range msgs {
		id, err := strconv.ParseInt(msgs[i].ID, 10, 64)
		if err != nil {
			continue
		}
		if g, ok := byMsg[id]; ok {
			msgs[i].Reactions = g
		}
	}
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.CurrentUser(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get message channel failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get channel")
		return
	}
	if !channelAccessAllowed(channel, user, s.whitelistEnabled(r.Context())) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	// afterId 指定時は、その ID より新しいメッセージを古い順に返す（スリープ復帰時の取りこぼし補完用）。
	if afterRaw := strings.TrimSpace(r.URL.Query().Get("afterId")); afterRaw != "" {
		afterID, parseErr := strconv.ParseInt(afterRaw, 10, 64)
		if parseErr != nil || afterID < 0 {
			writeError(w, http.StatusBadRequest, "invalid afterId")
			return
		}
		newer, err := s.q.ListChatMessagesAfterID(r.Context(), db.ListChatMessagesAfterIDParams{
			ChannelID: channel.ID,
			ID:        afterID,
			Limit:     200,
		})
		if err != nil {
			slog.Error("list messages after id failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list messages")
			return
		}
		out := make([]chat.ServerMessage, 0, len(newer))
		for _, msg := range newer {
			out = append(out, chat.MessageFromDB(channel.Slug, msg))
		}
		s.attachReactions(r.Context(), out)
		writeJSON(w, http.StatusOK, map[string]any{"messages": out})
		return
	}
	messages, err := s.q.ListRecentChatMessagesByChannel(r.Context(), db.ListRecentChatMessagesByChannelParams{
		ChannelID: channel.ID,
		Limit:     50,
	})
	if err != nil {
		slog.Error("list messages failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	slices.Reverse(messages)
	out := make([]chat.ServerMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, chat.MessageFromDB(channel.Slug, msg))
	}
	s.attachReactions(r.Context(), out)
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

// clearChannelMessages はチャンネルオーナー（または管理者）がメッセージを一括削除する。
func (s *Server) clearChannelMessages(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for clear failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear messages")
		return
	}
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "channel owner only")
		return
	}
	// メッセージ削除前に紐づく画像ファイルを削除する。
	s.deleteChannelImageFiles(r.Context(), channel.ID)
	if err := s.q.DeleteChatMessagesByChannelID(r.Context(), channel.ID); err != nil {
		slog.Error("clear channel messages failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to clear messages")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.bus.Publish(ctx, channel.Slug, chat.ChannelCleared(channel.Slug)); err != nil {
		slog.Error("publish channel cleared failed", "channel", channel.Slug, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// deleteChannel はチャンネルオーナー（または管理者）がチャンネルを削除する。
// チャットメッセージ・来訪者は外部キーの ON DELETE CASCADE で併せて削除される。
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "channel owner only")
		return
	}
	// チャンネル削除前に紐づく画像ファイルを削除する（DB行はCASCADEで消える）。
	s.deleteChannelImageFiles(r.Context(), channel.ID)
	if err := s.q.DeleteChannelBySlug(r.Context(), channel.Slug); err != nil {
		slog.Error("delete channel failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	// 接続中のクライアントを退出させる（フロントは channel.kicked で一覧へ戻る）。
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.bus.Publish(ctx, channel.Slug, chat.ChannelKicked(channel.Slug)); err != nil {
		slog.Error("publish channel deleted failed", "channel", channel.Slug, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// updateChannelSettings はチャンネルオーナー（または管理者）が機能トグル
// （URLリンク化・画像アップロード）を設定する。
func (s *Server) updateChannelSettings(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for settings failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "channel owner only")
		return
	}
	var req struct {
		Title              *string         `json:"title"`
		Description        *string         `json:"description"`
		UrlLinkifyEnabled  *bool           `json:"urlLinkifyEnabled"`
		ImageUploadEnabled *bool           `json:"imageUploadEnabled"`
		PostTtlHours       *int32          `json:"postTtlHours"`
		AccessMode         *string         `json:"accessMode"`
		AccessList         *[]access.Entry `json:"accessList"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// タイトル・説明はどちらかが指定されたときのみ更新する（未指定は現状値を保持）。
	if req.Title != nil || req.Description != nil {
		title := channel.Title
		if req.Title != nil {
			title = strings.TrimSpace(*req.Title)
		}
		description := textValue(channel.Description)
		if req.Description != nil {
			description = strings.TrimSpace(*req.Description)
		}
		if msg := validateChannelText(title, description); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if title == "" {
			title = channel.Slug
		}
		if err := s.q.SetChannelProfile(r.Context(), db.SetChannelProfileParams{
			Slug:        channel.Slug,
			Title:       title,
			Description: nullableText(description),
		}); err != nil {
			slog.Error("set channel profile failed", "channel", channel.Slug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update settings")
			return
		}
	}
	// 未指定の項目は現状値を保持する。
	urlLinkify := channel.UrlLinkifyEnabled
	if req.UrlLinkifyEnabled != nil {
		urlLinkify = *req.UrlLinkifyEnabled
	}
	imageUpload := channel.ImageUploadEnabled
	if req.ImageUploadEnabled != nil {
		imageUpload = *req.ImageUploadEnabled
	}
	if err := s.q.SetChannelFeatures(r.Context(), db.SetChannelFeaturesParams{
		Slug:               channel.Slug,
		UrlLinkifyEnabled:  urlLinkify,
		ImageUploadEnabled: imageUpload,
	}); err != nil {
		slog.Error("set channel features failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	// 投稿の寿命（6/24/72時間）。指定時のみ更新する。
	if req.PostTtlHours != nil {
		if *req.PostTtlHours != 6 && *req.PostTtlHours != 24 && *req.PostTtlHours != 72 {
			writeError(w, http.StatusBadRequest, "postTtlHours must be 6, 24, or 72")
			return
		}
		if err := s.q.SetChannelPostTTL(r.Context(), db.SetChannelPostTTLParams{
			Slug:         channel.Slug,
			PostTtlHours: *req.PostTtlHours,
		}); err != nil {
			slog.Error("set channel post ttl failed", "channel", channel.Slug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update settings")
			return
		}
	}
	// 入室許可制御（モード・リスト）はどちらかが指定されたときのみ更新する。
	if req.AccessMode != nil || req.AccessList != nil {
		mode := access.NormalizeMode(channel.AccessMode)
		if req.AccessMode != nil {
			if access.NormalizeMode(*req.AccessMode) != *req.AccessMode {
				writeError(w, http.StatusBadRequest, "accessMode must be none, whitelist, or blacklist")
				return
			}
			// ホワイトリスト機能がサーバ側で無効なとき、新規にホワイトリストへ切り替えるのは拒否する。
			// 既にホワイトリストのチャンネルはそのまま保存できる（休眠状態を維持し再有効化で復活）。
			if *req.AccessMode == access.ModeWhitelist &&
				access.NormalizeMode(channel.AccessMode) != access.ModeWhitelist &&
				!s.whitelistEnabled(r.Context()) {
				writeError(w, http.StatusBadRequest, "ホワイトリスト機能は無効化されています")
				return
			}
			mode = *req.AccessMode
		}
		list := access.ParseList(channel.AccessList)
		if req.AccessList != nil {
			list = access.CleanEntries(*req.AccessList)
			if len(list) > maxAccessListEntries {
				writeError(w, http.StatusBadRequest, "access list has too many entries")
				return
			}
		}
		raw, err := access.MarshalList(list)
		if err != nil {
			slog.Error("marshal access list failed", "channel", channel.Slug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update settings")
			return
		}
		if err := s.q.SetChannelAccess(r.Context(), db.SetChannelAccessParams{
			Slug:       channel.Slug,
			AccessMode: mode,
			AccessList: raw,
		}); err != nil {
			slog.Error("set channel access failed", "channel", channel.Slug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update settings")
			return
		}
	}
	updated, err := s.q.GetChannelBySlug(r.Context(), channel.Slug)
	if err != nil {
		slog.Error("reload channel after settings failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": apiChannelForOwner(updated)})
}

// maxAccessListEntries は入室許可リストに登録できる最大件数。
const maxAccessListEntries = 5000

// adminSetChannelSuspendRetention は管理者がチャンネルごとのサスペンド保持時間を設定する。
// null=既定値（環境変数）、負値=無限（削除しない）、0以上=時間。
func (s *Server) adminSetChannelSuspendRetention(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	slug := r.PathValue("slug")
	var req struct {
		SuspendRetentionHours *int32 `json:"suspendRetentionHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.q.GetChannelBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for suspend retention failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	retention := pgtype.Int4{}
	if req.SuspendRetentionHours != nil {
		retention = pgtype.Int4{Int32: *req.SuspendRetentionHours, Valid: true}
	}
	if err := s.q.SetChannelSuspendRetention(r.Context(), db.SetChannelSuspendRetentionParams{
		Slug:                  slug,
		SuspendRetentionHours: retention,
	}); err != nil {
		slog.Error("set channel suspend retention failed", "channel", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminSetChannelSuspendGrace は管理者がチャンネルごとのオーナー退出後サスペンドまでの
// 猶予を設定する。null=既定値（環境変数）、負値=無期限（サスペンドしない）、0以上=秒。
func (s *Server) adminSetChannelSuspendGrace(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	slug := r.PathValue("slug")
	var req struct {
		SuspendGraceSeconds *int32 `json:"suspendGraceSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.q.GetChannelBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for suspend grace failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	grace := pgtype.Int4{}
	if req.SuspendGraceSeconds != nil {
		grace = pgtype.Int4{Int32: *req.SuspendGraceSeconds, Valid: true}
	}
	if err := s.q.SetChannelSuspendGrace(r.Context(), db.SetChannelSuspendGraceParams{
		Slug:                slug,
		SuspendGraceSeconds: grace,
	}); err != nil {
		slog.Error("set channel suspend grace failed", "channel", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminSetChannelOperatingUnlimited は管理者がチャンネルの「時間制限なし」を切り替える。
// true のときカウントダウン・残り時間設定・自動閉店を行わず、開店/閉店のみになる。
func (s *Server) adminSetChannelOperatingUnlimited(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	slug := r.PathValue("slug")
	var req struct {
		OperatingUnlimited bool `json:"operatingUnlimited"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.q.GetChannelBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for operating unlimited failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	if err := s.hub.SetOperatingUnlimited(r.Context(), slug, req.OperatingUnlimited); err != nil {
		slog.Error("set channel operating unlimited failed", "channel", slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// deckColumn は Deck（複数カラム）レイアウトの1カラム。type="list"（チャンネル一覧）または
// type="channel"（slug のチャンネル）。クライアントが解釈し、サーバは保管のみ行う。
type deckColumn struct {
	Type  string `json:"type"`
	Slug  string `json:"slug,omitempty"`
	Width int    `json:"width,omitempty"`
}

// getDeckLayout はログインユーザーが手動保存した Deck レイアウト（1スロット）を返す。
// 未保存なら columns は空配列。
func (s *Server) getDeckLayout(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw, err := s.q.GetUserDeckLayout(r.Context(), session.User.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("get deck layout failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load deck")
		return
	}
	cols := []deckColumn{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cols); err != nil {
			cols = []deckColumn{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"columns": cols})
}

// saveDeckLayout は現在の Deck レイアウトをログインユーザーの1スロットへ上書き保存する。
func (s *Server) saveDeckLayout(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Columns []deckColumn `json:"columns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// 暴走防止の上限。
	if len(req.Columns) > 32 {
		req.Columns = req.Columns[:32]
	}
	// カラム幅は妥当な範囲へ丸める（0=未指定は既定幅）。
	for i := range req.Columns {
		if w := req.Columns[i].Width; w != 0 {
			if w < 200 {
				w = 200
			} else if w > 2000 {
				w = 2000
			}
			req.Columns[i].Width = w
		}
	}
	payload, err := json.Marshal(req.Columns)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode deck")
		return
	}
	if err := s.q.SetUserDeckLayout(r.Context(), db.SetUserDeckLayoutParams{
		UserID:     session.User.ID,
		DeckLayout: payload,
	}); err != nil {
		slog.Error("save deck layout failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save deck")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// requireChannelManager はオーナーまたは管理者のみを通し、対象チャンネルを返す。
func (s *Server) requireChannelManager(w http.ResponseWriter, r *http.Request) (db.Channel, bool) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return db.Channel{}, false
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return db.Channel{}, false
		}
		slog.Error("get channel for manage failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load channel")
		return db.Channel{}, false
	}
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "channel owner only")
		return db.Channel{}, false
	}
	return channel, true
}

// setChannelSuspendRetentionByOwner はオーナー/管理者がチャンネルの「休憩後の削除」を設定する。
// null=既定値。無期限（負値）はオーナー向けには許可しない。
func (s *Server) setChannelSuspendRetentionByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	var req struct {
		SuspendRetentionHours *int32 `json:"suspendRetentionHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.SuspendRetentionHours != nil && *req.SuspendRetentionHours < 0 {
		writeError(w, http.StatusBadRequest, "infinite is not allowed")
		return
	}
	retention := pgtype.Int4{}
	if req.SuspendRetentionHours != nil {
		retention = pgtype.Int4{Int32: *req.SuspendRetentionHours, Valid: true}
	}
	if err := s.q.SetChannelSuspendRetention(r.Context(), db.SetChannelSuspendRetentionParams{
		Slug:                  channel.Slug,
		SuspendRetentionHours: retention,
	}); err != nil {
		slog.Error("owner set channel suspend retention failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setChannelSuspendGraceByOwner はオーナー/管理者がチャンネルの「オーナー離席後の準備」を設定する。
// null=既定値、負値=無期限（離席で自動閉店しない）、0以上=その秒数。
func (s *Server) setChannelSuspendGraceByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	var req struct {
		SuspendGraceSeconds *int32 `json:"suspendGraceSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	grace := pgtype.Int4{}
	if req.SuspendGraceSeconds != nil {
		v := *req.SuspendGraceSeconds
		if v < 0 {
			v = -1 // 無期限は -1 に正規化する
		}
		grace = pgtype.Int4{Int32: v, Valid: true}
	}
	if err := s.q.SetChannelSuspendGrace(r.Context(), db.SetChannelSuspendGraceParams{
		Slug:                channel.Slug,
		SuspendGraceSeconds: grace,
	}); err != nil {
		slog.Error("owner set channel suspend grace failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// 「営業中」ボタンで設定できる営業時間（分）。30分・1時間・1時間30分・2時間・3時間。
var allowedOperatingMinutes = map[int]bool{30: true, 60: true, 90: true, 120: true, 180: true}

// 営業の延長で設定できる時間（分）。5分・15分・30分・1時間・2時間。
var allowedExtendMinutes = map[int]bool{5: true, 15: true, 30: true, 60: true, 120: true}

// startOperatingByOwner はオーナー/管理者の操作で営業を開始する（準備中→営業中、終了予定時刻を設定）。
func (s *Server) startOperatingByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	var req struct {
		DurationMinutes int `json:"durationMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !allowedOperatingMinutes[req.DurationMinutes] {
		writeError(w, http.StatusBadRequest, "durationMinutes must be one of 30, 60, 90, 120, 180")
		return
	}
	dur := time.Duration(req.DurationMinutes) * time.Minute
	if err := s.hub.RequestStartOperating(r.Context(), channel.Slug, dur); err != nil {
		slog.Error("request start operating failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start operating")
		return
	}
	// 営業中になったので、通知をオンにしているユーザーへPushを送る。
	s.notifyChannelOperating(channel)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// openChannelByOwner は「時間制限なし」チャンネルの開店を行う（準備中→営業中、終了予定なし）。
func (s *Server) openChannelByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	if err := s.hub.RequestOpenChannel(r.Context(), channel.Slug); err != nil {
		slog.Error("request open channel failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to open channel")
		return
	}
	// 営業中になったので、通知をオンにしているユーザーへPushを送る。
	s.notifyChannelOperating(channel)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// extendOperatingByOwner はオーナー/管理者の操作で営業の終了予定時刻を延長する。
func (s *Server) extendOperatingByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	var req struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !allowedExtendMinutes[req.Minutes] {
		writeError(w, http.StatusBadRequest, "minutes must be one of 5, 15, 30, 60, 120")
		return
	}
	dur := time.Duration(req.Minutes) * time.Minute
	if err := s.hub.RequestExtendOperating(r.Context(), channel.Slug, dur); err != nil {
		if errors.Is(err, chat.ErrNotOperating) {
			writeError(w, http.StatusConflict, "channel is not operating")
			return
		}
		slog.Error("request extend operating failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to extend operating")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setOperatingDurationByOwner は終了時刻指定から算出した残り時間で、営業終了予定時刻を設定する。
// 準備中なら営業開始、営業中なら残り時間の変更として扱う。
func (s *Server) setOperatingDurationByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	var req struct {
		DurationMinutes int `json:"durationMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DurationMinutes < 1 || req.DurationMinutes > 24*60 {
		writeError(w, http.StatusBadRequest, "durationMinutes must be between 1 and 1440")
		return
	}
	if err := s.hub.RequestSetOperatingDuration(
		r.Context(),
		channel.Slug,
		time.Duration(req.DurationMinutes)*time.Minute,
	); err != nil {
		if errors.Is(err, chat.ErrNotOperating) {
			writeError(w, http.StatusConflict, "channel is not operating")
			return
		}
		slog.Error("request set operating duration failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to set operating duration")
		return
	}
	if channel.SuspendedAt.Valid {
		s.notifyChannelOperating(channel)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// suspendNowByOwner はオーナー/管理者の操作で即座に準備中へ移行する（営業の途中終了）。
func (s *Server) suspendNowByOwner(w http.ResponseWriter, r *http.Request) {
	channel, ok := s.requireChannelManager(w, r)
	if !ok {
		return
	}
	if err := s.hub.RequestSuspendNow(r.Context(), channel.Slug); err != nil {
		slog.Error("request suspend now failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to suspend channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type userResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Handle      string `json:"handle,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Role        string `json:"role"`
	// Status はユーザー状態（"active" / "suspended"）。停止ユーザーの誘導判定に使う。
	Status string `json:"status,omitempty"`
	// GhostMode 有効時はどのチャンネルにも入れるが書き込めない。
	GhostMode bool `json:"ghostMode"`
	// CanManage は「管理」設定（ゴーストモード）を使えるか（管理者/オーナー or チャンネル所有者）。
	// /api/me でのみ算出する。
	CanManage          bool                `json:"canManage"`
	ProfileURL         string              `json:"profileUrl,omitempty"`
	TTSVoicevoxSpeaker *ttsSpeakerResponse `json:"ttsVoicevoxSpeaker,omitempty"`
}

type ttsSpeakerResponse struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type channelResponse struct {
	ID                    string `json:"id"`
	Slug                  string `json:"slug"`
	Title                 string `json:"title"`
	Description           string `json:"description,omitempty"`
	OwnerUserID           string `json:"ownerUserId,omitempty"`
	Suspended             bool   `json:"suspended"`
	SuspendRetentionHours *int32 `json:"suspendRetentionHours,omitempty"`
	SuspendGraceSeconds   *int32 `json:"suspendGraceSeconds,omitempty"`
	// OperatingDeadline は営業の終了予定時刻（RFC3339）。準備中なら空。
	OperatingDeadline string `json:"operatingDeadline,omitempty"`
	// OperatingUnlimited は「時間制限なし」チャンネルか。
	OperatingUnlimited bool `json:"operatingUnlimited"`
	// NotifyEnabled は現在のユーザーが営業開始通知をオンにしているか（getChannelでのみ設定）。
	NotifyEnabled bool `json:"notifyEnabled,omitempty"`
	// PostTtlHours は投稿の寿命（時間）。6/24/72。
	PostTtlHours       int32  `json:"postTtlHours"`
	UrlLinkifyEnabled  bool   `json:"urlLinkifyEnabled"`
	ImageUploadEnabled bool   `json:"imageUploadEnabled"`
	CreatedAt          string `json:"createdAt"`
	AccessMode         string `json:"accessMode"`
	// AccessList はオーナー/管理者向けのチャンネル取得・設定更新時のみ含める
	// （第三者に許可/拒否リストの中身を漏らさない）。
	AccessList *[]access.Entry `json:"accessList,omitempty"`
}

type statsResponse struct {
	UsersCount          int64 `json:"usersCount"`
	ChannelsCount       int64 `json:"channelsCount"`
	ChatMessagesCount   int64 `json:"chatMessagesCount"`
	ActiveSessionsCount int64 `json:"activeSessionsCount"`
}

type sessionResponse struct {
	ID              string `json:"id"`
	UserID          string `json:"userId"`
	UserDisplayName string `json:"userDisplayName"`
	UserHandle      string `json:"userHandle,omitempty"`
	ExpiresAt       string `json:"expiresAt"`
	LastSeenAt      string `json:"lastSeenAt,omitempty"`
	UserAgent       string `json:"userAgent,omitempty"`
	IPPrefix        string `json:"ipPrefix,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

type ttsAutoDictionaryEntryResponse struct {
	TermKey            string `json:"termKey"`
	Term               string `json:"term"`
	Reading            string `json:"reading"`
	RegisteredByUserID string `json:"registeredByUserId,omitempty"`
	RegisteredByHandle string `json:"registeredByHandle,omitempty"`
	RegisteredAt       string `json:"registeredAt"`
}

type adminUserResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Handle      string `json:"handle,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	Status      string `json:"status"`
	Role        string `json:"role"`
	Provider    string `json:"provider,omitempty"`
	Subject     string `json:"subject,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (auth.SessionSnapshot, bool) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.SessionSnapshot{}, false
	}
	if !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "admin only")
		return auth.SessionSnapshot{}, false
	}
	return session, true
}

func apiUser(user auth.UserSnapshot) userResponse {
	return userResponse{
		ID:                 strconv.FormatInt(user.ID, 10),
		DisplayName:        user.DisplayName,
		Handle:             user.Handle,
		AvatarURL:          user.AvatarURL,
		Provider:           user.Provider,
		Role:               user.Role,
		Status:             user.Status,
		GhostMode:          user.GhostMode,
		ProfileURL:         user.ProfileURL,
		TTSVoicevoxSpeaker: apiTTSSpeaker(user.TTSVoicevoxSpeaker),
	}
}

func apiTTSSpeaker(speaker *auth.TTSVoicevoxSpeakerSnapshot) *ttsSpeakerResponse {
	if speaker == nil {
		return nil
	}
	return &ttsSpeakerResponse{
		UUID: speaker.UUID,
		Name: speaker.Name,
		URL:  speaker.URL,
	}
}

func apiSession(session db.ListActiveSessionsRow) sessionResponse {
	return sessionResponse{
		ID:              strconv.FormatInt(session.ID, 10),
		UserID:          strconv.FormatInt(session.UserID, 10),
		UserDisplayName: session.UserDisplayName,
		UserHandle:      textValue(session.UserHandle),
		ExpiresAt:       session.ExpiresAt.UTC().Format(time.RFC3339Nano),
		LastSeenAt:      timeValue(session.LastSeenAt),
		UserAgent:       textValue(session.UserAgent),
		IPPrefix:        textValue(session.IpPrefix),
		CreatedAt:       session.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func apiTTSAutoDictionaryEntry(entry db.TTSAutoDictionaryEntry) ttsAutoDictionaryEntryResponse {
	out := ttsAutoDictionaryEntryResponse{
		TermKey:            entry.TermKey,
		Term:               entry.Term,
		Reading:            entry.Reading,
		RegisteredByHandle: textValue(entry.RegisteredByHandle),
		RegisteredAt:       entry.RegisteredAt.UTC().Format(time.RFC3339Nano),
	}
	if entry.RegisteredByUserID.Valid {
		out.RegisteredByUserID = strconv.FormatInt(entry.RegisteredByUserID.Int64, 10)
	}
	return out
}

func apiAdminUser(user db.ListAdminUsersRow) adminUserResponse {
	return adminUserResponse{
		ID:          strconv.FormatInt(user.ID, 10),
		DisplayName: user.DisplayName,
		Handle:      textValue(user.Handle),
		AvatarURL:   textValue(user.AvatarUrl),
		Status:      user.Status,
		Role:        user.Role,
		Provider:    user.Provider,
		Subject:     user.Subject,
		CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   user.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func apiChannel(ch db.Channel) channelResponse {
	out := channelResponse{
		ID:                 strconv.FormatInt(ch.ID, 10),
		Slug:               ch.Slug,
		Title:              ch.Title,
		Suspended:          ch.SuspendedAt.Valid,
		OperatingUnlimited: ch.OperatingUnlimited,
		PostTtlHours:       ch.PostTtlHours,
		UrlLinkifyEnabled:  ch.UrlLinkifyEnabled,
		ImageUploadEnabled: ch.ImageUploadEnabled,
		CreatedAt:          ch.CreatedAt.UTC().Format(time.RFC3339Nano),
		AccessMode:         access.NormalizeMode(ch.AccessMode),
	}
	if ch.Description.Valid {
		out.Description = ch.Description.String
	}
	if ch.OwnerUserID.Valid {
		out.OwnerUserID = strconv.FormatInt(ch.OwnerUserID.Int64, 10)
	}
	if ch.SuspendRetentionHours.Valid {
		v := ch.SuspendRetentionHours.Int32
		out.SuspendRetentionHours = &v
	}
	if ch.SuspendGraceSeconds.Valid {
		v := ch.SuspendGraceSeconds.Int32
		out.SuspendGraceSeconds = &v
	}
	if ch.OperatingDeadline.Valid {
		out.OperatingDeadline = ch.OperatingDeadline.Time.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// apiChannelForOwner はオーナー/管理者向けに入室許可リストの中身も含めて返す。
func apiChannelForOwner(ch db.Channel) channelResponse {
	out := apiChannel(ch)
	list := access.ParseList(ch.AccessList)
	if list == nil {
		list = []access.Entry{}
	}
	out.AccessList = &list
	return out
}

// channelAccessAllowed はサスペンドを除く入室許可制御（ホワイト/ブラックリスト）に基づき、
// 当該ユーザーがチャンネルを閲覧・入室できるかを返す。オーナー/管理者は常に許可。
// whitelistEnabled はサーバ全体のホワイトリスト機能フラグ。無効時は whitelist を none 扱いにする。
func channelAccessAllowed(ch db.Channel, user auth.UserSnapshot, whitelistEnabled bool) bool {
	if ch.OwnerUserID.Valid && ch.OwnerUserID.Int64 == user.ID {
		return true
	}
	if auth.IsPrivileged(user.Role) {
		return true
	}
	// ゴーストモードはどのチャンネルにも入れる（書き込みはWS側で禁止）。
	if user.GhostMode {
		return true
	}
	keys := access.UserKeys(user.Provider, user.Subject, user.Handle, user.ProfileURL)
	return access.Allowed(access.EffectiveMode(ch.AccessMode, whitelistEnabled), access.ParseList(ch.AccessList), keys)
}

func normalizeSlug(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", "-")
	return v
}

func normalizeRole(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func nullableText(v string) pgtype.Text {
	v = strings.TrimSpace(v)
	if v == "" {
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

func timeValue(v pgtype.Timestamptz) string {
	if !v.Valid {
		return ""
	}
	return v.Time.UTC().Format(time.RFC3339Nano)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json failed", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
