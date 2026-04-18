package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chempik1234/room-service-proxy/internal/models"
	"github.com/chempik1234/room-service-proxy/internal/ports"
)

// PostgresAuthTokenStorage implements AuthTokenStorage using PostgreSQL
type PostgresAuthTokenStorage struct {
	db *pgxpool.Pool
}

// NewPostgresAuthTokenStorage creates a new PostgreSQL auth token storage
func NewPostgresAuthTokenStorage(db *pgxpool.Pool) (*PostgresAuthTokenStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("database pool cannot be nil")
	}
	return &PostgresAuthTokenStorage{db: db}, nil
}

// CreateToken stores a new authentication token
func (s *PostgresAuthTokenStorage) CreateToken(ctx context.Context, token *models.AuthToken) error {
	query := `
		INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
	`
	_, err := s.db.Exec(ctx, query, token.Token, token.UserID, token.ExpiresAt)
	return err
}

// GetToken retrieves an auth token by its value
func (s *PostgresAuthTokenStorage) GetToken(ctx context.Context, token string) (*models.AuthToken, error) {
	var authToken models.AuthToken
	query := `
		SELECT token, user_id, expires_at, created_at
		FROM auth_tokens
		WHERE token = $1 AND expires_at > NOW()
	`
	err := s.db.QueryRow(ctx, query, token).Scan(
		&authToken.Token, &authToken.UserID, &authToken.ExpiresAt, &authToken.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &authToken, nil
}

// DeleteToken removes a specific token from storage
func (s *PostgresAuthTokenStorage) DeleteToken(ctx context.Context, token string) error {
	result, err := s.db.Exec(ctx, "DELETE FROM auth_tokens WHERE token = $1", token)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// DeleteExpiredTokens removes all expired tokens from storage
func (s *PostgresAuthTokenStorage) DeleteExpiredTokens(ctx context.Context) error {
	_, err := s.db.Exec(ctx, "DELETE FROM auth_tokens WHERE expires_at <= NOW()")
	return err
}
