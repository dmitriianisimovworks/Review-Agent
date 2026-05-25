package comment

import (
	"strings"
	"testing"

	"technical-specification-review-agent/internal/domain"
)

func TestDefaultFormatter_Format(t *testing.T) {
	t.Parallel()

	document := domain.Document{
		NormalizedContent: "Раздел 1\n\nПользователь может удалить аккаунт.\n\nРаздел 2\n\nНе описано, что происходит с данными после удаления.",
	}
	analysis := domain.Analysis{
		Summary: "Анализ завершён.",
		Findings: []domain.Finding{
			{
				Role:         string(domain.ReviewerRoleSeniorBackendEngineer),
				Severity:     domain.SeverityCritical,
				SectionTitle: "Удаление аккаунта",
				Problem:      "Не описано удаление связанных данных.",
				WhyItIsBad:   "Возможна потеря согласованности.",
				HowToFix:     "Описать lifecycle связанных сущностей.",
				SourceChunk:  "Пользователь может удалить аккаунт.",
			},
		},
	}

	formatter := NewDefaultFormatter()
	drafts := formatter.Format(document, analysis, PublishModeBoth)
	if len(drafts) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(drafts))
	}

	inline := drafts[0]
	if inline.AnchorLine == nil || *inline.AnchorLine < 1 {
		t.Fatalf("expected anchor line to be detected")
	}
	if !strings.Contains(inline.Content, "🧱 Senior Backend Engineer") || !strings.Contains(inline.Content, "Ключевые замечания:") {
		t.Fatalf("inline draft does not follow role-based format: %q", inline.Content)
	}
	if !strings.Contains(inline.Content, "Связано с разделом:") || !strings.Contains(inline.Content, "Фрагмент:") {
		t.Fatalf("inline draft should contain section and fragment references: %q", inline.Content)
	}

	summary := drafts[1]
	if !strings.Contains(summary.Content, "Итоговый комментарий") {
		t.Fatalf("summary draft missing title: %q", summary.Content)
	}
	if !strings.Contains(summary.Content, "Ключевых тем: 1") {
		t.Fatalf("summary draft should contain compact totals: %q", summary.Content)
	}
	if !strings.Contains(summary.Content, "Активные роли:") || !strings.Contains(summary.Content, "Без замечаний:") {
		t.Fatalf("summary draft should contain role coverage lines: %q", summary.Content)
	}
	if summary.AnchorLine == nil || *summary.AnchorLine < *inline.AnchorLine {
		t.Fatalf("expected summary anchor to be at end of document")
	}
}

func TestSelectInlineFindingsFiltersSeverityAndLimitsWarnings(t *testing.T) {
	t.Parallel()

	findings := []domain.Finding{
		{Severity: domain.SeverityInfo, Problem: "info"},
		{Severity: domain.SeverityWarning, Problem: "w1"},
		{Severity: domain.SeverityWarning, Problem: "w2"},
		{Severity: domain.SeverityWarning, Problem: "w3"},
		{Severity: domain.SeverityWarning, Problem: "w4"},
		{Severity: domain.SeverityError, Problem: "e1"},
		{Severity: domain.SeverityCritical, Problem: "c1"},
	}

	selected := selectInlineFindings(findings)
	if len(selected) != 5 {
		t.Fatalf("expected 5 selected findings, got %d", len(selected))
	}

	for _, finding := range selected {
		if finding.Severity == domain.SeverityInfo {
			t.Fatalf("info finding should not be selected for inline comments")
		}
	}
}

func TestBuildSummaryDraftsGroupsRemainingFindings(t *testing.T) {
	t.Parallel()

	document := domain.Document{
		NormalizedContent: "Раздел 1\n\nТекст документа.\n\nФинал документа.",
	}
	analysis := domain.Analysis{
		Findings: []domain.Finding{
			{Role: string(domain.ReviewerRoleTechLead), Severity: domain.SeverityError, Category: "missing_requirement", Problem: "Не определены финальные статусы кейса."},
			{Role: string(domain.ReviewerRoleTechLead), Severity: domain.SeverityWarning, Category: "missing_requirement", Problem: "Не описано ограничение на повторные эскалации."},
			{Role: string(domain.ReviewerRoleQAReviewer), Severity: domain.SeverityWarning, Category: "missing_requirement", Problem: "Не описаны правила редактирования комментариев."},
			{Role: string(domain.ReviewerRoleDevOpsReviewer), Severity: domain.SeverityWarning, Category: "missing_requirement", Problem: "Не указан формат экспорта отчётов."},
		},
	}

	drafts := buildSummaryDrafts(document, analysis)
	if len(drafts) != 1 {
		t.Fatalf("expected 1 summary draft, got %d", len(drafts))
	}

	content := drafts[0].Content
	if strings.Contains(content, "1. [") {
		t.Fatalf("summary should not contain long enumerated list anymore: %q", content)
	}
	if strings.Contains(content, "important") || strings.Contains(content, "blocker-like") || strings.Contains(content, "Основные роли:") {
		t.Fatalf("summary should use human russian wording without role line: %q", content)
	}
	if !strings.Contains(content, "Дополнительно:") {
		t.Fatalf("summary should contain grouped section: %q", content)
	}
	if !strings.Contains(content, "Активные роли:") || !strings.Contains(content, "Без замечаний:") {
		t.Fatalf("summary should contain role coverage summary: %q", content)
	}
	if !strings.Contains(content, "- ") {
		t.Fatalf("summary should contain grouped bullet points: %q", content)
	}
}

func TestBuildSummaryDraftsUsesUnifiedThemeGrouping(t *testing.T) {
	t.Parallel()

	document := domain.Document{
		NormalizedContent: "Раздел 1\n\nТекст документа.\n\nФинал документа.",
	}
	analysis := domain.Analysis{
		Findings: []domain.Finding{
			{
				Role:     string(domain.ReviewerRoleTechLead),
				Severity: domain.SeverityError,
				Category: "missing_requirement",
				Problem:  "Не описано поведение системы при одновременной попытке нескольких пользователей взять в работу один и тот же кейс.",
			},
			{
				Role:     string(domain.ReviewerRoleSeniorBackendEngineer),
				Severity: domain.SeverityError,
				Category: "technical_risk",
				Problem:  "Не описано поведение при конкурентном доступе к кейсу, когда два пользователя одновременно пытаются взять один и тот же кейс.",
			},
		},
	}

	drafts := buildSummaryDrafts(document, analysis)
	content := drafts[0].Content
	if strings.Count(content, "Конкурентный доступ и блокировки") != 1 {
		t.Fatalf("expected unified concurrency theme once, got %q", content)
	}
}

func TestBuildRoleDraftsCreatesSingleCommentPerRole(t *testing.T) {
	t.Parallel()

	document := domain.Document{
		NormalizedContent: "Строка 1\n\nСтрока 2\n\nСтрока 3",
	}
	findings := []domain.Finding{
		{
			Role:        string(domain.ReviewerRoleTechLead),
			Severity:    domain.SeverityCritical,
			Problem:     "Проблема 1",
			WhyItIsBad:  "Последствие 1",
			HowToFix:    "Исправление 1",
			SourceChunk: "Строка 1",
		},
		{
			Role:        string(domain.ReviewerRoleTechLead),
			Severity:    domain.SeverityError,
			Problem:     "Проблема 2",
			WhyItIsBad:  "Последствие 2",
			HowToFix:    "Исправление 2",
			SourceChunk: "Строка 1",
		},
		{
			Role:        string(domain.ReviewerRoleQAReviewer),
			Severity:    domain.SeverityWarning,
			Problem:     "Проблема 3",
			WhyItIsBad:  "Последствие 3",
			HowToFix:    "Исправление 3",
			SourceChunk: "Строка 2",
		},
	}

	drafts := buildRoleDrafts(document, findings)
	if len(drafts) != 2 {
		t.Fatalf("expected 2 role drafts, got %d", len(drafts))
	}
	if !strings.Contains(drafts[0].Content, "🧭 Tech Lead") {
		t.Fatalf("expected tech lead header, got %q", drafts[0].Content)
	}
	if !strings.Contains(drafts[1].Content, "🧪 QA Reviewer") {
		t.Fatalf("expected qa reviewer header, got %q", drafts[1].Content)
	}
}
