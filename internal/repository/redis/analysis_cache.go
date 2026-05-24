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

type cachedAnalysis struct {
	ID                 string                `json:"id"`
	DocumentID         string                `json:"document_id"`
	Mode               domain.AnalysisMode   `json:"mode"`
	Status             domain.AnalysisStatus `json:"status"`
	Provider           string                `json:"provider"`
	Model              string                `json:"model"`
	ChunkCount         int                   `json:"chunk_count"`
	MergeBlocked       bool                  `json:"merge_blocked"`
	BlockingFindings   int                   `json:"blocking_findings"`
	SuppressedFindings int                   `json:"suppressed_findings"`
	Findings           []domain.Finding      `json:"findings"`
	Summary            string                `json:"summary"`
	ErrorMessage       string                `json:"error_message"`
	CreatedAt          time.Time             `json:"created_at"`
	CompletedAt        *time.Time            `json:"completed_at"`
}

func NewAnalysisCache(client *goredis.Client, ttl time.Duration) *AnalysisCache {
	return &AnalysisCache{client: client, ttl: ttl}
}

func (c *AnalysisCache) Set(ctx context.Context, analysis domain.Analysis) error {
	payload, err := json.Marshal(toCachedAnalysis(analysis))
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
	var cached cachedAnalysis
	if err := json.Unmarshal([]byte(value), &cached); err != nil {
		return domain.Analysis{}, false, err
	}
	return cached.toDomain(), true, nil
}

func (c *AnalysisCache) Delete(ctx context.Context, id string) error {
	return c.client.Del(ctx, cacheKey(id)).Err()
}

func cacheKey(id string) string {
	return "analysis:" + id
}

func toCachedAnalysis(analysis domain.Analysis) cachedAnalysis {
	return cachedAnalysis{
		ID:                 analysis.ID,
		DocumentID:         analysis.DocumentID,
		Mode:               analysis.Mode,
		Status:             analysis.Status,
		Provider:           analysis.Provider,
		Model:              analysis.Model,
		ChunkCount:         analysis.ChunkCount,
		MergeBlocked:       analysis.MergeBlocked,
		BlockingFindings:   analysis.BlockingFindings,
		SuppressedFindings: analysis.SuppressedFindings,
		Findings:           analysis.Findings,
		Summary:            analysis.Summary,
		ErrorMessage:       analysis.ErrorMessage,
		CreatedAt:          analysis.CreatedAt,
		CompletedAt:        analysis.CompletedAt,
	}
}

func (c cachedAnalysis) toDomain() domain.Analysis {
	return domain.Analysis{
		ID:                 c.ID,
		DocumentID:         c.DocumentID,
		Mode:               c.Mode,
		Status:             c.Status,
		Provider:           c.Provider,
		Model:              c.Model,
		ChunkCount:         c.ChunkCount,
		MergeBlocked:       c.MergeBlocked,
		BlockingFindings:   c.BlockingFindings,
		SuppressedFindings: c.SuppressedFindings,
		Findings:           c.Findings,
		Summary:            c.Summary,
		ErrorMessage:       c.ErrorMessage,
		CreatedAt:          c.CreatedAt,
		CompletedAt:        c.CompletedAt,
	}
}
