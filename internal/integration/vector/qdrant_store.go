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

	"github.com/google/uuid"

	"technical-specification-review-agent/internal/config"
)

type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

type SearchRequest struct {
	Vector           []float32
	Limit            int
	Kinds            []string
	ExcludeReviewKey string
}

type SearchResult struct {
	ID      string
	Score   float64
	Payload map[string]any
}

type Store interface {
	Upsert(ctx context.Context, points []Point) error
	Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)
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
			"id":      stablePointUUID(point.ID),
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

func (s *QdrantStore) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if len(req.Vector) == 0 {
		return nil, fmt.Errorf("qdrant search vector is empty")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 4
	}

	body := map[string]any{
		"query":        req.Vector,
		"limit":        limit,
		"with_payload": true,
	}
	if filter := buildQdrantFilter(req); len(filter) > 0 {
		body["filter"] = filter
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal qdrant search request: %w", err)
	}

	respBody, err := s.doJSONRead(ctx, http.MethodPost, s.collectionPath()+"/points/query", payload)
	if err != nil {
		return nil, err
	}

	var decoded struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
		ResultObject struct {
			Points []struct {
				ID      any            `json:"id"`
				Score   float64        `json:"score"`
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode qdrant search response: %w", err)
	}

	rawResults := decoded.Result
	if len(rawResults) == 0 && len(decoded.ResultObject.Points) > 0 {
		rawResults = make([]struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		}, 0, len(decoded.ResultObject.Points))
		for _, point := range decoded.ResultObject.Points {
			rawResults = append(rawResults, struct {
				ID      any            `json:"id"`
				Score   float64        `json:"score"`
				Payload map[string]any `json:"payload"`
			}{
				ID:      point.ID,
				Score:   point.Score,
				Payload: point.Payload,
			})
		}
	}

	results := make([]SearchResult, 0, len(rawResults))
	for _, item := range rawResults {
		results = append(results, SearchResult{
			ID:      fmt.Sprint(item.ID),
			Score:   item.Score,
			Payload: item.Payload,
		})
	}
	return results, nil
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
	_, err := s.doJSONRead(ctx, method, path, body)
	return err
}

func (s *QdrantStore) doJSONRead(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build qdrant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request qdrant: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read qdrant response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("qdrant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

func (s *QdrantStore) collectionPath() string {
	return "/collections/" + s.collection
}

func stablePointUUID(id string) string {
	normalized := strings.TrimSpace(id)
	if normalized == "" {
		normalized = "empty-point-id"
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(normalized)).String()
}

func buildQdrantFilter(req SearchRequest) map[string]any {
	must := make([]map[string]any, 0, 1)
	if len(req.Kinds) > 0 {
		must = append(must, map[string]any{
			"key": "kind",
			"match": map[string]any{
				"any": req.Kinds,
			},
		})
	}

	mustNot := make([]map[string]any, 0, 1)
	if trimmed := strings.TrimSpace(req.ExcludeReviewKey); trimmed != "" {
		mustNot = append(mustNot, map[string]any{
			"key": "review_key",
			"match": map[string]any{
				"value": trimmed,
			},
		})
	}

	filter := make(map[string]any, 2)
	if len(must) > 0 {
		filter["must"] = must
	}
	if len(mustNot) > 0 {
		filter["must_not"] = mustNot
	}
	return filter
}
