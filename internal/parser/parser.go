package parser

import "context"

type ParsedDocument struct {
	Text   string
	Chunks []string
}

type DocumentParser interface {
	Parse(ctx context.Context, content string) (ParsedDocument, error)
}

type NoopParser struct{}

func NewNoopParser() *NoopParser {
	return &NoopParser{}
}

func (p *NoopParser) Parse(_ context.Context, content string) (ParsedDocument, error) {
	return ParsedDocument{
		Text:   content,
		Chunks: []string{content},
	}, nil
}
