package tts

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mamemomonga/mamechat/backend/internal/chat"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
	"github.com/mamemomonga/mamechat/backend/internal/voicevox"
)

// ErrTTSDisabled は読み上げ機能が無効なときに返す。
var ErrTTSDisabled = errors.New("tts disabled")

// speakerPreviewText はキャラクター切り替え時の試聴で読み上げる固定文。
const speakerPreviewText = "この声で読み上げます"

type Bus interface {
	Publish(ctx context.Context, channelSlug string, msg chat.ServerMessage) error
}

type Enqueuer struct {
	settings Settings
	q        *db.Queries
	queue    *Queue
	bus      Bus
	resolver *voicevox.Resolver
	client   *http.Client
}

func NewEnqueuer(settings Settings, q *db.Queries, queue *Queue, bus Bus) *Enqueuer {
	return &Enqueuer{settings: settings, q: q, queue: queue, bus: bus, resolver: voicevox.NewResolver(primaryVoicevoxURL(settings)), client: &http.Client{}}
}

// SpeakerPreview は指定キャラクターでプレビュー用の固定文を同期合成し、
// 生成済みアセットのコンテンツハッシュを返す。同一内容がキャッシュ済みなら再利用する。
func (e *Enqueuer) SpeakerPreview(ctx context.Context, speakerUUID string) (string, error) {
	if !e.settings.Enabled {
		return "", ErrTTSDisabled
	}
	speaker, err := e.resolveSpeaker(ctx, speakerUUID)
	if err != nil {
		return "", err
	}
	normalized, ok := NormalizeText(speakerPreviewText)
	if !ok {
		normalized = speakerPreviewText
	}
	hash := ContentHash(normalized, e.settings, speaker)
	if asset, err := e.q.GetTTSAsset(ctx, hash); err == nil && assetUsable(asset) {
		if err := e.q.TouchTTSAsset(ctx, hash); err != nil {
			slog.Warn("touch tts asset failed", "content_hash", hash, "error", err)
		}
		return hash, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	wav, err := synthesizeWAV(ctx, e.client, primaryVoicevoxURL(e.settings), normalized, speaker.StyleID, synthParams{
		SpeedScale:        e.settings.SpeedScale,
		PitchScale:        e.settings.PitchScale,
		IntonationScale:   e.settings.IntonationScale,
		VolumeScale:       e.settings.VolumeScale,
		PrePhonemeLength:  e.settings.PrePhonemeLength,
		PostPhonemeLength: e.settings.PostPhonemeLength,
	})
	if err != nil {
		return "", err
	}
	filePath, size, err := convertWAVToM4A(e.settings.StorageDir, hash, wav)
	if err != nil {
		return "", err
	}
	if _, err := e.q.UpsertTTSAsset(ctx, db.UpsertTTSAssetParams{
		ContentHash:           hash,
		FilePath:              filePath,
		FileSizeBytes:         size,
		DurationMs:            pgtype.Int4{},
		TextPreview:           nullableText(textPreview(normalized)),
		TextLength:            int32(len([]rune(normalized))),
		SpeakerID:             speaker.StyleID,
		SpeakerName:           speaker.Character.Name,
		SpeakerStyleName:      nullableText(speaker.StyleName),
		SpeedScale:            e.settings.SpeedScale,
		PitchScale:            e.settings.PitchScale,
		IntonationScale:       e.settings.IntonationScale,
		VolumeScale:           e.settings.VolumeScale,
		PrePhonemeLength:      e.settings.PrePhonemeLength,
		PostPhonemeLength:     e.settings.PostPhonemeLength,
		VoicevoxEngineVersion: e.settings.VoicevoxEngineVersion,
		NormalizerVersion:     normalizerVersion,
		SplitterVersion:       splitterVersion,
	}); err != nil {
		return "", err
	}
	return hash, nil
}

// SynthPart は「ここから読み上げる」で個別に取得する音声パート。
type SynthPart struct {
	PartIndex   int    `json:"partIndex"`
	ContentHash string `json:"contentHash"`
	AudioURL    string `json:"audioUrl"`
	DurationMs  int32  `json:"durationMs,omitempty"`
}

// SynthesizeMessageParts は1メッセージ分の読み上げ音声を同期生成し、再生用URLを返す。
// バスへは配信せず、要求したクライアントだけが個別に再生する（既存投稿の遡り読み上げ用）。
// キャッシュ済みなら再利用する。読み上げ不可（無効・話者なし・無音テキスト）なら空を返す。
func (e *Enqueuer) SynthesizeMessageParts(ctx context.Context, speakerUUID string, body string) ([]SynthPart, error) {
	if !e.settings.Enabled || speakerUUID == "" {
		return nil, nil
	}
	speaker, err := e.resolveSpeaker(ctx, speakerUUID)
	if err != nil {
		return nil, err
	}
	dictionary := e.loadAutoDictionary(ctx)
	normalized, ok := NormalizeTextWithDictionary(body, dictionary)
	if !ok {
		return nil, nil
	}
	parts := SplitText(normalized)
	if len(parts) == 0 {
		return nil, nil
	}
	messageSettings := e.settings
	messageSettings.SpeedScale = SpeedScaleForMessage(e.settings, len([]rune(normalized)))

	out := make([]SynthPart, 0, len(parts))
	for i, part := range parts {
		hash := ContentHash(part, messageSettings, speaker)
		durationMs := int32(0)
		if asset, err := e.q.GetTTSAsset(ctx, hash); err == nil && assetUsable(asset) {
			if err := e.q.TouchTTSAsset(ctx, hash); err != nil {
				slog.Warn("touch tts asset failed", "content_hash", hash, "error", err)
			}
			durationMs = asset.DurationMs.Int32
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		} else {
			wav, err := synthesizeWAV(ctx, e.client, primaryVoicevoxURL(e.settings), part, speaker.StyleID, synthParams{
				SpeedScale:        messageSettings.SpeedScale,
				PitchScale:        e.settings.PitchScale,
				IntonationScale:   e.settings.IntonationScale,
				VolumeScale:       e.settings.VolumeScale,
				PrePhonemeLength:  e.settings.PrePhonemeLength,
				PostPhonemeLength: e.settings.PostPhonemeLength,
			})
			if err != nil {
				return nil, err
			}
			filePath, size, err := convertWAVToM4A(e.settings.StorageDir, hash, wav)
			if err != nil {
				return nil, err
			}
			if _, err := e.q.UpsertTTSAsset(ctx, db.UpsertTTSAssetParams{
				ContentHash:           hash,
				FilePath:              filePath,
				FileSizeBytes:         size,
				DurationMs:            pgtype.Int4{},
				TextPreview:           nullableText(textPreview(part)),
				TextLength:            int32(len([]rune(part))),
				SpeakerID:             speaker.StyleID,
				SpeakerName:           speaker.Character.Name,
				SpeakerStyleName:      nullableText(speaker.StyleName),
				SpeedScale:            messageSettings.SpeedScale,
				PitchScale:            e.settings.PitchScale,
				IntonationScale:       e.settings.IntonationScale,
				VolumeScale:           e.settings.VolumeScale,
				PrePhonemeLength:      e.settings.PrePhonemeLength,
				PostPhonemeLength:     e.settings.PostPhonemeLength,
				VoicevoxEngineVersion: e.settings.VoicevoxEngineVersion,
				NormalizerVersion:     normalizerVersion,
				SplitterVersion:       splitterVersion,
			}); err != nil {
				return nil, err
			}
		}
		out = append(out, SynthPart{
			PartIndex:   i,
			ContentHash: hash,
			AudioURL:    "/api/tts/" + hash + ".m4a",
			DurationMs:  durationMs,
		})
	}
	return out, nil
}

func (e *Enqueuer) EnqueueMessage(ctx context.Context, channelID int64, channelSlug string, messageID int64, speakerUUID string, body string) error {
	if !e.settings.Enabled {
		return nil
	}
	if speakerUUID == "" {
		return nil
	}
	speaker, err := e.resolveSpeaker(ctx, speakerUUID)
	if err != nil {
		return err
	}
	dictionary := e.loadAutoDictionary(ctx)
	normalized, ok := NormalizeTextWithDictionary(body, dictionary)
	if !ok {
		return e.publish(ctx, channelSlug, chat.TTSSkipped(strconv.FormatInt(messageID, 10), "text_not_readable"))
	}
	totalRunes := len([]rune(normalized))
	parts := SplitText(normalized)
	if len(parts) == 0 {
		return e.publish(ctx, channelSlug, chat.TTSSkipped(strconv.FormatInt(messageID, 10), "empty"))
	}
	if err := e.publish(ctx, channelSlug, chat.TTSQueued(strconv.FormatInt(messageID, 10), len(parts))); err != nil {
		slog.Warn("publish tts queued failed", "message_id", messageID, "error", err)
	}

	messageSettings := e.settings
	messageSettings.SpeedScale = SpeedScaleForMessage(e.settings, totalRunes)
	for i, part := range parts {
		hash := ContentHash(part, messageSettings, speaker)
		if asset, err := e.q.GetTTSAsset(ctx, hash); err == nil && assetUsable(asset) {
			if err := e.q.TouchTTSAsset(ctx, hash); err != nil {
				slog.Warn("touch tts asset failed", "content_hash", hash, "error", err)
			}
			_ = e.q.CreateTTSMessagePart(ctx, db.CreateTTSMessagePartParams{
				ID:          mustUUID(),
				ChannelID:   channelID,
				MessageID:   messageID,
				ContentHash: hash,
				PartIndex:   int32(i),
				TextPreview: nullableText(textPreview(part)),
				TextLength:  int32(len([]rune(part))),
			})
			if err := e.publish(ctx, channelSlug, chat.TTSPartReady(strconv.FormatInt(messageID, 10), i, hash, asset.DurationMs.Int32)); err != nil {
				slog.Warn("publish cached tts ready failed", "message_id", messageID, "error", err)
			}
			continue
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		jobID, err := newUUID()
		if err != nil {
			return err
		}
		job := Job{
			ID:                    jobID,
			ChannelID:             channelID,
			ChannelSlug:           channelSlug,
			MessageID:             messageID,
			ContentHash:           hash,
			PartIndex:             int32(i),
			PartCount:             int32(len(parts)),
			Text:                  part,
			SpeakerID:             speaker.StyleID,
			SpeakerName:           speaker.Character.Name,
			SpeakerStyleName:      speaker.StyleName,
			SpeedScale:            messageSettings.SpeedScale,
			PitchScale:            e.settings.PitchScale,
			IntonationScale:       e.settings.IntonationScale,
			VolumeScale:           e.settings.VolumeScale,
			PrePhonemeLength:      e.settings.PrePhonemeLength,
			PostPhonemeLength:     e.settings.PostPhonemeLength,
			VoicevoxEngineVersion: e.settings.VoicevoxEngineVersion,
			NormalizerVersion:     normalizerVersion,
			SplitterVersion:       splitterVersion,
		}
		if err := e.q.CreateTTSJob(ctx, db.CreateTTSJobParams{
			ID:          jobID,
			ChannelID:   channelID,
			MessageID:   messageID,
			ContentHash: hash,
			Priority:    0,
			SpeakerID:   job.SpeakerID,
			TextPreview: nullableText(textPreview(part)),
			TextLength:  int32(len([]rune(part))),
		}); err != nil {
			return err
		}
		if err := e.queue.Enqueue(ctx, job); err != nil {
			return err
		}
	}
	e.skipOverflow(ctx, channelID, channelSlug)
	return nil
}

func (e *Enqueuer) loadAutoDictionary(ctx context.Context) []RuntimeDictionaryEntry {
	rows, err := e.q.ListTTSAutoDictionaryEntries(ctx)
	if err != nil {
		slog.Warn("list tts auto dictionary entries failed", "error", err)
		return nil
	}
	entries := make([]RuntimeDictionaryEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, RuntimeDictionaryEntry{
			Term:    row.Term,
			Reading: row.Reading,
		})
	}
	return entries
}

func (e *Enqueuer) resolveSpeaker(ctx context.Context, speakerUUID string) (voicevox.ResolvedSpeaker, error) {
	resolved, err := e.resolver.Resolve(ctx, speakerUUID)
	if err == nil {
		return resolved, nil
	}
	slog.Warn("resolve voicevox speaker failed", "speaker_uuid", speakerUUID, "error", err)
	return voicevox.ResolvedSpeaker{}, err
}

func assetUsable(asset db.TTSAsset) bool {
	info, err := os.Stat(asset.FilePath)
	return err == nil && info.Size() > 0
}

func (e *Enqueuer) skipOverflow(ctx context.Context, channelID int64, channelSlug string) {
	if e.settings.MaxPendingPerChannel <= 0 {
		return
	}
	skipped, err := e.q.MarkOldQueuedTTSJobsSkipped(ctx, db.MarkOldQueuedTTSJobsSkippedParams{
		ChannelID: channelID,
		Offset:    e.settings.MaxPendingPerChannel,
	})
	if err != nil {
		slog.Warn("skip overflow tts jobs failed", "channel_id", channelID, "error", err)
		return
	}
	for _, messageID := range skipped {
		msgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = e.publish(msgCtx, channelSlug, chat.TTSSkipped(strconv.FormatInt(messageID, 10), "queue_overflow"))
		cancel()
	}
}

func (e *Enqueuer) publish(ctx context.Context, channelSlug string, msg chat.ServerMessage) error {
	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return e.bus.Publish(pubCtx, channelSlug, msg)
}

func nullableText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func mustUUID() string {
	id, err := newUUID()
	if err != nil {
		return ""
	}
	return id
}
