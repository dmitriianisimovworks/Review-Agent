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

type Comment struct {
	ID         string
	Content    string
	CreatedAt  int64
	Resolved   bool
	AuthorIsMe bool
}

type PublishedComment struct {
	ID   string
	Type string
}

type CommentReader interface {
	List(ctx context.Context, documentExternalID string) ([]Comment, error)
}

type CommentPublisher interface {
	Publish(ctx context.Context, documentExternalID string, comments []CommentDraft) ([]PublishedComment, error)
	Delete(ctx context.Context, documentExternalID string, commentIDs []string) error
}

type NoopDocumentReader struct{}

func NewNoopDocumentReader() *NoopDocumentReader {
	return &NoopDocumentReader{}
}

func (r *NoopDocumentReader) Read(_ context.Context, _ string) (Document, error) {
	return Document{}, nil
}

type NoopCommentReader struct{}

func NewNoopCommentReader() *NoopCommentReader {
	return &NoopCommentReader{}
}

func (r *NoopCommentReader) List(_ context.Context, _ string) ([]Comment, error) {
	return nil, nil
}

type NoopCommentPublisher struct{}

func NewNoopCommentPublisher() *NoopCommentPublisher {
	return &NoopCommentPublisher{}
}

func (p *NoopCommentPublisher) Publish(_ context.Context, _ string, _ []CommentDraft) ([]PublishedComment, error) {
	return nil, nil
}

func (p *NoopCommentPublisher) Delete(_ context.Context, _ string, _ []string) error {
	return nil
}
