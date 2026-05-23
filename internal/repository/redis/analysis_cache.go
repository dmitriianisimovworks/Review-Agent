package redis

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"technical-specification-review-agent/internal/domain"
)

type AnalysisCache struct {
	client *goredis.Client
	ttl    time.Duration
}

func NewAnalysisCache(client *goredis.Client, ttl time.Duration) *AnalysisCache {
	return &AnalysisCache{client: client, ttl: ttl}
}

func (c *AnalysisCache) Set(ctx context.Context, analysis domain.Analysis) error {
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, cacheKey(analysis.ID), payload, c.ttl).Err()
}

func (c *AnalysisCache) Get(ctx context.Context, id string) (domain.Analysis, bool, error) {
	value, err := c.client.Get(ctx, cacheKey(id)).Result()
	if err == goredis.Nil {
		return domain.Analysis{}, false, nil
	}
	if err != nil {
		return domain.Analysis{}, false, err
	}
	var analysis domain.Analysis
	if err := json.Unmarshal([]byte(value), &analysis); err != nil {
		return domain.Analysis{}, false, err
	}
	return analysis, true, nil
}

func cacheKey(id string) string {
	return "analysis:" + id
}
