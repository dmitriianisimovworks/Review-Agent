package parser

import (
	"context"
	"strings"

	"technical-specification-review-agent/internal/config"
)

type ParsedDocument struct {
	Text   string
	Chunks []string
}

type DocumentParser interface {
	Parse(ctx context.Context, content string) (ParsedDocument, error)
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

func (p *ChunkingParser) Parse(_ context.Context, content string) (ParsedDocument, error) {
	normalized := normalizeWhitespace(content)
	chunks := splitIntoChunks(normalized, p.chunkSize, p.maxChunks)

	return ParsedDocument{
		Text:   normalized,
		Chunks: chunks,
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

var _ DocumentParser = (*ChunkingParser)(nil)
