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

	"technical-specification-review-agent/internal/apperrors"
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
	SectionTitle string
	SectionLevel int
	Mode         domain.AnalysisMode
	Source       domain.DocumentSource
	Role         domain.ReviewerRole
	Memory       domain.ReviewMemory
	Temperature  float64
	TopP         float64
	MaxTokens    int
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
	useJSONPrefix bool
	temperature   float64
	topP          float64
	maxTokens     int
	httpClient    *http.Client
	promptBuilder prompt.Builder
}

type chatCompletionRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	TopP           float64           `json:"top_p,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
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

const maxStructuredOutputAttempts = 2

func NewOpenAICompatibleClient(cfg config.LLMConfig, promptBuilder prompt.Builder) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:        cfg.APIKey,
		model:         cfg.Model,
		useJSONPrefix: shouldUseDeepSeekJSONPrefix(cfg),
		temperature:   cfg.Temperature,
		topP:          cfg.TopP,
		maxTokens:     cfg.MaxTokens,
		httpClient:    &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second},
		promptBuilder: promptBuilder,
	}
}

func (c *OpenAICompatibleClient) AnalyzeChunk(ctx context.Context, input AnalyzeInput) (ChunkAnalysisResult, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return ChunkAnalysisResult{}, apperrors.New(apperrors.KindDependency, "llm api key is not configured")
	}

	builtPrompt := c.promptBuilder.Build(prompt.Input{
		DocumentName: input.DocumentName,
		DocumentText: input.DocumentText,
		ChunkText:    input.ChunkText,
		ChunkIndex:   input.ChunkIndex,
		ChunkCount:   input.ChunkCount,
		SectionTitle: input.SectionTitle,
		SectionLevel: input.SectionLevel,
		Mode:         input.Mode,
		Source:       input.Source,
		Role:         input.Role,
		Memory:       input.Memory,
	})

	return c.executePrompt(ctx, builtPrompt, input, domain.DocumentSection{
		Title: input.SectionTitle,
		Level: input.SectionLevel,
	})
}

func (c *OpenAICompatibleClient) executePrompt(
	ctx context.Context,
	builtPrompt prompt.BuiltPrompt,
	input AnalyzeInput,
	section domain.DocumentSection,
) (ChunkAnalysisResult, error) {
	var (
		content string
		parsed  chunkAnalysisResponse
		err     error
	)
	for attempt := 1; attempt <= maxStructuredOutputAttempts; attempt++ {
		content, err = c.requestLLMContent(ctx, builtPrompt, input)
		if err != nil {
			return ChunkAnalysisResult{}, err
		}

		parsed, err = parseChunkAnalysis(content)
		if err == nil {
			break
		}
		if attempt == maxStructuredOutputAttempts {
			return ChunkAnalysisResult{}, apperrors.Wrap(apperrors.KindDependency, "llm returned invalid structured output", err)
		}
	}

	findings := make([]domain.Finding, 0, len(parsed.Findings))
	for _, finding := range parsed.Findings {
		item := domain.Finding{
			ChunkIndex:   input.ChunkIndex,
			Role:         normalizeRole(input.Role, finding.Role),
			Category:     normalizeCategory(finding.Category),
			Severity:     normalizeSeverity(finding.Severity),
			Problem:      strings.TrimSpace(finding.Problem),
			WhyItIsBad:   strings.TrimSpace(finding.WhyItIsBad),
			HowToFix:     strings.TrimSpace(finding.HowToFix),
			SourceChunk:  input.ChunkText,
			SectionID:    section.ID,
			SectionTitle: section.Title,
		}
		findings = append(findings, item)
	}

	return ChunkAnalysisResult{
		Findings:      findings,
		PromptVersion: promptVersion(input.ChunkIndex),
		SystemPrompt:  builtPrompt.System,
		UserPrompt:    builtPrompt.User,
		RawResponse:   content,
	}, nil
}

func (c *OpenAICompatibleClient) requestLLMContent(ctx context.Context, builtPrompt prompt.BuiltPrompt, input AnalyzeInput) (string, error) {
	temperature := c.temperature
	if input.Temperature >= 0 {
		temperature = input.Temperature
	}
	topP := c.topP
	if input.TopP > 0 {
		topP = input.TopP
	}
	maxTokens := c.maxTokens
	if input.MaxTokens > 0 {
		maxTokens = input.MaxTokens
	}

	payload := chatCompletionRequest{
		Model:       c.model,
		Messages:    buildChatMessages(builtPrompt, c.useJSONPrefix),
		Temperature: temperature,
		TopP:        topP,
		MaxTokens:   maxTokens,
		ResponseFormat: map[string]string{
			"type": "json_object",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", apperrors.Wrap(apperrors.KindInternal, "failed to build llm request", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", apperrors.Wrap(apperrors.KindInternal, "failed to build llm request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", apperrors.Wrap(apperrors.KindDependency, "failed to reach llm provider", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", apperrors.Wrap(apperrors.KindDependency, "failed to read llm response", err)
	}

	var completion chatCompletionResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", apperrors.Wrap(apperrors.KindDependency, "failed to decode llm response", err)
	}

	if completion.Error != nil {
		return "", apperrors.New(apperrors.KindDependency, fmt.Sprintf("llm provider returned an error: %s", strings.TrimSpace(completion.Error.Message)))
	}

	if resp.StatusCode >= http.StatusBadRequest {
		message := fmt.Sprintf("llm provider returned status %d", resp.StatusCode)
		if trimmed := strings.TrimSpace(string(respBody)); trimmed != "" {
			message = fmt.Sprintf("%s: %s", message, safeErrorSnippet(trimmed, 280))
		}
		return "", apperrors.New(apperrors.KindDependency, message)
	}

	if len(completion.Choices) == 0 {
		return "", apperrors.New(apperrors.KindDependency, "llm returned no choices")
	}

	return completion.Choices[0].Message.Content, nil
}

func buildChatMessages(builtPrompt prompt.BuiltPrompt, useJSONPrefix bool) []chatMessage {
	messages := []chatMessage{
		{Role: "system", Content: builtPrompt.System},
		{Role: "user", Content: builtPrompt.User},
	}
	if useJSONPrefix {
		messages = append(messages, chatMessage{
			Role:    "assistant",
			Content: "{\"findings\":",
		})
	}
	return messages
}

func shouldUseDeepSeekJSONPrefix(cfg config.LLMConfig) bool {
	baseURL := strings.ToLower(strings.TrimSpace(cfg.BaseURL))
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	return strings.Contains(baseURL, "deepseek.com") || strings.Contains(model, "deepseek")
}

func promptVersion(chunkIndex int) string {
	if chunkIndex >= 0 {
		return "v3-role-memory"
	}
	return "v3-role-memory"
}

func parseChunkAnalysis(content string) (chunkAnalysisResponse, error) {
	var parsed chunkAnalysisResponse

	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		return parsed, nil
	}

	candidate, ok := extractFirstJSONObject(content)
	if !ok {
		return chunkAnalysisResponse{}, errors.New("llm response does not contain json object")
	}

	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return chunkAnalysisResponse{}, fmt.Errorf("decode structured llm output: %w", err)
	}

	return parsed, nil
}

func extractFirstJSONObject(content string) (string, bool) {
	start := strings.Index(content, "{")
	if start == -1 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false
	for idx := start; idx < len(content); idx++ {
		ch := content[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[start : idx+1], true
			}
		}
	}

	return "", false
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

func normalizeRole(fallback domain.ReviewerRole, input string) string {
	value := strings.TrimSpace(strings.ToLower(input))
	if value == "" {
		return string(fallback)
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

func safeErrorSnippet(value string, maxRunes int) string {
	runes := []rune(value)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

var _ Client = (*OpenAICompatibleClient)(nil)
