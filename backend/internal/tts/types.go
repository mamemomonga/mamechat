package tts

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/config"
)

const (
	outputFormat    = "m4a"
	outputCodec     = "aac-lc"
	outputBitrate   = 48000
	outputChannels  = 1
	splitterVersion = "splitter-v1"
	hashVersion     = "tts-cache-v1"
	maxPartRunes    = 100

	speedRampStartRunes = 50
	maxSpeedScale       = 2.0
)

var normalizerVersion string

type Settings struct {
	Enabled               bool
	StorageDir            string
	VoicevoxURLs          []string
	VoicevoxEngineVersion string
	MessageMaxRunes       int
	MaxPendingPerChannel  int32
	WorkerConcurrency     int
	GCInterval            time.Duration
	SpeedScale            float64
	PitchScale            float64
	IntonationScale       float64
	VolumeScale           float64
	PrePhonemeLength      float64
	PostPhonemeLength     float64
}

func SettingsFromConfig(cfg config.Config) Settings {
	return Settings{
		Enabled:               cfg.TTSEnabled,
		StorageDir:            cfg.TTSStorageDir,
		VoicevoxURLs:          cfg.TTSVoicevoxURLs,
		VoicevoxEngineVersion: cfg.TTSVoicevoxEngineVersion,
		MessageMaxRunes:       cfg.MessageMaxLength,
		MaxPendingPerChannel:  int32(cfg.TTSMaxPendingPerChannel),
		WorkerConcurrency:     cfg.TTSWorkerConcurrency,
		GCInterval:            cfg.TTSGCInterval,
		SpeedScale:            1.0,
		PitchScale:            0.0,
		IntonationScale:       1.0,
		VolumeScale:           1.0,
		PrePhonemeLength:      0.1,
		PostPhonemeLength:     0.1,
	}
}

func primaryVoicevoxURL(settings Settings) string {
	if len(settings.VoicevoxURLs) == 0 {
		return ""
	}
	return settings.VoicevoxURLs[0]
}

type Job struct {
	ID                    string  `json:"id"`
	ChannelID             int64   `json:"channelId"`
	ChannelSlug           string  `json:"channelSlug"`
	MessageID             int64   `json:"messageId"`
	ContentHash           string  `json:"contentHash"`
	PartIndex             int32   `json:"partIndex"`
	PartCount             int32   `json:"partCount"`
	Text                  string  `json:"text"`
	SpeakerID             int32   `json:"speakerId"`
	SpeakerName           string  `json:"speakerName"`
	SpeakerStyleName      string  `json:"speakerStyleName"`
	SpeedScale            float64 `json:"speedScale"`
	PitchScale            float64 `json:"pitchScale"`
	IntonationScale       float64 `json:"intonationScale"`
	VolumeScale           float64 `json:"volumeScale"`
	PrePhonemeLength      float64 `json:"prePhonemeLength"`
	PostPhonemeLength     float64 `json:"postPhonemeLength"`
	VoicevoxEngineVersion string  `json:"voicevoxEngineVersion"`
	NormalizerVersion     string  `json:"normalizerVersion"`
	SplitterVersion       string  `json:"splitterVersion"`
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}

func textPreview(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}
