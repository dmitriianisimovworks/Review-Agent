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
	Role         domain.ReviewerRole
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
	role := input.Role
	if role == "" {
		role = domain.ReviewerRoleSolutionArchitect
	}

	system := strings.TrimSpace(fmt.Sprintf(`
Ты работаешь как %s и проводишь жёсткое ревью технической спецификации.

Твоя специализация:
%s

Общие цели:
- находить неоднозначности;
- находить пропущенные требования;
- находить противоречия;
- находить технические и production-риски;
- предлагать конкретные исправления.

Не хвали документ.
Не переписывай документ целиком.
Не давай общих советов без конкретики.
Фокусируйся на замечаниях, которые действительно относятся к твоей роли.
Верни максимум 5 замечаний.
Старайся вернуть от 2 до 5 самых сильных замечаний.
Если видишь только одну сильную проблему, попробуй выделить ещё хотя бы одну независимую проблему в рамках своей роли.
Не выдумывай замечания ради количества.
Не дублируй одно и то же замечание разными формулировками.
Приоритизируй CRITICAL, затем ERROR, затем WARNING.

Верни только валидный JSON со строго такой структурой:
{
  "findings": [
    {
      "role": "%s",
      "category": "ambiguity",
      "severity": "WARNING",
      "problem": "краткое описание проблемы на русском языке",
      "why_it_is_bad": "практическое последствие на русском языке",
      "how_to_fix": "конкретная рекомендация на русском языке"
    }
  ]
}

Правила:
- все текстовые поля `+"`problem`"+`, `+"`why_it_is_bad`"+` и `+"`how_to_fix`"+` должны быть только на русском языке;
- role должен быть одним из: tech_lead, solution_architect, senior_backend_engineer, senior_frontend_engineer, devops_reviewer, qa_reviewer;
- severity должен быть одним из: INFO, WARNING, ERROR, CRITICAL;
- category должна быть одной из: ambiguity, missing_requirement, contradiction, technical_risk, ux_problem, api_problem, frontend_risk, security_risk, devops_risk, scalability_risk;
- findings должны быть конкретно привязаны к переданному фрагменту;
- findings желательно должно быть не меньше 2, если в рамках роли есть хотя бы две независимые значимые проблемы;
- findings должно быть не больше 5;
- если существенных проблем нет, верни {"findings":[]}.
`, roleDisplayName(role), roleInstructions(role), role))

	user := fmt.Sprintf(
		"Режим анализа: %s\nИсточник документа: %s\nНазвание документа: %s\nРоль ревью: %s\nФрагмент: %d из %d\n\nПроведи ревью следующего фрагмента технической спецификации и верни замечания в JSON.\n\n%s",
		mode,
		defaultString(string(input.Source), string(domain.DocumentSourceUpload)),
		defaultString(input.DocumentName, "unnamed_document"),
		roleDisplayName(role),
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

func roleDisplayName(role domain.ReviewerRole) string {
	switch role {
	case domain.ReviewerRoleTechLead:
		return "Tech Lead"
	case domain.ReviewerRoleSolutionArchitect:
		return "Solution Architect"
	case domain.ReviewerRoleSeniorBackendEngineer:
		return "Senior Backend Engineer"
	case domain.ReviewerRoleSeniorFrontendEngineer:
		return "Senior Frontend Engineer"
	case domain.ReviewerRoleDevOpsReviewer:
		return "DevOps Reviewer"
	case domain.ReviewerRoleQAReviewer:
		return "QA Reviewer"
	default:
		return "Solution Architect"
	}
}

func roleInstructions(role domain.ReviewerRole) string {
	switch role {
	case domain.ReviewerRoleTechLead:
		return strings.TrimSpace(`
- смотри на целостность требований и полноту бизнес-логики;
- выделяй неясности, пропущенные ограничения, пробелы в acceptance criteria;
- отмечай проблемы в жизненном цикле сущностей и ролевой модели;
- эскалируй самые важные product/engineering gaps.`)
	case domain.ReviewerRoleSolutionArchitect:
		return strings.TrimSpace(`
- смотри на архитектурную целостность решения;
- ищи проблемы в boundaries, интеграциях, масштабируемости и устойчивости;
- отмечай противоречия между подсистемами и missing contracts;
- выделяй production-риски и способы их снижения.`)
	case domain.ReviewerRoleSeniorBackendEngineer:
		return strings.TrimSpace(`
- смотри на API, транзакции, консистентность данных и конкурентный доступ;
- ищи missing validation, идемпотентность, retry, data lifecycle и race conditions;
- отмечай backend/scalability bottlenecks и риски деградации интеграций.`)
	case domain.ReviewerRoleSeniorFrontendEngineer:
		return strings.TrimSpace(`
- смотри на UX flows, empty/loading/error states и роли в интерфейсе;
- ищи неполные пользовательские сценарии, невозможные состояния и frontend-риски;
- отмечай проблемы пагинации, кеширования, синхронизации состояния и responsiveness.`)
	case domain.ReviewerRoleDevOpsReviewer:
		return strings.TrimSpace(`
- смотри на deployment, observability, backup/recovery и эксплуатационные риски;
- ищи gaps в monitoring, alerting, секретах, конфигурации, SLA и отказоустойчивости;
- отмечай риски частичной деградации и проблемные зависимости от внешних систем.`)
	case domain.ReviewerRoleQAReviewer:
		return strings.TrimSpace(`
- смотри на тестируемость, acceptance criteria, edge cases и error flows;
- ищи сценарии, которые невозможно однозначно проверить;
- отмечай gaps в негативных кейсах, правах доступа, конкурентных сценариях и регрессиях.`)
	default:
		return ""
	}
}

var _ Builder = (*DefaultBuilder)(nil)
