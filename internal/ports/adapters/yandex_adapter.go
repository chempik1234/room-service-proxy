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
	folderID          string
	zone              string
	subnetID          string
	serviceAccountKey string
	sshKeyPath        string
	sshUser           string
	baseImageID       string
	platform          string // "standard-v2" or "standard-v3"
	coreFraction      int    // Core fraction percentage (5, 20, 100)
	memory            int    // Memory in GB
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
		sshUser:           "yc-user", // Yandex Cloud creates 'yc-user' when using --ssh-key
		baseImageID:       "fd8qbv9cd4p6tcp1f4dj0",
		platform:          "standard-v2", // 2 vCPU
		coreFraction:      20,            // 20% guaranteed CPU (cost-effective)
		memory:            4,             // 4 GB RAM
	}, nil
}

// DeployTenant deploys all services (MongoDB, Redis, RoomService) using docker-compose on a single Yandex compute instance
func (y *YandexServiceDeployer) DeployTenant(ctx context.Context, tenantID string, config dto.ApplicationConfig) (*dto.TenantDeployment, error) {
	instanceName := tenantID

	// Generate random passwords for this tenant
	mongoPassword := generateRandomPassword(32)
	redisPassword := generateRandomPassword(32)
	apiKey := generateRandomPassword(32)

	// Read SSH public key
	sshPublicKey, err := os.ReadFile(y.sshKeyPath + ".pub")
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH public key: %w", err)
	}

	// Create cloud-config for docker-compose deployment
	cloudConfig := fmt.Sprintf(`#cloud-config
ssh_pwauth: no
users:
  - name: yc-user
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    shell: /bin/bash
    ssh_authorized_keys:
      - %s
runcmd:
  - apt update
  - apt install -y docker.io docker-compose
  - systemctl enable docker
  - usermod -aG docker yc-user
  - systemctl restart docker
  - mkdir -p /opt/roomservice
`, strings.TrimSpace(string(sshPublicKey)))

	// Create docker-compose configuration with dynamic passwords
	dockerComposeConfig := fmt.Sprintf(`
version: '3.8'

services:
  roomservice:
    image: chempik1234/roomservice:latest
    container_name: roomservice
    ports:
      - "50051:50050"
    environment:
      - ROOM_SERVICE_GRPC_PORT=50050
      - ROOM_SERVICE_USE_AUTH=true
      - ROOM_SERVICE_API_KEY=%s
      - ROOM_SERVICE_ROOMS_MONGODB_DATABASE=rooms_db
      - ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION=rooms
      - ROOM_SERVICE_MONGODB_HOSTS=mongodb:27017
      - ROOM_SERVICE_MONGODB_USERNAME=admin
      - ROOM_SERVICE_MONGODB_PASSWORD=%s
      - ROOM_SERVICE_REDIS_ADDR=redis:6379
      - ROOM_SERVICE_REDIS_PASSWORD=%s
      - ROOM_SERVICE_REDIS_DB=0
    depends_on:
      mongodb:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped
    networks:
      - backend

  mongodb:
    image: mongo:6
    container_name: mongodb
    environment:
      - MONGO_INITDB_ROOT_USERNAME=admin
      - MONGO_INITDB_ROOT_PASSWORD=%s
      - MONGO_INITDB_DATABASE=rooms_db
    volumes:
      - mongo_data:/data/db
    healthcheck:
      test: ["CMD", "mongosh", "--eval", "db.adminCommand('ping')"]
      interval: 30s
      timeout: 10s
      retries: 5
    restart: unless-stopped
    networks:
      - backend

  redis:
    image: redis:7
    container_name: redis
    command: ["redis-server", "--requirepass", "%s"]
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "%s", "ping"]
      interval: 30s
      timeout: 5s
      retries: 3
    restart: unless-stopped
    networks:
      - backend

volumes:
  mongo_data:
  redis_data:

networks:
  backend:
    driver: bridge
`, apiKey, mongoPassword, redisPassword, mongoPassword, redisPassword, redisPassword)

	// Create compute instance with docker-compose
	instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig, dockerComposeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant instance: %w", err)
	}

	log.Printf("🌐 Deployed tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return &dto.TenantDeployment{
		TenantID: tenantID,
		Database: dto.DatabaseDeployment{
			ConnectionString: fmt.Sprintf("mongodb://admin:%s@%s:27017", mongoPassword, instanceIP),
			Host:            instanceIP,
			Port:            27017,
			Username:        "admin",
			Password:        mongoPassword,
			Database:        "rooms_db",
			Type:            "mongodb",
		},
		Cache: dto.CacheDeployment{
			ConnectionString: fmt.Sprintf("redis://:%s@%s:6379/0", redisPassword, instanceIP),
			Host:            instanceIP,
			Port:            6379,
			Password:        redisPassword,
			DB:              0,
			Type:            "redis",
		},
		Application: dto.ApplicationDeployment{
			Endpoint: fmt.Sprintf("%s:50051", instanceIP),
			Host:     instanceIP,
			Port:     50051,
			Status:   "healthy",
			APIKey:   apiKey,
		},
	}, nil
}

// DeployDatabase deploys MongoDB using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployDatabase(ctx context.Context, tenantID string) (dto.DatabaseDeployment, error) {
	/*
			instanceName := fmt.Sprintf("%s-mongo", tenantID)
			password := generateRandomPassword(32)

			// Read SSH public key
			sshPublicKey, err := os.ReadFile(y.sshKeyPath + ".pub")
			if err != nil {
				return dto.DatabaseDeployment{}, fmt.Errorf("failed to read SSH public key: %w", err)
			}

			// Create cloud-config for MongoDB with proper YAML format
			cloudConfig := fmt.Sprintf(`#cloud-config
		ssh_pwauth: no
		users:
		  - name: yc-user
		    sudo: "ALL=(ALL) NOPASSWD:ALL"
		    shell: /bin/bash
		    ssh_authorized_keys:
		      - %s
		`, strings.TrimSpace(string(sshPublicKey)))

			dockerFileConfig := fmt.Sprintf(`
		x-room_service-template: &room_service-template
		  image: chempik1234/roomservice:latest
		  restart: unless-stopped
		  # entrypoint: "sleep 1h"
		  depends_on:
		    redis:
		      condition: service_healthy
		    mongodb:
		      condition: service_healthy
		  environment:
		      - ROOM_SERVICE_GRPC_PORT=50050
		      - ROOM_SERVICE_USE_AUTH=true
		      - ROOM_SERVICE_API_KEY=123
		      - ROOM_SERVICE_RETRY_ATTEMPTS=3
		      - ROOM_SERVICE_RETRY_DELAY_MILLISECONDS=500
		      - ROOM_SERVICE_RETRY_BACKOFF=1
		      - ROOM_SERVICE_LOG_LEVEL=info
		      - ROOM_SERVICE_ROOMS_MONGODB_DATABASE=rooms_db
		      - ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION=rooms
		      - ROOM_SERVICE_ROOMS_MONGODB_READ_CONCERN=available
		      - ROOM_SERVICE_ROOMS_MONGODB_WRITE_CONCERN=w: 0
		      - ROOM_SERVICE_MONGODB_HOSTS=mongodb:27017
		      - ROOM_SERVICE_MONGODB_MIN_POOL_SIZE=1
		      - ROOM_SERVICE_MONGODB_MAX_POOL_SIZE=10
		      - ROOM_SERVICE_MONGODB_USERNAME=admin
		      - ROOM_SERVICE_MONGODB_PASSWORD=securepassword
		      - ROOM_SERVICE_MONGODB_PASSWORD_SET=true
		      - ROOM_SERVICE_MONGODB_RETRY_WRITES=true
		      - ROOM_SERVICE_MONGODB_RETRY_READS=true
		      - ROOM_SERVICE_REDIS_ADDR=redis:6379
		      - ROOM_SERVICE_REDIS_PASSWORD=redis_pass
		      - ROOM_SERVICE_REDIS_DB=0
		      - ROOM_SERVICE_REDIS_TTL_SECONDS=0
		      - ROOM_SERVICE_REDIS_TIMEOUT_DIAL_MILLISECONDS=5000
		      - ROOM_SERVICE_REDIS_TIMEOUT_READ_MILLISECONDS=1000
		      - ROOM_SERVICE_REDIS_TIMEOUT_WRITE_MILLISECONDS=1000
		      - ROOM_SERVICE_REDIS_RETRIES_MAX_RETRIES=3
		      - ROOM_SERVICE_REDIS_POOL_SIZE=3
		      - ROOM_SERVICE_REDIS_POOL_MIN_IDLE_CONNECTIONS=2


		services:
		  room_service:
		    <<: *room_service-template
		    ports:
		      - "50051:50050"
		    # scale: 1
		    networks:
		      - backend

		  redis:
		    image: redis:latest
		    container_name: redis
		    expose:
		      - "6379"
		    environment:
		      - REDIS_PASSWORD=redis_pass
		    volumes:
		      - redis_data:/data
		    command: ["sh", "-c", "redis-server /usr/local/etc/redis/redis.conf --requirepass $$REDIS_PASSWORD --maxmemory 524288000"]
		    healthcheck:
		      test: [ "CMD-SHELL", "redis-cli", "-a", "$$REDIS_PASSWORD", "ping" ]
		      interval: 30s
		      timeout: 5s
		      retries: 3
		    restart: unless-stopped
		    networks:
		      - backend

		  mongodb:
		    image: mongo:latest
		    environment:
		      - MONGO_INITDB_ROOT_USERNAME=admin
		      - MONGO_INITDB_ROOT_PASSWORD=securepassword
		      - MONGO_INITDB_DATABASE=rooms_db
		    volumes:
		      - mongo_data:/data/db
		    restart: unless-stopped
		    healthcheck:
		      test: [ "CMD", "mongosh", "--eval", "db.adminCommand('ping')" ]
		      interval: 30s
		      timeout: 10s
		      retries: 5
		    networks:
		      - backend

		volumes:
		  redis_data:
		  mongo_data:

		networks:
		  backend:
		    driver: bridge`)

			// Create compute instance with cloud-config
			instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig, dockerFileConfig)
			if err != nil {
				return dto.DatabaseDeployment{}, fmt.Errorf("failed to create MongoDB instance: %w", err)
			}
	*/

	// log.Printf("🌐 Deployed MongoDB for tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return dto.DatabaseDeployment{
		ConnectionString: fmt.Sprintf("mongodb://admin:%s@%s:27017", "1", "1.1.1.1"),
		Host:             "1.1.1.1",
		Port:             27017,
		Username:         "admin",
		Password:         "1",
		Database:         "rooms_db",
		Type:             "mongodb",
	}, nil
}

// DeployCache deploys Redis using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployCache(ctx context.Context, tenantID string) (dto.CacheDeployment, error) {
	/*
			instanceName := fmt.Sprintf("%s-redis", tenantID)
			password := generateRandomPassword(32)

			// Read SSH public key
			sshPublicKey, err := os.ReadFile(y.sshKeyPath + ".pub")
			if err != nil {
				return dto.CacheDeployment{}, fmt.Errorf("failed to read SSH public key: %w", err)
			}

			// Create cloud-config for Redis with proper YAML format
			cloudConfig := fmt.Sprintf(`#cloud-config
		ssh_pwauth: no
		users:
		  - name: yc-user
		    sudo: "ALL=(ALL) NOPASSWD:ALL"
		    shell: /bin/bash
		    ssh_authorized_keys:
		      - %s
		runcmd:
		  - apt update
		  - apt install -y docker.io
		  - systemctl enable docker
		  - usermod -aG docker yc-user
		  - systemctl restart docker
		  - docker run -d --name redis -p 6379:6379 redis:7 redis-server --requirepass %s
		bootcmd:
		  - 'if ! docker ps -q -f name=redis | grep -q .; then docker start redis 2>/dev/null || true; fi'
		`, strings.TrimSpace(string(sshPublicKey)), password)

			// Create compute instance with cloud-config
			instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig)
			if err != nil {
				return dto.CacheDeployment{}, fmt.Errorf("failed to create Redis instance: %w", err)
			}
	*/

	// log.Printf("🌐 Deployed Redis for tenant %s on Yandex instance %s (IP: %s)", tenantID, instanceName, instanceIP)

	return dto.CacheDeployment{
		ConnectionString: fmt.Sprintf("redis://:%s@%s:6379/0", "1234", "1.1.1.1"),
		Host:             "1.1.1.1",
		Port:             6379,
		Password:         "1234",
		DB:               0,
		Type:             "redis",
	}, nil
}

// DeployApplication deploys RoomService using Docker on Yandex compute instance
func (y *YandexServiceDeployer) DeployApplication(ctx context.Context, tenantID string, config dto.ApplicationConfig) (dto.ApplicationDeployment, error) {
	instanceName := tenantID

	// Read SSH public key
	sshPublicKey, err := os.ReadFile(y.sshKeyPath + ".pub")
	if err != nil {
		return dto.ApplicationDeployment{}, fmt.Errorf("failed to read SSH public key: %w", err)
	}

	// Create cloud-config for RoomService with proper YAML format
	cloudConfig := fmt.Sprintf(`#cloud-config
ssh_pwauth: no
users:
  - name: yc-user
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    shell: /bin/bash
    ssh_authorized_keys:
      - %s
`, strings.TrimSpace(string(sshPublicKey)))

	dockerFileConfig := `
x-room_service-template: &room_service-template
  image: chempik1234/roomservice:latest
  restart: unless-stopped
  # entrypoint: "sleep 1h"
  depends_on:
    redis:
      condition: service_healthy
    mongodb:
      condition: service_healthy
  environment:
      - ROOM_SERVICE_GRPC_PORT=50050
      - ROOM_SERVICE_USE_AUTH=true
      - ROOM_SERVICE_API_KEY=123
      - ROOM_SERVICE_RETRY_ATTEMPTS=3
      - ROOM_SERVICE_RETRY_DELAY_MILLISECONDS=500
      - ROOM_SERVICE_RETRY_BACKOFF=1
      - ROOM_SERVICE_LOG_LEVEL=info
      - ROOM_SERVICE_ROOMS_MONGODB_DATABASE=rooms_db
      - ROOM_SERVICE_ROOMS_MONGODB_ROOMS_COLLECTION=rooms
      - ROOM_SERVICE_ROOMS_MONGODB_READ_CONCERN=available
      - "ROOM_SERVICE_ROOMS_MONGODB_WRITE_CONCERN=w: 0"
      - "ROOM_SERVICE_MONGODB_HOSTS=mongodb:27017"
      - ROOM_SERVICE_MONGODB_MIN_POOL_SIZE=1
      - ROOM_SERVICE_MONGODB_MAX_POOL_SIZE=10
      - ROOM_SERVICE_MONGODB_USERNAME=admin
      - ROOM_SERVICE_MONGODB_PASSWORD=securepassword
      - ROOM_SERVICE_MONGODB_PASSWORD_SET=true
      - ROOM_SERVICE_MONGODB_RETRY_WRITES=true
      - ROOM_SERVICE_MONGODB_RETRY_READS=true
      - "ROOM_SERVICE_REDIS_ADDR=redis:6379"
      - ROOM_SERVICE_REDIS_PASSWORD=redis_pass
      - ROOM_SERVICE_REDIS_DB=0
      - ROOM_SERVICE_REDIS_TTL_SECONDS=0
      - ROOM_SERVICE_REDIS_TIMEOUT_DIAL_MILLISECONDS=5000
      - ROOM_SERVICE_REDIS_TIMEOUT_READ_MILLISECONDS=1000
      - ROOM_SERVICE_REDIS_TIMEOUT_WRITE_MILLISECONDS=1000
      - ROOM_SERVICE_REDIS_RETRIES_MAX_RETRIES=3
      - ROOM_SERVICE_REDIS_POOL_SIZE=3
      - ROOM_SERVICE_REDIS_POOL_MIN_IDLE_CONNECTIONS=2


services:
  room_service:
    <<: *room_service-template
    ports:
      - "50051:50050"
    # scale: 1
    networks:
      - backend

  redis:
    image: redis:latest
    container_name: redis
    expose:
      - "6379"
    environment:
      - REDIS_PASSWORD=redis_pass
    volumes:
      - redis_data:/data
    command: ["sh", "-c", "redis-server --requirepass $$REDIS_PASSWORD --maxmemory 524288000"]
    healthcheck:
      test: [ "CMD-SHELL", "redis-cli", "-a", "$$REDIS_PASSWORD", "ping" ]
      interval: 30s
      timeout: 5s
      retries: 3
    restart: unless-stopped
    networks:
      - backend

  mongodb:
    image: mongo:latest
    environment:
      - MONGO_INITDB_ROOT_USERNAME=admin
      - MONGO_INITDB_ROOT_PASSWORD=securepassword
      - MONGO_INITDB_DATABASE=rooms_db
    volumes:
      - mongo_data:/data/db
    restart: unless-stopped
    healthcheck:
      test: [ "CMD", "mongosh", "--eval", "db.adminCommand('ping')" ]
      interval: 30s
      timeout: 10s
      retries: 5
    networks:
      - backend

volumes:
  redis_data:
  mongo_data:

networks:
  backend:
    driver: bridge
`

	// Create compute instance with cloud-config
	instanceIP, err := y.createComputeInstanceWithConfig(ctx, instanceName, cloudConfig, dockerFileConfig)
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
	// With docker-compose approach, we only check the single tenant VM
	instanceName := tenantID

	healthy, err := y.checkInstanceHealth(ctx, instanceName)
	if err != nil {
		log.Printf("⚠️  Instance %s health check failed: %v", instanceName, err)
		return false, err
	}

	if !healthy {
		log.Printf("⚠️  Instance %s is not healthy", instanceName)
		return false, nil
	}

	log.Printf("✅ Instance %s is healthy", instanceName)
	return true, nil
}

// DeleteServices removes all compute instances for a tenant
func (y *YandexServiceDeployer) DeleteServices(ctx context.Context, tenantID string) error {
	// With docker-compose approach, we only have one VM per tenant
	instanceName := tenantID

	log.Printf("🌐 Deleting Yandex instance: %s", instanceName)

	// Add timeout to prevent hanging (using async so should be fast)
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

	err := y.deleteComputeInstance(deleteCtx, instanceName)
	cancel() // Cancel context immediately after deletion completes
	if err != nil {
		log.Printf("⚠️  Failed to delete instance %s: %v", instanceName, err)
		return err
	}

	log.Printf("✅ Successfully deleted tenant instance: %s", instanceName)
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
func (y *YandexServiceDeployer) createComputeInstanceWithConfig(ctx context.Context, instanceName string, cloudConfig string, dockerFileConfig string) (string, error) {
	// Initialize yc config with service account key
	initCmd := exec.CommandContext(ctx, "yc", "config", "set", "service-account-key", y.serviceAccountKey)
	if output, err := initCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to initialize yc config: %w, output: %s", err, string(output))
	}

	// Read SSH public key for metadata
	sshPublicKey, err := os.ReadFile(y.sshKeyPath + ".pub")
	if err != nil {
		return "", fmt.Errorf("failed to read SSH public key: %w", err)
	}

	// Create temporary files for metadata (to avoid shell escaping issues)
	userDataFile := fmt.Sprintf("/tmp/cloud-config-%s.yaml", instanceName)
	sshKeysFile := fmt.Sprintf("/tmp/ssh-keys-%s.txt", instanceName)
	dockerContainerFile := fmt.Sprintf("/tmp/docker-%s.txt", instanceName)

	// Write cloud-config to file
	if err := os.WriteFile(userDataFile, []byte(cloudConfig), 0644); err != nil {
		return "", fmt.Errorf("failed to write user-data file: %w", err)
	}
	defer func() { _ = os.Remove(userDataFile) }() // Clean up

	// Write SSH keys to file
	sshKeyMetadata := fmt.Sprintf("yc-user:%s", strings.TrimSpace(string(sshPublicKey)))
	if err := os.WriteFile(sshKeysFile, []byte(sshKeyMetadata), 0644); err != nil {
		return "", fmt.Errorf("failed to write ssh-keys file: %w", err)
	}
	defer func() { _ = os.Remove(sshKeysFile) }() // Clean up

	// Write Docker config to file
	if err := os.WriteFile(dockerContainerFile, []byte(dockerFileConfig), 0644); err != nil {
		return "", fmt.Errorf("failed to write dokcer container spec file: %w", err)
	}
	defer func() { _ = os.Remove(dockerContainerFile) }() // Clean up

	// Create instance with metadata-from-file (avoids truncation issues)
	cmd := exec.CommandContext(ctx, "yc", "compute", "instance", "create",
		"--name", instanceName,
		"--folder-id", y.folderID,
		"--zone", y.zone,
		"--platform", y.platform,
		"--core-fraction", fmt.Sprintf("%d", y.coreFraction),
		"--memory", fmt.Sprintf("%d", y.memory),
		"--create-boot-disk", "size=20GB,image-folder-id=standard-images,image-id=fd8o107igivalvo4qola",
		"--network-interface", "subnet-id="+y.subnetID+",nat-ip-version=ipv4",
		"--metadata-from-file", "user-data="+userDataFile,
		"--metadata-from-file", "ssh-keys="+sshKeysFile,
		"--docker-compose-file", dockerContainerFile,
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
		"--async", // Don't wait for deletion to complete - prevents hanging on third VM
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w, output: %s", err, string(output))
	}

	return nil
}

// checkInstanceHealth checks if an instance is running and healthy
func (y *YandexServiceDeployer) checkInstanceHealth(ctx context.Context, instanceName string) (bool, error) {
	// Initialize yc config with service account key
	initCmd := exec.CommandContext(ctx, "yc", "config", "set", "service-account-key", y.serviceAccountKey)
	if output, err := initCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to initialize yc config: %w, output: %s", err, string(output))
	}

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
