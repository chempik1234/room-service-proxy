package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chempik1234/room-service-proxy/pkg/utils"
)

// Standard error types for Railway operations
var (
	ErrServiceCreate  = fmt.Errorf("railway service create failed")
	ErrServiceDelete  = fmt.Errorf("railway service delete failed")
	ErrProjectDelete  = fmt.Errorf("railway project delete failed")
	ErrServiceDeploy  = fmt.Errorf("railway service deploy failed")
	ErrServiceURL     = fmt.Errorf("railway service URL fetch failed")
	ErrAPIRequest     = fmt.Errorf("railway API request failed")
	ErrHealthCheck    = fmt.Errorf("railway health check failed")
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
		"query": fmt.Sprintf(`
			mutation($projectId: String!, $name: String!) {
				serviceCreate(input: { projectId: $projectId, name: $name, source: { image: "mongo:6" } } ) {
					id
				}
			}
		`),
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
		"query": fmt.Sprintf(`
			mutation($projectId: String!, $name: String!) {
				serviceCreate(input: { projectId: $projectId, name: $name, source: { image: "redis:7" } }) {
					id
				}
			}
		`),
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

// CreateRoomService creates a RoomService with environment variables
func (r *RailwayService) CreateRoomService(projectID, tenantID, mongoURL, redisURL string) (*RoomServiceInfo, error) {
	dockerImage := "chempik1234/roomservice:latest"

	// Prepare environment variables
	vars := []map[string]string{
		// Service configuration
		{"key": "ROOM_SERVICE_GRPC_PORT", "value": "50050"},
		{"key": "ROOM_SERVICE_USE_AUTH", "value": "true"},
		{"key": "ROOM_SERVICE_API_KEY", "value": generateRandomPassword(32)},
		{"key": "ROOM_SERVICE_RETRY_ATTEMPTS", "value": "3"},
		{"key": "ROOM_SERVICE_RETRY_DELAY_MILLISECONDS", "value": "500"},
		{"key": "ROOM_SERVICE_RETRY_BACKOFF", "value": "1"},
		{"key": "ROOM_SERVICE_LOG_LEVEL", "value": "info"},

		// MongoDB configuration
		{"key": "ROOM_SERVICE_ROOMS_MONGODB_DATABASE", "value": "rooms_db"},
		{"key": "ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION", "value": "rooms"},
		{"key": "ROOM_SERVICE_ROOMS_MONGODB_READ_CONCERN", "value": "available"},
		{"key": "ROOM_SERVICE_ROOMS_MONGODB_WRITE_CONCERN", "value": "w: 1"},
		{"key": "ROOM_SERVICE_MONGODB_HOSTS", "value": mongoURL},
		{"key": "ROOM_SERVICE_MONGODB_MIN_POOL_SIZE", "value": "1"},
		{"key": "ROOM_SERVICE_MONGODB_MAX_POOL_SIZE", "value": "10"},
		{"key": "ROOM_SERVICE_MONGODB_USERNAME", "value": "admin"},
		{"key": "ROOM_SERVICE_MONGODB_PASSWORD", "value": generateRandomPassword(32)},
		{"key": "ROOM_SERVICE_MONGODB_PASSWORD_SET", "value": "true"},
		{"key": "ROOM_SERVICE_MONGODB_RETRY_WRITES", "value": "true"},
		{"key": "ROOM_SERVICE_MONGODB_RETRY_READS", "value": "true"},

		// Redis configuration
		{"key": "ROOM_SERVICE_REDIS_ADDR", "value": redisURL},
		{"key": "ROOM_SERVICE_REDIS_PASSWORD", "value": generateRandomPassword(32)},
		{"key": "ROOM_SERVICE_REDIS_DB", "value": "0"},
		{"key": "ROOM_SERVICE_REDIS_TTL_SECONDS", "value": "3600"},
		{"key": "ROOM_SERVICE_REDIS_TIMEOUT_DIAL_MILLISECONDS", "value": "5000"},
		{"key": "ROOM_SERVICE_REDIS_TIMEOUT_READ_MILLISECONDS", "value": "1000"},
		{"key": "ROOM_SERVICE_REDIS_TIMEOUT_WRITE_MILLISECONDS", "value": "1000"},
		{"key": "ROOM_SERVICE_REDIS_RETRIES_MAX_RETRIES", "value": "3"},
		{"key": "ROOM_SERVICE_REDIS_POOL_SIZE", "value": "3"},
		{"key": "ROOM_SERVICE_REDIS_POOL_MIN_IDLE_CONNECTIONS", "value": "2"},

		// Tenant identification
		{"key": "TENANT_ID", "value": tenantID},
	}

	// Create service with environment variables in one call
	payload := map[string]interface{}{
		"query": `
			mutation($projectId: String!, $name: String!, $image: String!, $vars: [ServiceVariableInput!]!) {
				serviceCreate(input: {
					projectId: $projectId,
					name: $name,
					source: { image: $image },
					vars: $vars
				}) {
					id
				}
			}
		`,
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      tenantID,
			"image":     dockerImage,
			"vars":      vars,
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

	// Get service URL
	host, err := r.getServiceURL(projectID, serviceID, "")
	if err != nil {
		// Clean up the created service since URL fetch failed
		_ = r.DeleteService(serviceID)
		return nil, fmt.Errorf("%w: %w", ErrServiceURL, err)
	}

	// Deploy service with retry
	ctx := context.Background()
	err = utils.RetryWithBackoff(ctx, 3, 2*time.Second, func() error {
		if err := r.deployService(projectID, serviceID); err != nil {
			return fmt.Errorf("%w: %w", ErrServiceDeploy, err)
		}
		return nil
	})

	if err != nil {
		// Clean up the created service since deploy failed
		_ = r.DeleteService(serviceID)
		return nil, fmt.Errorf("failed to deploy service after retries: %w", err)
	}

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
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			{
				project(id: "%s") {
					services {
						id
						status
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
				Services []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"services"`
			} `json:"project"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return false, fmt.Errorf("%w: %w", ErrHealthCheck, err)
	}

	// Check if all services are running
	for _, service := range result.Data.Project.Services {
		if service.Status != "RUNNING" && service.Status != "READY" {
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrAPIRequest, resp.StatusCode, string(bodyBytes))
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

func (r *RailwayService) deployService(projectID, serviceID string) error {
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation {
				serviceRestart(services: ["%s"])
			}
		`, serviceID),
	}

	_, err := r.makeRequest(payload)
	return err
}
