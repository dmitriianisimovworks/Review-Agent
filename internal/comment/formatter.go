package comment

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"technical-specification-review-agent/internal/domain"
)

type PublishMode string

const (
	PublishModeInline  PublishMode = "inline"
	PublishModeSummary PublishMode = "summary"
	PublishModeBoth    PublishMode = "both"
)

type Draft struct {
	Type          string
	Content       string
	AnchorLine    *int
	QuotedContent string
}

type Formatter interface {
	Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft
}

type DefaultFormatter struct{}

func NewDefaultFormatter() *DefaultFormatter {
	return &DefaultFormatter{}
}

const (
	maxInlineWarningComments = 3
	maxTotalInlineComments   = 7
	maxSummaryThemes         = 4
	maxThemeExamples         = 2
)

func (f *DefaultFormatter) Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft {
	switch mode {
	case PublishModeInline:
		return buildInlineDrafts(document, selectInlineFindings(analysis.Findings))
	case PublishModeSummary:
		return buildSummaryDrafts(document, analysis, selectInlineFindings(analysis.Findings))
	default:
		inlineFindings := selectInlineFindings(analysis.Findings)
		drafts := buildInlineDrafts(document, inlineFindings)
		return append(drafts, buildSummaryDrafts(document, analysis, inlineFindings)...)
	}
}

func buildInlineDrafts(document domain.Document, findings []domain.Finding) []Draft {
	drafts := make([]Draft, 0, len(findings))
	for _, finding := range findings {
		draft := Draft{
			Type:          "inline",
			Content:       formatFinding(finding),
			QuotedContent: quoteForComment(finding.SourceChunk),
		}
		if line := findAnchorLine(document.NormalizedContent, finding.SourceChunk); line != nil {
			draft.AnchorLine = line
		}
		drafts = append(drafts, draft)
	}
	return drafts
}

func buildSummaryDrafts(document domain.Document, analysis domain.Analysis, publishedInline []domain.Finding) []Draft {
	endLine := endAnchorLine(document.NormalizedContent)

	if len(analysis.Findings) == 0 {
		return []Draft{{
			Type:       "summary",
			Content:    "Итоговый комментарий\n\nСущественных замечаний по документу не обнаружено.",
			AnchorLine: &endLine,
		}}
	}

	findings := append([]domain.Finding(nil), analysis.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		left := severityRank(findings[i].Severity)
		right := severityRank(findings[j].Severity)
		if left == right {
			return findings[i].Category < findings[j].Category
		}
		return left > right
	})

	inlineKeys := make(map[string]struct{}, len(publishedInline))
	for _, finding := range publishedInline {
		inlineKeys[findingKey(finding)] = struct{}{}
	}

	remaining := make([]domain.Finding, 0, len(findings))
	for _, finding := range findings {
		if _, exists := inlineKeys[findingKey(finding)]; exists {
			continue
		}
		remaining = append(remaining, finding)
	}

	lines := []string{
		"Итоговый комментарий",
		"",
		compactSummary(analysis.Findings),
	}

	if len(remaining) > 0 {
		groups := groupFindingsByTheme(remaining)
		if len(groups) > 0 {
			lines = append(lines, "", "Дополнительно:")
			limit := min(len(groups), maxSummaryThemes)
			for i := 0; i < limit; i++ {
				lines = append(lines, formatThemeGroup(groups[i]))
			}
			if len(groups) > limit {
				lines = append(lines, fmt.Sprintf("- Ещё %d тематических блока есть в полном результате анализа.", len(groups)-limit))
			}
		}
	}

	return []Draft{{
		Type:          "summary",
		Content:       strings.Join(lines, "\n"),
		AnchorLine:    &endLine,
		QuotedContent: quoteLastMeaningfulLine(document.NormalizedContent),
	}}
}

func formatFinding(finding domain.Finding) string {
	return strings.Join([]string{
		fmt.Sprintf("%s %s", roleEmoji(finding.Role), roleLabel(finding.Role)),
		"",
		fmt.Sprintf("[%s]", finding.Severity),
		"",
		"Проблема:",
		strings.TrimSpace(finding.Problem),
		"",
		"Почему это плохо:",
		strings.TrimSpace(finding.WhyItIsBad),
		"",
		"Как исправить:",
		strings.TrimSpace(finding.HowToFix),
	}, "\n")
}

func findAnchorLine(documentText, sourceChunk string) *int {
	documentText = strings.TrimSpace(documentText)
	sourceChunk = strings.TrimSpace(sourceChunk)
	if documentText == "" || sourceChunk == "" {
		return nil
	}

	index := strings.Index(documentText, sourceChunk)
	if index == -1 {
		return nil
	}

	line := 1
	for _, char := range documentText[:index] {
		if char == '\n' {
			line++
		}
	}
	return &line
}

func quoteForComment(sourceChunk string) string {
	lines := strings.Split(strings.TrimSpace(sourceChunk), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if len(trimmed) > 220 {
				return trimmed[:220]
			}
			return trimmed
		}
	}
	return ""
}

func quoteLastMeaningfulLine(documentText string) string {
	lines := strings.Split(strings.TrimSpace(documentText), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 220 {
			return trimmed[:220]
		}
		return trimmed
	}
	return ""
}

func severityRank(severity domain.Severity) int {
	switch severity {
	case domain.SeverityCritical:
		return 4
	case domain.SeverityError:
		return 3
	case domain.SeverityWarning:
		return 2
	default:
		return 1
	}
}

type themeGroup struct {
	Title    string
	Findings []domain.Finding
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func endAnchorLine(documentText string) int {
	text := strings.TrimSpace(documentText)
	if text == "" {
		return 1
	}

	line := 1
	for _, char := range text {
		if char == '\n' {
			line++
		}
	}
	return line
}

func selectInlineFindings(findings []domain.Finding) []domain.Finding {
	scored := append([]domain.Finding(nil), findings...)
	sort.SliceStable(scored, func(i, j int) bool {
		left := severityRank(scored[i].Severity)
		right := severityRank(scored[j].Severity)
		if left == right {
			return scored[i].Category < scored[j].Category
		}
		return left > right
	})

	selected := make([]domain.Finding, 0, min(len(scored), maxTotalInlineComments))
	warningCount := 0
	for _, finding := range scored {
		switch finding.Severity {
		case domain.SeverityCritical, domain.SeverityError:
			selected = append(selected, finding)
		case domain.SeverityWarning:
			if warningCount >= maxInlineWarningComments {
				continue
			}
			selected = append(selected, finding)
			warningCount++
		default:
			continue
		}

		if len(selected) >= maxTotalInlineComments {
			break
		}
	}

	return selected
}

func compactSummary(findings []domain.Finding) string {
	if len(findings) == 0 {
		return "Существенных замечаний по документу не обнаружено."
	}

	critical := 0
	errorsCount := 0
	warnings := 0
	for _, finding := range findings {
		switch finding.Severity {
		case domain.SeverityCritical:
			critical++
		case domain.SeverityError:
			errorsCount++
		case domain.SeverityWarning:
			warnings++
		}
	}

	parts := []string{fmt.Sprintf("Найдено %d замечаний", len(findings))}
	if critical > 0 {
		parts = append(parts, fmt.Sprintf("%d CRITICAL", critical))
	}
	if errorsCount > 0 {
		parts = append(parts, fmt.Sprintf("%d ERROR", errorsCount))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d WARNING", warnings))
	}

	roleParts := summarizeRoles(findings)
	if len(roleParts) > 0 {
		return strings.Join(parts, ": ") + ". Роли с замечаниями: " + strings.Join(roleParts, ", ") + "."
	}

	return strings.Join(parts, ": ")
}

func groupFindingsByTheme(findings []domain.Finding) []themeGroup {
	buckets := map[string][]domain.Finding{}
	for _, finding := range findings {
		title := themeTitle(finding)
		buckets[title] = append(buckets[title], finding)
	}

	groups := make([]themeGroup, 0, len(buckets))
	for title, bucket := range buckets {
		sort.SliceStable(bucket, func(i, j int) bool {
			left := severityRank(bucket[i].Severity)
			right := severityRank(bucket[j].Severity)
			if left == right {
				return bucket[i].Problem < bucket[j].Problem
			}
			return left > right
		})
		groups = append(groups, themeGroup{
			Title:    title,
			Findings: bucket,
		})
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Findings) == len(groups[j].Findings) {
			left := severityRank(groups[i].Findings[0].Severity)
			right := severityRank(groups[j].Findings[0].Severity)
			if left == right {
				return groups[i].Title < groups[j].Title
			}
			return left > right
		}
		return len(groups[i].Findings) > len(groups[j].Findings)
	})

	return groups
}

func formatThemeGroup(group themeGroup) string {
	examples := make([]string, 0, min(len(group.Findings), maxThemeExamples))
	limit := min(len(group.Findings), maxThemeExamples)
	for i := 0; i < limit; i++ {
		examples = append(examples, shortProblem(group.Findings[i].Problem))
	}

	line := fmt.Sprintf("- %s: %s.", group.Title, strings.Join(examples, "; "))
	if len(group.Findings) > limit {
		line += fmt.Sprintf(" Ещё %d замеч. в этой теме.", len(group.Findings)-limit)
	}
	return line
}

func shortProblem(problem string) string {
	problem = strings.TrimSpace(problem)
	if problem == "" {
		return "нужна дополнительная детализация"
	}
	if len(problem) > 140 {
		return strings.TrimSpace(problem[:140]) + "..."
	}
	return problem
}

func themeTitle(finding domain.Finding) string {
	switch finding.Category {
	case "technical_risk", "scalability_risk", "devops_risk":
		return "Технические и интеграционные риски"
	case "security_risk":
		return "Безопасность и доступы"
	case "api_problem":
		return "API и интеграционные контракты"
	case "ux_problem", "frontend_risk":
		return "UX и поведение интерфейса"
	case "ambiguity":
		return "Неоднозначные требования"
	case "contradiction":
		return "Противоречия"
	default:
		problem := strings.ToLower(finding.Problem)
		switch {
		case strings.Contains(problem, "роль"), strings.Contains(problem, "доступ"), strings.Contains(problem, "прав"):
			return "Роли и права доступа"
		case strings.Contains(problem, "sla"), strings.Contains(problem, "slo"), strings.Contains(problem, "производительност"):
			return "SLA и нефункциональные требования"
		case strings.Contains(problem, "коммент"), strings.Contains(problem, "audit"), strings.Contains(problem, "истори"):
			return "Audit trail и история изменений"
		case strings.Contains(problem, "эскалац"), strings.Contains(problem, "статус"), strings.Contains(problem, "жизненн"):
			return "Жизненный цикл кейса"
		case strings.Contains(problem, "экспорт"), strings.Contains(problem, "отч"), strings.Contains(problem, "анонимизац"):
			return "Отчёты и выгрузки"
		case strings.Contains(problem, "ai"), strings.Contains(problem, "рекомендац"):
			return "AI-рекомендации и ответственность"
		case strings.Contains(problem, "вложен"), strings.Contains(problem, "mime"), strings.Contains(problem, "файл"):
			return "Вложения и файловая безопасность"
		case strings.Contains(problem, "интеграц"), strings.Contains(problem, "retry"), strings.Contains(problem, "идемпотент"), strings.Contains(problem, "асинхрон"):
			return "Технические и интеграционные риски"
		default:
			return "Пропущенные требования"
		}
	}
}

func findingKey(finding domain.Finding) string {
	return strings.Join([]string{
		string(finding.Severity),
		finding.Category,
		strings.TrimSpace(finding.Problem),
	}, "|")
}

func summarizeRoles(findings []domain.Finding) []string {
	counts := map[string]int{}
	order := make([]string, 0)
	for _, finding := range findings {
		if _, exists := counts[finding.Role]; !exists {
			order = append(order, finding.Role)
		}
		counts[finding.Role]++
	}

	result := make([]string, 0, len(order))
	for _, role := range order {
		result = append(result, fmt.Sprintf("%s %s (%d)", roleEmoji(role), roleLabel(role), counts[role]))
	}
	return result
}

func roleLabel(role string) string {
	switch role {
	case string(domain.ReviewerRoleTechLead):
		return "Tech Lead"
	case string(domain.ReviewerRoleSolutionArchitect):
		return "Solution Architect"
	case string(domain.ReviewerRoleSeniorBackendEngineer):
		return "Senior Backend Engineer"
	case string(domain.ReviewerRoleSeniorFrontendEngineer):
		return "Senior Frontend Engineer"
	case string(domain.ReviewerRoleDevOpsReviewer):
		return "DevOps Reviewer"
	case string(domain.ReviewerRoleQAReviewer):
		return "QA Reviewer"
	default:
		return "Reviewer"
	}
}

func roleEmoji(role string) string {
	switch role {
	case string(domain.ReviewerRoleTechLead):
		return "🧭"
	case string(domain.ReviewerRoleSolutionArchitect):
		return "🏗"
	case string(domain.ReviewerRoleSeniorBackendEngineer):
		return "🧱"
	case string(domain.ReviewerRoleSeniorFrontendEngineer):
		return "🖥"
	case string(domain.ReviewerRoleDevOpsReviewer):
		return "⚙️"
	case string(domain.ReviewerRoleQAReviewer):
		return "🧪"
	default:
		return "📌"
	}
}

func MarshalAnchor(line int) string {
	anchor := map[string]any{
		"region": map[string]any{
			"kind": "drive#commentRegion",
			"line": line,
			"rev":  "head",
		},
	}
	data, _ := json.Marshal(anchor)
	return string(data)
}
