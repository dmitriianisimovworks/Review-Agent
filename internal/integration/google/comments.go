package google

import "context"

type Document struct {
	ExternalID string
	Title      string
	Content    string
}

type DocumentReader interface {
	Read(ctx context.Context, documentURL string) (Document, error)
}

type CommentPublisher interface {
	Publish(ctx context.Context, documentExternalID string, comments []string) error
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

func (p *NoopCommentPublisher) Publish(_ context.Context, _ string, _ []string) error {
	return nil
}
