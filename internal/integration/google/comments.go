package google

import "context"

type Document struct {
	ExternalID string
	Title      string
	Content    string
	Sections   []Section
	Blocks     []Block
}

type Range struct {
	StartIndex int64
	EndIndex   int64
}

type Section struct {
	ID      string
	Title   string
	Level   int
	Range   Range
	Content string
}

type Block struct {
	Kind         string
	Text         string
	Range        Range
	HeadingLevel int
	ListLevel    int
	SectionID    string
	SectionTitle string
}

type DocumentReader interface {
	Read(ctx context.Context, documentURL string) (Document, error)
}

type CommentDraft struct {
	Content       string
	AnchorLine    *int
	QuotedContent string
	Type          string
}

type CommentPublisher interface {
	Publish(ctx context.Context, documentExternalID string, comments []CommentDraft) error
}

type NoopDocumentReader struct{}

func NewNoopDocumentReader() *NoopDocumentReader {
	return &NoopDocumentReader{}
}

func (r *NoopDocumentReader) Read(_ context.Context, _ string) (Document, error) {
	return Document{}, nil
}

type NoopCommentPublisher struct{}

func NewNoopCommentPublisher() *NoopCommentPublisher {
	return &NoopCommentPublisher{}
}

func (p *NoopCommentPublisher) Publish(_ context.Context, _ string, _ []CommentDraft) error {
	return nil
}
