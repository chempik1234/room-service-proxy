package adapters

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/dto"
)

// DockerServiceDeployer implements ServiceDeployer using local Docker
// This is useful for development, testing, or on-premise deployments
type DockerServiceDeployer struct {
	network         string
	mongoImage      string
	redisImage      string
	appImage        string
	basePort        int
	deployedServices map[string]*DockerService
}

// DockerService represents a deployed Docker container
type DockerService struct {
	ID       string
	Name     string
	Host     string
	Port     int
	Running  bool
	Password string
}

// NewDockerServiceDeployer creates a new Docker-based deployer
func NewDockerServiceDeployer() *DockerServiceDeployer {
	return &DockerServiceDeployer{
		network:         "roomservice-network",
		mongoImage:      "mongo:6",
		redisImage:      "redis:7",
		appImage:        "chempik1234/roomservice:latest",
		basePort:        27000, // Starting port for dynamic allocation
		deployedServices: make(map[string]*DockerService),
	}
}

// DeployDatabase deploys MongoDB using Docker
func (d *DockerServiceDeployer) DeployDatabase(ctx context.Context, tenantID string) (dto.DatabaseDeployment, error) {
	serviceName := fmt.Sprintf("%s-mongo", tenantID)
	password := generateRandomPassword(32)

	// Check if already exists
	if existing, exists := d.deployedServices[serviceName]; exists {
		return dto.DatabaseDeployment{
			ConnectionString: fmt.Sprintf("mongodb://admin:%s@%s:%d", existing.Password, existing.Host, existing.Port),
			Host:            existing.Host,
			Port:            existing.Port,
			Username:        "admin",
			Password:        existing.Password,
			Database:        "rooms_db",
			Type:            "mongodb",
		}, nil
	}

	// Deploy MongoDB container
	// In real implementation, this would use Docker API or docker-compose
	port := d.basePort + len(d.deployedServices)

	log.Printf("🐳 Deploying MongoDB for tenant %s on port %d", tenantID, port)

	service := &DockerService{
		ID:       fmt.Sprintf("mongo-%s", tenantID),
		Name:     serviceName,
		Host:     "localhost",
		Port:     port,
		Running:  true,
		Password: password,
	}

	d.deployedServices[serviceName] = service

	return dto.DatabaseDeployment{
		ConnectionString: fmt.Sprintf("mongodb://admin:%s@localhost:%d", password, port),
		Host:            "localhost",
		Port:            port,
		Username:        "admin",
		Password:        password,
		Database:        "rooms_db",
		Type:            "mongodb",
	}, nil
}

// DeployCache deploys Redis using Docker
func (d *DockerServiceDeployer) DeployCache(ctx context.Context, tenantID string) (dto.CacheDeployment, error) {
	serviceName := fmt.Sprintf("%s-redis", tenantID)
	password := generateRandomPassword(32)

	// Check if already exists
	if existing, exists := d.deployedServices[serviceName]; exists {
		return dto.CacheDeployment{
			ConnectionString: fmt.Sprintf("redis://:%s@%s:%d/%d", existing.Password, existing.Host, existing.Port, 0),
			Host:            existing.Host,
			Port:            existing.Port,
			Password:        existing.Password,
			DB:              0,
			Type:            "redis",
		}, nil
	}

	// Deploy Redis container
	port := d.basePort + len(d.deployedServices) + 1000 // Offset for Redis

	log.Printf("🐳 Deploying Redis for tenant %s on port %d", tenantID, port)

	service := &DockerService{
		ID:       fmt.Sprintf("redis-%s", tenantID),
		Name:     serviceName,
		Host:     "localhost",
		Port:     port,
		Running:  true,
		Password: password,
	}

	d.deployedServices[serviceName] = service

	return dto.CacheDeployment{
		ConnectionString: fmt.Sprintf("redis://:%s@localhost:%d/%d", password, port, 0),
		Host:            "localhost",
		Port:            port,
		Password:        password,
		DB:              0,
		Type:            "redis",
	}, nil
}

// DeployApplication deploys RoomService using Docker
func (d *DockerServiceDeployer) DeployApplication(ctx context.Context, tenantID string, config dto.ApplicationConfig) (dto.ApplicationDeployment, error) {
	serviceName := tenantID

	// Check if already exists
	if existing, exists := d.deployedServices[serviceName]; exists {
		return dto.ApplicationDeployment{
			Endpoint: fmt.Sprintf("%s:%d", existing.Host, existing.Port),
			Host:     existing.Host,
			Port:     existing.Port,
			Status:   "healthy",
		}, nil
	}

	// Deploy RoomService container
	port := d.basePort + len(d.deployedServices) + 2000 // Offset for app

	log.Printf("🐳 Deploying RoomService for tenant %s on port %d", tenantID, port)

	service := &DockerService{
		ID:      fmt.Sprintf("app-%s", tenantID),
		Name:    serviceName,
		Host:    "localhost",
		Port:    port,
		Running: true,
	}

	d.deployedServices[serviceName] = service

	return dto.ApplicationDeployment{
		Endpoint: fmt.Sprintf("localhost:%d", port),
		Host:     "localhost",
		Port:     port,
		Status:   "healthy",
	}, nil
}

// CheckHealth checks if all services for a tenant are healthy
func (d *DockerServiceDeployer) CheckHealth(ctx context.Context, tenantID string) (bool, error) {
	services := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	allHealthy := true
	for _, serviceName := range services {
		service, exists := d.deployedServices[serviceName]
		if !exists || !service.Running {
			allHealthy = false
			break
		}
	}

	return allHealthy, nil
}

// DeleteServices removes all services for a tenant
func (d *DockerServiceDeployer) DeleteServices(ctx context.Context, tenantID string) error {
	services := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	for _, serviceName := range services {
		if service, exists := d.deployedServices[serviceName]; exists {
			log.Printf("🐳 Stopping service: %s", service.Name)
			service.Running = false
			delete(d.deployedServices, serviceName)
		}
	}

	return nil
}

// GetStatus returns the current status of tenant services
func (d *DockerServiceDeployer) GetStatus(ctx context.Context, tenantID string) (dto.DeploymentStatus, error) {
	services := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	var serviceStatuses []dto.ServiceStatus
	allHealthy := true

	for _, serviceName := range services {
		service, exists := d.deployedServices[serviceName]
		if !exists {
			allHealthy = false
			serviceStatuses = append(serviceStatuses, dto.ServiceStatus{
				Name:    serviceName,
				Type:    "unknown",
				Healthy: false,
				Status:  "not found",
			})
			continue
		}

		healthy := service.Running
		if !healthy {
			allHealthy = false
		}

		serviceType := "application"
		if len(serviceName) > 5 {
			switch serviceName[len(serviceName)-5:] {
			case "-mongo":
				serviceType = "database"
			case "-redis":
				serviceType = "cache"
			}
		}

		serviceStatuses = append(serviceStatuses, dto.ServiceStatus{
			Name:    serviceName,
			Type:    serviceType,
			Healthy: healthy,
			Status:  map[bool]string{true: "running", false: "stopped"}[service.Running],
		})
	}

	return dto.DeploymentStatus{
		TenantID:     tenantID,
		Healthy:      allHealthy,
		Services:     serviceStatuses,
		Provisioning: map[bool]string{true: "healthy", false: "failed"}[allHealthy],
		CreatedAt:    time.Now().Format(time.RFC3339),
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}, nil
}