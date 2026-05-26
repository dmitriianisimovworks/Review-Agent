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
					Problem:    "Не описано, как определяется порог для дополнительного согласования возврата",
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
	if len(memory.ConsistencyHints) == 0 {
		t.Fatalf("expected consistency hints to be built")
	}
}

func TestEnrichReviewMemoryAddsStructuredKnowledge(t *testing.T) {
	memory := domain.ReviewMemory{
		ReviewKey:     "google_docs:test-doc",
		PriorRunCount: 1,
	}

	sections := []domain.DocumentSection{
		{
			ID:      "roles",
			Title:   "User Roles",
			Content: "Администратор управляет BILLING API. Пользователь может создавать заявки.",
		},
		{
			ID:      "modules",
			Title:   "Billing Module",
			Content: "CRM синхронизируется с OMS и SLA dashboard.",
		},
	}
	findings := []domain.Finding{
		{
			Role:         string(domain.ReviewerRoleTechLead),
			Problem:      "Не описана сущность \"заявка\" и права admin",
			HowToFix:     "Добавить glossary для заявки и роли admin",
			SectionTitle: "User Roles",
		},
	}

	enriched := enrichReviewMemory(memory, sections, findings, "CRM и OMS не согласованы по SLA.")
	if len(enriched.Modules) == 0 {
		t.Fatalf("expected modules to be collected")
	}
	if len(enriched.Entities) == 0 {
		t.Fatalf("expected entities to be collected")
	}
	if len(enriched.Glossary) == 0 {
		t.Fatalf("expected glossary to be collected")
	}
	if len(enriched.UserRoles) == 0 {
		t.Fatalf("expected user roles to be collected")
	}
	if len(enriched.ArchitectureDecisions) == 0 {
		t.Fatalf("expected architecture decisions to be collected")
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

	filtered, suppressed := filterFindingsByMode(current, memory, domain.AnalysisModeIncrementalReview)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 finding after duplicate suppression, got %d", len(filtered))
	}
	if suppressed != 1 {
		t.Fatalf("expected 1 suppressed duplicate, got %d", suppressed)
	}
	if !strings.Contains(filtered[0].Problem, "race condition") {
		t.Fatalf("expected the new finding to remain, got %q", filtered[0].Problem)
	}
}

func TestFilterFindingsByModeSuppressesLightlyRephrasedDuplicate(t *testing.T) {
	memory := domain.ReviewMemory{
		ReviewKey:     "upload:spec",
		PriorRunCount: 1,
		KnownFindings: []domain.Finding{
			{
				Role:     string(domain.ReviewerRoleSeniorBackendEngineer),
				Category: "missing_requirement",
				Severity: domain.SeverityError,
				Problem:  "Не описан механизм блокировки при одновременном захвате кейса двумя пользователями",
			},
		},
	}

	current := []domain.Finding{
		{
			Role:     string(domain.ReviewerRoleSeniorBackendEngineer),
			Category: "missing_requirement",
			Severity: domain.SeverityError,
			Problem:  "Отсутствует описание блокировки при одновременном захвате кейса двумя пользователями",
		},
	}

	filtered, suppressed := filterFindingsByMode(current, memory, domain.AnalysisModeIncrementalReview)
	if len(filtered) != 0 {
		t.Fatalf("expected duplicate to be suppressed, got %d findings", len(filtered))
	}
	if suppressed != 1 {
		t.Fatalf("expected 1 suppressed duplicate, got %d", suppressed)
	}
}

func TestFilterFindingsByModeSuppressesFingerprintDuplicateInSameSection(t *testing.T) {
	memory := domain.ReviewMemory{
		ReviewKey:     "upload:spec",
		PriorRunCount: 1,
		KnownFindings: []domain.Finding{
			{
				Role:         string(domain.ReviewerRoleQAReviewer),
				Category:     "ambiguity",
				Severity:     domain.SeverityWarning,
				Problem:      "Не указано, для каких действий обязательна причина из справочника.",
				SectionID:    "4.2",
				SectionTitle: "4.2 Case Review",
			},
		},
	}

	current := []domain.Finding{
		{
			Role:         string(domain.ReviewerRoleSeniorFrontendEngineer),
			Category:     "frontend_risk",
			Severity:     domain.SeverityWarning,
			Problem:      "Не определено, для каких именно действий обязательна причина из справочника.",
			SectionID:    "4.2",
			SectionTitle: "4.2 Case Review",
		},
	}

	filtered, suppressed := filterFindingsByMode(current, memory, domain.AnalysisModeIncrementalReview)
	if len(filtered) != 0 {
		t.Fatalf("expected fingerprint duplicate to be suppressed, got %d findings", len(filtered))
	}
	if suppressed != 1 {
		t.Fatalf("expected 1 suppressed duplicate, got %d", suppressed)
	}
}

func TestCompactMemoryFindingsLimitsRoleFlood(t *testing.T) {
	findings := []domain.Finding{
		{Role: string(domain.ReviewerRoleTechLead), Category: "missing_requirement", Severity: domain.SeverityCritical, Problem: "Проблема 1"},
		{Role: string(domain.ReviewerRoleTechLead), Category: "missing_requirement", Severity: domain.SeverityCritical, Problem: "Проблема 2"},
		{Role: string(domain.ReviewerRoleTechLead), Category: "missing_requirement", Severity: domain.SeverityCritical, Problem: "Проблема 3"},
		{Role: string(domain.ReviewerRoleQAReviewer), Category: "technical_risk", Severity: domain.SeverityError, Problem: "Проблема 4"},
		{Role: string(domain.ReviewerRoleQAReviewer), Category: "technical_risk", Severity: domain.SeverityError, Problem: "Проблема 5"},
		{Role: string(domain.ReviewerRoleQAReviewer), Category: "technical_risk", Severity: domain.SeverityError, Problem: "Проблема 6"},
	}

	compacted := compactMemoryFindings(findings)
	techLeadCount := 0
	qaCount := 0
	for _, finding := range compacted {
		switch finding.Role {
		case string(domain.ReviewerRoleTechLead):
			techLeadCount++
		case string(domain.ReviewerRoleQAReviewer):
			qaCount++
		}
	}

	if techLeadCount > reviewMemoryRoleLimit {
		t.Fatalf("expected tech lead memory cap %d, got %d", reviewMemoryRoleLimit, techLeadCount)
	}
	if qaCount > reviewMemoryRoleLimit {
		t.Fatalf("expected qa memory cap %d, got %d", reviewMemoryRoleLimit, qaCount)
	}
}

func TestCompactMemoryFindingsDeduplicatesByFingerprint(t *testing.T) {
	findings := []domain.Finding{
		{
			Role:         string(domain.ReviewerRoleQAReviewer),
			Category:     "ambiguity",
			Severity:     domain.SeverityWarning,
			Problem:      "Не указано, для каких действий обязательна причина из справочника.",
			SectionID:    "4.2",
			SectionTitle: "4.2 Case Review",
		},
		{
			Role:         string(domain.ReviewerRoleSeniorFrontendEngineer),
			Category:     "frontend_risk",
			Severity:     domain.SeverityWarning,
			Problem:      "Не определено, для каких именно действий обязательна причина из справочника.",
			SectionID:    "4.2",
			SectionTitle: "4.2 Case Review",
		},
	}

	compacted := compactMemoryFindings(findings)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 compacted finding after fingerprint dedup, got %d", len(compacted))
	}
}

func TestFilterFindingsByModeDoesNotSuppressSameProblemFromDifferentSection(t *testing.T) {
	memory := domain.ReviewMemory{
		ReviewKey:     "google_docs:test",
		PriorRunCount: 1,
		KnownFindings: []domain.Finding{
			{
				Role:         string(domain.ReviewerRoleSeniorBackendEngineer),
				Category:     "missing_requirement",
				Severity:     domain.SeverityError,
				Problem:      "Не описаны финальные статусы",
				SectionID:    "scope",
				SectionTitle: "Scope",
			},
		},
	}

	current := []domain.Finding{
		{
			Role:         string(domain.ReviewerRoleSeniorBackendEngineer),
			Category:     "missing_requirement",
			Severity:     domain.SeverityError,
			Problem:      "Отсутствует описание финальных статусов",
			SectionID:    "sla",
			SectionTitle: "SLA",
		},
	}

	filtered, suppressed := filterFindingsByMode(current, memory, domain.AnalysisModeIncrementalReview)
	if suppressed != 0 {
		t.Fatalf("expected no suppression across different sections, got %d", suppressed)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected finding from different section to remain, got %d", len(filtered))
	}
}
