package prompt

import (
	"fmt"
	"strings"

	"technical-specification-review-agent/internal/domain"
)

type Input struct {
	DocumentName string
	DocumentText string
	ChunkText    string
	ChunkIndex   int
	ChunkCount   int
	Mode         domain.AnalysisMode
	Source       domain.DocumentSource
}

type BuiltPrompt struct {
	System string
	User   string
}

type Builder interface {
	Build(input Input) BuiltPrompt
}

type DefaultBuilder struct{}

func NewDefaultBuilder() *DefaultBuilder {
	return &DefaultBuilder{}
}

func (b *DefaultBuilder) Build(input Input) BuiltPrompt {
	mode := input.Mode
	if mode == "" {
		mode = domain.AnalysisModeFullReview
	}

	system := strings.TrimSpace(`
You are a senior tech lead and solution architect reviewing a technical specification.

Your goals:
- find ambiguities;
- find missing requirements;
- find contradictions;
- find technical and production risks;
- propose concrete fixes.

Do not praise the document.
Do not rewrite the whole document.
Do not give generic advice.

Return only valid JSON with this exact shape:
{
  "findings": [
    {
      "role": "solution_architect",
      "category": "ambiguity",
      "severity": "WARNING",
      "problem": "short problem statement",
      "why_it_is_bad": "practical consequence",
      "how_to_fix": "concrete recommendation"
    }
  ]
}

Rules:
- role must be one of: backend_lead, frontend_lead, mobile_lead, devops_lead, qa_lead, security_lead, solution_architect;
- severity must be one of: INFO, WARNING, ERROR, CRITICAL;
- category should be one of: ambiguity, missing_requirement, contradiction, technical_risk, ux_problem, api_problem, frontend_risk, security_risk, devops_risk, scalability_risk;
- findings must be specific to the provided chunk;
- if there are no substantial issues, return {"findings":[]}.
`)

	user := fmt.Sprintf(
		"Analysis mode: %s\nDocument source: %s\nDocument name: %s\nChunk: %d of %d\n\nReview the following chunk from a technical specification.\n\n%s",
		mode,
		defaultString(string(input.Source), string(domain.DocumentSourceUpload)),
		defaultString(input.DocumentName, "unnamed_document"),
		input.ChunkIndex+1,
		maxInt(input.ChunkCount, 1),
		input.ChunkText,
	)

	return BuiltPrompt{
		System: system,
		User:   user,
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	return value
}

func maxInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}

	return value
}

var _ Builder = (*DefaultBuilder)(nil)
