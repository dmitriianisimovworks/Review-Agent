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
	SectionTitle string
	SectionLevel int
	Mode         domain.AnalysisMode
	Source       domain.DocumentSource
	Role         domain.ReviewerRole
	Memory       domain.ReviewMemory
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
Если передан контекст прошлых ревью, учитывай его как память документа:
- помни уже найденные замечания, риски и архитектурные договорённости;
- не повторяй слово в слово уже известные проблемы, если в них нет новой грани, новой причины или роста severity;
- в режиме incremental_review фокусируйся на новых проблемах, новых противоречиях и изменениях относительно предыдущих замечаний;
- если старая проблема остаётся актуальной, упоминай её только когда появился новый слой риска или новое последствие.

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
- role должен быть одним из: tech_lead, solution_architect, senior_backend_engineer, senior_frontend_engineer, mobile_lead, devops_reviewer, qa_reviewer, security_lead;
- severity должен быть одним из: INFO, WARNING, ERROR, CRITICAL;
- category должна быть одной из: ambiguity, missing_requirement, contradiction, technical_risk, ux_problem, api_problem, frontend_risk, security_risk, devops_risk, scalability_risk;
- findings должны быть конкретно привязаны к переданному фрагменту;
- findings желательно должно быть не меньше 2, если в рамках роли есть хотя бы две независимые значимые проблемы;
- findings должно быть не больше 5;
- если существенных проблем нет, верни {"findings":[]}.
`, roleDisplayName(role), roleInstructions(role), role))

	user := fmt.Sprintf(
		"Режим анализа: %s\nИсточник документа: %s\nНазвание документа: %s\nРоль ревью: %s\nФрагмент: %d из %d\n%s%s\nПроведи ревью следующего фрагмента технической спецификации и верни замечания в JSON.\n\n%s",
		mode,
		defaultString(string(input.Source), string(domain.DocumentSourceUpload)),
		defaultString(input.DocumentName, "unnamed_document"),
		roleDisplayName(role),
		input.ChunkIndex+1,
		maxInt(input.ChunkCount, 1),
		formatSectionContext(input.SectionTitle, input.SectionLevel),
		formatMemorySection(input.Memory, role),
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
	case domain.ReviewerRoleMobileLead:
		return "Mobile Lead"
	case domain.ReviewerRoleDevOpsReviewer:
		return "DevOps Reviewer"
	case domain.ReviewerRoleQAReviewer:
		return "QA Reviewer"
	case domain.ReviewerRoleSecurityLead:
		return "Security Lead"
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
	case domain.ReviewerRoleMobileLead:
		return strings.TrimSpace(`
- смотри на mobile-specific сценарии: offline mode, unstable network, battery-sensitive flows и background behavior;
- ищи проблемы синхронизации состояния, загрузки больших списков, пагинации и кеширования на мобильных клиентах;
- отмечай риски слабой адаптации UX под мобильные ограничения и недостающие mobile edge cases.`)
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
	case domain.ReviewerRoleSecurityLead:
		return strings.TrimSpace(`
- смотри на auth, permissions, data exposure, file uploads и security boundaries;
- ищи insecure flows, недостаточную валидацию, утечки секретов и missing security requirements;
- отмечай production-риски вокруг доступа, инъекций, хранения чувствительных данных и abuse scenarios.`)
	default:
		return ""
	}
}

func formatMemorySection(memory domain.ReviewMemory, role domain.ReviewerRole) string {
	if !memory.HasContext() {
		return ""
	}

	parts := []string{
		fmt.Sprintf("Ключ памяти документа: %s", memory.ReviewKey),
		fmt.Sprintf("Предыдущих прогонов: %d", memory.PriorRunCount),
	}

	if summaries := limitStrings(memory.PriorSummaries, 1); len(summaries) > 0 {
		parts = append(parts, "Краткие summary прошлых ревью:")
		for idx, summary := range summaries {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, summary))
		}
	}

	if findings := roleRelevantFindings(memory.KnownFindings, role, 2); len(findings) > 0 {
		parts = append(parts, "Уже обсуждённые замечания и риски:")
		for idx, finding := range findings {
			parts = append(parts, fmt.Sprintf("%d. [%s][%s][%s] %s", idx+1, roleDisplayName(domain.ReviewerRole(finding.Role)), finding.Severity, finding.Category, finding.Problem))
		}
	}

	if notes := limitStrings(memory.ArchitecturalNotes, 1); len(notes) > 0 {
		parts = append(parts, "Архитектурные заметки из прошлых ревью:")
		for idx, note := range notes {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, note))
		}
	}

	if decisions := limitArchitectureDecisions(memory.ArchitectureDecisions, 2); len(decisions) > 0 {
		parts = append(parts, "Архитектурные решения из прошлых ревью:")
		for idx, decision := range decisions {
			line := fmt.Sprintf("%d. %s", idx+1, decision.Decision)
			if decision.Context != "" {
				line += fmt.Sprintf(" (контекст: %s)", decision.Context)
			}
			parts = append(parts, line)
		}
	}

	if modules := limitStrings(memory.Modules, 2); len(modules) > 0 {
		parts = append(parts, "Известные модули/разделы:")
		for idx, module := range modules {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, module))
		}
	}

	if roles := limitStrings(memory.UserRoles, 2); len(roles) > 0 {
		parts = append(parts, "Известные роли пользователей:")
		for idx, roleName := range roles {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, roleName))
		}
	}

	if entities := limitStrings(memory.Entities, 2); len(entities) > 0 {
		parts = append(parts, "Известные сущности/термины:")
		for idx, entity := range entities {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, entity))
		}
	}

	parts = append(parts, "Учитывай этот контекст как память документа и не повторяй дословно уже известные замечания без новой ценности.")
	return strings.Join(parts, "\n") + "\n"
}

func roleRelevantFindings(findings []domain.Finding, role domain.ReviewerRole, limit int) []domain.Finding {
	if limit <= 0 {
		return nil
	}

	selected := make([]domain.Finding, 0, limit)
	for _, finding := range findings {
		if strings.TrimSpace(finding.Role) == string(role) {
			selected = append(selected, finding)
			if len(selected) >= limit {
				return selected
			}
		}
	}

	for _, finding := range findings {
		if strings.TrimSpace(finding.Role) != string(role) {
			selected = append(selected, finding)
			if len(selected) >= limit {
				return selected
			}
		}
	}

	return selected
}

func limitArchitectureDecisions(values []domain.ArchitectureDecision, limit int) []domain.ArchitectureDecision {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func formatSectionContext(title string, level int) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if level > 0 {
		return fmt.Sprintf("Раздел документа: %s (heading level %d)\n", title, level)
	}
	return fmt.Sprintf("Раздел документа: %s\n", title)
}

var _ Builder = (*DefaultBuilder)(nil)
