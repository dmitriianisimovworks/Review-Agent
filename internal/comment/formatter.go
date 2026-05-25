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
	maxRoleCommentFindings   = 2
	maxRoleDrafts            = 5
)

func (f *DefaultFormatter) Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft {
	switch mode {
	case PublishModeInline:
		return buildRoleDrafts(document, analysis.Findings)
	case PublishModeSummary:
		return buildSummaryDrafts(document, analysis)
	default:
		drafts := buildRoleDrafts(document, analysis.Findings)
		return append(drafts, buildSummaryDrafts(document, analysis)...)
	}
}

func buildRoleDrafts(document domain.Document, findings []domain.Finding) []Draft {
	grouped := groupFindingsByRole(findings)
	order := domain.DefaultReviewerRoles()

	type roleDraft struct {
		draft Draft
		score int
	}

	collected := make([]roleDraft, 0, len(grouped))
	for _, role := range order {
		roleFindings := grouped[string(role)]
		if len(roleFindings) == 0 {
			continue
		}

		sort.SliceStable(roleFindings, func(i, j int) bool {
			left := severityRank(roleFindings[i].Severity)
			right := severityRank(roleFindings[j].Severity)
			if left == right {
				if roleFindings[i].Category == roleFindings[j].Category {
					return roleFindings[i].Problem < roleFindings[j].Problem
				}
				return roleFindings[i].Category < roleFindings[j].Category
			}
			return left > right
		})

		if len(roleFindings) > maxRoleCommentFindings {
			roleFindings = roleFindings[:maxRoleCommentFindings]
		}

		topFinding := roleFindings[0]
		draft := Draft{
			Type:          "inline",
			Content:       formatRoleComment(string(role), roleFindings),
			QuotedContent: quoteForComment(topFinding.SourceChunk),
		}
		if line := findAnchorLine(document.NormalizedContent, topFinding.SourceChunk); line != nil {
			draft.AnchorLine = line
		}
		collected = append(collected, roleDraft{draft: draft, score: roleSeverityScore(roleFindings)})
	}

	sort.SliceStable(collected, func(i, j int) bool {
		return collected[i].score > collected[j].score
	})
	if len(collected) > maxRoleDrafts {
		collected = collected[:maxRoleDrafts]
	}

	drafts := make([]Draft, 0, len(collected))
	for _, item := range collected {
		drafts = append(drafts, item.draft)
	}
	return drafts
}

func buildSummaryDrafts(document domain.Document, analysis domain.Analysis) []Draft {
	endLine := endAnchorLine(document.NormalizedContent)

	if len(analysis.Findings) == 0 {
		return []Draft{{
			Type:       "summary",
			Content:    "Итоговый комментарий\n\nСущественных замечаний по документу не обнаружено.",
			AnchorLine: &endLine,
		}}
	}

	lines := []string{
		"Итоговый комментарий",
		"",
		compactSummary(analysis.Findings),
	}
	if roleSummary := summarizeRoleCoverage(analysis.Findings); len(roleSummary) > 0 {
		lines = append(lines, roleSummary...)
	}

	if len(analysis.Findings) > 0 {
		groups := groupFindingsByTheme(analysis.Findings)
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

func formatRoleComment(role string, findings []domain.Finding) string {
	lines := []string{
		fmt.Sprintf("%s %s", roleEmoji(role), roleLabel(role)),
		"",
		"Ключевые замечания:",
	}

	for idx, finding := range findings {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", idx+1, finding.Severity, strings.TrimSpace(finding.Problem)))
		if section := strings.TrimSpace(finding.SectionTitle); section != "" {
			lines = append(lines, "Связано с разделом:", section)
		}
		if fragment := quoteForComment(finding.SourceChunk); fragment != "" {
			lines = append(lines, "Фрагмент:", fragment)
		}
		lines = append(lines,
			"Почему это плохо:",
			strings.TrimSpace(finding.WhyItIsBad),
			"Как исправить:",
			strings.TrimSpace(finding.HowToFix),
			"",
		)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
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
			return safeTruncate(trimmed, 220)
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
		return safeTruncate(trimmed, 220)
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

func groupFindingsByRole(findings []domain.Finding) map[string][]domain.Finding {
	grouped := make(map[string][]domain.Finding)
	for _, finding := range findings {
		role := strings.TrimSpace(finding.Role)
		if role == "" {
			role = string(domain.ReviewerRoleSolutionArchitect)
		}
		grouped[role] = append(grouped[role], finding)
	}
	return grouped
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

	groups := groupFindingsByTheme(findings)
	criticalThemes := 0
	errorThemes := 0
	for _, group := range groups {
		switch highestSeverity(group.Findings) {
		case domain.SeverityCritical:
			criticalThemes++
		case domain.SeverityError:
			errorThemes++
		}
	}

	parts := []string{fmt.Sprintf("Ключевых тем: %d", len(groups))}
	if criticalThemes > 0 {
		parts = append(parts, fmt.Sprintf("Критичных: %d", criticalThemes))
	}
	if errorThemes > 0 {
		parts = append(parts, fmt.Sprintf("Важных: %d", errorThemes))
	}

	return strings.Join(parts, ". ") + "."
}

func summarizeRoleCoverage(findings []domain.Finding) []string {
	if len(findings) == 0 {
		return nil
	}

	activeSet := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		role := strings.TrimSpace(finding.Role)
		if role == "" {
			continue
		}
		activeSet[role] = struct{}{}
	}

	active := make([]string, 0, len(activeSet))
	quiet := make([]string, 0)
	for _, role := range domain.DefaultReviewerRoles() {
		label := roleLabel(string(role))
		if _, ok := activeSet[string(role)]; ok {
			active = append(active, label)
			continue
		}
		quiet = append(quiet, label)
	}

	lines := make([]string, 0, 2)
	if len(active) > 0 {
		lines = append(lines, "Активные роли: "+strings.Join(active, ", ")+".")
	}
	if len(quiet) > 0 {
		lines = append(lines, "Без замечаний: "+strings.Join(quiet, ", ")+".")
	}
	return lines
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
	return safeTruncate(problem, 140)
}

func themeTitle(finding domain.Finding) string {
	switch finding.Category {
	case "technical_risk", "scalability_risk", "devops_risk":
		problem := strings.ToLower(finding.Problem)
		switch {
		case strings.Contains(problem, "refund"), strings.Contains(problem, "возврат"), strings.Contains(problem, "invoice"), strings.Contains(problem, "платеж"):
			return "Refund и финансовые операции"
		case strings.Contains(problem, "конкур"), strings.Contains(problem, "одноврем"), strings.Contains(problem, "блокиров"), strings.Contains(problem, "race condition"), strings.Contains(problem, "atomic"):
			return "Конкурентный доступ и блокировки"
		case strings.Contains(problem, "audit"), strings.Contains(problem, "истори"), strings.Contains(problem, "лог"):
			return "Аудит и история изменений"
		default:
			return "Технические и интеграционные риски"
		}
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
		case strings.Contains(problem, "refund"), strings.Contains(problem, "возврат"), strings.Contains(problem, "invoice"), strings.Contains(problem, "платеж"):
			return "Refund и финансовые операции"
		case strings.Contains(problem, "конкур"), strings.Contains(problem, "одноврем"), strings.Contains(problem, "блокиров"), strings.Contains(problem, "race condition"), strings.Contains(problem, "atomic"):
			return "Конкурентный доступ и блокировки"
		case strings.Contains(problem, "роль"), strings.Contains(problem, "доступ"), strings.Contains(problem, "прав"):
			return "Роли и права доступа"
		case strings.Contains(problem, "sla"), strings.Contains(problem, "slo"), strings.Contains(problem, "производительност"):
			return "SLA и нефункциональные требования"
		case strings.Contains(problem, "коммент"), strings.Contains(problem, "audit"), strings.Contains(problem, "истори"):
			return "Аудит и история изменений"
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
	case string(domain.ReviewerRoleMobileLead):
		return "Mobile Lead"
	case string(domain.ReviewerRoleDevOpsReviewer):
		return "DevOps Reviewer"
	case string(domain.ReviewerRoleQAReviewer):
		return "QA Reviewer"
	case string(domain.ReviewerRoleSecurityLead):
		return "Security Lead"
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
	case string(domain.ReviewerRoleMobileLead):
		return "📱"
	case string(domain.ReviewerRoleDevOpsReviewer):
		return "⚙️"
	case string(domain.ReviewerRoleQAReviewer):
		return "🧪"
	case string(domain.ReviewerRoleSecurityLead):
		return "🔐"
	default:
		return "📌"
	}
}

func safeTruncate(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}

	cut := runes[:limit]
	text := strings.TrimSpace(string(cut))
	if lastSpace := strings.LastIndex(text, " "); lastSpace > 0 {
		text = strings.TrimSpace(text[:lastSpace])
	}

	return text + "..."
}

func roleSeverityScore(findings []domain.Finding) int {
	score := 0
	for _, finding := range findings {
		score += severityRank(finding.Severity) * 10
	}
	return score
}

func highestSeverity(findings []domain.Finding) domain.Severity {
	top := domain.SeverityInfo
	for _, finding := range findings {
		if severityRank(finding.Severity) > severityRank(top) {
			top = finding.Severity
		}
	}
	return top
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
