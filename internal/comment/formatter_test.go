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
				Severity:    domain.SeverityCritical,
				Problem:     "Не описано удаление связанных данных.",
				WhyItIsBad:  "Возможна потеря согласованности.",
				HowToFix:    "Описать lifecycle связанных сущностей.",
				SourceChunk: "Пользователь может удалить аккаунт.",
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
	if !strings.Contains(inline.Content, "Проблема:") || !strings.Contains(inline.Content, "Как исправить:") {
		t.Fatalf("inline draft does not follow required format: %q", inline.Content)
	}

	summary := drafts[1]
	if !strings.Contains(summary.Content, "Итоговый комментарий") {
		t.Fatalf("summary draft missing title: %q", summary.Content)
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
