package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/prompt"
)

type stubPromptBuilder struct{}

func (stubPromptBuilder) Build(prompt.Input) prompt.BuiltPrompt {
	return prompt.BuiltPrompt{
		System: "system",
		User:   "user",
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAICompatibleClientUsesMaxCompletionTokensForDirectOpenAIGPT5(t *testing.T) {
	var captured chatCompletionRequest

	client := NewOpenAICompatibleClient(config.LLMConfig{
		BaseURL:   "https://api.openai.com/v1",
		APIKey:    "test-key",
		Model:     "gpt-5.5",
		Timeout:   30,
		MaxTokens: 1100,
	}, stubPromptBuilder{})
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v1/chat/completions" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if err := json.Unmarshal(body, &captured); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}]}`)),
			}, nil
		}),
	}

	_, err := client.AnalyzeChunk(context.Background(), AnalyzeInput{
		DocumentName: "spec.md",
		ChunkText:    "chunk",
		ChunkIndex:   0,
		ChunkCount:   1,
		Mode:         domain.AnalysisModeFullReview,
		Source:       domain.DocumentSourceUpload,
		Role:         domain.ReviewerRoleTechLead,
		Temperature:  -1,
	})
	if err != nil {
		t.Fatalf("AnalyzeChunk() error = %v", err)
	}

	if captured.MaxCompletionTokens != 1100 {
		t.Fatalf("expected max_completion_tokens 1100, got %d", captured.MaxCompletionTokens)
	}
	if captured.MaxTokens != 0 {
		t.Fatalf("expected max_tokens to be omitted, got %d", captured.MaxTokens)
	}
	if captured.Temperature != 0 {
		t.Fatalf("expected temperature to be omitted, got %v", captured.Temperature)
	}
	if captured.TopP != 0 {
		t.Fatalf("expected top_p to be omitted, got %v", captured.TopP)
	}
}

func TestOpenAICompatibleClientUsesMaxTokensForCompatibleNonOpenAIProviders(t *testing.T) {
	var captured chatCompletionRequest

	client := NewOpenAICompatibleClient(config.LLMConfig{
		BaseURL:     "https://openrouter.ai/api/v1",
		APIKey:      "test-key",
		Model:       "openai/gpt-5.5",
		Timeout:     30,
		MaxTokens:   900,
		Temperature: 0.3,
		TopP:        0.8,
	}, stubPromptBuilder{})
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if err := json.Unmarshal(body, &captured); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}]}`)),
			}, nil
		}),
	}

	_, err := client.AnalyzeChunk(context.Background(), AnalyzeInput{
		DocumentName: "spec.md",
		ChunkText:    "chunk",
		ChunkIndex:   0,
		ChunkCount:   1,
		Mode:         domain.AnalysisModeFullReview,
		Source:       domain.DocumentSourceUpload,
		Role:         domain.ReviewerRoleTechLead,
		Temperature:  -1,
	})
	if err != nil {
		t.Fatalf("AnalyzeChunk() error = %v", err)
	}

	if captured.MaxTokens != 900 {
		t.Fatalf("expected max_tokens 900, got %d", captured.MaxTokens)
	}
	if captured.MaxCompletionTokens != 0 {
		t.Fatalf("expected max_completion_tokens to be omitted, got %d", captured.MaxCompletionTokens)
	}
	if captured.Temperature == 0 {
		t.Fatalf("expected temperature to be present")
	}
	if captured.TopP == 0 {
		t.Fatalf("expected top_p to be present")
	}
}
