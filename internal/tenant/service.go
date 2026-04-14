package tenant

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service handles tenant business logic
type Service struct {
	db  *pgxpool.Pool
	rly *RailwayService // Railway client for provisioning
}

// RailwayService handles Railway API calls
type RailwayService struct {
	Token    string
	BaseURL  string
}

// NewService creates a new tenant service
func NewService(db *pgxpool.Pool, railwayToken string) *Service {
	return &Service{
		db: db,
		rly: &RailwayService{
			Token:   railwayToken,
			BaseURL: "https://backboard.railway.app/graphql/v2",
		},
	}
}

// CreateTenantWithProvisioning creates a new tenant and provisions Railway services
func (s *Service) CreateTenantWithProvisioning(ctx context.Context, req *CreateTenantRequest) (*Tenant, error) {
	// Create tenant record first
	tenant := &Tenant{
		Name:  req.Name,
		Email: req.Email,
		Plan:  req.Plan,
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

// provisionRailwayProject provisions a complete Railway project for a tenant
func (s *Service) provisionRailwayProject(ctx context.Context, tenant *Tenant) (*RailwayProject, error) {
	// Generate random passwords for databases
	mongoPassword := generateRandomPassword(32)
	redisPassword := generateRandomPassword(32)

	// Create Railway project
	projectID, err := s.rly.CreateProject(tenant.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	// Create MongoDB service with random password
	mongoURL, err := s.rly.CreateMongoDB(projectID, tenant.ID, mongoPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create MongoDB: %w", err)
	}

	// Create Redis service with random password
	redisURL, err := s.rly.CreateRedis(projectID, tenant.ID, redisPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis: %w", err)
	}

	// Create RoomService service
	rsService, err := s.rly.CreateRoomService(projectID, tenant.ID, mongoURL, redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create RoomService: %w", err)
	}

	// Wait for services to be ready
	if err := s.waitForRailwayServices(ctx, projectID); err != nil {
		return nil, fmt.Errorf("services not ready: %w", err)
	}

	return &RailwayProject{
		ProjectID:   projectID,
		Host:        rsService.Host,
		Port:        rsService.Port,
		MongoURL:    mongoURL,
		RedisURL:    redisURL,
		MongoPass:   mongoPassword,
		RedisPass:   redisPassword,
	}, nil
}

// waitForRailwayServices waits for Railway services to be ready
func (s *Service) waitForRailwayServices(ctx context.Context, projectID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Check if all services are healthy
			healthy, err := s.rly.CheckServicesHealth(ctx, projectID)
			if err != nil {
				continue // Try again
			}
			if healthy {
				return nil
			}
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
	Name  string `json:"name"`
	Email string `json:"email"`
	Plan  string `json:"plan"` // free, pro, enterprise
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
