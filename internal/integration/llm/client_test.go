package llm

import "testing"

func TestParseChunkAnalysisExtractsFirstBalancedJSONObject(t *testing.T) {
	content := "```json\n{\"findings\":[{\"role\":\"qa_reviewer\",\"category\":\"missing_requirement\",\"severity\":\"ERROR\",\"problem\":\"Проблема\",\"why_it_is_bad\":\"Последствие\",\"how_to_fix\":\"Исправление\"}]}\n```\nДополнительный хвост"

	parsed, err := parseChunkAnalysis(content)
	if err != nil {
		t.Fatalf("parseChunkAnalysis() error = %v", err)
	}
	if len(parsed.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(parsed.Findings))
	}
	if parsed.Findings[0].Role != "qa_reviewer" {
		t.Fatalf("unexpected role: %s", parsed.Findings[0].Role)
	}
}

func TestExtractFirstJSONObjectHandlesBracesInsideStrings(t *testing.T) {
	content := `prefix {"findings":[{"problem":"Нужно сохранить формат {json} внутри строки","why_it_is_bad":"ok","how_to_fix":"ok","role":"qa_reviewer","category":"contradiction","severity":"WARNING"}]} suffix`

	got, ok := extractFirstJSONObject(content)
	if !ok {
		t.Fatalf("expected object to be extracted")
	}
	expected := `{"findings":[{"problem":"Нужно сохранить формат {json} внутри строки","why_it_is_bad":"ok","how_to_fix":"ok","role":"qa_reviewer","category":"contradiction","severity":"WARNING"}]}`
	if got != expected {
		t.Fatalf("unexpected extracted object:\nwant: %s\ngot:  %s", expected, got)
	}
}
