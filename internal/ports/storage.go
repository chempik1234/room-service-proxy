package ports

import (
	"context"
	"time"
)

// Tenant represents a tenant in the system
// This is a lightweight DTO to avoid import cycles
type Tenant struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"` // Owner of the tenant
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	APIKey             string    `json:"api_key"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	Status             string    `json:"status"`                       // active, suspended, deleted, provisioning, provisioning_failed, deleting
	ProvisioningStatus string    `json:"provisioning_status"`          // For detailed progress: pending, creating_services, configuring_services, ready, failed
	ProvisioningError  string    `json:"provisioning_error,omitempty"` // Error message if provisioning failed
	Plan               string    `json:"plan"`                         // free, pro, enterprise
	MaxRooms           int       `json:"max_rooms"`
	MaxRPS             int       `json:"max_rps"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// TenantStorage defines the port for tenant data storage operations
// Implementations can store tenants in PostgreSQL, MongoDB, Redis, etc.
type TenantStorage interface {
	// CreateTenant creates a new tenant record
	CreateTenant(ctx context.Context, tenant *Tenant) error

	// GetTenant retrieves a tenant by ID
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)

	// GetTenantByAPIKey retrieves a tenant by API key
	GetTenantByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)

	// ListTenants retrieves all tenants
	ListTenants(ctx context.Context) ([]*Tenant, error)

	// ListTenantsByUserID retrieves tenants for a specific user
	ListTenantsByUserID(ctx context.Context, userID string) ([]*Tenant, error)

	// UpdateTenant updates an existing tenant record
	UpdateTenant(ctx context.Context, tenant *Tenant) error

	// DeleteTenant soft deletes a tenant (sets status to deleted)
	DeleteTenant(ctx context.Context, tenantID string) error

	// RegenerateAPIKey generates a new API key for a tenant
	RegenerateAPIKey(ctx context.Context, tenantID string) (string, error)
}
