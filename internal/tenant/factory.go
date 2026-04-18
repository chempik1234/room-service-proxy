package tenant

import (
	"fmt"

	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
)

// NewTenantServiceWithPostgresAndDocker creates a tenant service with PostgreSQL storage and Docker deployment
// NOTE: Docker adapter is currently disabled due to API changes. Use Railway instead.
func NewTenantServiceWithPostgresAndDocker(dbURL string) (*Service, error) {
	return nil, fmt.Errorf("docker adapter is currently disabled; please use Railway deployment instead")
}

// NewTenantServiceWithPostgresAndRailway creates a tenant service with PostgreSQL storage and Railway deployment
func NewTenantServiceWithPostgresAndRailway(railwayToken, railwayProjectID, railwayEnvID string) (*Service, error) {
	// Create deployer adapter
	if railwayToken != "" && railwayProjectID != "" && railwayEnvID != "" {
		_ = adapters.NewRailwayServiceDeployer(railwayToken, railwayProjectID, railwayEnvID)
	} else {
		// Docker adapter is currently disabled, require Railway credentials
		return nil, fmt.Errorf("railway credentials are required; docker adapter is currently disabled")
	}

	// Note: storageRepo should be passed in from main.go
	return nil, fmt.Errorf("this factory function is deprecated; use NewTenantServiceFromAdapters instead")
}

// NewTenantServiceWithPostgresAndYandex creates a tenant service with PostgreSQL storage and Yandex Cloud deployment
func NewTenantServiceWithPostgresAndYandex(yandexFolderID string, yandexZone string, yandexSubnetID string, yandexServiceAccountKey string, yandexSSHKeyPath string) (*Service, error) {
	// Create Yandex deployer adapter
	_, err := adapters.NewYandexServiceDeployer(yandexFolderID, yandexZone, yandexSubnetID, yandexServiceAccountKey, yandexSSHKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create Yandex deployer: %w", err)
	}

	// Note: storageRepo should be passed in from main.go
	return nil, fmt.Errorf("this factory function is deprecated; use NewTenantServiceFromAdapters instead")
}

// NewTenantServiceFromAdapters creates a tenant service with custom storage and deployer adapters
func NewTenantServiceFromAdapters(storage ports.TenantStorage, deployer ports.ServiceDeployer) (*Service, error) {
	return NewService(storage, deployer)
}
