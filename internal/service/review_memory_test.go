package service

import (
	"strings"
	"testing"

	"technical-specification-review-agent/internal/domain"
)

func TestBuildReviewMemoryCompactsHistory(t *testing.T) {
	analyses := []domain.Analysis{
		{
			Summary: "Первый review summary",
			Findings: []domain.Finding{
				{
					Role:       string(domain.ReviewerRoleSolutionArchitect),
					Category:   "technical_risk",
					Severity:   domain.SeverityCritical,
					Problem:    "Нет стратегии graceful degradation",
					WhyItIsBad: "Система будет падать целиком",
					HowToFix:   "Нужно добавить fallback и circuit breaker",
				},
			},
		},
		{
			Summary: "Второй review summary",
			Findings: []domain.Finding{
				{
					Role:       string(domain.ReviewerRoleTechLead),
					Category:   "missing_requirement",
					Severity:   domain.SeverityError,
					Problem:    "Не описаны финальные статусы",
					WhyItIsBad: "Нельзя замкнуть жизненный цикл",
					HowToFix:   "Определить полный state machine",
				},
			},
		},
	}

	memory := buildReviewMemory("google_docs:test-doc", analyses)
	if !memory.HasContext() {
		t.Fatalf("expected memory context to be present")
	}
	if memory.PriorRunCount != 2 {
		t.Fatalf("expected 2 prior runs, got %d", memory.PriorRunCount)
	}
	if len(memory.PriorSummaries) != 2 {
		t.Fatalf("expected 2 prior summaries, got %d", len(memory.PriorSummaries))
	}
	if len(memory.KnownFindings) != 2 {
		t.Fatalf("expected 2 known findings, got %d", len(memory.KnownFindings))
	}
	if len(memory.ArchitecturalNotes) == 0 {
		t.Fatalf("expected architectural notes to be built")
	}
}

func TestFilterFindingsByModeSuppressesKnownIncrementalDuplicates(t *testing.T) {
	memory := domain.ReviewMemory{
		ReviewKey:     "upload:spec",
		PriorRunCount: 1,
		KnownFindings: []domain.Finding{
			{
				Role:     string(domain.ReviewerRoleTechLead),
				Category: "missing_requirement",
				Severity: domain.SeverityError,
				Problem:  "Не описаны финальные статусы",
			},
		},
	}

	current := []domain.Finding{
		{
			Role:     string(domain.ReviewerRoleTechLead),
			Category: "missing_requirement",
			Severity: domain.SeverityError,
			Problem:  "Не описаны финальные статусы",
		},
		{
			Role:     string(domain.ReviewerRoleQAReviewer),
			Category: "technical_risk",
			Severity: domain.SeverityCritical,
			Problem:  "Не описан race condition при захвате кейса",
		},
	}

	filtered := filterFindingsByMode(current, memory, domain.AnalysisModeIncrementalReview)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 finding after duplicate suppression, got %d", len(filtered))
	}
	if !strings.Contains(filtered[0].Problem, "race condition") {
		t.Fatalf("expected the new finding to remain, got %q", filtered[0].Problem)
	}
}
