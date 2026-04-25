package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTenantStorage implements TenantStorage using PostgreSQL
type PostgresTenantStorage struct {
	db *pgxpool.Pool
}

// NewPostgresTenantStorage creates a new PostgreSQL tenant storage adapter
func NewPostgresTenantStorage(db *pgxpool.Pool) (*PostgresTenantStorage, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection pool cannot be nil")
	}
	return &PostgresTenantStorage{db: db}, nil
}

// NewPostgresTenantStorageFromURL creates a new PostgreSQL tenant storage adapter from connection URL
func NewPostgresTenantStorageFromURL(dbURL string) (*PostgresTenantStorage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return &PostgresTenantStorage{db: db}, nil
}

// CreateTenant creates a new tenant
func (s *PostgresTenantStorage) CreateTenant(ctx context.Context, tenant *ports.Tenant) error {
	now := time.Now()
	tenant.ID = generateTenantID(tenant.Name)
	tenant.APIKey = generateAPIKey(tenant.ID)
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	// Log tenant creation for debugging
	fmt.Printf("🆕 Creating tenant: name=%q, id=%s, apiKey=%s\n", tenant.Name, tenant.ID, tenant.APIKey)

	// Set defaults
	if tenant.Status == "" {
		tenant.Status = "active"
	}
	if tenant.Plan == "" {
		tenant.Plan = "free"
	}
	if tenant.MaxRooms == 0 {
		tenant.MaxRooms = 50 // Free tier default
	}
	if tenant.MaxRPS == 0 {
		tenant.MaxRPS = 100 // Free tier default
	}

	query := `
		INSERT INTO tenants (id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
	`

	err := s.db.QueryRow(ctx, query,
		tenant.ID, tenant.UserID, tenant.Name, tenant.Email, tenant.APIKey,
		tenant.Host, tenant.Port, tenant.Status, tenant.Plan,
		tenant.MaxRooms, tenant.MaxRPS, tenant.CreatedAt, tenant.UpdatedAt,
	).Scan(
		&tenant.ID, &tenant.UserID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	return nil
}

// GetTenant retrieves a tenant by ID
func (s *PostgresTenantStorage) GetTenant(ctx context.Context, tenantID string) (*ports.Tenant, error) {
	var t ports.Tenant
	query := `
		SELECT id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants WHERE id = $1
	`

	err := s.db.QueryRow(ctx, query, tenantID).Scan(
		&t.ID, &t.UserID, &t.Name, &t.Email, &t.APIKey,
		&t.Host, &t.Port, &t.Status, &t.Plan,
		&t.MaxRooms, &t.MaxRPS, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	return &t, nil
}

// GetTenantByAPIKey retrieves a tenant by their API key
func (s *PostgresTenantStorage) GetTenantByAPIKey(ctx context.Context, apiKey string) (*ports.Tenant, error) {
	var t ports.Tenant
	query := `
		SELECT id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants WHERE api_key = $1
	`

	err := s.db.QueryRow(ctx, query, apiKey).Scan(
		&t.ID, &t.UserID, &t.Name, &t.Email, &t.APIKey,
		&t.Host, &t.Port, &t.Status, &t.Plan,
		&t.MaxRooms, &t.MaxRPS, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	return &t, nil
}

// ListTenants retrieves all tenants from storage
func (s *PostgresTenantStorage) ListTenants(ctx context.Context) ([]*ports.Tenant, error) {
	query := `
		SELECT id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants ORDER BY created_at DESC
	`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*ports.Tenant
	for rows.Next() {
		var t ports.Tenant
		err := rows.Scan(
			&t.ID, &t.UserID, &t.Name, &t.Email, &t.APIKey,
			&t.Host, &t.Port, &t.Status, &t.Plan,
			&t.MaxRooms, &t.MaxRPS, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &t)
	}

	return tenants, nil
}

// ListTenantsByUserID retrieves all tenants for a specific user
func (s *PostgresTenantStorage) ListTenantsByUserID(ctx context.Context, userID string) ([]*ports.Tenant, error) {
	query := `
		SELECT id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants WHERE user_id = $1 ORDER BY created_at DESC
	`

	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants by user ID: %w", err)
	}
	defer rows.Close()

	var tenants []*ports.Tenant
	for rows.Next() {
		var t ports.Tenant
		err := rows.Scan(
			&t.ID, &t.UserID, &t.Name, &t.Email, &t.APIKey,
			&t.Host, &t.Port, &t.Status, &t.Plan,
			&t.MaxRooms, &t.MaxRPS, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &t)
	}

	return tenants, nil
}

// UpdateTenant updates an existing tenant record
func (s *PostgresTenantStorage) UpdateTenant(ctx context.Context, tenant *ports.Tenant) error {
	tenant.UpdatedAt = time.Now()

	query := `
		UPDATE tenants
		SET user_id = $2, name = $3, email = $4, api_key = $5, host = $6, port = $7,
		    status = $8, plan = $9, max_rooms = $10, max_rps = $11, updated_at = $12
		WHERE id = $1
		RETURNING id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
	`

	err := s.db.QueryRow(ctx, query,
		tenant.ID, tenant.UserID, tenant.Name, tenant.Email, tenant.APIKey,
		tenant.Host, tenant.Port, tenant.Status, tenant.Plan,
		tenant.MaxRooms, tenant.MaxRPS, tenant.UpdatedAt,
	).Scan(
		&tenant.ID, &tenant.UserID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	return nil
}

// DeleteTenant soft deletes a tenant by setting status to "deleted"
func (s *PostgresTenantStorage) DeleteTenant(ctx context.Context, tenantID string) error {
	query := `UPDATE tenants SET status = 'deleted' WHERE id = $1`

	result, err := s.db.Exec(ctx, query, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("tenant not found")
	}

	return nil
}

// RegenerateAPIKey generates a new API key for a tenant
func (s *PostgresTenantStorage) RegenerateAPIKey(ctx context.Context, tenantID string) (string, error) {
	// Verify tenant exists
	_, err := s.GetTenant(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("tenant not found: %w", err)
	}

	// Generate new API key
	newAPIKey := generateAPIKey(tenantID)

	// Update tenant with new API key
	query := `UPDATE tenants SET api_key = $2, updated_at = NOW() WHERE id = $1`
	_, err = s.db.Exec(ctx, query, tenantID, newAPIKey)
	if err != nil {
		return "", fmt.Errorf("failed to update API key: %w", err)
	}

	return newAPIKey, nil
}

// Helper functions

// generateTenantID generates a unique tenant ID from the tenant name
func generateTenantID(name string) string {
	// Convert name to lowercase and replace spaces with hyphens
	cleanName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	// Remove any non-alphanumeric characters except hyphens
	cleanName = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, cleanName)

	// Generate a UUID for uniqueness
	uniqueID := strings.ReplaceAll(uuid.New().String(), "-", "")[:12]

	return fmt.Sprintf("tenant_%s_%s", cleanName, uniqueID)
}

// generateAPIKey generates a unique API key for a tenant
func generateAPIKey(_ string) string {
	// Generate a UUID and format it as an API key
	apiKey := strings.ReplaceAll(uuid.New().String(), "-", "")
	return fmt.Sprintf("rs_live_%s", apiKey)
}
