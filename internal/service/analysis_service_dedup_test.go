package service

import (
	"strings"
	"testing"

	"technical-specification-review-agent/internal/domain"
)

func TestShapeFinalFindingsPrefersBackendOwnerForConcurrency(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleTechLead),
			Category:   "missing_requirement",
			Severity:   domain.SeverityError,
			Problem:    "Не описано поведение системы при одновременной попытке нескольких пользователей взять в работу один и тот же кейс.",
			WhyItIsBad: "Это приведет к неконсистентности данных.",
			HowToFix:   "Добавить правила блокировки кейса.",
		},
		{
			Role:       string(domain.ReviewerRoleSeniorBackendEngineer),
			Category:   "technical_risk",
			Severity:   domain.SeverityError,
			Problem:    "Не описано поведение при конкурентном доступе к кейсу, когда два пользователя одновременно пытаются взять один и тот же кейс.",
			WhyItIsBad: "Отсутствует атомарный захват кейса и возможен race condition.",
			HowToFix:   "Добавить optimistic locking или атомарную операцию захвата.",
		},
	}

	shaped := shapeFinalFindings(findings, domain.AnalysisModeFullReview)
	if len(shaped) != 1 {
		t.Fatalf("expected 1 shaped finding, got %d", len(shaped))
	}
	if shaped[0].Role != string(domain.ReviewerRoleSeniorBackendEngineer) {
		t.Fatalf("expected backend finding to win ownership, got role %q", shaped[0].Role)
	}
}

func TestShapeFinalFindingsPrefersBackendOwnerForPartialRefundRules(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleSecurityLead),
			Category:   "security_risk",
			Severity:   domain.SeverityWarning,
			Problem:    "Не описаны правила и ограничения для частичных возвратов (partial refund) и их взаимодействия с уже частично компенсированными инвойсами.",
			WhyItIsBad: "Это создает риск финансовых потерь.",
			HowToFix:   "Добавить правила для partial refund.",
		},
		{
			Role:       string(domain.ReviewerRoleSeniorBackendEngineer),
			Category:   "missing_requirement",
			Severity:   domain.SeverityWarning,
			Problem:    "Не описаны правила и ограничения для partial refund по одному invoice и взаимодействие с уже частично компенсированными инвойсами.",
			WhyItIsBad: "Нельзя корректно реализовать логику расчета остатка.",
			HowToFix:   "Добавить инварианты и ограничения partial refund.",
		},
	}

	shaped := shapeFinalFindings(findings, domain.AnalysisModeFullReview)
	if len(shaped) != 1 {
		t.Fatalf("expected 1 shaped finding, got %d", len(shaped))
	}
	if shaped[0].Role != string(domain.ReviewerRoleSeniorBackendEngineer) {
		t.Fatalf("expected backend finding to win ownership, got role %q", shaped[0].Role)
	}
}

func TestBuildSummaryMergesConcurrencyIntoSingleTheme(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleTechLead),
			Category:   "missing_requirement",
			Severity:   domain.SeverityError,
			Problem:    "Не описано поведение системы при одновременной попытке нескольких пользователей взять в работу один и тот же кейс.",
			WhyItIsBad: "Это приведет к неконсистентности данных.",
			HowToFix:   "Добавить правила блокировки кейса.",
		},
		{
			Role:       string(domain.ReviewerRoleSeniorBackendEngineer),
			Category:   "technical_risk",
			Severity:   domain.SeverityError,
			Problem:    "Не описано поведение при конкурентном доступе к кейсу, когда два пользователя одновременно пытаются взять один и тот же кейс.",
			WhyItIsBad: "Отсутствует атомарный захват кейса и возможен race condition.",
			HowToFix:   "Добавить optimistic locking или атомарную операцию захвата.",
		},
	}

	shaped := shapeFinalFindings(findings, domain.AnalysisModeFullReview)
	summary := buildSummary(shaped, 1)
	if strings.Count(summary, "Конкурентный доступ и блокировки") != 1 {
		t.Fatalf("expected concurrency theme once in summary, got %q", summary)
	}
}
