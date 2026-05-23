package repository

import (
	"context"

	"technical-specification-review-agent/internal/domain"
)

type AnalysisRepository interface {
	Save(ctx context.Context, analysis domain.Analysis) error
	GetByID(ctx context.Context, id string) (domain.Analysis, error)
}

type DocumentRepository interface {
	Save(ctx context.Context, document domain.Document) error
	GetByID(ctx context.Context, id string) (domain.Document, error)
}
