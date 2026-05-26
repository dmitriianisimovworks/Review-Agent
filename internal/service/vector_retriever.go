package service

import (
	"context"
	"fmt"
	"strings"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/vector"
	"technical-specification-review-agent/internal/parser"
)

type AnalysisVectorRetriever interface {
	EnrichMemory(ctx context.Context, document domain.Document, parsed parser.ParsedDocument, memory domain.ReviewMemory) (domain.ReviewMemory, error)
}

type VectorRetriever struct {
	embedder vector.Embedder
	store    vector.Store
}

func NewVectorRetriever(embedder vector.Embedder, store vector.Store) *VectorRetriever {
	return &VectorRetriever{
		embedder: embedder,
		store:    store,
	}
}

func (v *VectorRetriever) EnrichMemory(ctx context.Context, document domain.Document, parsed parser.ParsedDocument, memory domain.ReviewMemory) (domain.ReviewMemory, error) {
	queryText := buildVectorQueryText(document, parsed)
	if strings.TrimSpace(queryText) == "" {
		return memory, nil
	}

	vectors, err := v.embedder.EmbedTexts(ctx, []string{queryText})
	if err != nil {
		return memory, err
	}
	if len(vectors) != 1 {
		return memory, fmt.Errorf("vector retrieval expected 1 embedding, got %d", len(vectors))
	}

	results, err := v.store.Search(ctx, vector.SearchRequest{
		Vector:           vectors[0],
		Limit:            4,
		Kinds:            []string{"finding", "analysis_summary", "document_section"},
		ExcludeReviewKey: document.ReviewKey,
	})
	if err != nil {
		return memory, err
	}
	if len(results) == 0 {
		return memory, nil
	}

	hints := append([]string(nil), memory.VectorHints...)
	seen := make(map[string]struct{}, len(hints))
	for _, hint := range hints {
		seen[strings.ToLower(strings.TrimSpace(hint))] = struct{}{}
	}

	for _, result := range results {
		hint := vectorResultHint(result)
		if hint == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(hint))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		hints = append(hints, hint)
	}

	memory.VectorHints = limitUniqueStrings(hints, 4)
	return memory, nil
}

func buildVectorQueryText(document domain.Document, parsed parser.ParsedDocument) string {
	parts := make([]string, 0, 4)
	if name := strings.TrimSpace(document.Name); name != "" {
		parts = append(parts, name)
	}

	for idx, section := range parsed.Sections {
		if idx >= 2 {
			break
		}
		if title := strings.TrimSpace(section.Title); title != "" {
			parts = append(parts, title)
		}
	}

	text := strings.TrimSpace(parsed.Text)
	if text == "" {
		text = strings.TrimSpace(document.NormalizedContent)
	}
	if text != "" {
		parts = append(parts, safeVectorTruncate(text, 1000))
	}

	return strings.Join(parts, "\n")
}

func vectorResultHint(result vector.SearchResult) string {
	payload := result.Payload
	kind := strings.TrimSpace(stringPayload(payload, "kind"))
	switch kind {
	case "finding":
		problem := strings.TrimSpace(stringPayload(payload, "problem"))
		if problem == "" {
			return ""
		}
		role := strings.TrimSpace(stringPayload(payload, "role"))
		severity := strings.TrimSpace(stringPayload(payload, "severity"))
		section := strings.TrimSpace(stringPayload(payload, "section_title"))
		prefix := "Похожий прошлый сигнал"
		details := make([]string, 0, 3)
		if role != "" {
			details = append(details, role)
		}
		if severity != "" {
			details = append(details, severity)
		}
		if section != "" {
			details = append(details, section)
		}
		if len(details) > 0 {
			prefix += " [" + strings.Join(details, " | ") + "]"
		}
		return safeTruncate(prefix+": "+problem, 220)
	case "analysis_summary":
		summary := strings.TrimSpace(stringPayload(payload, "summary"))
		if summary == "" {
			return ""
		}
		return safeTruncate("Похожий прошлый итог: "+summary, 220)
	case "document_section":
		section := strings.TrimSpace(stringPayload(payload, "section_title"))
		content := strings.TrimSpace(stringPayload(payload, "content"))
		if section == "" && content == "" {
			return ""
		}
		base := "Похожий прошлый раздел"
		if section != "" {
			base += " [" + section + "]"
		}
		if content != "" {
			base += ": " + content
		}
		return safeTruncate(base, 220)
	default:
		return ""
	}
}

func stringPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, exists := payload[key]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
