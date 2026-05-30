package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"technical-specification-review-agent/internal/domain"
)

type TrackedDocumentRepository struct {
	pool *pgxpool.Pool
}

func NewTrackedDocumentRepository(pool *pgxpool.Pool) *TrackedDocumentRepository {
	return &TrackedDocumentRepository{pool: pool}
}

func (r *TrackedDocumentRepository) Save(ctx context.Context, document domain.TrackedDocument) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tracked_documents (
			id, source, external_id, name, document_url, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (source, external_id) DO UPDATE
		SET name = EXCLUDED.name,
		    document_url = EXCLUDED.document_url
	`, document.ID, document.Source, document.ExternalID, document.Name, document.DocumentURL, document.CreatedAt)
	if err != nil {
		return fmt.Errorf("save tracked document: %w", err)
	}
	return nil
}

func (r *TrackedDocumentRepository) ListBySource(ctx context.Context, source domain.DocumentSource) ([]domain.TrackedDocument, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, source, external_id, name, document_url, created_at
		FROM tracked_documents
		WHERE source = $1
		ORDER BY created_at ASC
	`, source)
	if err != nil {
		return nil, fmt.Errorf("list tracked documents by source: %w", err)
	}
	defer rows.Close()

	result := make([]domain.TrackedDocument, 0)
	for rows.Next() {
		var item domain.TrackedDocument
		if err := rows.Scan(
			&item.ID,
			&item.Source,
			&item.ExternalID,
			&item.Name,
			&item.DocumentURL,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tracked document: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tracked documents: %w", err)
	}
	return result, nil
}
