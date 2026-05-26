package service

import (
	"context"
	"testing"
	"time"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/vector"
)

type stubEmbedder struct {
	calls [][]string
}

func (s *stubEmbedder) EmbedTexts(_ context.Context, texts []string) ([][]float32, error) {
	s.calls = append(s.calls, texts)
	vectors := make([][]float32, 0, len(texts))
	for range texts {
		vectors = append(vectors, []float32{0.1, 0.2, 0.3})
	}
	return vectors, nil
}

type stubVectorStore struct {
	points         []vector.Point
	searchRequests []vector.SearchRequest
	searchResults  []vector.SearchResult
}

func (s *stubVectorStore) Upsert(_ context.Context, points []vector.Point) error {
	s.points = append([]vector.Point(nil), points...)
	return nil
}

func (s *stubVectorStore) Search(_ context.Context, req vector.SearchRequest) ([]vector.SearchResult, error) {
	s.searchRequests = append(s.searchRequests, req)
	return append([]vector.SearchResult(nil), s.searchResults...), nil
}

func TestVectorIndexerIndexesSummaryFindingsAndSections(t *testing.T) {
	embedder := &stubEmbedder{}
	store := &stubVectorStore{}
	indexer := NewVectorIndexer(embedder, store)

	now := time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
	document := domain.Document{
		ID:         "doc_1",
		Name:       "Billing Ops",
		ExternalID: "google-doc-1",
		ReviewKey:  "google_docs:google-doc-1",
	}
	analysis := domain.Analysis{
		ID:              "analysis_1",
		DocumentID:      document.ID,
		Mode:            domain.AnalysisModeIncrementalReview,
		Summary:         "Краткий итог review.",
		CreatedAt:       now,
		TargetSectionID: "4.2",
		DocumentSections: []domain.DocumentSection{
			{
				ID:      "4.2",
				Title:   "4.2 Case Review",
				Content: "Section content for vector storage.",
			},
		},
		Findings: []domain.Finding{
			{
				Role:         string(domain.ReviewerRoleQAReviewer),
				Category:     "Requirements",
				Severity:     domain.SeverityWarning,
				Problem:      "Не указано обязательное поле причины.",
				WhyItIsBad:   "Нельзя однозначно протестировать сценарий.",
				HowToFix:     "Перечислить действия, где причина обязательна.",
				SectionID:    "4.2",
				SectionTitle: "4.2 Case Review",
			},
		},
	}

	if err := indexer.IndexAnalysis(context.Background(), document, analysis); err != nil {
		t.Fatalf("IndexAnalysis() error = %v", err)
	}

	if len(embedder.calls) != 1 {
		t.Fatalf("expected 1 embedder call, got %d", len(embedder.calls))
	}
	if got := len(embedder.calls[0]); got != 3 {
		t.Fatalf("expected 3 embedded texts, got %d", got)
	}
	if got := len(store.points); got != 3 {
		t.Fatalf("expected 3 upserted points, got %d", got)
	}
}
