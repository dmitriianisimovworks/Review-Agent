package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/repository"
)

type TrackedDocumentService struct {
	repo   repository.TrackedDocumentRepository
	reader google.DocumentReader
}

type RegisterTrackedGoogleDocInput struct {
	DocumentURL string
}

func NewTrackedDocumentService(
	repo repository.TrackedDocumentRepository,
	reader google.DocumentReader,
) *TrackedDocumentService {
	return &TrackedDocumentService{
		repo:   repo,
		reader: reader,
	}
}

func (s *TrackedDocumentService) RegisterGoogleDoc(ctx context.Context, input RegisterTrackedGoogleDocInput) (domain.TrackedDocument, error) {
	if s == nil || s.repo == nil || s.reader == nil {
		return domain.TrackedDocument{}, apperrors.New(apperrors.KindInternal, "tracked document service is not configured")
	}

	documentURL := strings.TrimSpace(input.DocumentURL)
	if documentURL == "" {
		return domain.TrackedDocument{}, apperrors.New(apperrors.KindInvalidArgument, "google_doc_url is required")
	}

	doc, err := s.reader.Read(ctx, documentURL)
	if err != nil {
		return domain.TrackedDocument{}, apperrors.Wrap(apperrors.KindDependency, "failed to read google doc", err)
	}
	if strings.TrimSpace(doc.ExternalID) == "" {
		return domain.TrackedDocument{}, apperrors.New(apperrors.KindDependency, "google doc external id is missing")
	}

	tracked := domain.TrackedDocument{
		ID:          fmt.Sprintf("tracked_doc_%d", time.Now().UTC().UnixNano()),
		Source:      domain.DocumentSourceGoogleDocs,
		ExternalID:  strings.TrimSpace(doc.ExternalID),
		Name:        strings.TrimSpace(doc.Title),
		DocumentURL: fmt.Sprintf("https://docs.google.com/document/d/%s/edit", strings.TrimSpace(doc.ExternalID)),
		CreatedAt:   time.Now().UTC(),
	}
	if tracked.Name == "" {
		tracked.Name = "Google Doc"
	}

	if err := s.repo.Save(ctx, tracked); err != nil {
		return domain.TrackedDocument{}, apperrors.Wrap(apperrors.KindInternal, "failed to store tracked document", err)
	}
	return tracked, nil
}
