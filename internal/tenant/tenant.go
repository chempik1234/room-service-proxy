package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant represents a tenant in the system
type Tenant struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	APIKey         string    `json:"api_key"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Status         string    `json:"status"` // active, suspended, deleted
	Plan           string    `json:"plan"`   // free, pro, enterprise
	MaxRooms       int       `json:"max_rooms"`
	MaxRPS         int       `json:"max_rps"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Repository handles tenant database operations
type Repository struct {
	db *pgxpool.Pool
}

// NewRepository creates a new tenant repository
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Create creates a new tenant
func (r *Repository) Create(ctx context.Context, tenant *Tenant) error {
	now := time.Now()
	tenant.ID = generateTenantID(tenant.Name)
	tenant.APIKey = generateAPIKey(tenant.ID)
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

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
		INSERT INTO tenants (id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		tenant.ID, tenant.Name, tenant.Email, tenant.APIKey,
		tenant.Host, tenant.Port, tenant.Status, tenant.Plan,
		tenant.MaxRooms, tenant.MaxRPS, tenant.CreatedAt, tenant.UpdatedAt,
	).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create tenant: %w", err)
	}

	return nil
}

// GetByID retrieves a tenant by ID
func (r *Repository) GetByID(ctx context.Context, id string) (*Tenant, error) {
	var tenant Tenant
	query := `
		SELECT id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants WHERE id = $1
	`

	err := r.db.QueryRow(ctx, query, id).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	return &tenant, nil
}

// GetByAPIKey retrieves a tenant by API key
func (r *Repository) GetByAPIKey(ctx context.Context, apiKey string) (*Tenant, error) {
	var tenant Tenant
	query := `
		SELECT id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants WHERE api_key = $1
	`

	err := r.db.QueryRow(ctx, query, apiKey).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get tenant by API key: %w", err)
	}

	return &tenant, nil
}

// List retrieves all tenants
func (r *Repository) List(ctx context.Context) ([]*Tenant, error) {
	query := `
		SELECT id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
		FROM tenants ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var tenant Tenant
		err := rows.Scan(
			&tenant.ID, &tenant.Name, &tenant.Email, &tenant.APIKey,
			&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
			&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, &tenant)
	}

	return tenants, nil
}

// Update updates a tenant
func (r *Repository) Update(ctx context.Context, tenant *Tenant) error {
	tenant.UpdatedAt = time.Now()

	query := `
		UPDATE tenants
		SET name = $2, email = $3, host = $4, port = $5, status = $6,
		    plan = $7, max_rooms = $8, max_rps = $9, updated_at = $10
		WHERE id = $1
		RETURNING id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		tenant.ID, tenant.Name, tenant.Email, tenant.Host, tenant.Port,
		tenant.Status, tenant.Plan, tenant.MaxRooms, tenant.MaxRPS, tenant.UpdatedAt,
	).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.APIKey,
		&tenant.Host, &tenant.Port, &tenant.Status, &tenant.Plan,
		&tenant.MaxRooms, &tenant.MaxRPS, &tenant.CreatedAt, &tenant.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	return nil
}

// Delete soft deletes a tenant (sets status to deleted)
func (r *Repository) Delete(ctx context.Context, id string) error {
	query := `UPDATE tenants SET status = 'deleted', updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}
	return nil
}

// RegenerateAPIKey generates a new API key for a tenant
func (r *Repository) RegenerateAPIKey(ctx context.Context, id string) (string, error) {
	newAPIKey := generateAPIKey(id)
	query := `UPDATE tenants SET api_key = $2, updated_at = NOW() WHERE id = $1 RETURNING api_key`

	err := r.db.QueryRow(ctx, query, id, newAPIKey).Scan(&newAPIKey)
	if err != nil {
		return "", fmt.Errorf("failed to regenerate API key: %w", err)
	}

	return newAPIKey, nil
}

// Helper functions
func generateTenantID(name string) string {
	// Generate a unique tenant ID based on name and UUID
	return fmt.Sprintf("tenant-%s-%s", name, uuid.New().String()[:8])
}

func generateAPIKey(tenantID string) string {
	// Generate a unique API key
	return fmt.Sprintf("rs_live_%s_%s", tenantID, uuid.New().String())
}
