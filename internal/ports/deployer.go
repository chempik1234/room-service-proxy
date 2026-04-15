package ports

import (
	"context"

	"github.com/chempik1234/room-service-proxy/internal/dto"
)

// ServiceDeployer defines the port for deploying tenant services
// Implementations can deploy to Railway, Docker, Kubernetes, etc.
type ServiceDeployer interface {
	// DeployDatabase deploys a database for the tenant and returns connection string
	DeployDatabase(ctx context.Context, tenantID string) (dto.DatabaseDeployment, error)

	// DeployCache deploys a cache for the tenant and returns connection string
	DeployCache(ctx context.Context, tenantID string) (dto.CacheDeployment, error)

	// DeployApplication deploys the main application for the tenant
	DeployApplication(ctx context.Context, tenantID string, config dto.ApplicationConfig) (dto.ApplicationDeployment, error)

	// CheckHealth checks if all services for a tenant are healthy
	CheckHealth(ctx context.Context, tenantID string) (bool, error)

	// DeleteServices removes all services for a tenant (cleanup/rollback)
	DeleteServices(ctx context.Context, tenantID string) error

	// GetStatus returns the current status of tenant services
	GetStatus(ctx context.Context, tenantID string) (dto.DeploymentStatus, error)
}