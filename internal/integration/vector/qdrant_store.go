package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"technical-specification-review-agent/internal/config"
)

type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

type Store interface {
	Upsert(ctx context.Context, points []Point) error
}

type QdrantStore struct {
	baseURL        string
	apiKey         string
	collection     string
	httpClient     *http.Client
	collectionSize int
}

func NewQdrantStore(cfg config.VectorConfig) *QdrantStore {
	return &QdrantStore{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.DBURL), "/"),
		apiKey:     strings.TrimSpace(cfg.DBAPIKey),
		collection: strings.TrimSpace(cfg.Collection),
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *QdrantStore) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	size := len(points[0].Vector)
	if size == 0 {
		return fmt.Errorf("qdrant point vector is empty")
	}
	if err := s.ensureCollection(ctx, size); err != nil {
		return err
	}

	bodyPoints := make([]map[string]any, 0, len(points))
	for _, point := range points {
		bodyPoints = append(bodyPoints, map[string]any{
			"id":      point.ID,
			"vector":  point.Vector,
			"payload": point.Payload,
		})
	}
	body, err := json.Marshal(map[string]any{
		"points": bodyPoints,
	})
	if err != nil {
		return fmt.Errorf("marshal qdrant upsert request: %w", err)
	}
	return s.doJSON(ctx, http.MethodPut, s.collectionPath()+"/points", body)
}

func (s *QdrantStore) ensureCollection(ctx context.Context, size int) error {
	if s.collectionSize == size {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     size,
			"distance": "Cosine",
		},
	})
	if err != nil {
		return fmt.Errorf("marshal qdrant collection request: %w", err)
	}
	if err := s.doJSONAllowConflict(ctx, http.MethodPut, s.collectionPath(), body); err != nil {
		return err
	}
	s.collectionSize = size
	return nil
}

func (s *QdrantStore) doJSONAllowConflict(ctx context.Context, method, path string, body []byte) error {
	err := s.doJSON(ctx, method, path, body)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "status 409") {
		return nil
	}
	return err
}

func (s *QdrantStore) doJSON(ctx context.Context, method, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build qdrant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request qdrant: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read qdrant response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("qdrant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}

func (s *QdrantStore) collectionPath() string {
	return "/collections/" + s.collection
}
