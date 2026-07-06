package tts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const queueKey = "tts:jobs"

type Queue struct {
	client *redis.Client
}

func NewQueue(redisURL string) (*Queue, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Queue{client: redis.NewClient(opt)}, nil
}

func (q *Queue) Close() error {
	return q.client.Close()
}

func (q *Queue) Ping(ctx context.Context) error {
	return q.client.Ping(ctx).Err()
}

func (q *Queue) Enqueue(ctx context.Context, job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if err := q.client.LPush(ctx, queueKey, payload).Err(); err != nil {
		return fmt.Errorf("enqueue tts job: %w", err)
	}
	return nil
}

func (q *Queue) Dequeue(ctx context.Context, timeout time.Duration) (*Job, error) {
	result, err := q.client.BRPop(ctx, timeout, queueKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(result) != 2 {
		return nil, fmt.Errorf("unexpected tts queue result: %v", result)
	}
	var job Job
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (q *Queue) AcquireLock(ctx context.Context, contentHash, jobID string, ttl time.Duration) (bool, error) {
	ok, err := q.client.SetNX(ctx, "tts:lock:"+contentHash, jobID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire tts lock: %w", err)
	}
	return ok, nil
}
