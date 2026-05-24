package google

import (
	"context"
	"fmt"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	"technical-specification-review-agent/internal/comment"
)

type DriveCommentPublisher struct {
	driveService *drive.Service
}

func NewDriveCommentPublisher(ctx context.Context, credentialsFile string) (*DriveCommentPublisher, error) {
	service, err := drive.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create drive comment publisher: %w", err)
	}

	return &DriveCommentPublisher{driveService: service}, nil
}

func (p *DriveCommentPublisher) Publish(ctx context.Context, documentExternalID string, comments []CommentDraft) error {
	for _, draft := range comments {
		body := &drive.Comment{
			Content: draft.Content,
		}
		if draft.AnchorLine != nil {
			body.Anchor = comment.MarshalAnchor(*draft.AnchorLine)
		}
		if draft.QuotedContent != "" {
			body.QuotedFileContent = &drive.CommentQuotedFileContent{
				MimeType: "text/plain",
				Value:    draft.QuotedContent,
			}
		}

		if _, err := p.driveService.Comments.
			Create(documentExternalID, body).
			Fields("id,content,anchor,quotedFileContent").
			Context(ctx).
			Do(); err != nil {
			return fmt.Errorf("create drive comment: %w", err)
		}
	}

	return nil
}
