package service

import (
	"sort"
	"strings"

	"technical-specification-review-agent/internal/domain"
)

const (
	reviewMemoryRunLimit           = 5
	reviewMemorySummaryLimit       = 3
	reviewMemoryFindingsLimit      = 12
	reviewMemoryArchitecturesLimit = 4
)

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
	return memory
}

func compactMemoryFindings(findings []domain.Finding) []domain.Finding {
	deduped := deduplicateFindings(findings)
	sort.SliceStable(deduped, func(i, j int) bool {
		left := findingScore(deduped[i])
		right := findingScore(deduped[j])
		if left == right {
			return deduped[i].Problem < deduped[j].Problem
		}
		return left > right
	})
	if len(deduped) > reviewMemoryFindingsLimit {
		deduped = deduped[:reviewMemoryFindingsLimit]
	}
	return deduped
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
