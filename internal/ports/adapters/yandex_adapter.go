package adapters

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/dto"
)

// YandexServiceDeployer implements ServiceDeployer using Yandex Cloud Compute
type YandexServiceDeployer struct {
	folderID     string
	zone         string
	serviceAccountKey string
	sshKeyPath    string
	sshUser      string
	baseImageID   string
	platform     string // "standard-v2" or "standard-v3"
}

// NewYandexServiceDeployer creates a new Yandex Cloud deployer
func NewYandexServiceDeployer(folderID, zone, serviceAccountKey, sshKeyPath string) (*YandexServiceDeployer, error) {
	if folderID == "" {
		return nil, fmt.Errorf("YANDEX_FOLDER_ID environment variable is required")
	}

	return &YandexServiceDeployer{
		folderID:          folderID,
		zone:              zone,
		serviceAccountKey: serviceAccountKey,
		sshKeyPath:        sshKeyPath,
		sshUser:          "yandex",
		baseImageID:      "fd8qbv9cd4p6tcp1f4dj0",
		platform:         "standard-v2", // 2 vCPU, 2 GB RAM
	}, nil
}

// DeployDatabase deploys MongoDB using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployDatabase(ctx context.Context, tenantID string) (dto.DatabaseDeployment, error) {
	// For now, we'll deploy MongoDB as a Docker container on the compute instance
	// In the future, we could use Yandex Managed MongoDB
	instanceName := fmt.Sprintf("%s-mongo", tenantID)

	// Create compute instance
	instanceIP, err := y.createComputeInstance(ctx, instanceName, tenantID)
	if err != nil {
		return dto.DatabaseDeployment{}, fmt.Errorf("failed to create MongoDB instance: %w", err)
	}

	// Deploy MongoDB container via SSH
	password := generateRandomPassword(32)
	err = y.deployMongoDBContainer(ctx, instanceIP, password)
	if err != nil {
		// Cleanup instance on failure
		y.deleteComputeInstance(ctx, instanceName)
		return dto.DatabaseDeployment{}, fmt.Errorf("failed to deploy MongoDB container: %w", err)
	}

	log.Printf("🌐 Deployed MongoDB for tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return dto.DatabaseDeployment{
		ConnectionString: fmt.Sprintf("mongodb://admin:%s@%s:27017", password, instanceIP),
		Host:            instanceIP,
		Port:            27017,
		Username:        "admin",
		Password:        password,
		Database:        "rooms_db",
		Type:            "mongodb",
	}, nil
}

// DeployCache deploys Redis using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployCache(ctx context.Context, tenantID string) (dto.CacheDeployment, error) {
	instanceName := fmt.Sprintf("%s-redis", tenantID)

	// Create compute instance
	instanceIP, err := y.createComputeInstance(ctx, instanceName, tenantID)
	if err != nil {
		return dto.CacheDeployment{}, fmt.Errorf("failed to create Redis instance: %w", err)
	}

	// Deploy Redis container via SSH
	password := generateRandomPassword(32)
	err = y.deployRedisContainer(ctx, instanceIP, password)
	if err != nil {
		// Cleanup instance on failure
		y.deleteComputeInstance(ctx, instanceName)
		return dto.CacheDeployment{}, fmt.Errorf("failed to deploy Redis container: %w", err)
	}

	log.Printf("🌐 Deployed Redis for tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return dto.CacheDeployment{
		ConnectionString: fmt.Sprintf("redis://:%s@%s:6379/0", password, instanceIP),
		Host:            instanceIP,
		Port:            6379,
		Password:        password,
		DB:              0,
		Type:            "redis",
	}, nil
}

// DeployApplication deploys RoomService as a host-run binary on Yandex compute instance
func (y *YandexServiceDeployer) DeployApplication(ctx context.Context, tenantID string, config dto.ApplicationConfig) (dto.ApplicationDeployment, error) {
	instanceName := tenantID

	// Create compute instance
	instanceIP, err := y.createComputeInstance(ctx, instanceName, tenantID)
	if err != nil {
		return dto.ApplicationDeployment{}, fmt.Errorf("failed to create application instance: %w", err)
	}

	// Deploy RoomService binary via SSH
	err = y.deployRoomServiceBinary(ctx, instanceIP, tenantID, config.Environment)
	if err != nil {
		// Cleanup instance on failure
		y.deleteComputeInstance(ctx, instanceName)
		return dto.ApplicationDeployment{}, fmt.Errorf("failed to deploy RoomService binary: %w", err)
	}

	log.Printf("🌐 Deployed RoomService for tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return dto.ApplicationDeployment{
		Endpoint: fmt.Sprintf("%s:50051", instanceIP),
		Host:     instanceIP,
		Port:     50051,
		Status:   "healthy",
	}, nil
}

// CheckHealth checks if all services for a tenant are healthy
func (y *YandexServiceDeployer) CheckHealth(ctx context.Context, tenantID string) (bool, error) {
	// Check if instances are running and services are accessible
	instances := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	allHealthy := true
	for _, instanceName := range instances {
		healthy, err := y.checkInstanceHealth(ctx, instanceName)
		if err != nil || !healthy {
			allHealthy = false
			log.Printf("⚠️  Instance %s health check failed: %v", instanceName, err)
		}
	}

	return allHealthy, nil
}

// DeleteServices removes all compute instances for a tenant
func (y *YandexServiceDeployer) DeleteServices(ctx context.Context, tenantID string) error {
	instances := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	for _, instanceName := range instances {
		log.Printf("🌐 Deleting Yandex instance: %s", instanceName)
		err := y.deleteComputeInstance(ctx, instanceName)
		if err != nil {
			log.Printf("⚠️  Failed to delete instance %s: %v", instanceName, err)
		}
	}

	return nil
}

// GetStatus returns the current status of tenant services
func (y *YandexServiceDeployer) GetStatus(ctx context.Context, tenantID string) (dto.DeploymentStatus, error) {
	instances := []string{
		fmt.Sprintf("%s-mongo", tenantID),
		fmt.Sprintf("%s-redis", tenantID),
		tenantID,
	}

	var serviceStatuses []dto.ServiceStatus
	allHealthy := true

	for _, instanceName := range instances {
		healthy, _ := y.checkInstanceHealth(ctx, instanceName)

		// Determine service type
		serviceType := "application"
		if strings.HasSuffix(instanceName, "-mongo") {
			serviceType = "database"
		} else if strings.HasSuffix(instanceName, "-redis") {
			serviceType = "cache"
		}

		status := "running"
		if !healthy {
			status = "unhealthy"
			allHealthy = false
		}

		serviceStatuses = append(serviceStatuses, dto.ServiceStatus{
			Name:    instanceName,
			Type:    serviceType,
			Healthy: healthy,
			Status:  status,
		})
	}

	return dto.DeploymentStatus{
		TenantID:     tenantID,
		Healthy:      allHealthy,
		Services:     serviceStatuses,
		Provisioning: map[bool]string{true: "healthy", false: "unhealthy"}[allHealthy],
		CreatedAt:    time.Now().Format(time.RFC3339),
		UpdatedAt:    time.Now().Format(time.RFC3339),
	}, nil
}

// createComputeInstance creates a new Yandex compute instance
func (y *YandexServiceDeployer) createComputeInstance(ctx context.Context, instanceName, tenantID string) (string, error) {
	// Use yc CLI to create instance
	cmd := exec.CommandContext(ctx, "yc", "compute", "instance", "create",
		"--name", instanceName,
		"--folder-id", y.folderID,
		"--zone", y.zone,
		"--platform", y.platform,
		"--create-boot-disk", "size=20GB,image-folder-id=standard-images",
		"--ssh", fmt.Sprintf("user=%s", y.sshUser),
		"--format", "json",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create instance: %w, output: %s", err, string(output))
	}

	// Wait for instance to be ready and get IP
	time.Sleep(30 * time.Second) // Give Yandex time to provision

	// Get instance IP
	cmd = exec.CommandContext(ctx, "yc", "compute", "instance", "get",
		instanceName,
		"--folder-id", y.folderID,
		"--format", "json",
	)

	output, err = cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get instance IP: %w, output: %s", err, string(output))
	}

	// Parse JSON to extract IP (simple parsing for now)
	ip := y.parseInstanceIP(string(output))
	if ip == "" {
		return "", fmt.Errorf("failed to parse instance IP from output: %s", string(output))
	}

	return ip, nil
}

// deleteComputeInstance deletes a Yandex compute instance
func (y *YandexServiceDeployer) deleteComputeInstance(ctx context.Context, instanceName string) error {
	cmd := exec.CommandContext(ctx, "yc", "compute", "instance", "delete",
		instanceName,
		"--folder-id", y.folderID,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w, output: %s", err, string(output))
	}

	return nil
}

// deployMongoDBContainer deploys MongoDB via SSH
func (y *YandexServiceDeployer) deployMongoDBContainer(ctx context.Context, instanceIP, password string) error {
	commands := []string{
		"sudo docker update && sudo docker install -y",
		fmt.Sprintf("sudo docker run -d --name mongodb -p 27017:27017 -e MONGO_INITDB_ROOT_USERNAME=admin -e MONGO_INITDB_ROOT_PASSWORD=%s mongo:6", password),
	}

	return y.executeSSHCommands(ctx, instanceIP, commands)
}

// deployRedisContainer deploys Redis via SSH
func (y *YandexServiceDeployer) deployRedisContainer(ctx context.Context, instanceIP, password string) error {
	commands := []string{
		"sudo docker run -d --name redis -p 6379:6379 redis:7 redis-server --requirepass "+password,
	}

	return y.executeSSHCommands(ctx, instanceIP, commands)
}

// deployRoomServiceBinary deploys RoomService binary via SSH
func (y *YandexServiceDeployer) deployRoomServiceBinary(ctx context.Context, instanceIP, tenantID string, env map[string]string) error {
	// Build deployment command
	commands := []string{
		"mkdir -p /opt/roomservice",
		"cd /opt/roomservice",
		// Download binary from your CI/CD system
		fmt.Sprintf("wget -O roomservice-proxy https://your-ci-cd.com/roomservice-proxy-%s.tar.gz", tenantID),
		"tar -xzf roomservice-proxy.tar.gz",
		"sudo systemctl stop roomservice-proxy || true",
		"sudo systemctl start roomservice-proxy",
	}

	return y.executeSSHCommands(ctx, instanceIP, commands)
}

// checkInstanceHealth checks if an instance is running and healthy
func (y *YandexServiceDeployer) checkInstanceHealth(ctx context.Context, instanceName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "yc", "compute", "instance", "get",
		instanceName,
		"--folder-id", y.folderID,
		"--format", "json",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to get instance status: %w", err)
	}

	// Simple check - if instance exists and status contains "RUNNING"
	return strings.Contains(string(output), "RUNNING"), nil
}

// executeSSHCommands executes commands on remote instance via SSH
func (y *YandexServiceDeployer) executeSSHCommands(ctx context.Context, instanceIP string, commands []string) error {
	for _, cmd := range commands {
		sshCmd := exec.CommandContext(ctx, "ssh", "-i", y.sshKeyPath,
			fmt.Sprintf("%s@%s", y.sshUser, instanceIP),
			cmd,
		)

		output, err := sshCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("SSH command failed: %w, output: %s", err, string(output))
		}
	}

	return nil
}

// parseInstanceIP extracts IP address from yc CLI output
func (y *YandexServiceDeployer) parseInstanceIP(output string) string {
	// Simple parsing - look for IP address pattern
	// In production, use proper JSON parsing
	if strings.Contains(output, "one_to_one_nat") {
		// Extract IP from JSON response
		start := strings.Index(output, "address\": \"")
		if start != -1 {
			start += len("address\": \"")
			end := strings.Index(output[start:], "\"")
			if end != -1 {
				return output[start : start+end]
			}
		}
	}
	return ""
}

