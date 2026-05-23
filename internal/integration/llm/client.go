package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/prompt"
)

type AnalyzeInput struct {
	DocumentName string
	DocumentText string
	ChunkText    string
	ChunkIndex   int
	ChunkCount   int
	Mode         domain.AnalysisMode
	Source       domain.DocumentSource
}

type ChunkAnalysisResult struct {
	Findings      []domain.Finding
	PromptVersion string
	SystemPrompt  string
	UserPrompt    string
	RawResponse   string
}

type Client interface {
	AnalyzeChunk(ctx context.Context, input AnalyzeInput) (ChunkAnalysisResult, error)
}

type OpenAICompatibleClient struct {
	baseURL       string
	apiKey        string
	model         string
	httpClient    *http.Client
	promptBuilder prompt.Builder
}

type chatCompletionRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type chunkAnalysisResponse struct {
	Findings []findingPayload `json:"findings"`
}

type findingPayload struct {
	Role       string `json:"role"`
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Problem    string `json:"problem"`
	WhyItIsBad string `json:"why_it_is_bad"`
	HowToFix   string `json:"how_to_fix"`
}

func NewOpenAICompatibleClient(cfg config.LLMConfig, promptBuilder prompt.Builder) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		httpClient:    &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second},
		promptBuilder: promptBuilder,
	}
}

func (c *OpenAICompatibleClient) AnalyzeChunk(ctx context.Context, input AnalyzeInput) (ChunkAnalysisResult, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return ChunkAnalysisResult{}, errors.New("llm api key is not configured")
	}

	builtPrompt := c.promptBuilder.Build(prompt.Input{
		DocumentName: input.DocumentName,
		DocumentText: input.DocumentText,
		ChunkText:    input.ChunkText,
		ChunkIndex:   input.ChunkIndex,
		ChunkCount:   input.ChunkCount,
		Mode:         input.Mode,
		Source:       input.Source,
	})

	payload := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: builtPrompt.System},
			{Role: "user", Content: builtPrompt.User},
		},
		Temperature: 0.1,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("marshal llm request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("build llm request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("execute llm request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("read llm response: %w", err)
	}

	var completion chatCompletionResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("decode llm response: %w", err)
	}

	if completion.Error != nil {
		return ChunkAnalysisResult{}, fmt.Errorf("llm api error: %s", completion.Error.Message)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return ChunkAnalysisResult{}, fmt.Errorf("llm status %d", resp.StatusCode)
	}

	if len(completion.Choices) == 0 {
		return ChunkAnalysisResult{}, errors.New("llm returned no choices")
	}

	content := completion.Choices[0].Message.Content
	parsed, err := parseChunkAnalysis(content)
	if err != nil {
		return ChunkAnalysisResult{}, err
	}

	findings := make([]domain.Finding, 0, len(parsed.Findings))
	for _, finding := range parsed.Findings {
		findings = append(findings, domain.Finding{
			ChunkIndex:  input.ChunkIndex,
			Role:        normalizeRole(finding.Role),
			Category:    normalizeCategory(finding.Category),
			Severity:    normalizeSeverity(finding.Severity),
			Problem:     strings.TrimSpace(finding.Problem),
			WhyItIsBad:  strings.TrimSpace(finding.WhyItIsBad),
			HowToFix:    strings.TrimSpace(finding.HowToFix),
			SourceChunk: input.ChunkText,
		})
	}

	return ChunkAnalysisResult{
		Findings:      findings,
		PromptVersion: "v1",
		SystemPrompt:  builtPrompt.System,
		UserPrompt:    builtPrompt.User,
		RawResponse:   content,
	}, nil
}

func parseChunkAnalysis(content string) (chunkAnalysisResponse, error) {
	var parsed chunkAnalysisResponse

	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		return parsed, nil
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return chunkAnalysisResponse{}, errors.New("llm response does not contain json object")
	}

	if err := json.Unmarshal([]byte(content[start:end+1]), &parsed); err != nil {
		return chunkAnalysisResponse{}, fmt.Errorf("decode structured llm output: %w", err)
	}

	return parsed, nil
}

func normalizeSeverity(input string) domain.Severity {
	switch strings.ToUpper(strings.TrimSpace(input)) {
	case string(domain.SeverityCritical):
		return domain.SeverityCritical
	case string(domain.SeverityError):
		return domain.SeverityError
	case string(domain.SeverityWarning):
		return domain.SeverityWarning
	default:
		return domain.SeverityInfo
	}
}

func normalizeRole(input string) string {
	value := strings.TrimSpace(strings.ToLower(input))
	if value == "" {
		return "solution_architect"
	}

	return strings.ReplaceAll(value, " ", "_")
}

func normalizeCategory(input string) string {
	value := strings.TrimSpace(strings.ToLower(input))
	if value == "" {
		return "general"
	}

	return strings.ReplaceAll(value, " ", "_")
}

var _ Client = (*OpenAICompatibleClient)(nil)
