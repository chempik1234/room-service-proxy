package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewTenantServiceWithPostgresAndDocker creates a tenant service with PostgreSQL storage and Docker deployment
// NOTE: Docker adapter is currently disabled due to API changes. Use Railway instead.
func NewTenantServiceWithPostgresAndDocker(dbURL string) (*Service, error) {
	return nil, fmt.Errorf("Docker adapter is currently disabled. Please use Railway deployment instead.")
}

// NewTenantServiceWithPostgresAndRailway creates a tenant service with PostgreSQL storage and Railway deployment
func NewTenantServiceWithPostgresAndRailway(dbURL, railwayToken, railwayProjectID, railwayEnvID string) (*Service, error) {
	// Create storage adapter
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage, err := adapters.NewPostgresTenantStorage(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create storage adapter: %w", err)
	}

	// Create deployer adapter
	var deployer ports.ServiceDeployer
	if railwayToken != "" && railwayProjectID != "" && railwayEnvID != "" {
		deployer = adapters.NewRailwayServiceDeployer(railwayToken, railwayProjectID, railwayEnvID)
	} else {
		// Docker adapter is currently disabled, require Railway credentials
		return nil, fmt.Errorf("Railway credentials are required. Docker adapter is currently disabled.")
	}

	return NewService(storage, deployer)
}

// NewTenantServiceWithPoolAndDocker creates a tenant service with existing PostgreSQL pool and Docker deployment
// NOTE: Docker adapter is currently disabled. Use Railway instead.
func NewTenantServiceWithPoolAndDocker(db *pgxpool.Pool) (*Service, error) {
	return nil, fmt.Errorf("Docker adapter is currently disabled. Please use Railway deployment instead.")
}

// NewTenantServiceWithPoolAndRailway creates a tenant service with existing PostgreSQL pool and Railway deployment
func NewTenantServiceWithPoolAndRailway(db *pgxpool.Pool, railwayToken, railwayProjectID, railwayEnvID string) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection pool cannot be nil")
	}

	// Create storage adapter
	storage, err := adapters.NewPostgresTenantStorage(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage adapter: %w", err)
	}

	// Create deployer adapter
	if railwayToken == "" || railwayProjectID == "" || railwayEnvID == "" {
		return nil, fmt.Errorf("Railway credentials are required. Docker adapter is currently disabled.")
	}

	deployer := adapters.NewRailwayServiceDeployer(railwayToken, railwayProjectID, railwayEnvID)

	return NewService(storage, deployer)
}

// NewTenantServiceFromAdapters creates a tenant service with custom storage and deployer adapters
func NewTenantServiceFromAdapters(storage ports.TenantStorage, deployer ports.ServiceDeployer) (*Service, error) {
	return NewService(storage, deployer)
}

// NewTenantServiceWithPostgresAndYandex creates a tenant service with PostgreSQL storage and Yandex Cloud deployment
func NewTenantServiceWithPostgresAndYandex(dbURL string, yandexFolderID string, yandexZone string, yandexServiceAccountKey string, yandexSSHKeyPath string) (*Service, error) {
	// Create storage adapter
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	storage, err := adapters.NewPostgresTenantStorage(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create storage adapter: %w", err)
	}

	// Create Yandex deployer adapter
	deployer, err := adapters.NewYandexServiceDeployer(yandexFolderID, yandexZone, yandexServiceAccountKey, yandexSSHKeyPath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create Yandex deployer: %w", err)
	}

	return NewService(storage, deployer)
}