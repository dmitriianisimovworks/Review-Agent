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
}
