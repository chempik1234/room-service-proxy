package adapters

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
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	return &t, nil
}

// GetTenantByAPIKey retrieves a tenant by API key
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
		return nil, fmt.Errorf("failed to get tenant by API key: %w", err)
	}

	return &t, nil
}

// ListTenants retrieves all tenants
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

// ListTenantsByUserID retrieves tenants for a specific user
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

// UpdateTenant updates a tenant
func (s *PostgresTenantStorage) UpdateTenant(ctx context.Context, t *ports.Tenant) error {
	t.UpdatedAt = time.Now()

	query := `
		UPDATE tenants
		SET name = $2, email = $3, host = $4, port = $5, status = $6,
		    plan = $7, max_rooms = $8, max_rps = $9, updated_at = $10
		WHERE id = $1
		RETURNING id, user_id, name, email, api_key, host, port, status, plan, max_rooms, max_rps, created_at, updated_at
	`

	err := s.db.QueryRow(ctx, query,
		t.ID, t.Name, t.Email, t.Host, t.Port,
		t.Status, t.Plan, t.MaxRooms, t.MaxRPS, t.UpdatedAt,
	).Scan(
		&t.ID, &t.UserID, &t.Name, &t.Email, &t.APIKey,
		&t.Host, &t.Port, &t.Status, &t.Plan,
		&t.MaxRooms, &t.MaxRPS, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	return nil
}

// DeleteTenant soft deletes a tenant (sets status to deleted)
func (s *PostgresTenantStorage) DeleteTenant(ctx context.Context, id string) error {
	nowStr := time.Now().Format(time.RFC3339)
	query := `UPDATE tenants SET status = 'deleted', updated_at = $1 WHERE id = $2`
	_, err := s.db.Exec(ctx, query, nowStr, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}
	return nil
}

// RegenerateAPIKey generates a new API key for a tenant
func (s *PostgresTenantStorage) RegenerateAPIKey(ctx context.Context, id string) (string, error) {
	newAPIKey := generateAPIKey(id)
	nowStr := time.Now().Format(time.RFC3339)
	query := `UPDATE tenants SET api_key = $2, updated_at = $3 WHERE id = $1 RETURNING api_key`

	err := s.db.QueryRow(ctx, query, id, newAPIKey, nowStr).Scan(&newAPIKey)
	if err != nil {
		return "", fmt.Errorf("failed to regenerate API key: %w", err)
	}

	return newAPIKey, nil
}

// Close closes the database connection pool
func (s *PostgresTenantStorage) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

// Helper functions
func generateTenantID(name string) string {
	// Sanitize name to remove URL-unsafe characters
	fmt.Printf("🔧 [DEBUG] Original tenant name: %q\n", name)

	sanitizedName := strings.ToLower(strings.TrimSpace(name))

	// Replace ALL URL-unsafe characters
	sanitizedName = strings.ReplaceAll(sanitizedName, "/", "-")
	sanitizedName = strings.ReplaceAll(sanitizedName, " ", "-")
	sanitizedName = strings.ReplaceAll(sanitizedName, "_", "-")
	sanitizedName = strings.ReplaceAll(sanitizedName, "\\", "-") // Remove backslashes
	sanitizedName = strings.ReplaceAll(sanitizedName, ":", "-")  // Remove colons
	sanitizedName = strings.ReplaceAll(sanitizedName, "@", "-")  // Remove at signs
	sanitizedName = strings.ReplaceAll(sanitizedName, ".", "-")  // Remove dots
	sanitizedName = strings.ReplaceAll(sanitizedName, "*", "-")  // Remove asterisks

	fmt.Printf("🔧 [DEBUG] Sanitized name: %s\n", sanitizedName)

	// Double-check no unsafe characters remain
	unsafe := []string{"/", " ", "_", "\\", ":", "@", "."}
	for _, char := range unsafe {
		if strings.Contains(sanitizedName, char) {
			fmt.Printf("❌ [ERROR] Sanitization failed! Still contains: %s\n", char)
		}
	}

	// Generate a unique tenant ID
	tenantID := fmt.Sprintf("tenant-%s-%s", sanitizedName, uuid.New().String()[:8])

	// Final verification
	if strings.Contains(tenantID, "/") {
		fmt.Printf("❌ [CRITICAL] Generated ID still contains slash! ID: %s\n", tenantID)
	}

	fmt.Printf("🔧 [DEBUG] Generated tenant ID: name=%q → id=%s\n", name, tenantID)

	return tenantID
}

func generateAPIKey(tenantID string) string {
	// Generate a unique API key
	return fmt.Sprintf("rs_live_%s_%s", tenantID, uuid.New().String())
}