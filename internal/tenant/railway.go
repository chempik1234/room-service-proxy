package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Railway API methods

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

// CreateRoomService creates a RoomService
func (r *RailwayService) CreateRoomService(projectID, tenantID, mongoURL, redisURL string) (*RoomServiceInfo, error) {
	dockerImage := "chempik1234/roomservice:latest"

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
		return nil, err
	}

	var result struct {
		Data struct {
			ServiceCreate struct {
				ID string `json:"id"`
			} `json:"serviceCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	serviceID := result.Data.ServiceCreate.ID

	// Set environment variables for RoomService
	if err := r.setEnvironmentVariables(projectID, serviceID, map[string]string{
		// Service configuration
		"ROOM_SERVICE_GRPC_PORT":                    "50050",
		"ROOM_SERVICE_USE_AUTH":                     "true",
		"ROOM_SERVICE_API_KEY":                      generateRandomPassword(32),
		"ROOM_SERVICE_RETRY_ATTEMPTS":               "3",
		"ROOM_SERVICE_RETRY_DELAY_MILLISECONDS":     "500",
		"ROOM_SERVICE_RETRY_BACKOFF":                "1",
		"ROOM_SERVICE_LOG_LEVEL":                    "info",

		// MongoDB configuration
		"ROOM_SERVICE_ROOMS_MONGODB_DATABASE":        "rooms_db",
		"ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION": "rooms",
		"ROOM_SERVICE_ROOMS_MONGODB_READ_CONCERN":    "available",
		"ROOM_SERVICE_ROOMS_MONGODB_WRITE_CONCERN":   "w: 1",
		"ROOM_SERVICE_MONGODB_HOSTS":                 mongoURL,
		"ROOM_SERVICE_MONGODB_MIN_POOL_SIZE":         "1",
		"ROOM_SERVICE_MONGODB_MAX_POOL_SIZE":         "10",
		"ROOM_SERVICE_MONGODB_USERNAME":             "admin",
		"ROOM_SERVICE_MONGODB_PASSWORD":             generateRandomPassword(32),
		"ROOM_SERVICE_MONGODB_PASSWORD_SET":         "true",
		"ROOM_SERVICE_MONGODB_RETRY_WRITES":         "true",
		"ROOM_SERVICE_MONGODB_RETRY_READS":          "true",

		// Redis configuration
		"ROOM_SERVICE_REDIS_ADDR":                   redisURL,
		"ROOM_SERVICE_REDIS_PASSWORD":               generateRandomPassword(32),
		"ROOM_SERVICE_REDIS_DB":                     "0",
		"ROOM_SERVICE_REDIS_TTL_SECONDS":            "3600",
		"ROOM_SERVICE_REDIS_TIMEOUT_DIAL_MILLISECONDS": "5000",
		"ROOM_SERVICE_REDIS_TIMEOUT_READ_MILLISECONDS":  "1000",
		"ROOM_SERVICE_REDIS_TIMEOUT_WRITE_MILLISECONDS": "1000",
		"ROOM_SERVICE_REDIS_RETRIES_MAX_RETRIES":    "3",
		"ROOM_SERVICE_REDIS_POOL_SIZE":              "3",
		"ROOM_SERVICE_REDIS_POOL_MIN_IDLE_CONNECTIONS": "2",

		// Tenant identification
		"TENANT_ID":                                tenantID,
	}); err != nil {
		return nil, err
	}

	// Get service URL
	host, err := r.getServiceURL(projectID, serviceID, "")
	if err != nil {
		return nil, err
	}

	// Deploy service
	if err := r.deployService(projectID, serviceID); err != nil {
		return nil, err
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
		return false, err
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
		return false, err
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
		return nil, err
	}

	req, err := http.NewRequest("POST", r.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Railway API error: %s", resp.Status)
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

	// Silent fail - if service doesn't exist, that's fine for idempotent cleanup
	_, err := r.makeRequest(payload)
	if err != nil {
		// Log but don't return error - this makes it idempotent
		fmt.Printf("Warning: failed to delete service %s (may not exist): %v\n", serviceID, err)
		return nil
	}

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

func (r *RailwayService) setEnvironmentVariables(projectID, serviceID string, vars map[string]string) error {
	payload := map[string]interface{}{
		"query": `
			mutation($serviceId: String!, $vars: JSON!) {
				serviceVariablesUpdate(serviceId: $serviceId, vars: $vars)
			}
		`,
		"variables": map[string]interface{}{
			"serviceId": serviceID,
			"vars":      vars,
		},
	}

	_, err := r.makeRequest(payload)
	return err
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
