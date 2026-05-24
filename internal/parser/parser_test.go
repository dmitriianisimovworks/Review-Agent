package parser

import (
	"context"
	"testing"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
)

func TestChunkingParserParsePreservesSectionMetadata(t *testing.T) {
	parser := NewChunkingParser(config.DocumentConfig{
		ChunkSize: 120,
		MaxChunks: 10,
	})

	parsed, err := parser.Parse(context.Background(), ParseInput{
		Content: "ignored because sections drive the structure-aware path",
		Sections: []domain.DocumentSection{
			{
				ID:      "section_1",
				Title:   "1. Scope",
				Level:   1,
				Range:   domain.DocumentRange{StartIndex: 1, EndIndex: 80},
				Content: "The system must support complaint intake.\n\nIt should validate required fields.",
			},
			{
				ID:      "section_2",
				Title:   "2. Integrations",
				Level:   1,
				Range:   domain.DocumentRange{StartIndex: 81, EndIndex: 180},
				Content: "The service calls Marketplace API.\n\nIt retries transient failures with backoff.",
			},
		},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(parsed.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(parsed.Chunks))
	}
	if len(parsed.ChunkDescriptors) != 2 {
		t.Fatalf("expected 2 chunk descriptors, got %d", len(parsed.ChunkDescriptors))
	}

	first := parsed.ChunkDescriptors[0]
	if first.SectionID != "section_1" {
		t.Fatalf("expected first chunk section ID to be section_1, got %q", first.SectionID)
	}
	if first.SectionTitle != "1. Scope" {
		t.Fatalf("expected first chunk section title to be preserved, got %q", first.SectionTitle)
	}
	if first.Range.StartIndex != 1 || first.Range.EndIndex != 80 {
		t.Fatalf("expected first chunk range to match source section, got %+v", first.Range)
	}

	second := parsed.ChunkDescriptors[1]
	if second.SectionID != "section_2" {
		t.Fatalf("expected second chunk section ID to be section_2, got %q", second.SectionID)
	}
	if second.SectionLevel != 1 {
		t.Fatalf("expected second chunk section level to be 1, got %d", second.SectionLevel)
	}
}

func TestChunkingParserParseFallsBackToPlainTextChunkingWithoutSections(t *testing.T) {
	parser := NewChunkingParser(config.DocumentConfig{
		ChunkSize: 40,
		MaxChunks: 10,
	})

	parsed, err := parser.Parse(context.Background(), ParseInput{
		Content: "First paragraph.\n\nSecond paragraph with more text.",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(parsed.Chunks) == 0 {
		t.Fatalf("expected plain-text chunking to produce at least one chunk")
	}
	if len(parsed.Sections) != 0 {
		t.Fatalf("expected no sections in fallback mode, got %d", len(parsed.Sections))
	}
	if len(parsed.ChunkDescriptors) != 0 {
		t.Fatalf("expected no chunk descriptors in fallback mode, got %d", len(parsed.ChunkDescriptors))
	}
}
