package httpserver

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mamemomonga/mamechat/backend/internal/access"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

// app_settings のキー。
const (
	settingServerOverview  = "server_overview"
	settingHeaderImagePath = "header_image_path"
)

// サーバ概要の最大文字数（1000文字程度＋余裕）。rune 単位で制限する。
const serverOverviewMaxRunes = 4000

// appInfoMarkdown はアプリケーション情報の表示内容（Markdown）。ソースに埋め込み、
// `{{VERSION}}` を起動中サーバのバージョンへ置換して表示する。編集は appinfo.md で行う。
//
//go:embed appinfo.md
var appInfoMarkdown string

// getAppSettingOr は app_settings の値を取得する。未設定（行なし）なら fallback を返す。
func (s *Server) getAppSettingOr(r *http.Request, key, fallback string) string {
	value, err := s.q.GetAppSetting(r.Context(), key)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("get app setting failed", "key", key, "error", err)
		}
		return fallback
	}
	return value
}

// whitelistEnabled はサーバ全体のホワイトリスト機能フラグを返す。未設定（行なし）は無効(false)。
func (s *Server) whitelistEnabled(ctx context.Context) bool {
	value, err := s.q.GetAppSetting(ctx, access.SettingWhitelistEnabled)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("get whitelist setting failed", "error", err)
		}
		return false
	}
	return access.SettingEnabled(value)
}

// serviceInfo はサービス情報ページ（サービス名クリックで開く）向けの公開情報を返す。
func (s *Server) serviceInfo(w http.ResponseWriter, r *http.Request) {
	overview := s.getAppSettingOr(r, settingServerOverview, "")
	headerPath := s.getAppSettingOr(r, settingHeaderImagePath, "")

	appInfoSrc := strings.ReplaceAll(appInfoMarkdown, "{{VERSION}}", s.cfg.Version)

	var headerImageURL string
	if headerPath != "" {
		headerImageURL = uploadURL(headerPath)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"serviceName":    s.cfg.ServiceName,
		"version":        s.cfg.Version,
		"headerImageUrl": headerImageURL,
		"overviewHtml":   renderMarkdown(overview),
		"appInfoHtml":    renderMarkdown(appInfoSrc),
	})
}

// adminServiceSettings は管理画面のサーバ設定タブ向けに、編集可能な設定の生の値を返す。
func (s *Server) adminServiceSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	overview := s.getAppSettingOr(r, settingServerOverview, "")
	headerPath := s.getAppSettingOr(r, settingHeaderImagePath, "")
	var headerImageURL string
	if headerPath != "" {
		headerImageURL = uploadURL(headerPath)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"overview":         overview,
		"headerImageUrl":   headerImageURL,
		"whitelistEnabled": s.whitelistEnabled(r.Context()),
	})
}

// adminUpdateWhitelistEnabled はホワイトリスト機能のサーバ全体 有効/無効 を保存する。
func (s *Server) adminUpdateWhitelistEnabled(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	value := "false"
	if req.Enabled {
		value = "true"
	}
	if err := s.q.SetAppSetting(r.Context(), db.SetAppSettingParams{Key: access.SettingWhitelistEnabled, Value: value}); err != nil {
		slog.Error("save whitelist setting failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save setting")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"whitelistEnabled": req.Enabled})
}

// adminUpdateServiceOverview はサーバ概要（Markdown）を保存する。
func (s *Server) adminUpdateServiceOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		Overview string `json:"overview"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	overview := strings.TrimRight(req.Overview, "\n")
	if len([]rune(overview)) > serverOverviewMaxRunes {
		writeError(w, http.StatusBadRequest, "サーバ概要が長すぎます")
		return
	}
	if err := s.q.SetAppSetting(r.Context(), db.SetAppSettingParams{Key: settingServerOverview, Value: overview}); err != nil {
		slog.Error("save server overview failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save overview")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminUploadServiceHeader はサービスヘッダ画像（横長）をアップロードして差し替える。
// JPEG/PNG/GIF を受け付ける。旧画像は削除する。
func (s *Server) adminUploadServiceHeader(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	maxBytes := s.cfg.UploadMaxBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "画像が大きすぎます")
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像ファイルが必要です")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像を読み取れませんでした")
		return
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusBadRequest, "対応していない画像形式です（JPEG/PNG/GIF）")
		return
	}
	ext := "." + format
	if format == "jpeg" {
		ext = ".jpg"
	}
	relPath, err := s.uploads.SaveRaw("_server", data, ext)
	if err != nil {
		slog.Error("save service header failed", "error", err)
		writeError(w, http.StatusInternalServerError, "画像の保存に失敗しました")
		return
	}
	// 旧ヘッダ画像を削除してから設定を更新する。
	oldPath := s.getAppSettingOr(r, settingHeaderImagePath, "")
	if err := s.q.SetAppSetting(r.Context(), db.SetAppSettingParams{Key: settingHeaderImagePath, Value: relPath}); err != nil {
		slog.Error("save service header path failed", "error", err)
		_ = s.uploads.Delete(relPath)
		writeError(w, http.StatusInternalServerError, "画像の保存に失敗しました")
		return
	}
	if oldPath != "" && oldPath != relPath {
		if err := s.uploads.Delete(oldPath); err != nil {
			slog.Warn("delete old service header failed", "path", oldPath, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"headerImageUrl": uploadURL(relPath)})
}

// adminDeleteServiceHeader はサービスヘッダ画像を削除する（画像なしに戻す）。
func (s *Server) adminDeleteServiceHeader(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	oldPath := s.getAppSettingOr(r, settingHeaderImagePath, "")
	if err := s.q.DeleteAppSetting(r.Context(), settingHeaderImagePath); err != nil {
		slog.Error("delete service header setting failed", "error", err)
		writeError(w, http.StatusInternalServerError, "画像の削除に失敗しました")
		return
	}
	if oldPath != "" {
		if err := s.uploads.Delete(oldPath); err != nil {
			slog.Warn("delete service header file failed", "path", oldPath, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
