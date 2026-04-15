package ports

import (
	"context"
)

// ServiceDeployer defines the port for deploying tenant services
// Implementations can deploy to Railway, Docker, Kubernetes, etc.
type ServiceDeployer interface {
	// DeployDatabase deploys a database for the tenant and returns connection string
	DeployDatabase(ctx context.Context, tenantID string) (DatabaseDeployment, error)

	// DeployCache deploys a cache for the tenant and returns connection string
	DeployCache(ctx context.Context, tenantID string) (CacheDeployment, error)

	// DeployApplication deploys the main application for the tenant
	DeployApplication(ctx context.Context, tenantID string, config ApplicationConfig) (ApplicationDeployment, error)

	// CheckHealth checks if all services for a tenant are healthy
	CheckHealth(ctx context.Context, tenantID string) (bool, error)

	// DeleteServices removes all services for a tenant (cleanup/rollback)
	DeleteServices(ctx context.Context, tenantID string) error

	// GetStatus returns the current status of tenant services
	GetStatus(ctx context.Context, tenantID string) (DeploymentStatus, error)
}

// DatabaseDeployment contains information about a deployed database
type DatabaseDeployment struct {
	ConnectionString string
	Host            string
	Port            int
	Username        string
	Password        string // Caller should store securely if needed
	Database        string
	Type            string // "mongodb", "postgresql", etc.
}

// CacheDeployment contains information about a deployed cache
type CacheDeployment struct {
	ConnectionString string
	Host            string
	Port            int
	Password        string // Caller should store securely if needed
	DB              int    // Redis database number
	Type            string // "redis", "memcached", etc.
}

// ApplicationDeployment contains information about a deployed application
type ApplicationDeployment struct {
	Endpoint string // gRPC endpoint, HTTP URL, etc.
	Host     string
	Port     int
	Status   string // "deploying", "healthy", "failed"
}

// ApplicationConfig contains configuration for deploying the application
type ApplicationConfig struct {
	Image       string
	Environment map[string]string
	Resources   ResourceConfig
}

// ResourceConfig defines resource limits/requests
type ResourceConfig struct {
	CPU    string
	Memory string
}

// DeploymentStatus represents the current state of tenant services
type DeploymentStatus struct {
	TenantID      string
	Healthy       bool
	Services      []ServiceStatus
	Provisioning  string // "pending", "deploying", "healthy", "failed"
	CreatedAt     string
	UpdatedAt     string
}

// ServiceStatus represents the status of an individual service
type ServiceStatus struct {
	Name   string
	Type   string // "database", "cache", "application"
	Healthy bool
	Status string
}