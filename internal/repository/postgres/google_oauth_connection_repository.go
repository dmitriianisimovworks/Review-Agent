package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"technical-specification-review-agent/internal/domain"
)

type GoogleOAuthConnectionRepository struct {
	pool *pgxpool.Pool
}

func NewGoogleOAuthConnectionRepository(pool *pgxpool.Pool) *GoogleOAuthConnectionRepository {
	return &GoogleOAuthConnectionRepository{pool: pool}
}

func (r *GoogleOAuthConnectionRepository) Save(ctx context.Context, connection domain.GoogleOAuthConnection) error {
	now := time.Now().UTC()
	if connection.CreatedAt.IsZero() {
		connection.CreatedAt = now
	}
	connection.UpdatedAt = now

	_, err := r.pool.Exec(ctx, `
		INSERT INTO google_oauth_connections (
			id, google_user_id, email, access_token, refresh_token, expiry, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (google_user_id) DO UPDATE SET
			email = EXCLUDED.email,
			access_token = EXCLUDED.access_token,
			refresh_token = COALESCE(NULLIF(EXCLUDED.refresh_token, ''), google_oauth_connections.refresh_token),
			expiry = EXCLUDED.expiry,
			updated_at = EXCLUDED.updated_at
	`, connection.ID, connection.GoogleUserID, connection.Email, connection.AccessToken, nullString(connection.RefreshToken), connection.Expiry, connection.CreatedAt, connection.UpdatedAt)
	return err
}

func (r *GoogleOAuthConnectionRepository) GetByGoogleUserID(ctx context.Context, googleUserID string) (domain.GoogleOAuthConnection, error) {
	var connection domain.GoogleOAuthConnection
	err := r.pool.QueryRow(ctx, `
		SELECT id, google_user_id, email, access_token, COALESCE(refresh_token, ''), expiry, created_at, updated_at
		FROM google_oauth_connections
		WHERE google_user_id = $1
	`, googleUserID).Scan(
		&connection.ID,
		&connection.GoogleUserID,
		&connection.Email,
		&connection.AccessToken,
		&connection.RefreshToken,
		&connection.Expiry,
		&connection.CreatedAt,
		&connection.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.GoogleOAuthConnection{}, errNotFound
		}
		return domain.GoogleOAuthConnection{}, err
	}

	return connection, nil
}
