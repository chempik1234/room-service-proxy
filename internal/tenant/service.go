package tenant

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/dto"
	"github.com/chempik1234/room-service-proxy/internal/ports"
)

// Service handles tenant business logic
type Service struct {
	storage           ports.TenantStorage   // Tenant storage port
	deployer          ports.ServiceDeployer // Service deployment port
	provisioningQueue chan *ports.Tenant    // Queue for async provisioning
}

// NewService creates a new tenant service with storage and deployer interfaces
func NewService(storage ports.TenantStorage, deployer ports.ServiceDeployer) (*Service, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}
	if deployer == nil {
		return nil, fmt.Errorf("deployer cannot be nil")
	}

	service := &Service{
		storage:           storage,
		deployer:          deployer,
		provisioningQueue: make(chan *ports.Tenant, 100),
	}

	// Start background provisioning worker
	go service.provisioningWorker()

	return service, nil
}

// GetStorage returns the tenant storage interface
func (s *Service) GetStorage() ports.TenantStorage {
	return s.storage
}

// GetDeployer returns the service deployer interface
func (s *Service) GetDeployer() ports.ServiceDeployer {
	return s.deployer
}

// ProvisioningJob represents a tenant provisioning job
type ProvisioningJob struct {
	Tenant  *ports.Tenant
	Context context.Context
	Error   error
}

// CreateTenantWithProvisioning creates a new tenant and provisions services
func (s *Service) CreateTenantWithProvisioning(ctx context.Context, req *CreateTenantRequest) (*ports.Tenant, error) {
	// Create tenant record first
	tenant := &ports.Tenant{
		UserID: req.UserID,
		Name:   req.Name,
		Email:  req.Email,
		Plan:   req.Plan,
	}

	if err := s.storage.CreateTenant(ctx, tenant); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	// Provision tenant services
	provisionedProject, err := s.provisionTenantServices(ctx, tenant)
	if err != nil {
		// Rollback tenant creation
		if delErr := s.storage.DeleteTenant(ctx, tenant.ID); delErr != nil {
			log.Printf("Failed to delete tenant after provisioning failure: %v", delErr)
		}
		return nil, fmt.Errorf("failed to provision tenant services: %w", err)
	}

	// Update tenant with service info
	tenant.Host = provisionedProject.Host
	tenant.Port = provisionedProject.Port

	if err := s.storage.UpdateTenant(ctx, tenant); err != nil {
		return nil, fmt.Errorf("failed to update tenant: %w", err)
	}

	return tenant, nil
}

// CreateTenantAsync creates a new tenant and queues async provisioning
func (s *Service) CreateTenantAsync(ctx context.Context, req *CreateTenantRequest) (*ports.Tenant, error) {
	// Create tenant record first
	tenant := &ports.Tenant{
		UserID:             req.UserID,
		Name:               req.Name,
		Email:              req.Email,
		Plan:               req.Plan,
		Status:             "provisioning",
		ProvisioningStatus: "pending",
	}

	if err := s.storage.CreateTenant(ctx, tenant); err != nil {
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
		tenant.Host = provisionedProject.Host
		tenant.Port = provisionedProject.Port
		tenant.Status = "active"
		tenant.ProvisioningStatus = "ready"
		tenant.ProvisioningError = ""

		if err := s.storage.UpdateTenant(ctx, tenant); err != nil {
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

	tenant, err := s.storage.GetTenant(ctx, tenantID)
	if err != nil {
		log.Printf("Failed to get tenant %s for status update: %v", tenantID, err)
		return
	}

	tenant.ProvisioningStatus = status
	if errorMsg != "" {
		tenant.ProvisioningError = errorMsg
		tenant.Status = "provisioning_failed"
	}

	if err := s.storage.UpdateTenant(ctx, tenant); err != nil {
		log.Printf("Failed to update tenant %s status: %v", tenantID, err)
	}
}

// GetTenantProvisioningStatus returns detailed provisioning status
func (s *Service) GetTenantProvisioningStatus(ctx context.Context, tenantID string) (map[string]interface{}, error) {
	tenant, err := s.storage.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"tenant_id":           tenant.ID,
		"status":              tenant.Status,
		"provisioning_status": tenant.ProvisioningStatus,
		"provisioning_error":  tenant.ProvisioningError,
		"host":                tenant.Host,
		"port":                tenant.Port,
		"created_at":          tenant.CreatedAt,
		"updated_at":          tenant.UpdatedAt,
	}, nil
}

// provisionTenantServices provisions a complete project for a tenant using the configured deployer
func (s *Service) provisionTenantServices(ctx context.Context, tenant *ports.Tenant) (*ProvisionedProject, error) {
	// Deploy all services in one operation using docker-compose
	appConfig := dto.ApplicationConfig{
		Environment: map[string]string{},
	}

	tenantDeployment, err := s.deployer.DeployTenant(ctx, tenant.ID, appConfig)
	if err != nil {
		log.Printf("ERROR: Failed to deploy tenant %s: %v", tenant.ID, err)
		// Cleanup in case partial resources were created
		if delErr := s.deployer.DeleteServices(ctx, tenant.ID); delErr != nil {
			log.Printf("Failed to cleanup after deployment failure: %v", delErr)
		}
		return nil, fmt.Errorf("failed to deploy tenant: %w", err)
	}
	log.Println("- deployed all services for tenant", tenant.ID)

	// Store generated API key from deployment
	tenant.APIKey = tenantDeployment.Application.APIKey

	// Wait for services to be ready
	if err := s.waitForTenantServices(ctx, tenant.ID); err != nil {
		// Idempotent cleanup: delete all services
		log.Printf("ERROR: Services not ready, attempting cleanup for tenant %s", tenant.ID)
		if delErr := s.deployer.DeleteServices(ctx, tenant.ID); delErr != nil {
			log.Printf("Failed to cleanup after services not ready: %v", delErr)
		}
		return nil, fmt.Errorf("services not ready: %w", err)
	}

	log.Println("Tenant provisioned!", tenant.ID)

	return &ProvisionedProject{
		Host:      tenantDeployment.Application.Host,
		Port:      tenantDeployment.Application.Port,
		MongoURL:  tenantDeployment.Database.ConnectionString,
		RedisURL:  tenantDeployment.Cache.ConnectionString,
		MongoPass: tenantDeployment.Database.Password,
		RedisPass: tenantDeployment.Cache.Password,
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
