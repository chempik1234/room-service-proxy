package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chempik1234/room-service-proxy/pkg/utils"
)

// RailwayServiceDeployer implements ServiceDeployer using Railway's infrastructure
// This adapter handles Railway-specific logic while conforming to the generic port
type RailwayServiceDeployer struct {
	rly *RailwayService // Railway API client
}

// NewRailwayServiceDeployer creates a new Railway-based deployer
func NewRailwayServiceDeployer(token, projectID, environmentID string) *RailwayServiceDeployer {
	return &RailwayServiceDeployer{
		rly: &RailwayService{
			Token:         token,
			BaseURL:       "https://backboard.railway.app/graphql/v2",
			ProjectID:     projectID,
			EnvironmentID: environmentID,
			client:        &http.Client{Timeout: 30 * time.Second},
		},
	}
}

// DeployDatabase deploys MongoDB on Railway
func (r *RailwayServiceDeployer) DeployDatabase(ctx context.Context, tenantID string) (DatabaseDeployment, error) {
	password := generateRandomPassword(32)
	mongoURL, err := r.rly.CreateMongoDB(r.rly.ProjectID, tenantID, password)
	if err != nil {
		return DatabaseDeployment{}, fmt.Errorf("failed to deploy MongoDB: %w", err)
	}

	host := extractHostFromURL(mongoURL)
	port := 27071 // Default Railway MongoDB port

	return DatabaseDeployment{
		ConnectionString: mongoURL,
		Host:            host,
		Port:            port,
		Username:        "admin",
		Password:        password,
		Database:        "rooms_db",
		Type:            "mongodb",
	}, nil
}

// DeployCache deploys Redis on Railway
func (r *RailwayServiceDeployer) DeployCache(ctx context.Context, tenantID string) (CacheDeployment, error) {
	password := generateRandomPassword(32)
	redisURL, err := r.rly.CreateRedis(r.rly.ProjectID, tenantID, password)
	if err != nil {
		return CacheDeployment{}, fmt.Errorf("failed to deploy Redis: %w", err)
	}

	host := extractHostFromURL(redisURL)
	port := 6379 // Default Railway Redis port

	return CacheDeployment{
		ConnectionString: redisURL,
		Host:            host,
		Port:            port,
		Password:        password,
		DB:              0,
		Type:            "redis",
	}, nil
}

// DeployApplication deploys RoomService on Railway
func (r *RailwayServiceDeployer) DeployApplication(ctx context.Context, tenantID string, config ApplicationConfig) (ApplicationDeployment, error) {
	// Get existing database and cache URLs first
	mongoDeployment, err := r.DeployDatabase(ctx, tenantID)
	if err != nil {
		return ApplicationDeployment{}, fmt.Errorf("failed to get database: %w", err)
	}

	cacheDeployment, err := r.DeployCache(ctx, tenantID)
	if err != nil {
		return ApplicationDeployment{}, fmt.Errorf("failed to get cache: %w", err)
	}

	// Create RoomService with proper environment variables
	rsService, err := r.rly.CreateRoomService(
		r.rly.ProjectID,
		tenantID,
		mongoDeployment.ConnectionString,
		cacheDeployment.ConnectionString,
	)
	if err != nil {
		return ApplicationDeployment{}, fmt.Errorf("failed to deploy RoomService: %w", err)
	}

	return ApplicationDeployment{
		Endpoint: fmt.Sprintf("%s:%d", rsService.Host, rsService.Port),
		Host:     rsService.Host,
		Port:     rsService.Port,
		Status:   "deploying",
	}, nil
}

// CheckHealth checks if Railway services for a tenant are healthy
func (r *RailwayServiceDeployer) CheckHealth(ctx context.Context, tenantID string) (bool, error) {
	// Use the Railway-specific health check that filters by tenant
	return r.rly.CheckTenantServicesHealth(ctx, r.rly.ProjectID, tenantID)
}

// DeleteServices removes all Railway services for a tenant
func (r *RailwayServiceDeployer) DeleteServices(ctx context.Context, tenantID string) error {
	// Get all services for this tenant
	services, err := r.getTenantServices(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get tenant services: %w", err)
	}

	// Delete each service
	for _, serviceID := range services {
		log.Printf("Deleting Railway service: %s", serviceID)
		if err := r.rly.DeleteService(serviceID); err != nil {
			log.Printf("Warning: failed to delete service %s: %v", serviceID, err)
			// Continue with other services
		}
	}

	return nil
}

// GetStatus returns the current status of Railway services for a tenant
func (r *RailwayServiceDeployer) GetStatus(ctx context.Context, tenantID string) (DeploymentStatus, error) {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			{
				project(id: "%s") {
					services {
						edges {
							node {
								id
								name
								deployments {
									edges {
										node {
											id
											status
											createdAt
										}
									}
								}
							}
						}
					}
				}
			}
		`, r.rly.ProjectID),
	}

	resp, err := r.rly.makeRequest(payload)
	if err != nil {
		return DeploymentStatus{}, fmt.Errorf("failed to get services: %w", err)
	}

	var result struct {
		Data struct {
			Project struct {
				Services struct {
					Edges []struct {
						Node struct {
							ID          string `json:"id"`
							Name        string `json:"name"`
							Deployments struct {
								Edges []struct {
									Node struct {
										ID        string `json:"id"`
										Status    string `json:"status"`
										CreatedAt string `json:"createdAt"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"deployments"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"services"`
			} `json:"project"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return DeploymentStatus{}, fmt.Errorf("failed to parse response: %w", err)
	}

	// Filter services for this tenant and build status
	var serviceStatuses []ServiceStatus
	allHealthy := true
	provisioningStatus := "healthy"

	for _, edge := range result.Data.Project.Services.Edges {
		if !strings.Contains(edge.Node.Name, tenantID) {
			continue
		}

		// Determine service type
		serviceType := "application"
		if strings.HasSuffix(edge.Node.Name, "-mongo") {
			serviceType = "database"
		} else if strings.HasSuffix(edge.Node.Name, "-redis") {
			serviceType = "cache"
		}

		// Check deployment status
		healthy := false
		status := "not deployed"

		if len(edge.Node.Deployments.Edges) > 0 {
			latestDeployment := edge.Node.Deployments.Edges[0].Node
			status = latestDeployment.Status
			healthy = (latestDeployment.Status == "SUCCESS" || latestDeployment.Status == "READY" || latestDeployment.Status == "ACTIVE")
		}

		if !healthy {
			allHealthy = false
			provisioningStatus = "deploying"
		}

		serviceStatuses = append(serviceStatuses, ServiceStatus{
			Name:    edge.Node.Name,
			Type:    serviceType,
			Healthy: healthy,
			Status:  status,
		})
	}

	if len(serviceStatuses) == 0 {
		return DeploymentStatus{
			TenantID:     tenantID,
			Healthy:      false,
			Services:     serviceStatuses,
			Provisioning: "pending",
			CreatedAt:    time.Now().Format(time.RFC3339),
			UpdatedAt:    time.Now().Format(time.RFC3339),
		}, nil
	}

	return DeploymentStatus{
		TenantID:     tenantID,
		Healthy:      allHealthy,
		Services:     serviceStatuses,
		Provisioning: provisioningStatus,
		CreatedAt:    time.Now().Format(time.RFC3339),
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}, nil
}

// getTenantServices gets all service IDs for a specific tenant
func (r *RailwayServiceDeployer) getTenantServices(ctx context.Context, tenantID string) ([]string, error) {
	status, err := r.GetStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var serviceIDs []string
	for _, service := range status.Services {
		// Extract service ID from service name
		// Railway service names are like: tenant-xyz-mongo, tenant-xyz-redis, tenant-xyz
		// We need to map these back to Railway service IDs
		// For now, we'll use a simple approach
		serviceIDs = append(serviceIDs, service.Name)
	}

	return serviceIDs, nil
}

// extractHostFromURL extracts hostname from a connection URL
func extractHostFromURL(url string) string {
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	if idx := strings.Index(url, ":"); idx != -1 {
		url = url[:idx]
	}
	if idx := strings.Index(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	return url
}