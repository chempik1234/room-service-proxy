package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chempik1234/room-service-proxy/internal/models"
	"github.com/chempik1234/room-service-proxy/internal/ports"
)

// PostgresUserStorage implements UserStorage using PostgreSQL
type PostgresUserStorage struct {
	db *pgxpool.Pool
}

// NewPostgresUserStorage creates a new PostgreSQL user storage
func NewPostgresUserStorage(db *pgxpool.Pool) (*PostgresUserStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("database pool cannot be nil")
	}
	return &PostgresUserStorage{db: db}, nil
}

// CreateUser creates a new user in PostgreSQL
func (s *PostgresUserStorage) CreateUser(ctx context.Context, user *models.User) error {
	query := `
		INSERT INTO users (id, email, password_hash, name, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (email) DO NOTHING
	`
	_, err := s.db.Exec(ctx, query, user.ID, user.Email, user.PasswordHash, user.Name, user.Role)
	return err
}

// GetUserByEmail retrieves a user by email
func (s *PostgresUserStorage) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	query := `SELECT id, email, password_hash, name, role, created_at, updated_at FROM users WHERE email = $1`
	err := s.db.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID retrieves a user by ID
func (s *PostgresUserStorage) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	query := `SELECT id, email, password_hash, name, role, created_at, updated_at FROM users WHERE id = $1`
	err := s.db.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ListUsers retrieves all users
func (s *PostgresUserStorage) ListUsers(ctx context.Context) ([]*models.User, error) {
	query := `SELECT id, email, password_hash, name, role, created_at, updated_at FROM users ORDER BY created_at DESC`
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	return users, rows.Err()
}

// UpdateUser updates an existing user
func (s *PostgresUserStorage) UpdateUser(ctx context.Context, user *models.User) error {
	query := `
		UPDATE users
		SET name = $2, updated_at = NOW()
		WHERE id = $1
	`
	result, err := s.db.Exec(ctx, query, user.ID, user.Name)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// DeleteUser removes a user from storage
func (s *PostgresUserStorage) DeleteUser(ctx context.Context, userID string) error {
	result, err := s.db.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}
