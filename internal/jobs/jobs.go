package jobs

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const analysisQueueKey = "jobs:analysis"

type Runner interface {
	EnqueueAnalysis(ctx context.Context, analysisID string) error
	Run(ctx context.Context, handler func(context.Context, string) error) error
}

type RedisRunner struct {
	client *goredis.Client
}

func NewRedisRunner(client *goredis.Client) *RedisRunner {
	return &RedisRunner{client: client}
}

func (r *RedisRunner) EnqueueAnalysis(ctx context.Context, analysisID string) error {
	return r.client.RPush(ctx, analysisQueueKey, analysisID).Err()
}

func (r *RedisRunner) Run(ctx context.Context, handler func(context.Context, string) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		result, err := r.client.BLPop(ctx, 2*time.Second, analysisQueueKey).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			if err == goredis.Nil {
				continue
			}
			return err
		}
		if len(result) < 2 {
			continue
		}

		if err := handler(ctx, result[1]); err != nil {
			return err
		}
	}
}

var _ Runner = (*RedisRunner)(nil)
