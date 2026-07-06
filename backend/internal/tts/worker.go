package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mamemomonga/mamechat/backend/internal/chat"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

type Worker struct {
	settings Settings
	q        *db.Queries
	queue    *Queue
	bus      Bus
	client   *http.Client
}

func NewWorker(settings Settings, q *db.Queries, queue *Queue, bus Bus) *Worker {
	return &Worker{
		settings: settings,
		q:        q,
		queue:    queue,
		bus:      bus,
		client:   &http.Client{},
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if !w.settings.Enabled {
		slog.Info("tts worker disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	concurrency := w.settings.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workerIndex := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.runLoop(ctx, workerIndex)
		}()
	}
	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (w *Worker) runLoop(ctx context.Context, workerIndex int) {
	voicevoxURL := w.voicevoxURLFor(workerIndex)
	slog.Info("tts worker loop started", "worker_index", workerIndex, "voicevox_url", voicevoxURL)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, err := w.queue.Dequeue(ctx, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("dequeue tts job failed", "worker_index", workerIndex, "error", err)
			continue
		}
		if job == nil {
			continue
		}
		w.process(ctx, *job, workerIndex)
	}
}

func (w *Worker) process(ctx context.Context, job Job, workerIndex int) {
	status, err := w.q.GetTTSJobStatus(ctx, job.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("tts job row missing", "job_id", job.ID)
			return
		}
		w.fail(ctx, job, "db_status_failed", err)
		return
	}
	if status != "queued" {
		slog.Info("skip inactive tts job", "job_id", job.ID, "status", status)
		return
	}
	if err := w.q.MarkTTSJobProcessing(ctx, job.ID); err != nil {
		slog.Warn("mark tts processing failed", "job_id", job.ID, "error", err)
	}

	if asset, ok := w.cachedAsset(ctx, job.ContentHash); ok {
		w.markPartReady(ctx, job, asset)
		return
	}

	locked, err := w.queue.AcquireLock(ctx, job.ContentHash, job.ID, 60*time.Second)
	if err != nil {
		w.fail(ctx, job, "lock_failed", err)
		return
	}
	if !locked {
		asset, err := w.waitCachedAsset(ctx, job.ContentHash, 12*time.Second)
		if err != nil {
			w.fail(ctx, job, "duplicate_generation_timeout", err)
			return
		}
		w.markPartReady(ctx, job, asset)
		return
	}

	wav, err := w.synthesize(ctx, job, workerIndex)
	if err != nil {
		w.fail(ctx, job, "synthesis_failed", err)
		return
	}
	filePath, size, err := w.convertAndSave(job.ContentHash, wav)
	if err != nil {
		w.fail(ctx, job, "ffmpeg_failed", err)
		return
	}

	asset, err := w.q.UpsertTTSAsset(ctx, db.UpsertTTSAssetParams{
		ContentHash:           job.ContentHash,
		FilePath:              filePath,
		FileSizeBytes:         size,
		DurationMs:            pgtype.Int4{},
		TextPreview:           nullableText(textPreview(job.Text)),
		TextLength:            int32(len([]rune(job.Text))),
		SpeakerID:             job.SpeakerID,
		SpeakerName:           job.SpeakerName,
		SpeakerStyleName:      nullableText(job.SpeakerStyleName),
		SpeedScale:            job.SpeedScale,
		PitchScale:            job.PitchScale,
		IntonationScale:       job.IntonationScale,
		VolumeScale:           job.VolumeScale,
		PrePhonemeLength:      job.PrePhonemeLength,
		PostPhonemeLength:     job.PostPhonemeLength,
		VoicevoxEngineVersion: job.VoicevoxEngineVersion,
		NormalizerVersion:     job.NormalizerVersion,
		SplitterVersion:       job.SplitterVersion,
	})
	if err != nil {
		w.fail(ctx, job, "db_update_failed", err)
		return
	}
	w.markPartReady(ctx, job, asset)
}

func (w *Worker) cachedAsset(ctx context.Context, contentHash string) (db.TTSAsset, bool) {
	asset, err := w.q.GetTTSAsset(ctx, contentHash)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("get tts asset failed", "content_hash", contentHash, "error", err)
		}
		return db.TTSAsset{}, false
	}
	info, err := os.Stat(asset.FilePath)
	if err != nil {
		slog.Warn("tts asset file missing", "content_hash", contentHash, "file_path", asset.FilePath, "error", err)
		return db.TTSAsset{}, false
	}
	if info.Size() == 0 {
		slog.Warn("tts asset file is empty", "content_hash", contentHash, "file_path", asset.FilePath)
		return db.TTSAsset{}, false
	}
	if err := w.q.TouchTTSAsset(ctx, contentHash); err != nil {
		slog.Warn("touch tts asset failed", "content_hash", contentHash, "error", err)
	}
	return asset, true
}

func (w *Worker) waitCachedAsset(ctx context.Context, contentHash string, timeout time.Duration) (db.TTSAsset, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if asset, ok := w.cachedAsset(ctx, contentHash); ok {
			return asset, nil
		}
		select {
		case <-ctx.Done():
			return db.TTSAsset{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return db.TTSAsset{}, fmt.Errorf("tts asset did not appear: %s", contentHash)
}

func (w *Worker) synthesize(ctx context.Context, job Job, workerIndex int) ([]byte, error) {
	return synthesizeWAV(ctx, w.client, w.voicevoxURLFor(workerIndex), job.Text, job.SpeakerID, synthParams{
		SpeedScale:        job.SpeedScale,
		PitchScale:        job.PitchScale,
		IntonationScale:   job.IntonationScale,
		VolumeScale:       job.VolumeScale,
		PrePhonemeLength:  job.PrePhonemeLength,
		PostPhonemeLength: job.PostPhonemeLength,
	})
}

func (w *Worker) voicevoxURLFor(workerIndex int) string {
	if len(w.settings.VoicevoxURLs) == 0 {
		return ""
	}
	return w.settings.VoicevoxURLs[workerIndex%len(w.settings.VoicevoxURLs)]
}

func (w *Worker) convertAndSave(contentHash string, wav []byte) (string, int64, error) {
	return convertWAVToM4A(w.settings.StorageDir, contentHash, wav)
}

func (w *Worker) markPartReady(ctx context.Context, job Job, asset db.TTSAsset) {
	if err := w.q.CreateTTSMessagePart(ctx, db.CreateTTSMessagePartParams{
		ID:          mustUUID(),
		ChannelID:   job.ChannelID,
		MessageID:   job.MessageID,
		ContentHash: job.ContentHash,
		PartIndex:   job.PartIndex,
		TextPreview: nullableText(textPreview(job.Text)),
		TextLength:  int32(len([]rune(job.Text))),
	}); err != nil {
		slog.Warn("create tts message part failed", "job_id", job.ID, "error", err)
	}
	if err := w.q.MarkTTSJobReady(ctx, job.ID); err != nil {
		slog.Warn("mark tts ready failed", "job_id", job.ID, "error", err)
	}
	msgCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := w.bus.Publish(msgCtx, job.ChannelSlug, chat.TTSPartReady(strconv.FormatInt(job.MessageID, 10), int(job.PartIndex), job.ContentHash, asset.DurationMs.Int32)); err != nil {
		slog.Warn("publish tts ready failed", "job_id", job.ID, "error", err)
	}
}

func (w *Worker) fail(ctx context.Context, job Job, reason string, err error) {
	slog.Warn("tts job failed", "job_id", job.ID, "message_id", job.MessageID, "reason", reason, "error", err)
	if markErr := w.q.MarkTTSJobFailed(ctx, db.MarkTTSJobFailedParams{ID: job.ID, ErrorMessage: reason}); markErr != nil {
		slog.Warn("mark tts failed failed", "job_id", job.ID, "error", markErr)
	}
	msgCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if pubErr := w.bus.Publish(msgCtx, job.ChannelSlug, chat.TTSError(strconv.FormatInt(job.MessageID, 10), reason)); pubErr != nil {
		slog.Warn("publish tts error failed", "job_id", job.ID, "error", pubErr)
	}
}
