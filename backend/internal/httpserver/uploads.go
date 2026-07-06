package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/uploads"
)

// stagedImage は Valkey に一時保存する、アップロード済み未投稿の画像/動画のメタ情報。
type stagedImage struct {
	ChannelID   int64  `json:"channelId"`
	UserID      int64  `json:"userId"`
	StoragePath string `json:"storagePath"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// uploadChannelImage はチャットに添付する画像を受け取り、ファイルとして保存する。
// 保存後はトークンを発行し、投稿（WS）で参照されるまで Valkey に一時保存する。
func (s *Server) uploadChannelImage(w http.ResponseWriter, r *http.Request) {
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
		slog.Error("get channel for upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to upload image")
		return
	}
	if !channel.ImageUploadEnabled {
		writeError(w, http.StatusForbidden, "image upload disabled")
		return
	}
	if channel.SuspendedAt.Valid {
		writeError(w, http.StatusForbidden, "channel suspended")
		return
	}

	// 動画素材(GIF/APNG/MP4)は静止画より大きいため、大きい方の上限で受け付ける。
	maxBytes := max(s.cfg.UploadMaxBytes, s.cfg.UploadMediaMaxBytes)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "image file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read image")
		return
	}

	relPath, width, height, kind, err := s.uploads.Save(r.Context(), channel.Slug, data)
	if err != nil {
		switch {
		case errors.Is(err, uploads.ErrNotJPEG):
			writeError(w, http.StatusBadRequest, "image must be a jpeg")
		case errors.Is(err, uploads.ErrVideoHasAudio):
			writeError(w, http.StatusBadRequest, "動画に音声を含めることはできません")
		case errors.Is(err, uploads.ErrVideoTooLong):
			writeError(w, http.StatusBadRequest, "動画は30秒以内にしてください")
		case errors.Is(err, uploads.ErrUnsupportedMedia):
			writeError(w, http.StatusBadRequest, "対応していないファイル形式です（JPEG/GIF/APNG/音声なしMP4）")
		default:
			slog.Error("save upload media failed", "channel", channel.Slug, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to save file")
		}
		return
	}

	token, err := randomToken()
	if err != nil {
		slog.Error("generate upload token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save image")
		return
	}
	payload, err := json.Marshal(stagedImage{
		ChannelID:   channel.ID,
		UserID:      session.User.ID,
		StoragePath: relPath,
		Width:       width,
		Height:      height,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save image")
		return
	}
	if err := s.valkey.StageUpload(r.Context(), token, payload, s.cfg.UploadStagingTTL); err != nil {
		slog.Error("stage upload token failed", "error", err)
		// ステージングに失敗したら保存ファイルも消しておく。
		_ = s.uploads.Delete(relPath)
		writeError(w, http.StatusInternalServerError, "failed to save image")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imageToken": token,
		"url":        uploadURL(relPath),
		"width":      width,
		"height":     height,
		"mediaKind":  kind,
	})
}

// serveUpload は保存済みの画像ファイルを配信する。
func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	relPath := r.PathValue("path")
	abs, err := s.uploads.AbsPath(relPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, abs)
}

// deleteChannelImageFiles はチャンネル内の全メッセージに紐づく画像ファイルを削除する。
func (s *Server) deleteChannelImageFiles(ctx context.Context, channelID int64) {
	paths, err := s.q.ListChannelImagePaths(ctx, channelID)
	if err != nil {
		slog.Warn("list channel image paths failed", "channel_id", channelID, "error", err)
		return
	}
	for _, p := range paths {
		if !p.Valid {
			continue
		}
		if err := s.uploads.Delete(p.String); err != nil {
			slog.Warn("delete channel image file failed", "path", p.String, "error", err)
		}
	}
}

// startUploadOrphanCleanup はアップロードされたが投稿されなかった孤立画像ファイルを
// 定期的に掃除する（どのメッセージからも参照されず、一定時間以上経過したもの）。
func (s *Server) startUploadOrphanCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweepOrphanImages(ctx)
			}
		}
	}()
}

func (s *Server) sweepOrphanImages(ctx context.Context) {
	paths, err := s.q.ListAllImagePaths(ctx)
	if err != nil {
		slog.Warn("list all image paths failed", "error", err)
		return
	}
	referenced := make(map[string]struct{}, len(paths)+1)
	for _, p := range paths {
		if p.Valid {
			referenced[p.String] = struct{}{}
		}
	}
	// サービスヘッダ画像はチャット添付ではないため参照一覧に含まれない。
	// 掃除対象から除外するために明示的に参照済みへ加える。
	if headerPath, err := s.q.GetAppSetting(ctx, settingHeaderImagePath); err == nil && headerPath != "" {
		referenced[headerPath] = struct{}{}
	}
	// ステージングTTLより十分に長い猶予を取り、投稿処理中のファイルを誤って消さない。
	removed, err := s.uploads.SweepOrphans(referenced, s.cfg.UploadStagingTTL+1*time.Hour)
	if err != nil {
		slog.Warn("sweep orphan images failed", "error", err)
		return
	}
	if removed > 0 {
		slog.Info("swept orphan upload images", "removed", removed)
	}
}

func uploadURL(relPath string) string {
	return "/api/uploads/" + relPath
}

func randomToken() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
