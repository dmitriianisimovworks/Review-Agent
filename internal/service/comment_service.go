package service

import (
	"context"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/repository"
)

type CommentService struct {
	documentRepo     repository.DocumentRepository
	analysisRepo     repository.AnalysisRepository
	commentFormatter comment.Formatter
	commentPublisher google.CommentPublisher
}

type PublishCommentsInput struct {
	AnalysisID string
	Mode       comment.PublishMode
}

type PublishCommentsResult struct {
	AnalysisID     string `json:"analysis_id"`
	DocumentID     string `json:"document_id"`
	DocumentSource string `json:"document_source"`
	PublishedCount int    `json:"published_count"`
	PublishMode    string `json:"publish_mode"`
}

func NewCommentService(
	documentRepo repository.DocumentRepository,
	analysisRepo repository.AnalysisRepository,
	commentFormatter comment.Formatter,
	commentPublisher google.CommentPublisher,
) *CommentService {
	return &CommentService{
		documentRepo:     documentRepo,
		analysisRepo:     analysisRepo,
		commentFormatter: commentFormatter,
		commentPublisher: commentPublisher,
	}
}

func (s *CommentService) PublishComments(ctx context.Context, input PublishCommentsInput) (PublishCommentsResult, error) {
	mode := input.Mode
	if mode == "" {
		mode = comment.PublishModeBoth
	}

	analysis, err := s.analysisRepo.GetByID(ctx, input.AnalysisID)
	if err != nil {
		return PublishCommentsResult{}, apperrors.Wrap(apperrors.KindNotFound, "analysis not found", err)
	}

	document, err := s.documentRepo.GetByID(ctx, analysis.DocumentID)
	if err != nil {
		return PublishCommentsResult{}, apperrors.Wrap(apperrors.KindNotFound, "document not found", err)
	}
	if document.Source != domain.DocumentSourceGoogleDocs {
		return PublishCommentsResult{}, apperrors.New(apperrors.KindInvalidArgument, "comments can be published only for google_docs source")
	}
	if document.ExternalID == "" {
		return PublishCommentsResult{}, apperrors.New(apperrors.KindInvalidArgument, "google document external id is missing")
	}

	drafts := s.commentFormatter.Format(document, analysis, mode)
	payload := make([]google.CommentDraft, 0, len(drafts))
	for _, draft := range drafts {
		payload = append(payload, google.CommentDraft{
			Content:       draft.Content,
			AnchorLine:    draft.AnchorLine,
			QuotedContent: draft.QuotedContent,
			Type:          draft.Type,
		})
	}

	if err := s.commentPublisher.Publish(ctx, document.ExternalID, payload); err != nil {
		return PublishCommentsResult{}, apperrors.Wrap(apperrors.KindDependency, "failed to publish comments to google docs", err)
	}

	return PublishCommentsResult{
		AnalysisID:     analysis.ID,
		DocumentID:     document.ID,
		DocumentSource: string(document.Source),
		PublishedCount: len(payload),
		PublishMode:    string(mode),
	}, nil
}
