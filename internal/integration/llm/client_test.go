package llm

import (
	"testing"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/prompt"
)

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

func TestBuildChatMessagesAddsAssistantPrefixForDeepSeek(t *testing.T) {
	cfg := config.LLMConfig{
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
	}
	if !shouldUseDeepSeekJSONPrefix(cfg) {
		t.Fatalf("expected deepseek prefix to be enabled")
	}

	messages := buildChatMessages(prompt.BuiltPrompt{
		System: "system",
		User:   "user",
	}, true)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[2].Role != "assistant" {
		t.Fatalf("expected assistant prefix message, got %s", messages[2].Role)
	}
	if messages[2].Content != "{\"findings\":" {
		t.Fatalf("unexpected assistant prefix: %s", messages[2].Content)
	}
}

func TestBuildChatMessagesSkipsAssistantPrefixForNonDeepSeek(t *testing.T) {
	cfg := config.LLMConfig{
		BaseURL: "https://api.cerebras.ai/v1",
		Model:   "gpt-oss-120b",
	}
	if shouldUseDeepSeekJSONPrefix(cfg) {
		t.Fatalf("expected deepseek prefix to be disabled")
	}

	messages := buildChatMessages(prompt.BuiltPrompt{
		System: "system",
		User:   "user",
	}, false)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
}
