package repository

import (
	"context"

	"technical-specification-review-agent/internal/domain"
)

type AnalysisRepository interface {
	Create(ctx context.Context, analysis domain.Analysis) error
	MarkStatus(ctx context.Context, id string, status domain.AnalysisStatus, errorMessage string) error
	Complete(ctx context.Context, analysis domain.Analysis) error
	Save(ctx context.Context, analysis domain.Analysis) error
	GetByID(ctx context.Context, id string) (domain.Analysis, error)
	ListByReviewKey(ctx context.Context, reviewKey string, limit int) ([]domain.Analysis, error)
}

type DocumentRepository interface {
	Save(ctx context.Context, document domain.Document) error
	Update(ctx context.Context, document domain.Document) error
	GetByID(ctx context.Context, id string) (domain.Document, error)
}

type GoogleOAuthConnectionRepository interface {
	Save(ctx context.Context, connection domain.GoogleOAuthConnection) error
	GetByGoogleUserID(ctx context.Context, googleUserID string) (domain.GoogleOAuthConnection, error)
}

type AnalysisCache interface {
	Set(ctx context.Context, analysis domain.Analysis) error
	Get(ctx context.Context, id string) (domain.Analysis, bool, error)
	Delete(ctx context.Context, id string) error
}
