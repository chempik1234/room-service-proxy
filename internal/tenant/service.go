package tenant

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/dto"
	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service handles tenant business logic
type Service struct {
	db                *pgxpool.Pool
	deployer          ports.ServiceDeployer // Service deployment port
	provisioningQueue chan *Tenant          // Queue for async provisioning
}

// NewService creates a new tenant service with Railway configuration
func NewService(db *pgxpool.Pool, railwayToken string, railwayProjectID string, railwayEnvironmentID string) (*Service, error) {
	// Create Railway deployer with provided credentials
	var deployer ports.ServiceDeployer
	if railwayToken != "" && railwayProjectID != "" && railwayEnvironmentID != "" {
		deployer = adapters.NewRailwayServiceDeployer(railwayToken, railwayProjectID, railwayEnvironmentID)
	} else {
		// Fall back to Docker for local development
		deployer = adapters.NewDockerServiceDeployer()
	}

	service := &Service{
		db:                db,
		deployer:          deployer,
		provisioningQueue: make(chan *Tenant, 100),
	}

	// Start background provisioning worker
	go service.provisioningWorker()

	return service, nil
}

// ProvisioningJob represents a tenant provisioning job
type ProvisioningJob struct {
	Tenant  *Tenant
	Context context.Context
	Error   error
}

// CreateTenantWithProvisioning creates a new tenant and provisions Railway services
func (s *Service) CreateTenantWithProvisioning(ctx context.Context, req *CreateTenantRequest) (*Tenant, error) {
	// Create tenant record first
	tenant := &Tenant{
		UserID: req.UserID,
		Name:   req.Name,
		Email:  req.Email,
		Plan:   req.Plan,
	}

	repo := NewRepository(s.db)
	if err := repo.Create(ctx, tenant); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	// Provision tenant services
	if s.deployer != nil {
		provisionedProject, err := s.provisionTenantServices(ctx, tenant)
		if err != nil {
			// Rollback tenant creation
			repo.Delete(ctx, tenant.ID)
			return nil, fmt.Errorf("failed to provision tenant services: %w", err)
		}

		// Update tenant with service info
		tenant.Host = provisionedProject.Host
		tenant.Port = provisionedProject.Port

		if err := repo.Update(ctx, tenant); err != nil {
			return nil, fmt.Errorf("failed to update tenant: %w", err)
		}
	}

	return tenant, nil
}

// CreateTenantAsync creates a new tenant and queues async provisioning
func (s *Service) CreateTenantAsync(ctx context.Context, req *CreateTenantRequest) (*Tenant, error) {
	// Create tenant record first
	tenant := &Tenant{
		UserID:            req.UserID,
		Name:              req.Name,
		Email:             req.Email,
		Plan:              req.Plan,
		Status:            "provisioning",
		ProvisioningStatus: "pending",
	}

	repo := NewRepository(s.db)
	if err := repo.Create(ctx, tenant); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	// Queue for async provisioning
	select {
	case s.provisioningQueue <- tenant:
		log.Printf("Tenant %s queued for provisioning", tenant.ID)
	default:
		// Queue full, handle synchronously (fallback)
		log.Printf("Provisioning queue full, handling %s synchronously", tenant.ID)
		return s.CreateTenantWithProvisioning(ctx, req)
	}

	return tenant, nil
}

// provisioningWorker handles tenant provisioning in the background
func (s *Service) provisioningWorker() {
	for tenant := range s.provisioningQueue {
		log.Printf("Processing tenant %s provisioning", tenant.ID)

		// Update status to creating_services
		s.updateTenantStatus(tenant.ID, "creating_services", "")

		// Provision tenant services
		ctx := context.Background()
		provisionedProject, err := s.provisionTenantServices(ctx, tenant)

		if err != nil {
			// Update tenant with failed status
			s.updateTenantStatus(tenant.ID, "failed", err.Error())
			log.Printf("Failed to provision tenant %s: %v", tenant.ID, err)
			continue
		}

		// Update tenant with service info
		repo := NewRepository(s.db)
		tenant.Host = provisionedProject.Host
		tenant.Port = provisionedProject.Port
		tenant.Status = "active"
		tenant.ProvisioningStatus = "ready"
		tenant.ProvisioningError = ""

		if err := repo.Update(ctx, tenant); err != nil {
			log.Printf("Failed to update tenant %s: %v", tenant.ID, err)
			s.updateTenantStatus(tenant.ID, "failed", fmt.Sprintf("Failed to update tenant: %v", err))
			continue
		}

		log.Printf("Successfully provisioned tenant %s", tenant.ID)
	}
}

// updateTenantStatus updates tenant provisioning status
func (s *Service) updateTenantStatus(tenantID, status, errorMsg string) {
	ctx := context.Background()
	repo := NewRepository(s.db)

	tenant, err := repo.GetByID(ctx, tenantID)
	if err != nil {
		log.Printf("Failed to get tenant %s for status update: %v", tenantID, err)
		return
	}

	tenant.ProvisioningStatus = status
	if errorMsg != "" {
		tenant.ProvisioningError = errorMsg
		tenant.Status = "provisioning_failed"
	}

	if err := repo.Update(ctx, tenant); err != nil {
		log.Printf("Failed to update tenant %s status: %v", tenantID, err)
	}
}

// GetTenantProvisioningStatus returns detailed provisioning status
func (s *Service) GetTenantProvisioningStatus(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	repo := NewRepository(s.db)
	tenant, err := repo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"tenant_id":          tenant.ID,
		"status":             tenant.Status,
		"provisioning_status": tenant.ProvisioningStatus,
		"provisioning_error":  tenant.ProvisioningError,
		"host":               tenant.Host,
		"port":               tenant.Port,
		"created_at":         tenant.CreatedAt,
		"updated_at":         tenant.UpdatedAt,
	}, nil
}

// provisionTenantServices provisions a complete project for a tenant using the configured deployer
func (s *Service) provisionTenantServices(ctx context.Context, tenant *Tenant) (*ProvisionedProject, error) {
	// Deploy database
	mongoDeployment, err := s.deployer.DeployDatabase(ctx, tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy database: %w", err)
	}
	log.Println("- deployed database for tenant", tenant.ID)

	// Deploy cache
	redisDeployment, err := s.deployer.DeployCache(ctx, tenant.ID)
	if err != nil {
		// Idempotent cleanup: delete database
		log.Printf("ERROR: Failed to deploy cache, attempting cleanup for tenant %s", tenant.ID)
		s.deployer.DeleteServices(ctx, tenant.ID)
		return nil, fmt.Errorf("failed to deploy cache: %w", err)
	}
	log.Println("- deployed cache for tenant", tenant.ID)

	// Deploy application
	appConfig := dto.ApplicationConfig{
		Environment: map[string]string{
			"MONGO_URL": mongoDeployment.ConnectionString,
			"REDIS_URL": redisDeployment.ConnectionString,
		},
	}
	appDeployment, err := s.deployer.DeployApplication(ctx, tenant.ID, appConfig)
	if err != nil {
		// Idempotent cleanup: delete cache and database
		log.Printf("ERROR: Failed to deploy application, attempting cleanup for tenant %s", tenant.ID)
		s.deployer.DeleteServices(ctx, tenant.ID)
		return nil, fmt.Errorf("failed to deploy application: %w", err)
	}
	log.Println("- deployed application for tenant", tenant.ID)

	// Wait for services to be ready
	if err := s.waitForTenantServices(ctx, tenant.ID); err != nil {
		// Idempotent cleanup: delete all services
		log.Printf("ERROR: Services not ready, attempting cleanup for tenant %s", tenant.ID)
		s.deployer.DeleteServices(ctx, tenant.ID)
		return nil, fmt.Errorf("services not ready: %w", err)
	}

	log.Println("Tenant provisioned!", tenant.ID)

	return &ProvisionedProject{
		Host:      appDeployment.Host,
		Port:      appDeployment.Port,
		MongoURL:  mongoDeployment.ConnectionString,
		RedisURL:  redisDeployment.ConnectionString,
		MongoPass: mongoDeployment.Password,
		RedisPass: redisDeployment.Password,
	}, nil
}

// waitForTenantServices waits for tenant services to be ready using the configured deployer
func (s *Service) waitForTenantServices(ctx context.Context, tenantID string) error {
	// Give services time to initialize - can take 30-60 seconds
	log.Println("Waiting for services to initialize...")
	time.Sleep(30 * time.Second)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// First check immediately after initial delay
	healthy, err := s.deployer.CheckHealth(ctx, tenantID)
	if err == nil && healthy {
		log.Printf("All services for tenant %s are healthy!", tenantID)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for services to be ready for tenant %s", tenantID)
		case <-ticker.C:
			log.Printf("Checking service health for tenant %s...", tenantID)
			// Check if tenant's services are healthy
			healthy, err := s.deployer.CheckHealth(ctx, tenantID)
			if err != nil {
				log.Printf("Health check failed: %v, retrying...", err)
				continue // Try again
			}
			if healthy {
				log.Printf("All services for tenant %s are healthy!", tenantID)
				return nil
			}
			log.Printf("Services for tenant %s not ready yet, waiting...", tenantID)
		}
	}
}

// ProvisionedProject represents a provisioned tenant project
type ProvisionedProject struct {
	Host      string
	Port      int
	MongoURL  string
	RedisURL  string
	MongoPass string
	RedisPass string
}

// CreateTenantRequest represents a request to create a tenant
type CreateTenantRequest struct {
	UserID string `json:"user_id"` // Optional user ID (for user-created tenants)
	Name   string `json:"name"`
	Email  string `json:"email"`
	Plan   string `json:"plan"` // free, pro, enterprise
}

// StorePassword securely stores a password for a tenant service
func (s *Service) StorePassword(ctx context.Context, tenantID, serviceType, password string) error {
	query := `
		INSERT INTO tenant_passwords (tenant_id, service_type, password, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (tenant_id, service_type) DO UPDATE
		SET password = $3, updated_at = NOW()
	`

	_, err := s.db.Exec(ctx, query, tenantID, serviceType, password)
	if err != nil {
		return fmt.Errorf("failed to store password: %w", err)
	}

	return nil
}

// GetPassword retrieves a password for a tenant service
func (s *Service) GetPassword(ctx context.Context, tenantID, serviceType string) (string, error) {
	var password string
	query := `
		SELECT password FROM tenant_passwords
		WHERE tenant_id = $1 AND service_type = $2
	`

	err := s.db.QueryRow(ctx, query, tenantID, serviceType).Scan(&password)
	if err != nil {
		return "", fmt.Errorf("failed to get password: %w", err)
	}

	return password, nil
}
