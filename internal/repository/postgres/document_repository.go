package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"technical-specification-review-agent/internal/domain"
)

type DocumentRepository struct {
	pool *pgxpool.Pool
}

func NewDocumentRepository(pool *pgxpool.Pool) *DocumentRepository {
	return &DocumentRepository{pool: pool}
}

func (r *DocumentRepository) Save(ctx context.Context, document domain.Document) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO documents (
			id, source, name, external_id, review_key, raw_content, normalized_content, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, document.ID, document.Source, document.Name, nullString(document.ExternalID), document.ReviewKey, document.RawContent, document.NormalizedContent, document.CreatedAt)
	return err
}

func (r *DocumentRepository) Update(ctx context.Context, document domain.Document) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE documents
		SET name = $2,
		    external_id = $3,
		    review_key = $4,
		    raw_content = $5,
		    normalized_content = $6
		WHERE id = $1
	`, document.ID, document.Name, nullString(document.ExternalID), document.ReviewKey, document.RawContent, document.NormalizedContent)
	return err
}

func (r *DocumentRepository) GetByID(ctx context.Context, id string) (domain.Document, error) {
	var document domain.Document
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, source, COALESCE(external_id, ''), review_key, raw_content, normalized_content, created_at
		FROM documents
		WHERE id = $1
	`, id).Scan(
		&document.ID,
		&document.Name,
		&document.Source,
		&document.ExternalID,
		&document.ReviewKey,
		&document.RawContent,
		&document.NormalizedContent,
		&document.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Document{}, errNotFound
		}
		return domain.Document{}, err
	}

	return document, nil
}
