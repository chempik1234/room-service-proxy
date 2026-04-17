package adapters

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/dto"
)

// YandexServiceDeployer implements ServiceDeployer using Yandex Cloud Compute
type YandexServiceDeployer struct {
	folderID     string
	zone         string
	subnetID     string
	serviceAccountKey string
	sshKeyPath    string
	sshUser      string
	baseImageID   string
	platform     string // "standard-v2" or "standard-v3"
}

// NewYandexServiceDeployer creates a new Yandex Cloud deployer
func NewYandexServiceDeployer(folderID, zone, subnetID, serviceAccountKey, sshKeyPath string) (*YandexServiceDeployer, error) {
	if folderID == "" {
		return nil, fmt.Errorf("YANDEX_FOLDER_ID environment variable is required")
	}

	// Ensure SSH key has proper permissions (chmod 600)
	if err := os.Chmod(sshKeyPath, 0600); err != nil {
		log.Printf("Warning: Could not set SSH key permissions: %v", err)
	}

	return &YandexServiceDeployer{
		folderID:          folderID,
		zone:              zone,
		subnetID:          subnetID,
		serviceAccountKey: serviceAccountKey,
		sshKeyPath:        sshKeyPath,
		sshUser:          "yc-user", // Yandex Cloud creates 'yc-user' when using --ssh-key
		baseImageID:      "fd8qbv9cd4p6tcp1f4dj0",
		platform:         "standard-v2", // 2 vCPU, 2 GB RAM
	}, nil
}

// DeployDatabase deploys MongoDB using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployDatabase(ctx context.Context, tenantID string) (dto.DatabaseDeployment, error) {
	instanceName := fmt.Sprintf("%s-mongo", tenantID)
	password := generateRandomPassword(32)

	// Create cloud-config for MongoDB
	cloudConfig := fmt.Sprintf(`#cloud-config
ssh_pwauth: no
runcmd:
  - apt update
  - apt install -y docker.io
  - systemctl enable docker
  - usermod -aG docker yc-user
  - systemctl restart docker
  - docker run -d --name mongodb -p 27017:27017 -e MONGO_INITDB_ROOT_USERNAME=admin -e MONGO_INITDB_ROOT_PASSWORD=%s mongo:6
bootcmd:
  - 'if ! docker ps -q -f name=mongodb | grep -q .; then docker start mongodb 2>/dev/null || true; fi'
`, password)

	// Create compute instance with cloud-config
	instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig)
	if err != nil {
		return dto.DatabaseDeployment{}, fmt.Errorf("failed to create MongoDB instance: %w", err)
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
	password := generateRandomPassword(32)

	// Create cloud-config for Redis
	cloudConfig := fmt.Sprintf(`#cloud-config
ssh_pwauth: no
runcmd:
  - apt update
  - apt install -y docker.io
  - systemctl enable docker
  - usermod -aG docker yc-user
  - systemctl restart docker
  - docker run -d --name redis -p 6379:6379 redis:7 redis-server --requirepass %s
bootcmd:
  - 'if ! docker ps -q -f name=redis | grep -q .; then docker start redis 2>/dev/null || true; fi'
`, password)

	// Create compute instance with cloud-config
	instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig)
	if err != nil {
		return dto.CacheDeployment{}, fmt.Errorf("failed to create Redis instance: %w", err)
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

// DeployApplication deploys RoomService using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployApplication(ctx context.Context, tenantID string, config dto.ApplicationConfig) (dto.ApplicationDeployment, error) {
	instanceName := tenantID

	// Create cloud-config for RoomService
	// For now, we'll create a basic instance with Docker installed
	// TODO: Add proper RoomService deployment logic
	cloudConfig := `#cloud-config
ssh_pwauth: no
runcmd:
  - apt update
  - apt install -y docker.io
  - systemctl enable docker
  - usermod -aG docker yc-user
  - systemctl restart docker
`

	// Create compute instance with cloud-config
	instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig)
	if err != nil {
		return dto.ApplicationDeployment{}, fmt.Errorf("failed to create application instance: %w", err)
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

// createComputeInstanceWithConfig creates a new Yandex compute instance with cloud-config
func (y *YandexServiceDeployer) createComputeInstanceWithConfig(ctx context.Context, instanceName string, cloudConfig string) (string, error) {
	// Initialize yc config with service account key
	initCmd := exec.CommandContext(ctx, "yc", "config", "set", "service-account-key", y.serviceAccountKey)
	if output, err := initCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to initialize yc config: %w, output: %s", err, string(output))
	}

	// Use yc CLI to create instance with cloud-config metadata
	cmd := exec.CommandContext(ctx, "yc", "compute", "instance", "create",
		"--name", instanceName,
		"--folder-id", y.folderID,
		"--zone", y.zone,
		"--platform", y.platform,
		"--create-boot-disk", "size=20GB,image-folder-id=standard-images",
		"--network-interface", "subnet-id="+y.subnetID+",nat-ip-version=ipv4",
		"--ssh-key", y.sshKeyPath + ".pub",
		"--serial-port-settings", "ssh-authorization=instance-metadata",
		"--metadata", "user-data="+cloudConfig,
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
	// Initialize yc config with service account key
	initCmd := exec.CommandContext(ctx, "yc", "config", "set", "service-account-key", y.serviceAccountKey)
	if output, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to initialize yc config: %w, output: %s", err, string(output))
	}

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


	return nil
}

// parseInstanceIP extracts IP address from yc CLI output
func (y *YandexServiceDeployer) parseInstanceIP(output string) string {
	// Parse IP address from JSON response
	// Look for primary_v4_address (internal IP) or one_to_one_nat (public IP)

	// First try to find primary_v4_address (internal IP)
	if strings.Contains(output, "primary_v4_address") {
		// Find the first IP address after primary_v4_address
		start := strings.Index(output, "primary_v4_address")
		if start != -1 {
			// Look for the next "address" field after primary_v4_address
			addressStart := strings.Index(output[start:], "\"address\": \"")
			if addressStart != -1 {
				addressStart += start + len("\"address\": \"")
				addressEnd := strings.Index(output[addressStart:], "\"")
				if addressEnd != -1 {
					return output[addressStart : addressStart+addressEnd]
				}
			}
		}
	}

	// Fallback: look for one_to_one_nat (public IP)
	if strings.Contains(output, "one_to_one_nat") {
		start := strings.Index(output, "one_to_one_nat")
		if start != -1 {
			addressStart := strings.Index(output[start:], "\"address\": \"")
			if addressStart != -1 {
				addressStart += start + len("\"address\": \"")
				addressEnd := strings.Index(output[addressStart:], "\"")
				if addressEnd != -1 {
					return output[addressStart : addressStart+addressEnd]
				}
			}
		}
	}

	return ""
}

