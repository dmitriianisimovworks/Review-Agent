package parser

import (
	"context"
	"fmt"
	"strings"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
)

type ParseInput struct {
	Content  string
	Sections []domain.DocumentSection
}

type ParsedChunk struct {
	Text         string
	SectionID    string
	SectionTitle string
	SectionLevel int
	Range        domain.DocumentRange
}

type ParsedDocument struct {
	Text             string
	Chunks           []string
	Sections         []domain.DocumentSection
	ChunkDescriptors []ParsedChunk
}

type DocumentParser interface {
	Parse(ctx context.Context, input ParseInput) (ParsedDocument, error)
}

type ChunkingParser struct {
	chunkSize int
	maxChunks int
}

func NewChunkingParser(cfg config.DocumentConfig) *ChunkingParser {
	return &ChunkingParser{
		chunkSize: cfg.ChunkSize,
		maxChunks: cfg.MaxChunks,
	}
}

func (p *ChunkingParser) Parse(_ context.Context, input ParseInput) (ParsedDocument, error) {
	normalized := normalizeWhitespace(input.Content)
	sections := normalizeSections(input.Sections)

	if len(sections) == 0 {
		chunks := splitIntoChunks(normalized, p.chunkSize, p.maxChunks)
		return ParsedDocument{
			Text:   normalized,
			Chunks: chunks,
		}, nil
	}

	descriptors := splitSectionsIntoChunks(sections, p.chunkSize, p.maxChunks)
	chunks := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		chunks = append(chunks, descriptor.Text)
	}

	return ParsedDocument{
		Text:             normalized,
		Chunks:           chunks,
		Sections:         sections,
		ChunkDescriptors: descriptors,
	}, nil
}

func normalizeWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(clean) == 0 || clean[len(clean)-1] == "" {
				continue
			}
			clean = append(clean, "")
			continue
		}

		clean = append(clean, strings.Join(strings.Fields(trimmed), " "))
	}

	return strings.TrimSpace(strings.Join(clean, "\n"))
}

func normalizeSections(sections []domain.DocumentSection) []domain.DocumentSection {
	result := make([]domain.DocumentSection, 0, len(sections))
	for idx, section := range sections {
		section.Content = normalizeWhitespace(section.Content)
		section.Title = strings.TrimSpace(section.Title)
		if section.Content == "" {
			continue
		}
		if section.ID == "" {
			section.ID = fmt.Sprintf("section_%d", idx+1)
		}
		if section.Title == "" {
			section.Title = fmt.Sprintf("Section %d", idx+1)
		}
		result = append(result, section)
	}
	return result
}

func splitIntoChunks(content string, chunkSize, maxChunks int) []string {
	if content == "" {
		return []string{""}
	}

	if chunkSize <= 0 {
		chunkSize = 5000
	}

	paragraphs := strings.Split(content, "\n\n")
	chunks := make([]string, 0)
	var current strings.Builder

	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, text)
		}
		current.Reset()
	}

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		if current.Len() == 0 {
			current.WriteString(paragraph)
		} else if current.Len()+2+len(paragraph) <= chunkSize {
			current.WriteString("\n\n")
			current.WriteString(paragraph)
		} else {
			flush()
			current.WriteString(paragraph)
		}

		if maxChunks > 0 && len(chunks) >= maxChunks {
			return chunks
		}
	}

	flush()

	if len(chunks) == 0 {
		return []string{content}
	}

	if maxChunks > 0 && len(chunks) > maxChunks {
		return chunks[:maxChunks]
	}

	return chunks
}

func splitSectionsIntoChunks(sections []domain.DocumentSection, chunkSize, maxChunks int) []ParsedChunk {
	if chunkSize <= 0 {
		chunkSize = 5000
	}

	result := make([]ParsedChunk, 0)
	for _, section := range sections {
		if maxChunks > 0 && len(result) >= maxChunks {
			return result[:maxChunks]
		}

		sectionChunks := splitIntoChunks(section.Content, chunkSize, 0)
		for _, chunk := range sectionChunks {
			result = append(result, ParsedChunk{
				Text:         chunk,
				SectionID:    section.ID,
				SectionTitle: section.Title,
				SectionLevel: section.Level,
				Range:        section.Range,
			})
			if maxChunks > 0 && len(result) >= maxChunks {
				return result[:maxChunks]
			}
		}
	}

	return result
}

var _ DocumentParser = (*ChunkingParser)(nil)
