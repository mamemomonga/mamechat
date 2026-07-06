// Package uploads は、チャットに添付する画像・動画ファイルの永続化ストレージを扱う。
// 静止画は JPEG として、アニメーション素材(GIF/APNG)と短いMP4は音声なしMP4に変換して、
// チャンネル別・年月別のディレクトリに保存する（相対パスを DB に記録する）。
package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// メディア種別。投稿表示時に <img> と <video> を切り替えるのに使う。
const (
	KindImage = "image"
	KindVideo = "video"
)

var (
	// ErrNotJPEG は静止画が JPEG として復号できない場合に返す。
	ErrNotJPEG = errors.New("uploaded image is not a valid jpeg")
	// ErrUnsupportedMedia は対応していない形式・壊れたデータの場合に返す。
	ErrUnsupportedMedia = errors.New("unsupported media format")
	// ErrVideoHasAudio はアップロードMP4に音声トラックが含まれる場合に返す。
	ErrVideoHasAudio = errors.New("uploaded video must not contain audio")
	// ErrVideoTooLong はアップロードMP4が長さ上限を超える場合に返す。
	ErrVideoTooLong = errors.New("uploaded video is too long")
)

var slugSafe = regexp.MustCompile(`[^a-z0-9-]+`)

type Store struct {
	dir             string
	videoMaxSeconds int
}

func New(dir string, videoMaxSeconds int) *Store {
	if videoMaxSeconds <= 0 {
		videoMaxSeconds = 30
	}
	return &Store{dir: dir, videoMaxSeconds: videoMaxSeconds}
}

// Save はアップロードされたバイト列を種別に応じて検証・変換・保存し、
// 相対パス・寸法・メディア種別(KindImage/KindVideo)を返す。
// 種別・寸法はクライアントを信頼せず、サーバ側で復号/ffprobe して得る。
func (s *Store) Save(ctx context.Context, channelSlug string, data []byte) (relPath string, width, height int, kind string, err error) {
	switch sniffFormat(data) {
	case formatJPEG:
		return s.saveJPEG(channelSlug, data)
	case formatGIF, formatAPNG, formatMP4:
		return s.saveVideo(ctx, channelSlug, data)
	default:
		return "", 0, 0, "", ErrUnsupportedMedia
	}
}

// saveJPEG は JPEG をそのまま保存する（従来の静止画フロー）。
func (s *Store) saveJPEG(channelSlug string, data []byte) (string, int, int, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		return "", 0, 0, "", ErrNotJPEG
	}
	// 念のため完全に復号できることも確認する（壊れたファイルの保存を防ぐ）。
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		return "", 0, 0, "", ErrNotJPEG
	}
	relPath, err := s.write(channelSlug, data, ".jpg")
	if err != nil {
		return "", 0, 0, "", err
	}
	return relPath, cfg.Width, cfg.Height, KindImage, nil
}

// saveVideo は GIF/APNG/MP4 を音声なしMP4へ変換して保存する。
func (s *Store) saveVideo(ctx context.Context, channelSlug string, data []byte) (string, int, int, string, error) {
	mp4, width, height, err := convertToMP4(ctx, sniffFormat(data), data, s.videoMaxSeconds)
	if err != nil {
		return "", 0, 0, "", err
	}
	relPath, err := s.write(channelSlug, mp4, ".mp4")
	if err != nil {
		return "", 0, 0, "", err
	}
	return relPath, width, height, KindVideo, nil
}

// write はバイト列をチャンネル別・年月別のディレクトリへ保存し、相対パスを返す。
func (s *Store) write(channelSlug string, data []byte, ext string) (string, error) {
	token, err := randomHex(16)
	if err != nil {
		return "", err
	}
	safeSlug := slugSafe.ReplaceAllString(strings.ToLower(channelSlug), "")
	if safeSlug == "" {
		safeSlug = "channel"
	}
	yyyymm := time.Now().UTC().Format("200601")
	relPath := filepath.ToSlash(filepath.Join(safeSlug, yyyymm, token+ext))

	absPath := filepath.Join(s.dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		return "", err
	}
	return relPath, nil
}

// SaveRaw は検証済みのバイト列を subdir 配下にランダム名で保存し、相対パスを返す。
// 画像の内容検証・変換はしないので、呼び出し側で形式を検証してから使うこと
// （サービスヘッダ画像などチャンネル添付以外の用途向け）。
func (s *Store) SaveRaw(subdir string, data []byte, ext string) (string, error) {
	return s.write(subdir, data, ext)
}

// AbsPath は相対パスを検証してストレージ内の絶対パスへ解決する。
// ディレクトリトラバーサル（../ など）を防ぐ。
func (s *Store) AbsPath(relPath string) (string, error) {
	clean := filepath.Clean("/" + filepath.FromSlash(relPath))
	abs := filepath.Join(s.dir, clean)
	rootAbs, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if target != rootAbs && !strings.HasPrefix(target, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes storage root: %s", relPath)
	}
	return target, nil
}

// Delete は相対パスの画像ファイルを削除する。存在しなくてもエラーにしない。
func (s *Store) Delete(relPath string) error {
	if strings.TrimSpace(relPath) == "" {
		return nil
	}
	abs, err := s.AbsPath(relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SweepOrphans はストレージ内の画像ファイルのうち、referenced に含まれず、かつ
// olderThan より古いものを削除する（アップロードされたが投稿されなかった孤立ファイルの掃除）。
func (s *Store) SweepOrphans(referenced map[string]struct{}, olderThan time.Duration) (int, error) {
	rootAbs, err := filepath.Abs(s.dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	removed := 0
	walkErr := filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, ok := referenced[rel]; ok {
			return nil
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
		return nil
	})
	return removed, walkErr
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
