package service

import (
	"context"
	"strings"
	"testing"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/vector"
	"technical-specification-review-agent/internal/parser"
)

func TestVectorRetrieverEnrichesMemoryWithVectorHints(t *testing.T) {
	embedder := &stubEmbedder{}
	store := &stubVectorStore{
		searchResults: []vector.SearchResult{
			{
				Score: 0.91,
				Payload: map[string]any{
					"kind":          "finding",
					"role":          "senior_backend_engineer",
					"severity":      "ERROR",
					"section_title": "5.1 Queue Management",
					"problem":       "Не описано поведение при конкурентном захвате кейса.",
				},
			},
			{
				Score: 0.87,
				Payload: map[string]any{
					"kind":    "analysis_summary",
					"summary": "Ранее уже всплывала тема порога согласования refund.",
				},
			},
		},
	}
	retriever := NewVectorRetriever(embedder, store)

	document := domain.Document{
		Name:      "Billing Ops",
		ReviewKey: "google_docs:doc-1",
	}
	parsed := parser.ParsedDocument{
		Text: "5.1 Queue Management\nНе описано поведение при захвате кейса.",
		Sections: []domain.DocumentSection{
			{ID: "5.1", Title: "5.1 Queue Management"},
		},
	}

	memory, err := retriever.EnrichMemory(context.Background(), document, parsed, domain.ReviewMemory{
		ReviewKey: "google_docs:doc-1",
	})
	if err != nil {
		t.Fatalf("EnrichMemory() error = %v", err)
	}
	if len(embedder.calls) != 1 {
		t.Fatalf("expected 1 embedder call, got %d", len(embedder.calls))
	}
	if len(store.searchRequests) != 1 {
		t.Fatalf("expected 1 vector search request, got %d", len(store.searchRequests))
	}
	if got := store.searchRequests[0].ExcludeReviewKey; got != "google_docs:doc-1" {
		t.Fatalf("expected review key exclusion, got %q", got)
	}
	if len(memory.VectorHints) != 2 {
		t.Fatalf("expected 2 vector hints, got %d", len(memory.VectorHints))
	}
	if !strings.Contains(memory.VectorHints[0], "Похожий прошлый сигнал") {
		t.Fatalf("expected finding hint, got %q", memory.VectorHints[0])
	}
	if !strings.Contains(memory.VectorHints[1], "Похожий прошлый итог") {
		t.Fatalf("expected summary hint, got %q", memory.VectorHints[1])
	}
}
