package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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
				serviceCreate(projectId: $projectId, name: $name, image: "mongo:6") {
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
				serviceCreate(projectId: $projectId, name: $name, image: "redis:7") {
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
	payload := map[string]interface{}{
		"query": fmt.Sprintf(`
			mutation($projectId: String!, $name: String!) {
				serviceCreate(projectId: $projectId, name: $name) {
					id
				}
			}
		`),
		"variables": map[string]interface{}{
			"projectId": projectID,
			"name":      tenantID,
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

	// Set environment variables
	if err := r.setEnvironmentVariables(projectID, serviceID, map[string]string{
		"MONGO_URL": mongoURL,
		"REDIS_URL": redisURL,
		"TENANT_ID": tenantID,
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

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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
	// This is a simplified version. In reality, you'd need to query Railway
	// for the actual service URL or domain.
	// For now, return a placeholder that would be replaced with the actual URL.

	// In production, you'd use Railway's domain API
	return fmt.Sprintf("%s-%s.up.railway.app", projectID, serviceID), nil
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
			"vars":     vars,
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
