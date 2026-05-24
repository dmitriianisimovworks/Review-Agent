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

type CrossSectionInput struct {
	DocumentName string
	Source       domain.DocumentSource
	Mode         domain.AnalysisMode
	SectionA     domain.DocumentSection
	SectionB     domain.DocumentSection
	Memory       domain.ReviewMemory
}

type BuiltPrompt struct {
	System string
	User   string
}

type Builder interface {
	Build(input Input) BuiltPrompt
	BuildCrossSectionContradiction(input CrossSectionInput) BuiltPrompt
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

func (b *DefaultBuilder) BuildCrossSectionContradiction(input CrossSectionInput) BuiltPrompt {
	system := strings.TrimSpace(`
Ты работаешь как Solution Architect и ищешь только противоречия между двумя разделами технической спецификации.

Твоя задача:
- сравнить два раздела между собой;
- найти conflicting requirements, inconsistent logic, conflicting permissions, contradictory lifecycle rules;
- вернуть только реальные противоречия, а не просто разные темы;
- если противоречия нет, вернуть {"findings":[]}.

Не хвали документ.
Не переписывай разделы.
Не давай общие замечания.
Не возвращай замечания, которые не являются межсекционным конфликтом.
Верни максимум 2 сильных противоречия.

Верни только валидный JSON со строго такой структурой:
{
  "findings": [
    {
      "role": "solution_architect",
      "category": "contradiction",
      "severity": "ERROR",
      "problem": "краткое описание противоречия на русском языке с упоминанием обоих разделов",
      "why_it_is_bad": "практическое последствие на русском языке",
      "how_to_fix": "конкретный способ устранить конфликт на русском языке"
    }
  ]
}
`)

	user := fmt.Sprintf(
		"Режим анализа: %s\nИсточник документа: %s\nНазвание документа: %s\n%s\nСравни два раздела и найди только межсекционные противоречия.\n\nРаздел A: %s\n%s\n\nРаздел B: %s\n%s",
		defaultString(string(input.Mode), string(domain.AnalysisModeFullReview)),
		defaultString(string(input.Source), string(domain.DocumentSourceUpload)),
		defaultString(input.DocumentName, "unnamed_document"),
		formatCrossSectionMemory(input.Memory),
		defaultString(input.SectionA.Title, "Section A"),
		input.SectionA.Content,
		defaultString(input.SectionB.Title, "Section B"),
		input.SectionB.Content,
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

	if modules := limitStrings(memory.Modules, 2); len(modules) > 0 {
		parts = append(parts, "Известные модули и разделы документа:")
		for idx, module := range modules {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, module))
		}
	}

	if roles := limitStrings(memory.UserRoles, 2); len(roles) > 0 {
		parts = append(parts, "Известные пользовательские роли:")
		for idx, roleValue := range roles {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, roleValue))
		}
	}

	if entities := limitStrings(memory.Entities, 2); len(entities) > 0 {
		parts = append(parts, "Известные сущности и термины:")
		for idx, entity := range entities {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, entity))
		}
	}

	if glossary := limitStrings(memory.Glossary, 2); len(glossary) > 0 {
		parts = append(parts, "Глоссарий и важные понятия из прошлых ревью:")
		for idx, term := range glossary {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, term))
		}
	}

	if decisions := limitStrings(memory.ArchitectureDecisions, 1); len(decisions) > 0 {
		parts = append(parts, "Архитектурные решения и договорённости:")
		for idx, decision := range decisions {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, decision))
		}
	}

	if sections := limitSections(memory.Sections, 2); len(sections) > 0 {
		parts = append(parts, "Контекст по разделам документа:")
		for idx, section := range sections {
			line := fmt.Sprintf("%d. %s", idx+1, section.SectionTitle)
			if len(section.KnownProblems) > 0 {
				line += fmt.Sprintf(" | known: %s", strings.Join(limitStrings(section.KnownProblems, 2), "; "))
			}
			if len(section.ResolvedProblems) > 0 {
				line += fmt.Sprintf(" | resolved: %s", strings.Join(limitStrings(section.ResolvedProblems, 1), "; "))
			}
			parts = append(parts, line)
		}
	}

	if resolved := limitResolvedFindings(memory.ResolvedFindings, 1); len(resolved) > 0 {
		parts = append(parts, "Ранее закрытые или исчезнувшие проблемы:")
		for idx, finding := range resolved {
			line := fmt.Sprintf("%d. %s", idx+1, finding.Problem)
			if strings.TrimSpace(finding.SectionTitle) != "" {
				line += fmt.Sprintf(" [%s]", finding.SectionTitle)
			}
			parts = append(parts, line)
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

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitSections(values []domain.MemorySection, limit int) []domain.MemorySection {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitResolvedFindings(values []domain.FindingRef, limit int) []domain.FindingRef {
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

func formatCrossSectionMemory(memory domain.ReviewMemory) string {
	if !memory.HasContext() {
		return ""
	}

	parts := []string{
		fmt.Sprintf("Ключ памяти документа: %s", memory.ReviewKey),
	}
	if sections := limitSections(memory.Sections, 3); len(sections) > 0 {
		parts = append(parts, "Контекст прошлых разделов:")
		for idx, section := range sections {
			line := fmt.Sprintf("%d. %s", idx+1, section.SectionTitle)
			if len(section.KnownProblems) > 0 {
				line += fmt.Sprintf(" | known: %s", strings.Join(limitStrings(section.KnownProblems, 2), "; "))
			}
			parts = append(parts, line)
		}
	}
	if resolved := limitResolvedFindings(memory.ResolvedFindings, 2); len(resolved) > 0 {
		parts = append(parts, "Ранее исчезнувшие конфликты/проблемы:")
		for idx, finding := range resolved {
			parts = append(parts, fmt.Sprintf("%d. %s", idx+1, finding.Problem))
		}
	}
	return strings.Join(parts, "\n") + "\n"
}

var _ Builder = (*DefaultBuilder)(nil)
