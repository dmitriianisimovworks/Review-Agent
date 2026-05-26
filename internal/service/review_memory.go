package service

import (
	"regexp"
	"sort"
	"strings"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/reviewshape"
)

const (
	reviewMemoryRunLimit           = 5
	reviewMemorySummaryLimit       = 2
	reviewMemoryFindingsLimit      = 8
	reviewMemoryArchitecturesLimit = 4
	reviewMemoryDecisionLimit      = 4
	reviewMemoryRoleLimit          = 2
	reviewMemoryEntityLimit        = 6
	reviewMemoryGlossaryLimit      = 6
	reviewMemoryModuleLimit        = 5
	reviewMemoryUserRoleLimit      = 5
)

var glossaryTokenPattern = regexp.MustCompile(`(?i)\b[A-Za-z][A-Za-z0-9_-]{2,}\b`)

func buildReviewMemory(reviewKey string, analyses []domain.Analysis) domain.ReviewMemory {
	memory := domain.ReviewMemory{
		ReviewKey:     reviewKey,
		PriorRunCount: len(analyses),
	}
	if reviewKey == "" || len(analyses) == 0 {
		return memory
	}

	summaries := make([]string, 0, reviewMemorySummaryLimit)
	seenSummaries := map[string]struct{}{}
	knownFindings := make([]domain.Finding, 0)
	architectureNotes := make([]string, 0, reviewMemoryArchitecturesLimit)
	seenArchitecture := map[string]struct{}{}

	for _, analysis := range analyses {
		summary := strings.TrimSpace(analysis.Summary)
		if summary != "" {
			key := strings.ToLower(summary)
			if _, exists := seenSummaries[key]; !exists {
				seenSummaries[key] = struct{}{}
				summaries = append(summaries, summary)
			}
		}

		for _, finding := range analysis.Findings {
			knownFindings = append(knownFindings, finding)

			if isArchitecturalMemoryCandidate(finding) && len(architectureNotes) < reviewMemoryArchitecturesLimit {
				note := strings.TrimSpace(finding.HowToFix)
				if note == "" {
					note = strings.TrimSpace(finding.Problem)
				}
				key := strings.ToLower(note)
				if note != "" {
					if _, exists := seenArchitecture[key]; !exists {
						seenArchitecture[key] = struct{}{}
						architectureNotes = append(architectureNotes, note)
					}
				}
			}
		}
	}

	if len(summaries) > reviewMemorySummaryLimit {
		summaries = summaries[:reviewMemorySummaryLimit]
	}

	memory.PriorSummaries = summarizeStrings(summaries)
	memory.KnownFindings = compactMemoryFindings(knownFindings)
	memory.ArchitecturalNotes = summarizeStrings(architectureNotes)
	memory.ConsistencyHints = buildConsistencyHints(memory.KnownFindings, analyses)
	return memory
}

func enrichReviewMemory(memory domain.ReviewMemory, sections []domain.DocumentSection, findings []domain.Finding, summary string) domain.ReviewMemory {
	modules := collectMemoryModules(sections, findings)
	entities := collectMemoryEntities(findings)
	glossary := collectMemoryGlossary(sections, findings, summary)
	userRoles := collectMemoryUserRoles(sections, findings)
	decisions := collectArchitectureDecisions(findings)

	memory.Modules = limitUniqueStrings(modules, reviewMemoryModuleLimit)
	memory.Entities = limitUniqueStrings(entities, reviewMemoryEntityLimit)
	memory.Glossary = limitUniqueStrings(glossary, reviewMemoryGlossaryLimit)
	memory.UserRoles = limitUniqueStrings(userRoles, reviewMemoryUserRoleLimit)
	memory.ArchitectureDecisions = limitArchitectureDecisions(decisions, reviewMemoryDecisionLimit)
	return memory
}

func compactMemoryFindings(findings []domain.Finding) []domain.Finding {
	deduped := deduplicateMemoryFindings(findings)
	sort.SliceStable(deduped, func(i, j int) bool {
		left := findingScore(deduped[i])
		right := findingScore(deduped[j])
		if left == right {
			return deduped[i].Problem < deduped[j].Problem
		}
		return left > right
	})
	roleCounts := make(map[string]int, len(domain.DefaultReviewerRoles()))
	selected := make([]domain.Finding, 0, minInt(len(deduped), reviewMemoryFindingsLimit))
	for _, finding := range deduped {
		roleKey := strings.TrimSpace(finding.Role)
		if roleCounts[roleKey] >= reviewMemoryRoleLimit {
			continue
		}
		roleCounts[roleKey]++
		selected = append(selected, finding)
		if len(selected) >= reviewMemoryFindingsLimit {
			break
		}
	}
	return selected
}

func deduplicateMemoryFindings(findings []domain.Finding) []domain.Finding {
	result := make([]domain.Finding, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		key := memoryIssueKey(finding)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, finding)
	}
	return result
}

func buildConsistencyHints(findings []domain.Finding, analyses []domain.Analysis) []string {
	hints := make([]string, 0, 4)
	seen := map[string]struct{}{}

	appendHint := func(value string) {
		value = safeTruncate(strings.TrimSpace(value), 180)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		hints = append(hints, value)
	}

	for _, finding := range findings {
		switch reviewshape.IssueFingerprint(finding) {
		case "refund_threshold", "partial_refund_rules", "reason_dictionary_rules", "billing_idempotency", "concurrency_case_assignment", "sla_slo_requirements", "report_export_rules", "roles_and_permissions":
			appendHint(finding.Problem)
		}
		if len(hints) >= 4 {
			return hints
		}
	}

	for _, analysis := range analyses {
		summary := strings.TrimSpace(analysis.Summary)
		if summary == "" {
			continue
		}
		if strings.Contains(strings.ToLower(summary), "противореч") || strings.Contains(strings.ToLower(summary), "несоглас") {
			appendHint(summary)
		}
		if len(hints) >= 4 {
			break
		}
	}

	return hints
}

func summarizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, safeTruncate(value, 220))
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func safeTruncate(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}

	cut := maxRunes
	for cut > 0 && runes[cut-1] != ' ' && runes[cut-1] != '\n' && runes[cut-1] != '\t' {
		cut--
	}
	if cut == 0 {
		cut = maxRunes
	}
	return strings.TrimSpace(string(runes[:cut])) + "..."
}

func isArchitecturalMemoryCandidate(finding domain.Finding) bool {
	switch finding.Role {
	case string(domain.ReviewerRoleTechLead),
		string(domain.ReviewerRoleSolutionArchitect),
		string(domain.ReviewerRoleSeniorBackendEngineer),
		string(domain.ReviewerRoleDevOpsReviewer):
	default:
		return false
	}

	return finding.Severity == domain.SeverityCritical ||
		finding.Severity == domain.SeverityError ||
		finding.Category == "contradiction" ||
		finding.Category == "technical_risk" ||
		finding.Category == "scalability_risk" ||
		finding.Category == "devops_risk" ||
		finding.Category == "api_problem"
}

func collectMemoryModules(sections []domain.DocumentSection, findings []domain.Finding) []string {
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		title := strings.TrimSpace(section.Title)
		if title == "" {
			continue
		}
		result = append(result, title)
	}
	for _, finding := range findings {
		title := strings.TrimSpace(finding.SectionTitle)
		if title == "" {
			continue
		}
		result = append(result, title)
	}
	return result
}

func collectMemoryEntities(findings []domain.Finding) []string {
	result := make([]string, 0, len(findings))
	for _, finding := range findings {
		result = append(result, extractQuotedTerms(finding.Problem)...)
		result = append(result, extractQuotedTerms(finding.HowToFix)...)
	}
	return result
}

func collectMemoryGlossary(sections []domain.DocumentSection, findings []domain.Finding, summary string) []string {
	result := make([]string, 0)
	for _, section := range sections {
		result = append(result, extractUpperTerms(section.Title)...)
		result = append(result, extractGlossaryTokens(section.Content)...)
	}
	for _, finding := range findings {
		result = append(result, extractUpperTerms(finding.Problem)...)
		result = append(result, extractGlossaryTokens(finding.Problem)...)
		result = append(result, extractGlossaryTokens(finding.HowToFix)...)
	}
	result = append(result, extractUpperTerms(summary)...)
	result = append(result, extractGlossaryTokens(summary)...)
	return result
}

func collectMemoryUserRoles(sections []domain.DocumentSection, findings []domain.Finding) []string {
	result := make([]string, 0)
	for _, section := range sections {
		result = append(result, extractRoleTerms(section.Title)...)
		result = append(result, extractRoleTerms(section.Content)...)
	}
	for _, finding := range findings {
		result = append(result, extractRoleTerms(finding.Problem)...)
		result = append(result, extractRoleTerms(finding.HowToFix)...)
	}
	return result
}

func extractQuotedTerms(value string) []string {
	replacer := strings.NewReplacer("«", "\"", "»", "\"", "'", "\"")
	value = replacer.Replace(value)
	parts := strings.Split(value, "\"")
	result := make([]string, 0)
	for idx := 1; idx < len(parts); idx += 2 {
		term := strings.TrimSpace(parts[idx])
		if len([]rune(term)) < 2 {
			continue
		}
		result = append(result, term)
	}
	return result
}

func extractUpperTerms(value string) []string {
	words := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '.' || r == ':' || r == ';' || r == '(' || r == ')' || r == '[' || r == ']'
	})
	result := make([]string, 0)
	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) < 2 {
			continue
		}
		if word == strings.ToUpper(word) && strings.IndexFunc(word, func(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'А' && r <= 'Я' }) != -1 {
			result = append(result, word)
		}
	}
	return result
}

func extractGlossaryTokens(value string) []string {
	matches := glossaryTokenPattern.FindAllString(value, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		normalized := strings.TrimSpace(match)
		if len(normalized) < 3 {
			continue
		}
		if isRoleWord(normalized) {
			continue
		}
		result = append(result, normalized)
	}
	return result
}

func extractRoleTerms(value string) []string {
	lowered := strings.ToLower(value)
	candidates := []string{
		"user", "users", "admin", "administrator", "moderator", "operator", "manager", "owner",
		"customer", "client", "guest", "seller", "buyer", "lead", "reviewer", "qa", "devops",
		"пользователь", "админ", "администратор", "модератор", "оператор", "менеджер", "владелец",
		"клиент", "гость", "покупатель", "продавец", "лид",
	}
	result := make([]string, 0)
	for _, candidate := range candidates {
		if strings.Contains(lowered, candidate) {
			result = append(result, candidate)
		}
	}
	return result
}

func isRoleWord(value string) bool {
	lowered := strings.ToLower(value)
	switch lowered {
	case "tech", "lead", "architect", "backend", "frontend", "mobile", "devops", "qa", "security", "reviewer":
		return true
	default:
		return false
	}
}

func limitUniqueStrings(values []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	result := make([]string, 0, limit)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, safeTruncate(value, 80))
		if len(result) >= limit {
			break
		}
	}
	return result
}

func collectArchitectureDecisions(findings []domain.Finding) []domain.ArchitectureDecision {
	result := make([]domain.ArchitectureDecision, 0)
	for _, finding := range findings {
		if !isArchitectureDecisionCandidate(finding) {
			continue
		}

		decision := strings.TrimSpace(finding.HowToFix)
		if decision == "" {
			continue
		}

		context := strings.TrimSpace(finding.Problem)
		if finding.SectionTitle != "" {
			context = strings.TrimSpace(finding.SectionTitle + ": " + context)
		}

		status := "proposed"
		if finding.Severity == domain.SeverityCritical || finding.Severity == domain.SeverityError {
			status = "important"
		}

		result = append(result, domain.ArchitectureDecision{
			Decision:  safeTruncate(decision, 140),
			Context:   safeTruncate(context, 120),
			Rationale: safeTruncate(strings.TrimSpace(finding.WhyItIsBad), 140),
			Status:    status,
		})
	}
	return result
}

func isArchitectureDecisionCandidate(finding domain.Finding) bool {
	switch finding.Role {
	case string(domain.ReviewerRoleTechLead),
		string(domain.ReviewerRoleSolutionArchitect),
		string(domain.ReviewerRoleSeniorBackendEngineer),
		string(domain.ReviewerRoleDevOpsReviewer),
		string(domain.ReviewerRoleSecurityLead):
	default:
		return false
	}

	return strings.TrimSpace(finding.HowToFix) != ""
}

func limitArchitectureDecisions(values []domain.ArchitectureDecision, limit int) []domain.ArchitectureDecision {
	if limit <= 0 || len(values) == 0 {
		return nil
	}

	result := make([]domain.ArchitectureDecision, 0, limit)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value.Decision))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}
