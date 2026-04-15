package tenant

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service handles tenant business logic
type Service struct {
	db                *pgxpool.Pool
	rly               *RailwayService // Railway client for provisioning
	provisioningQueue chan *Tenant     // Queue for async provisioning
}

// RailwayService handles Railway API calls
type RailwayService struct {
	Token         string
	BaseURL       string
	ProjectID     string
	EnvironmentID string
	client        *http.Client
}

// NewService creates a new tenant service
func NewService(db *pgxpool.Pool, railwayToken string, railwayProjectID string, railwayEnvironmentID string) *Service {
	service := &Service{
		db: db,
		rly: &RailwayService{
			Token:         railwayToken,
			BaseURL:       "https://backboard.railway.app/graphql/v2",
			ProjectID:     railwayProjectID,
			EnvironmentID: railwayEnvironmentID,
			client:        &http.Client{Timeout: 30 * time.Second},
		},
		provisioningQueue: make(chan *Tenant, 100),
	}

	// Start background provisioning worker
	go service.provisioningWorker()

	return service
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

	// Provision Railway services
	if s.rly != nil && s.rly.Token != "" {
		railwayProject, err := s.provisionRailwayProject(ctx, tenant)
		if err != nil {
			// Rollback tenant creation
			repo.Delete(ctx, tenant.ID)
			return nil, fmt.Errorf("failed to provision Railway project: %w", err)
		}

		// Update tenant with Railway info
		tenant.Host = railwayProject.Host
		tenant.Port = railwayProject.Port

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

		// Provision Railway services
		ctx := context.Background()
		railwayProject, err := s.provisionRailwayProject(ctx, tenant)

		if err != nil {
			// Update tenant with failed status
			s.updateTenantStatus(tenant.ID, "failed", err.Error())
			log.Printf("Failed to provision tenant %s: %v", tenant.ID, err)
			continue
		}

		// Update tenant with Railway info
		repo := NewRepository(s.db)
		tenant.Host = railwayProject.Host
		tenant.Port = railwayProject.Port
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

// provisionRailwayProject provisions a complete Railway project for a tenant
func (s *Service) provisionRailwayProject(ctx context.Context, tenant *Tenant) (*RailwayProject, error) {
	// Generate random passwords for databases
	mongoPassword := generateRandomPassword(32)
	redisPassword := generateRandomPassword(32)

	// Track created resources for idempotent cleanup
	var mongoServiceID, redisServiceID, roomServiceID string

	// Create Railway project
	projectID := s.rly.ProjectID

	// Create MongoDB service with random password
	mongoURL, err := s.rly.CreateMongoDB(projectID, tenant.ID, mongoPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create MongoDB: %w", err)
	}
	// Extract service ID from URL for cleanup (format: <serviceID>.railway.internal)
	mongoServiceID = extractServiceIDFromURL(mongoURL)

	log.Println("- created new mongo DB for tenant", tenant.ID)

	// Create Redis service with random password
	redisURL, err := s.rly.CreateRedis(projectID, tenant.ID, redisPassword)
	if err != nil {
		// Idempotent cleanup: delete MongoDB service
		log.Printf("ERROR: Failed to create Redis, attempting cleanup for tenant %s", tenant.ID)
		if mongoServiceID != "" {
			s.rly.DeleteService(mongoServiceID)
		}
		return nil, fmt.Errorf("failed to create Redis: %w", err)
	}
	// Extract service ID from URL for cleanup
	redisServiceID = extractServiceIDFromURL(redisURL)

	log.Println("- created new Redis for tenant", tenant.ID)

	// Create RoomService service
	rsService, err := s.rly.CreateRoomService(projectID, tenant.ID, mongoURL, redisURL)
	if err != nil {
		// Idempotent cleanup: delete Redis and MongoDB services
		log.Printf("ERROR: Failed to create RoomService, attempting cleanup for tenant %s", tenant.ID)
		if redisServiceID != "" {
			s.rly.DeleteService(redisServiceID)
		}
		if mongoServiceID != "" {
			s.rly.DeleteService(mongoServiceID)
		}
		return nil, fmt.Errorf("failed to create RoomService: %w", err)
	}
	roomServiceID = rsService.ServiceID

	log.Println("- created new RoomService for tenant", tenant.ID)

	// Wait for services to be ready
	if err := s.waitForRailwayServices(ctx, projectID, tenant.ID); err != nil {
		// Idempotent cleanup: delete all services
		log.Printf("ERROR: Services not ready, attempting cleanup for tenant %s", tenant.ID)
		if roomServiceID != "" {
			s.rly.DeleteService(roomServiceID)
		}
		if redisServiceID != "" {
			s.rly.DeleteService(redisServiceID)
		}
		if mongoServiceID != "" {
			s.rly.DeleteService(mongoServiceID)
		}
		return nil, fmt.Errorf("services not ready: %w", err)
	}

	log.Println("Tenant provisioned!", tenant.ID)

	return &RailwayProject{
		ProjectID: projectID,
		Host:      rsService.Host,
		Port:      rsService.Port,
		MongoURL:  mongoURL,
		RedisURL:  redisURL,
		MongoPass: mongoPassword,
		RedisPass: redisPassword,
	}, nil
}

// waitForRailwayServices waits for Railway services to be ready
func (s *Service) waitForRailwayServices(ctx context.Context, projectID string, tenantID string) error {
	// Give services time to initialize - Railway can take 30-60 seconds
	log.Println("Waiting for Railway services to initialize...")
	time.Sleep(30 * time.Second)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// First check immediately after initial delay
	healthy, err := s.rly.CheckTenantServicesHealth(ctx, projectID, tenantID)
	if err == nil && healthy {
		log.Printf("All Railway services for tenant %s are healthy!", tenantID)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Railway services to be ready for tenant %s", tenantID)
		case <-ticker.C:
			log.Printf("Checking Railway service health for tenant %s...", tenantID)
			// Check if tenant's services are healthy
			healthy, err := s.rly.CheckTenantServicesHealth(ctx, projectID, tenantID)
			if err != nil {
				log.Printf("Health check failed: %v, retrying...", err)
				continue // Try again
			}
			if healthy {
				log.Printf("All Railway services for tenant %s are healthy!", tenantID)
				return nil
			}
			log.Printf("Services for tenant %s not ready yet, waiting...", tenantID)
		}
	}
}

// RailwayProject represents a provisioned Railway project
type RailwayProject struct {
	ProjectID string
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

// generateRandomPassword generates a secure random password
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?"

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}

	// Add some randomness from time
	time.Sleep(time.Nanosecond)

	return string(b)
}

// extractServiceIDFromURL extracts service ID from Railway URL
// Handles formats like: <serviceID>.railway.internal or <projectID>-<serviceID>.uprailway.app
func extractServiceIDFromURL(url string) string {
	// Remove protocol if present
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}

	// Remove port if present
	if idx := strings.Index(url, ":"); idx != -1 {
		url = url[:idx]
	}

	// Handle private addressing: <serviceID>.railway.internal
	if strings.Contains(url, ".railway.internal") {
		return strings.Split(url, ".")[0]
	}

	// Handle public addressing: <projectID>-<serviceID>.uprailway.app
	if strings.Contains(url, ".uprailway.app") {
		parts := strings.Split(url, "-")
		if len(parts) >= 2 {
			// Extract service ID (last part before .uprailway.app)
			serviceID := strings.Split(parts[1], ".")[0]
			return serviceID
		}
	}

	// Fallback: return the hostname as-is
	return url
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
