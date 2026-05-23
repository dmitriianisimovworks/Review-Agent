package google

import "context"

type CommentPublisher interface {
	Publish(ctx context.Context, documentExternalID string, comments []string) error
}

type NoopCommentPublisher struct{}

func NewNoopCommentPublisher() *NoopCommentPublisher {
	return &NoopCommentPublisher{}
}

func (p *NoopCommentPublisher) Publish(_ context.Context, _ string, _ []string) error {
	return nil
}
