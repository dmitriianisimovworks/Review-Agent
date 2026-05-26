package google

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

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

func (p *DriveCommentPublisher) Publish(ctx context.Context, documentExternalID string, comments []CommentDraft) ([]PublishedComment, error) {
	published := make([]PublishedComment, 0, len(comments))
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

		created, err := p.driveService.Comments.
			Create(documentExternalID, body).
			Fields("id,content,anchor,quotedFileContent").
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("create drive comment: %w", err)
		}
		log.Printf(
			"drive comment published: document_id=%q type=%q requested_anchor_line=%v returned_anchor=%q quoted=%q",
			documentExternalID,
			draft.Type,
			draft.AnchorLine,
			created.Anchor,
			draft.QuotedContent,
		)
		published = append(published, PublishedComment{ID: created.Id, Type: draft.Type})
	}

	return published, nil
}

func (p *DriveCommentPublisher) List(ctx context.Context, documentExternalID string) ([]Comment, error) {
	comments := make([]Comment, 0)
	pageToken := ""
	for {
		call := p.driveService.Comments.
			List(documentExternalID).
			Fields("nextPageToken,comments(id,content,createdTime,resolved,author/me)").
			Context(ctx)
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("list drive comments: %w", err)
		}

		for _, item := range resp.Comments {
			createdAt := int64(0)
			if parsed, err := time.Parse(time.RFC3339, item.CreatedTime); err == nil {
				createdAt = parsed.UTC().UnixNano()
			}
			comments = append(comments, Comment{
				ID:         item.Id,
				Content:    item.Content,
				CreatedAt:  createdAt,
				Resolved:   item.Resolved,
				AuthorIsMe: item.Author != nil && item.Author.Me,
			})
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt > comments[j].CreatedAt
	})
	return comments, nil
}

func (p *DriveCommentPublisher) Delete(ctx context.Context, documentExternalID string, commentIDs []string) error {
	for _, commentID := range commentIDs {
		if commentID == "" {
			continue
		}
		if err := p.driveService.Comments.Delete(documentExternalID, commentID).Context(ctx).Do(); err != nil {
			return fmt.Errorf("delete drive comment %s: %w", commentID, err)
		}
	}
	return nil
}
