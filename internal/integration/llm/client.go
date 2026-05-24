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
}

type AnalyzeSectionPairInput struct {
	DocumentName string
	Source       domain.DocumentSource
	Mode         domain.AnalysisMode
	SectionA     domain.DocumentSection
	SectionB     domain.DocumentSection
	Memory       domain.ReviewMemory
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
	AnalyzeSectionPair(ctx context.Context, input AnalyzeSectionPairInput) (ChunkAnalysisResult, error)
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

const maxStructuredOutputAttempts = 2

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

	return c.executePrompt(ctx, builtPrompt, input.Role, input.ChunkText, input.ChunkIndex, domain.DocumentSection{
		Title: input.SectionTitle,
		Level: input.SectionLevel,
	}, nil)
}

func (c *OpenAICompatibleClient) AnalyzeSectionPair(ctx context.Context, input AnalyzeSectionPairInput) (ChunkAnalysisResult, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return ChunkAnalysisResult{}, apperrors.New(apperrors.KindDependency, "llm api key is not configured")
	}

	builtPrompt := c.promptBuilder.BuildCrossSectionContradiction(prompt.CrossSectionInput{
		DocumentName: input.DocumentName,
		Source:       input.Source,
		Mode:         input.Mode,
		SectionA:     input.SectionA,
		SectionB:     input.SectionB,
		Memory:       input.Memory,
	})

	return c.executePrompt(ctx, builtPrompt, domain.ReviewerRoleSolutionArchitect, input.SectionA.Content+"\n\n"+input.SectionB.Content, -1, input.SectionA, &input.SectionB)
}

func (c *OpenAICompatibleClient) executePrompt(
	ctx context.Context,
	builtPrompt prompt.BuiltPrompt,
	fallbackRole domain.ReviewerRole,
	sourceChunk string,
	chunkIndex int,
	section domain.DocumentSection,
	relatedSection *domain.DocumentSection,
) (ChunkAnalysisResult, error) {
	var (
		content string
		parsed  chunkAnalysisResponse
		err     error
	)
	for attempt := 1; attempt <= maxStructuredOutputAttempts; attempt++ {
		content, err = c.requestLLMContent(ctx, builtPrompt)
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
			ChunkIndex:   chunkIndex,
			Role:         normalizeRole(fallbackRole, finding.Role),
			Category:     normalizeCategory(finding.Category),
			Severity:     normalizeSeverity(finding.Severity),
			Problem:      strings.TrimSpace(finding.Problem),
			WhyItIsBad:   strings.TrimSpace(finding.WhyItIsBad),
			HowToFix:     strings.TrimSpace(finding.HowToFix),
			SourceChunk:  sourceChunk,
			SectionID:    section.ID,
			SectionTitle: section.Title,
		}
		if relatedSection != nil {
			item.RelatedSectionID = relatedSection.ID
			item.RelatedSectionTitle = relatedSection.Title
		}
		findings = append(findings, item)
	}

	return ChunkAnalysisResult{
		Findings:      findings,
		PromptVersion: promptVersion(chunkIndex, relatedSection != nil),
		SystemPrompt:  builtPrompt.System,
		UserPrompt:    builtPrompt.User,
		RawResponse:   content,
	}, nil
}

func (c *OpenAICompatibleClient) requestLLMContent(ctx context.Context, builtPrompt prompt.BuiltPrompt) (string, error) {
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

func promptVersion(chunkIndex int, crossSection bool) string {
	if crossSection {
		return "v4-cross-section"
	}
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
