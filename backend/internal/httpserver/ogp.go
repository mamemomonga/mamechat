package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/ogp"
)

const (
	// 取得成功はやや長め、失敗・OGP無しは短めにキャッシュして再取得を抑える。
	ogpCacheTTL      = 24 * time.Hour
	ogpNegCacheTTL   = 1 * time.Hour
	ogpRequestMaxLen = 2048
)

// getLinkPreview は投稿に貼られたURLのOGP（リンクプレビュー）を返す。
// 取得できない場合も 200 で空のプレビューを返し、フロント側の分岐を単純にする。
func (s *Server) getLinkPreview(w http.ResponseWriter, r *http.Request) {
	if _, err := s.auth.CurrentUser(r.Context(), r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" || len(raw) > ogpRequestMaxLen {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "invalid url")
		return
	}
	normalized := u.String()
	sum := sha256.Sum256([]byte(normalized))
	key := hex.EncodeToString(sum[:])

	// キャッシュヒットならそのまま返す。
	if cached, ok, err := s.valkey.GetOGP(r.Context(), key); err == nil && ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cached)
		return
	}

	preview, err := s.ogp.Fetch(r.Context(), normalized)
	if err != nil {
		// 取得失敗は空プレビュー（urlのみ）を短時間キャッシュして返す。
		preview = ogp.Preview{URL: normalized}
		if payload, mErr := json.Marshal(preview); mErr == nil {
			_ = s.valkey.SetOGP(r.Context(), key, payload, ogpNegCacheTTL)
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}
	if preview.URL == "" {
		preview.URL = normalized
	}

	payload, err := json.Marshal(preview)
	if err != nil {
		slog.Warn("marshal ogp preview failed", "error", err)
		writeJSON(w, http.StatusOK, preview)
		return
	}
	ttl := ogpCacheTTL
	if !preview.HasContent() {
		ttl = ogpNegCacheTTL
	}
	if err := s.valkey.SetOGP(r.Context(), key, payload, ttl); err != nil {
		slog.Warn("cache ogp preview failed", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}
