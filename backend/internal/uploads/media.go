package uploads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// mediaFormat はアップロードされたバイト列から判定した素材の種別。
type mediaFormat int

const (
	formatUnknown mediaFormat = iota
	formatJPEG                // 静止画（従来どおりそのまま保存）
	formatGIF                 // GIF（アニメ含む）→ MP4 へ変換
	formatAPNG                // アニメーションPNG → MP4 へ変換
	formatMP4                 // MP4（音声なし・短時間のみ受理）→ MP4 へ正規化
)

// 変換後MP4の長辺上限と、x264エンコード品質。静止画の MAX_EDGE と揃える。
const (
	videoMaxEdge = 800
	videoCRF     = "23"
	videoPreset  = "veryfast"
)

// sniffFormat はマジックバイトから素材種別を判定する。クライアントの申告は信用しない。
func sniffFormat(data []byte) mediaFormat {
	if len(data) < 12 {
		return formatUnknown
	}
	switch {
	case data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return formatJPEG
	case bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")):
		return formatGIF
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		// PNGシグネチャ。acTL チャンクがあれば APNG（アニメーション）。
		if isAPNG(data) {
			return formatAPNG
		}
		return formatUnknown // 静止PNGはクライアントがJPEG化して送るため受理しない。
	case bytes.Equal(data[4:8], []byte("ftyp")):
		return formatMP4
	}
	return formatUnknown
}

// isAPNG は PNG バイト列に acTL チャンクが（最初の IDAT より前に）存在するか調べる。
func isAPNG(data []byte) bool {
	actl := bytes.Index(data, []byte("acTL"))
	if actl < 0 {
		return false
	}
	idat := bytes.Index(data, []byte("IDAT"))
	return idat < 0 || actl < idat
}

// probedStreams は ffprobe の必要部分のみを取り出す。
type probedStreams struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// probeMP4 は入力MP4が「映像あり・音声なし・maxSeconds以下」かを検証する。
func probeMP4(ctx context.Context, inputPath string, maxSeconds int) error {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-hide_banner", "-loglevel", "error",
		"-show_streams", "-show_format",
		"-print_format", "json",
		inputPath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedMedia, stderr.String())
	}
	var probed probedStreams
	if err := json.Unmarshal(stdout.Bytes(), &probed); err != nil {
		return ErrUnsupportedMedia
	}
	hasVideo := false
	for _, s := range probed.Streams {
		switch s.CodecType {
		case "video":
			hasVideo = true
		case "audio":
			return ErrVideoHasAudio
		}
	}
	if !hasVideo {
		return ErrUnsupportedMedia
	}
	if probed.Format.Duration != "" {
		if dur, err := strconv.ParseFloat(probed.Format.Duration, 64); err == nil {
			// わずかな丸め誤差を許容する。
			if dur > float64(maxSeconds)+0.5 {
				return ErrVideoTooLong
			}
		}
	}
	return nil
}

// convertToMP4 は GIF/APNG/MP4 を音声なしの H.264 MP4 に変換し、出力バイト列と寸法を返す。
// ループはファイルに焼けないため、再生側の <video loop> で行う（ここでは付与しない）。
func convertToMP4(ctx context.Context, format mediaFormat, data []byte, maxSeconds int) (out []byte, width, height int, err error) {
	tmpDir, err := os.MkdirTemp("", "wschat-media-")
	if err != nil {
		return nil, 0, 0, err
	}
	defer os.RemoveAll(tmpDir)

	inPath := filepath.Join(tmpDir, "in"+inputExt(format))
	outPath := filepath.Join(tmpDir, "out.mp4")
	if err := os.WriteFile(inPath, data, 0o600); err != nil {
		return nil, 0, 0, err
	}

	if format == formatMP4 {
		if err := probeMP4(ctx, inPath, maxSeconds); err != nil {
			return nil, 0, 0, err
		}
	}

	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-nostdin"}
	if format == formatAPNG {
		// APNG はデフォルトのPNGデコーダだと1枚絵になるため apng デマクサを明示する。
		args = append(args, "-f", "apng")
	}
	args = append(args,
		"-i", inPath,
		"-an", // 音声は常に除去する。
		// 長辺を videoMaxEdge 以内に収め（拡大はしない）、H.264(yuv420p)の制約のため
		// 幅・高さを必ず偶数にする。長辺側は trunc(.../2)*2 で偶数に丸め、短辺側は -2 で偶数化する。
		// 従来は長辺側を偶数化していなかったため、小さい素材で元寸の奇数辺がそのまま残り
		// libx264 のエンコードに失敗して「対応していない形式」になっていた。
		"-vf", fmt.Sprintf("scale='if(gt(iw,ih),trunc(min(%d,iw)/2)*2,-2)':'if(gt(iw,ih),-2,trunc(min(%d,ih)/2)*2)':flags=lanczos", videoMaxEdge, videoMaxEdge),
		"-c:v", "libx264", "-preset", videoPreset, "-crf", videoCRF,
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, 0, 0, fmt.Errorf("%w: %s", ErrUnsupportedMedia, stderr.String())
	}

	w, h, err := probeDimensions(ctx, outPath)
	if err != nil {
		return nil, 0, 0, err
	}
	out, err = os.ReadFile(outPath)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(out) == 0 {
		return nil, 0, 0, fmt.Errorf("%w: empty output", ErrUnsupportedMedia)
	}
	return out, w, h, nil
}

// probeDimensions は MP4 の映像寸法を取得する。
func probeDimensions(ctx context.Context, path string) (int, int, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-hide_banner", "-loglevel", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-print_format", "json",
		path,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0, 0, ErrUnsupportedMedia
	}
	var probed probedStreams
	if err := json.Unmarshal(stdout.Bytes(), &probed); err != nil || len(probed.Streams) == 0 {
		return 0, 0, ErrUnsupportedMedia
	}
	return probed.Streams[0].Width, probed.Streams[0].Height, nil
}

func inputExt(format mediaFormat) string {
	switch format {
	case formatGIF:
		return ".gif"
	case formatAPNG:
		return ".png"
	case formatMP4:
		return ".mp4"
	default:
		return ".bin"
	}
}
