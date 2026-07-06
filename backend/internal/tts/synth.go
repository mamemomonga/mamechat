package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// synthParams は VOICEVOX 合成時の音声パラメータ。メッセージ読み上げ・プレビューで共有する。
type synthParams struct {
	SpeedScale        float64
	PitchScale        float64
	IntonationScale   float64
	VolumeScale       float64
	PrePhonemeLength  float64
	PostPhonemeLength float64
}

// synthesizeWAV は VOICEVOX エンジンに audio_query と synthesis を投げて WAV を生成する。
func synthesizeWAV(ctx context.Context, client *http.Client, voicevoxURL, text string, speakerID int32, p synthParams) ([]byte, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values := url.Values{}
	values.Set("text", text)
	values.Set("speaker", strconv.FormatInt(int64(speakerID), 10))
	values.Set("enable_katakana_english", "true")
	audioQueryURL := voicevoxURL + "/audio_query?" + values.Encode()
	req, err := http.NewRequestWithContext(queryCtx, http.MethodPost, audioQueryURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("audio_query status %d", res.StatusCode)
	}
	var query map[string]any
	if err := json.NewDecoder(res.Body).Decode(&query); err != nil {
		return nil, err
	}
	query["speedScale"] = p.SpeedScale
	query["pitchScale"] = p.PitchScale
	query["intonationScale"] = p.IntonationScale
	query["volumeScale"] = p.VolumeScale
	query["prePhonemeLength"] = p.PrePhonemeLength
	query["postPhonemeLength"] = p.PostPhonemeLength
	queryBody, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}

	synthCtx, synthCancel := context.WithTimeout(ctx, 30*time.Second)
	defer synthCancel()
	synthesisURL := fmt.Sprintf("%s/synthesis?speaker=%d", voicevoxURL, speakerID)
	synthReq, err := http.NewRequestWithContext(synthCtx, http.MethodPost, synthesisURL, bytes.NewReader(queryBody))
	if err != nil {
		return nil, err
	}
	synthReq.Header.Set("Content-Type", "application/json")
	synthReq.Header.Set("Accept", "audio/wav")
	synthRes, err := client.Do(synthReq)
	if err != nil {
		return nil, err
	}
	defer synthRes.Body.Close()
	if synthRes.StatusCode < 200 || synthRes.StatusCode >= 300 {
		return nil, fmt.Errorf("synthesis status %d", synthRes.StatusCode)
	}
	return io.ReadAll(synthRes.Body)
}

// convertWAVToM4A は WAV を ffmpeg で m4a(AAC) に変換し、コンテンツハッシュ単位で保存する。
func convertWAVToM4A(storageDir, contentHash string, wav []byte) (string, int64, error) {
	dir := filepath.Join(storageDir, contentHash[0:2], contentHash[2:4])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(dir, contentHash+".*.m4a")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	cmd := exec.Command("ffmpeg",
		"-y",
		"-hide_banner", "-loglevel", "error",
		"-f", "wav", "-i", "pipe:0",
		"-vn",
		"-ac", strconv.Itoa(outputChannels),
		"-c:a", "aac",
		"-b:a", "48k",
		"-movflags", "+faststart",
		tmpPath,
	)
	cmd.Stdin = bytes.NewReader(wav)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("%w: %s", err, stderr.String())
	}
	finalPath := filepath.Join(dir, contentHash+".m4a")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", 0, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return "", 0, err
	}
	if info.Size() == 0 {
		_ = os.Remove(finalPath)
		return "", 0, fmt.Errorf("ffmpeg created empty output")
	}
	return finalPath, info.Size(), nil
}
