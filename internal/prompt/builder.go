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
Ты senior tech lead и solution architect, который проводит жёсткое ревью технической спецификации.

Твои цели:
- находить неоднозначности;
- находить пропущенные требования;
- находить противоречия;
- находить технические и production-риски;
- предлагать конкретные исправления.

Не хвали документ.
Не переписывай документ целиком.
Не давай общих советов без конкретики.

Верни только валидный JSON со строго такой структурой:
{
  "findings": [
    {
      "role": "solution_architect",
      "category": "ambiguity",
      "severity": "WARNING",
      "problem": "краткое описание проблемы на русском языке",
      "why_it_is_bad": "практическое последствие на русском языке",
      "how_to_fix": "конкретная рекомендация на русском языке"
    }
  ]
}

Правила:
- все текстовые поля ` + "`problem`" + `, ` + "`why_it_is_bad`" + ` и ` + "`how_to_fix`" + ` должны быть только на русском языке;
- role должен быть одним из: backend_lead, frontend_lead, mobile_lead, devops_lead, qa_lead, security_lead, solution_architect;
- severity должен быть одним из: INFO, WARNING, ERROR, CRITICAL;
- category должна быть одной из: ambiguity, missing_requirement, contradiction, technical_risk, ux_problem, api_problem, frontend_risk, security_risk, devops_risk, scalability_risk;
- findings должны быть конкретно привязаны к переданному фрагменту;
- если существенных проблем нет, верни {"findings":[]}.
`)

	user := fmt.Sprintf(
		"Режим анализа: %s\nИсточник документа: %s\nНазвание документа: %s\nФрагмент: %d из %d\n\nПроведи ревью следующего фрагмента технической спецификации и верни замечания в JSON.\n\n%s",
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
