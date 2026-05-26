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

type Embedder interface {
	EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)
}

type OpenRouterEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenRouterEmbedder(cfg config.VectorConfig) *OpenRouterEmbedder {
	return &OpenRouterEmbedder{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.EmbeddingBaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.EmbeddingAPIKey),
		model:   strings.TrimSpace(cfg.EmbeddingModel),
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.EmbeddingTimeoutSeconds) * time.Second,
		},
	}
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *OpenRouterEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	filtered := make([]string, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		filtered = append(filtered, text)
	}
	if len(filtered) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embeddingsRequest{
		Model: e.model,
		Input: filtered,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request embeddings: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embeddings response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("embeddings provider returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded embeddingsResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("embeddings provider error: %s", decoded.Error.Message)
	}

	result := make([][]float32, 0, len(decoded.Data))
	for _, item := range decoded.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embeddings response contained an empty vector")
		}
		result = append(result, item.Embedding)
	}
	if len(result) != len(filtered) {
		return nil, fmt.Errorf("embeddings count mismatch: got %d vectors for %d texts", len(result), len(filtered))
	}
	return result, nil
}
