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

func TestShapeFinalFindingsPrefersQAOwnerForReasonDictionaryRules(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleSolutionArchitect),
			Category:   "missing_requirement",
			Severity:   domain.SeverityWarning,
			Problem:    "Не определено, для каких действий обязательна причина из справочника.",
			WhyItIsBad: "Это приводит к неоднородной реализации.",
			HowToFix:   "Явно указать список действий.",
		},
		{
			Role:       string(domain.ReviewerRoleQAReviewer),
			Category:   "ambiguity",
			Severity:   domain.SeverityWarning,
			Problem:    "Не указано, для каких конкретно действий обязательна причина из справочника.",
			WhyItIsBad: "Нельзя однозначно проверить сценарии валидации.",
			HowToFix:   "Перечислить действия и правила валидации.",
		},
	}

	shaped := shapeFinalFindings(findings, domain.AnalysisModeFullReview)
	if len(shaped) != 1 {
		t.Fatalf("expected 1 shaped finding, got %d", len(shaped))
	}
	if shaped[0].Role != string(domain.ReviewerRoleQAReviewer) {
		t.Fatalf("expected QA finding to win ownership, got role %q", shaped[0].Role)
	}
}

func TestShapeFinalFindingsPrefersQAOwnerForInternalCommentLifecycle(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{
			Role:       string(domain.ReviewerRoleTechLead),
			Category:   "missing_requirement",
			Severity:   domain.SeverityWarning,
			Problem:    "В разделе 5.2 не описаны правила управления комментариями: редактирование, удаление, история изменений, упоминания и ограничения по размеру.",
			WhyItIsBad: "Это приводит к неоднозначной реализации.",
			HowToFix:   "Добавить требования по правам, истории и лимитам комментариев.",
		},
		{
			Role:       string(domain.ReviewerRoleQAReviewer),
			Category:   "missing_requirement",
			Severity:   domain.SeverityError,
			Problem:    "Отсутствуют требования по управлению комментариями: редактирование, удаление, история изменений, упоминания и лимиты размера.",
			WhyItIsBad: "Нельзя однозначно проверить корректность работы с комментариями.",
			HowToFix:   "Добавить acceptance criteria для lifecycle комментариев.",
		},
		{
			Role:       string(domain.ReviewerRoleSeniorFrontendEngineer),
			Category:   "frontend_risk",
			Severity:   domain.SeverityWarning,
			Problem:    "Не описаны правила редактирования, удаления и упоминания комментариев, а также лимиты на размер.",
			WhyItIsBad: "Непонятно, как реализовать UX комментариев.",
			HowToFix:   "Описать UI-поведение и ограничения комментариев.",
		},
	}

	shaped := shapeFinalFindings(findings, domain.AnalysisModeFullReview)
	if len(shaped) != 1 {
		t.Fatalf("expected 1 shaped finding, got %d", len(shaped))
	}
	if shaped[0].Role != string(domain.ReviewerRoleQAReviewer) {
		t.Fatalf("expected QA finding to win ownership, got role %q", shaped[0].Role)
	}
}
