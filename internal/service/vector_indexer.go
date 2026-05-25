package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/vector"
)

type AnalysisVectorIndexer interface {
	IndexAnalysis(ctx context.Context, document domain.Document, analysis domain.Analysis) error
}

type VectorIndexer struct {
	embedder vector.Embedder
	store    vector.Store
}

func NewVectorIndexer(embedder vector.Embedder, store vector.Store) *VectorIndexer {
	return &VectorIndexer{
		embedder: embedder,
		store:    store,
	}
}

func (v *VectorIndexer) IndexAnalysis(ctx context.Context, document domain.Document, analysis domain.Analysis) error {
	records := buildVectorRecords(document, analysis)
	if len(records) == 0 {
		return nil
	}

	texts := make([]string, 0, len(records))
	for _, record := range records {
		texts = append(texts, record.text)
	}
	vectors, err := v.embedder.EmbedTexts(ctx, texts)
	if err != nil {
		return err
	}
	if len(vectors) != len(records) {
		return fmt.Errorf("vector count mismatch: got %d vectors for %d records", len(vectors), len(records))
	}

	points := make([]vector.Point, 0, len(records))
	for idx, record := range records {
		points = append(points, vector.Point{
			ID:      record.id,
			Vector:  vectors[idx],
			Payload: record.payload,
		})
	}
	return v.store.Upsert(ctx, points)
}

type vectorRecord struct {
	id      string
	text    string
	payload map[string]any
}

func buildVectorRecords(document domain.Document, analysis domain.Analysis) []vectorRecord {
	records := make([]vectorRecord, 0, 1+len(analysis.Findings)+len(analysis.DocumentSections))
	timestamp := analysis.CreatedAt.UTC().Format(time.RFC3339)

	if summary := strings.TrimSpace(analysis.Summary); summary != "" {
		records = append(records, vectorRecord{
			id:   "summary:" + analysis.ID,
			text: strings.TrimSpace(document.Name + "\n" + summary),
			payload: map[string]any{
				"kind":                 "analysis_summary",
				"analysis_id":          analysis.ID,
				"document_id":          document.ID,
				"document_external_id": document.ExternalID,
				"document_name":        document.Name,
				"review_key":           document.ReviewKey,
				"mode":                 string(analysis.Mode),
				"target_section_id":    analysis.TargetSectionID,
				"target_section_title": analysis.TargetSectionTitle,
				"created_at":           timestamp,
			},
		})
	}

	for idx, finding := range analysis.Findings {
		text := strings.TrimSpace(strings.Join([]string{
			document.Name,
			finding.SectionTitle,
			finding.Role,
			finding.Category,
			string(finding.Severity),
			finding.Problem,
			finding.WhyItIsBad,
			finding.HowToFix,
		}, "\n"))
		records = append(records, vectorRecord{
			id:   fmt.Sprintf("finding:%s:%d", analysis.ID, idx),
			text: text,
			payload: map[string]any{
				"kind":                 "finding",
				"analysis_id":          analysis.ID,
				"document_id":          document.ID,
				"document_external_id": document.ExternalID,
				"review_key":           document.ReviewKey,
				"mode":                 string(analysis.Mode),
				"role":                 finding.Role,
				"category":             finding.Category,
				"severity":             string(finding.Severity),
				"problem":              finding.Problem,
				"section_id":           finding.SectionID,
				"section_title":        finding.SectionTitle,
				"target_section_id":    analysis.TargetSectionID,
				"target_section_title": analysis.TargetSectionTitle,
				"created_at":           timestamp,
			},
		})
	}

	for idx, section := range analysis.DocumentSections {
		content := strings.TrimSpace(section.Content)
		if content == "" {
			continue
		}
		content = safeVectorTruncate(content, 2000)
		text := strings.TrimSpace(strings.Join([]string{
			document.Name,
			section.Title,
			content,
		}, "\n"))
		records = append(records, vectorRecord{
			id:   fmt.Sprintf("section:%s:%d", analysis.ID, idx),
			text: text,
			payload: map[string]any{
				"kind":                 "document_section",
				"analysis_id":          analysis.ID,
				"document_id":          document.ID,
				"document_external_id": document.ExternalID,
				"review_key":           document.ReviewKey,
				"section_id":           section.ID,
				"section_title":        section.Title,
				"target_section_id":    analysis.TargetSectionID,
				"target_section_title": analysis.TargetSectionTitle,
				"created_at":           timestamp,
			},
		})
	}

	return records
}

func safeVectorTruncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}
