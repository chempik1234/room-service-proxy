package ports

import (
	"context"
	"errors"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/models"
)

// Common errors
var ErrNotFound = errors.New("not found")

// Tenant represents a tenant in the system
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
type TenantStorage interface {
	// CreateTenant creates a new tenant record in storage
	// Expects: tenant.ID to be pre-generated (use uuid or similar)
	// Expects: tenant.Status to be set (typically "provisioning" initially)
	// Returns: error if tenant.ID already exists or validation fails
	CreateTenant(ctx context.Context, tenant *Tenant) error

	// GetTenant retrieves a tenant by ID
	// Returns: error if tenant not found or deleted
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)

	// GetTenantByAPIKey retrieves a tenant by their API key
	// Used for: request authentication and routing
	// Returns: error if API key invalid or tenant not active
	GetTenantByAPIKey(ctx context.Context, apiKey string) (*Tenant, error)

	// ListTenants retrieves all tenants from storage
	// Returns: empty slice if no tenants exist (not error)
	ListTenants(ctx context.Context) ([]*Tenant, error)

	// ListTenantsByUserID retrieves all tenants for a specific user
	// Useful for: user dashboards and ownership verification
	// Returns: empty slice if user has no tenants (not error)
	ListTenantsByUserID(ctx context.Context, userID string) ([]*Tenant, error)

	// UpdateTenant updates an existing tenant record
	// Expects: tenant.ID to exist and be valid
	// Returns: error if tenant not found or validation fails
	UpdateTenant(ctx context.Context, tenant *Tenant) error

	// DeleteTenant soft deletes a tenant by setting status to "deleted"
	// Does NOT actually remove the record from storage
	// Returns: error if tenant not found
	DeleteTenant(ctx context.Context, tenantID string) error

	// RegenerateAPIKey generates a new API key for a tenant
	// Returns: new API key, error if tenant not found
	// Note: Old API key is immediately invalidated
	RegenerateAPIKey(ctx context.Context, tenantID string) (string, error)
}

// UserStorage defines the interface for user data operations
type UserStorage interface {
	// CreateUser creates a new user account in storage
	// Expects: user.Email to be unique
	// Expects: user.PasswordHash to already be hashed (use hashPassword function)
	// Returns: error if email already exists
	CreateUser(ctx context.Context, user *models.User) error

	// GetUserByEmail retrieves a user by their email address
	// Used for: login and email uniqueness validation
	// Returns: error if user not found
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)

	// GetUserByID retrieves a user by their ID
	// Returns: error if user not found
	GetUserByID(ctx context.Context, userID string) (*models.User, error)

	// ListUsers retrieves all users from storage
	// Returns: empty slice if no users exist (not error)
	ListUsers(ctx context.Context) ([]*models.User, error)

	// UpdateUser updates an existing user record
	// Expects: user.ID to exist and be valid
	// Returns: error if user not found or validation fails
	UpdateUser(ctx context.Context, user *models.User) error

	// DeleteUser removes a user from storage
	// WARNING: This will cascade delete related records (tokens, etc.)
	// Returns: error if user not found
	DeleteUser(ctx context.Context, userID string) error
}

// AuthTokenStorage defines the interface for auth token operations
type AuthTokenStorage interface {
	// CreateToken stores a new authentication token
	// Expects: token.UserID to be valid and reference existing user
	// Expects: token.ExpiresAt to be in the future
	// Returns: error if token creation fails
	CreateToken(ctx context.Context, token *models.AuthToken) error

	// GetToken retrieves an auth token by its value
	// Used for: session validation and authentication
	// Returns: error if token not found or expired
	GetToken(ctx context.Context, token string) (*models.AuthToken, error)

	// DeleteToken removes a specific token from storage
	// Used for: logout functionality
	// Returns: error if token not found
	DeleteToken(ctx context.Context, token string) error

	// DeleteExpiredTokens removes all expired tokens from storage
	// Should be called periodically to clean up storage
	// Returns: error if cleanup fails
	DeleteExpiredTokens(ctx context.Context) error
}

// RequestLogStorage defines the interface for request log operations
type RequestLogStorage interface {
	// CreateRequestLog logs a single request for analytics and monitoring
	// Expects: log.TenantID to reference valid tenant
	// Returns: error if logging fails
	CreateRequestLog(ctx context.Context, log *models.RequestLog) error

	// GetRecentRequestCount counts requests within a time window
	// duration format: "1 hour", "30 minutes", "7 days", etc.
	// Returns: count (0 if no requests), error if query fails
	GetRecentRequestCount(ctx context.Context, duration string) (int64, error)

	// GetTotalRequestCount returns total number of logged requests
	// Returns: count (0 if no requests), error if query fails
	GetTotalRequestCount(ctx context.Context) (int64, error)

	// GetRequestCountByTenants counts requests for specific tenants
	// Useful for: per-tenant usage analytics and billing
	// Returns: count (0 if no requests), error if query fails
	GetRequestCountByTenants(ctx context.Context, tenantIDs []string) (int64, error)

	// GetRequestLogsByTenant retrieves recent logs for a specific tenant
	// limit: maximum number of logs to return (0 for no limit)
	// Returns: empty slice if no logs found (not error)
	GetRequestLogsByTenant(ctx context.Context, tenantID string, limit int) ([]*models.RequestLog, error)

	// DeleteOldRequestLogs removes old request logs to manage storage size
	// olderThan format: "7 days", "30 days", "3 months", etc.
	// Returns: error if cleanup fails
	DeleteOldRequestLogs(ctx context.Context, olderThan string) error
}
