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
			Memory: domain.ReviewMemory{
				Modules:               []string{"Billing"},
				UserRoles:             []string{"Administrator"},
				Entities:              []string{"`invoice`"},
				Glossary:              []string{"SLA"},
				ArchitectureDecisions: []string{"Использовать очередь для ретраев"},
			},
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
	if len(memory.Modules) == 0 || memory.Modules[0] != "Billing" {
		t.Fatalf("expected prior modules to be preserved, got %+v", memory.Modules)
	}
	if len(memory.UserRoles) == 0 || memory.UserRoles[0] != "Administrator" {
		t.Fatalf("expected prior roles to be preserved, got %+v", memory.UserRoles)
	}
	if len(memory.ArchitectureDecisions) == 0 {
		t.Fatalf("expected architecture decisions to be preserved")
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

func TestEnrichReviewMemoryBuildsStructuredKnowledgeFromCurrentDocument(t *testing.T) {
	base := domain.ReviewMemory{
		ReviewKey:     "upload:billing-spec",
		PriorRunCount: 2,
	}

	document := domain.Document{
		Name:       "billing-spec.md",
		RawContent: "Администратор может обновлять `invoice`. Пользователь видит статус платежа.\n\nSLA: 2 секунды.\n\n\"PaymentIntent\" должен синхронизироваться с CRM.",
		Sections: []domain.DocumentSection{
			{Title: "1. Billing"},
			{Title: "2. Integrations"},
		},
	}

	findings := []domain.Finding{
		{
			Role:     string(domain.ReviewerRoleSolutionArchitect),
			Category: "technical_risk",
			Severity: domain.SeverityCritical,
			Problem:  "Не описан контракт CRM sync",
			HowToFix: "Зафиксировать идемпотентный контракт синхронизации с CRM.",
		},
	}

	memory := enrichReviewMemory(base, document, nil, findings, domain.AnalysisModeFullReview)
	if len(memory.Modules) < 2 {
		t.Fatalf("expected modules to be extracted, got %+v", memory.Modules)
	}
	if len(memory.UserRoles) == 0 {
		t.Fatalf("expected user roles to be extracted, got %+v", memory.UserRoles)
	}
	if len(memory.Entities) == 0 {
		t.Fatalf("expected entities to be extracted, got %+v", memory.Entities)
	}
	if len(memory.Glossary) == 0 {
		t.Fatalf("expected glossary to be extracted, got %+v", memory.Glossary)
	}
	if len(memory.ArchitectureDecisions) == 0 {
		t.Fatalf("expected architecture decisions to be extracted, got %+v", memory.ArchitectureDecisions)
	}
}

func TestEnrichReviewMemoryTracksResolvedSectionProblems(t *testing.T) {
	base := domain.ReviewMemory{
		ReviewKey: "google_docs:test",
		Sections: []domain.MemorySection{{
			SectionID:     "scope",
			SectionTitle:  "Scope",
			KnownProblems: []string{"Не описаны финальные статусы"},
		}},
	}

	chunks := []domain.AnalysisChunk{{
		SectionID:    "scope",
		SectionTitle: "Scope",
		ChunkIndex:   0,
	}}

	memory := enrichReviewMemory(base, domain.Document{}, chunks, []domain.Finding{}, domain.AnalysisModeIncrementalReview)
	if len(memory.ResolvedFindings) != 1 {
		t.Fatalf("expected one resolved finding, got %+v", memory.ResolvedFindings)
	}
	if memory.ResolvedFindings[0].Problem != "Не описаны финальные статусы" {
		t.Fatalf("unexpected resolved problem: %+v", memory.ResolvedFindings[0])
	}
	if len(memory.Sections) == 0 || len(memory.Sections[0].ResolvedProblems) != 1 {
		t.Fatalf("expected resolved problems inside section memory, got %+v", memory.Sections)
	}
}
