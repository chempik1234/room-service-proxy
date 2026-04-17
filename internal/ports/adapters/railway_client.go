package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chempik1234/room-service-proxy/pkg/utils"
)

// RailwayService handles Railway API calls
type RailwayService struct {
	Token         string
	BaseURL       string
	ProjectID     string
	EnvironmentID string
	client        *http.Client
}

// Standard error types for Railway operations
var (
	ErrServiceCreate = fmt.Errorf("railway service create failed")
	ErrServiceDelete = fmt.Errorf("railway service delete failed")
	ErrProjectDelete = fmt.Errorf("railway project delete failed")
	ErrServiceURL    = fmt.Errorf("railway service URL fetch failed")
	ErrAPIRequest    = fmt.Errorf("railway API request failed")
	ErrHealthCheck   = fmt.Errorf("railway health check failed")
)

// CreateProject creates a new Railway project
func (r *RailwayService) CreateProject(projectName string) (string, error) {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation {
				projectCreate(name: "%s") {
					id
				}
			}
		`, projectName),
	}

	resp, err := r.makeRequest(payload)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			ProjectCreate struct {
				ID string `json:"id"`
			} `json:"projectCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	return result.Data.ProjectCreate.ID, nil
}

// CreateMongoDB creates a MongoDB service
func (r *RailwayService) CreateMongoDB(projectID, tenantID, password string) (string, error) {
	payload := map[string]interface{}{
		"query": `
			mutation($projectId: String!, $name: String!) {
				serviceCreate(input: { projectId: $projectId, name: $name, source: { image: "mongo:6" } } ) {
					id
				}
			}
		`,
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      fmt.Sprintf("%s-mongo", tenantID),
		},
	}

	resp, err := r.makeRequest(payload)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			ServiceCreate struct {
				ID string `json:"id"`
			} `json:"serviceCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	// Wait for service to be ready and get connection URL
	mongoURL, err := r.getServiceURL(projectID, result.Data.ServiceCreate.ID, "mongodb")
	if err != nil {
		return "", err
	}

	return mongoURL, nil
}

// CreateRedis creates a Redis service
func (r *RailwayService) CreateRedis(projectID, tenantID, password string) (string, error) {
	payload := map[string]interface{}{
		"query": `
			mutation($projectId: String!, $name: String!) {
				serviceCreate(input: { projectId: $projectId, name: $name, source: { image: "redis:7" } }) {
					id
				}
			}
		`,
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      fmt.Sprintf("%s-redis", tenantID),
		},
	}

	resp, err := r.makeRequest(payload)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			ServiceCreate struct {
				ID string `json:"id"`
			} `json:"serviceCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	// Wait for service to be ready and get connection URL
	redisURL, err := r.getServiceURL(projectID, result.Data.ServiceCreate.ID, "redis")
	if err != nil {
		return "", err
	}

	return redisURL, nil
}

// CreateRoomService creates a RoomService
func (r *RailwayService) CreateRoomService(projectID, tenantID, mongoURL, redisURL string) (*RoomServiceInfo, error) {
	dockerImage := "chempik1234/roomservice:latest"

	// Step 1: Create service
	payload := map[string]interface{}{
		"query": `
			mutation($projectId: String!, $name: String!, $image: String!) {
				serviceCreate(input: { projectId: $projectId, name: $name, source: { image: $image } }) {
					id
				}
			}
		`,
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      tenantID,
			"image":     dockerImage,
		},
	}

	resp, err := r.makeRequest(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServiceCreate, err)
	}

	var result struct {
		Data struct {
			ServiceCreate struct {
				ID string `json:"id"`
			} `json:"serviceCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrServiceCreate, err)
	}

	serviceID := result.Data.ServiceCreate.ID

	// Step 2: Set environment variables with retry
	ctx := context.Background()
	err = utils.RetryWithBackoff(ctx, 3, 2*time.Second, func() error {
		vars := map[string]string{
			// Service configuration
			"ROOM_SERVICE_GRPC_PORT":                "50050",
			"ROOM_SERVICE_USE_AUTH":                 "true",
			"ROOM_SERVICE_API_KEY":                  generateRandomPassword(32),
			"ROOM_SERVICE_RETRY_ATTEMPTS":           "3",
			"ROOM_SERVICE_RETRY_DELAY_MILLISECONDS": "500",
			"ROOM_SERVICE_RETRY_BACKOFF":            "1",
			"ROOM_SERVICE_LOG_LEVEL":                "info",

			// MongoDB configuration
			"ROOM_SERVICE_ROOMS_MONGODB_DATABASE":         "rooms_db",
			"ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION": "rooms",
			"ROOM_SERVICE_ROOMS_MONGODB_READ_CONCERN":     "available",
			"ROOM_SERVICE_ROOMS_MONGODB_WRITE_CONCERN":    "w: 1",
			"ROOM_SERVICE_MONGODB_HOSTS":                  mongoURL,
			"ROOM_SERVICE_MONGODB_MIN_POOL_SIZE":          "1",
			"ROOM_SERVICE_MONGODB_MAX_POOL_SIZE":          "10",
			"ROOM_SERVICE_MONGODB_USERNAME":               "admin",
			"ROOM_SERVICE_MONGODB_PASSWORD":               generateRandomPassword(32),
			"ROOM_SERVICE_MONGODB_PASSWORD_SET":           "true",
			"ROOM_SERVICE_MONGODB_RETRY_WRITES":           "true",
			"ROOM_SERVICE_MONGODB_RETRY_READS":            "true",

			// Redis configuration
			"ROOM_SERVICE_REDIS_ADDR":                       redisURL,
			"ROOM_SERVICE_REDIS_PASSWORD":                   generateRandomPassword(32),
			"ROOM_SERVICE_REDIS_DB":                         "0",
			"ROOM_SERVICE_REDIS_TTL_SECONDS":                "3600",
			"ROOM_SERVICE_REDIS_TIMEOUT_DIAL_MILLISECONDS":  "5000",
			"ROOM_SERVICE_REDIS_TIMEOUT_READ_MILLISECONDS":  "1000",
			"ROOM_SERVICE_REDIS_TIMEOUT_WRITE_MILLISECONDS": "1000",
			"ROOM_SERVICE_REDIS_RETRIES_MAX_RETRIES":        "3",
			"ROOM_SERVICE_REDIS_POOL_SIZE":                  "3",
			"ROOM_SERVICE_REDIS_POOL_MIN_IDLE_CONNECTIONS":  "2",

			// Tenant identification
			"TENANT_ID": tenantID,
		}

		if err := r.setEnvironmentVariables(serviceID, vars); err != nil {
			return fmt.Errorf("failed to set environment variables: %w", err)
		}
		return nil
	})

	if err != nil {
		// Clean up the created service since env setup failed
		_ = r.DeleteService(serviceID)
		return nil, fmt.Errorf("failed to set environment variables after retries: %w", err)
	}

	// Get service URL
	host, err := r.getServiceURL(projectID, serviceID, "")
	if err != nil {
		// Clean up the created service since URL fetch failed
		_ = r.DeleteService(serviceID)
		return nil, fmt.Errorf("%w: %w", ErrServiceURL, err)
	}

	// Railway automatically deploys when service is created and env vars are set
	return &RoomServiceInfo{
		ServiceID: serviceID,
		Host:      host,
		Port:      50051, // Default gRPC port
	}, nil
}

// RoomServiceInfo contains information about a deployed RoomService
type RoomServiceInfo struct {
	ServiceID string
	Host      string
	Port      int
}

// CheckServicesHealth checks if all services in a project are healthy
func (r *RailwayService) CheckServicesHealth(ctx context.Context, projectID string) (bool, error) {
	return r.CheckTenantServicesHealth(ctx, projectID, "")
}

// CheckTenantServicesHealth checks if specific tenant services are healthy
func (r *RailwayService) CheckTenantServicesHealth(ctx context.Context, projectID string, tenantID string) (bool, error) {
	// Check if services exist and have deployments
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
										}
									}
								}
							}
						}
					}
				}
			}
		`, projectID),
	}

	resp, err := r.makeRequest(payload)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrHealthCheck, err)
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
										ID     string `json:"id"`
										Status string `json:"status"`
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
		return false, fmt.Errorf("%w: %w", ErrHealthCheck, err)
	}

	// Filter services based on tenantID if provided
	var servicesToCheck []struct {
		Node struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Deployments struct {
				Edges []struct {
					Node struct {
						ID     string `json:"id"`
						Status string `json:"status"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"deployments"`
		} `json:"node"`
	}

	if tenantID != "" {
		// Only check services for this tenant
		for _, edge := range result.Data.Project.Services.Edges {
			if strings.Contains(edge.Node.Name, tenantID) {
				servicesToCheck = append(servicesToCheck, edge)
			}
		}
		// If no tenant services found, they don't exist
		if len(servicesToCheck) == 0 {
			return false, nil
		}
	} else {
		// Check all services in project
		servicesToCheck = result.Data.Project.Services.Edges
	}

	// Check if all relevant services have at least one successful deployment
	for _, edge := range servicesToCheck {
		if len(edge.Node.Deployments.Edges) == 0 {
			return false, nil
		}

		// Check latest deployment status
		latestDeployment := edge.Node.Deployments.Edges[0]
		if latestDeployment.Node.Status != "SUCCESS" && latestDeployment.Node.Status != "READY" && latestDeployment.Node.Status != "ACTIVE" {
			return false, nil
		}
	}

	return true, nil
}

// Helper methods

func (r *RailwayService) makeRequest(payload map[string]interface{}) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAPIRequest, err)
	}

	req, err := http.NewRequest("POST", r.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAPIRequest, err)
	}

	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAPIRequest, err)
	}
		defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errorMsg := string(bodyBytes)

		// Check for Railway-specific limit errors
		if strings.Contains(errorMsg, "exceeded limit") ||
			strings.Contains(errorMsg, "service limit") ||
			strings.Contains(errorMsg, "quota exceeded") {
			return nil, fmt.Errorf("railway service limit exceeded: %s", errorMsg)
		}

		return nil, fmt.Errorf("%w: status %d: %s", ErrAPIRequest, resp.StatusCode, errorMsg)
	}

	return io.ReadAll(resp.Body)
}

func (r *RailwayService) getServiceURL(projectID, serviceID, serviceType string) (string, error) {
	// Railway service addressing:
	// Private: <service>.railway.internal
	// Public: <something>.uprailway.app

	// For database connections (MongoDB, Redis), use private addressing for performance
	if serviceType == "mongodb" || serviceType == "redis" {
		return fmt.Sprintf("%s.railway.internal", serviceID), nil
	}

	// For RoomService, use public URL pattern
	// In production, you'd query Railway's API for the actual domain
	return fmt.Sprintf("%s-%s.uprailway.app", projectID, serviceID), nil
}

// DeleteService deletes a Railway service (idempotent - silent fail if not exists)
func (r *RailwayService) DeleteService(serviceID string) error {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation {
				serviceDelete(serviceId: "%s")
			}
		`, serviceID),
	}

	// Silent fail - if service doesn't exist or deletion fails, that's fine for idempotent cleanup
	_, err := r.makeRequest(payload)
	if err != nil {
		// Log but don't return error - this makes it idempotent
		fmt.Printf("Warning: failed to delete service %s (may not exist): %v\n", serviceID, err)
		return nil
	}

	fmt.Printf("Successfully deleted service %s\n", serviceID)
	return nil
}

// DeleteProject deletes a Railway project (idempotent - silent fail if not exists)
func (r *RailwayService) DeleteProject(projectID string) error {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation {
				projectDelete(projectId: "%s")
			}
		`, projectID),
	}

	// Silent fail - if project doesn't exist, that's fine for idempotent cleanup
	_, err := r.makeRequest(payload)
	if err != nil {
		// Log but don't return error - this makes it idempotent
		fmt.Printf("Warning: failed to delete project %s (may not exist): %v\n", projectID, err)
		return nil
	}

	return nil
}

func (r *RailwayService) setEnvironmentVariables(serviceID string, vars map[string]string) error {
	payload := map[string]interface{}{
		"query": `
			mutation($input: VariableCollectionUpsertInput!) {
				variableCollectionUpsert(input: $input)
			}
		`,
		"variables": map[string]interface{}{
			"input": map[string]interface{}{
				"projectId":     r.ProjectID,
				"environmentId": r.EnvironmentID,
				"serviceId":     serviceID,
				"variables":     vars,
			},
		},
	}

	if _, err := r.makeRequest(payload); err != nil {
		return fmt.Errorf("failed to set environment variables: %w", err)
	}

	return nil
}
