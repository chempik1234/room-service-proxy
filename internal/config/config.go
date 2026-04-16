package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the proxy service
type Config struct {
	// Database
	DatabaseURL string

	// gRPC Server
	GRPCPort int

	// Rate Limiting
	RateLimitRPS    int           // Requests per second per tenant
	RateLimitWindow time.Duration // Time window for rate limiting
	RateLimitBurst  int           // Burst size for rate limiting

	// Admin API
	AdminPort   int
	AdminAPIKey string

	// Feature flags
	EnableAuth      bool
	EnableRateLimit bool

	// Deployment Provider
	DeploymentProvider string // "railway", "yandex", or "docker"

	// Railway config
	RailwayToken        string
	RailwayProjectID    string
	RailwayEnvironmentID string

	// Yandex Cloud config
	YandexFolderID          string
	YandexZone              string
	YandexServiceAccountKey string
	YandexSSHKeyPath         string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if exists (for local development)
	_ = godotenv.Load()

	cfg := &Config{
		// Database (required)
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// gRPC Server
		GRPCPort: getEnvAsInt("GRPC_PORT", 50051),

		// Rate Limiting
		RateLimitRPS:    getEnvAsInt("RATE_LIMIT_RPS", 100),
		RateLimitWindow: getEnvAsDuration("RATE_LIMIT_WINDOW", "60s"),
		RateLimitBurst:  getEnvAsInt("RATE_LIMIT_BURST", 10),

		// Admin API
		AdminPort:   getEnvAsInt("ADMIN_PORT", 8080),
		AdminAPIKey: getEnv("ADMIN_API_KEY", ""),

		// Feature flags
		EnableAuth:      getEnvAsBool("ENABLE_AUTH", true),
		EnableRateLimit: getEnvAsBool("ENABLE_RATE_LIMIT", true),

		// Deployment Provider
		DeploymentProvider: getEnv("DEPLOYMENT_PROVIDER", "railway"),

		// Railway config
		RailwayToken:        getEnv("RAILWAY_TOKEN", ""),
		RailwayProjectID:    getEnv("RAILWAY_PROJECT_ID", ""),
		RailwayEnvironmentID: getEnv("RAILWAY_ENVIRONMENT_ID", ""),

		// Yandex Cloud config
		YandexFolderID:          getEnv("YANDEX_FOLDER_ID", ""),
		YandexZone:              getEnv("YANDEX_ZONE", "ru-central1-a"),
		YandexServiceAccountKey: getEnv("YANDEX_SERVICE_ACCOUNT_KEY", ""),
		YandexSSHKeyPath:         getEnv("YANDEX_SSH_KEY_PATH", ""),
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks if all required configuration is present
func (c *Config) Validate() error {
	// Common validations for all providers
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	if c.AdminAPIKey == "" {
		return errors.New("ADMIN_API_KEY is required")
	}

	if c.GRPCPort <= 0 || c.GRPCPort > 65535 {
		return fmt.Errorf("invalid GRPC_PORT: %d", c.GRPCPort)
	}

	if c.AdminPort <= 0 || c.AdminPort > 65535 {
		return fmt.Errorf("invalid ADMIN_PORT: %d", c.AdminPort)
	}

	if c.RateLimitRPS <= 0 {
		return errors.New("RATE_LIMIT_RPS must be positive")
	}

	// Provider-specific validations
	switch c.DeploymentProvider {
	case "railway":
		if c.RailwayToken == "" {
			return errors.New("RAILWAY_TOKEN is required when using Railway provider")
		}
		if c.RailwayProjectID == "" {
			return errors.New("RAILWAY_PROJECT_ID is required when using Railway provider")
		}
		if c.RailwayEnvironmentID == "" {
			return errors.New("RAILWAY_ENVIRONMENT_ID is required when using Railway provider")
		}
	case "yandex":
		if c.YandexFolderID == "" {
			return errors.New("YANDEX_FOLDER_ID is required when using Yandex provider")
		}
		if c.YandexZone == "" {
			return errors.New("YANDEX_ZONE is required when using Yandex provider")
		}
		if c.YandexServiceAccountKey == "" {
			return errors.New("YANDEX_SERVICE_ACCOUNT_KEY is required when using Yandex provider")
		}
		if c.YandexSSHKeyPath == "" {
			return errors.New("YANDEX_SSH_KEY_PATH is required when using Yandex provider")
		}
	case "docker":
		// Docker doesn't require any additional configuration
		return nil
	default:
		return fmt.Errorf("unknown DEPLOYMENT_PROVIDER: %s (must be 'railway', 'yandex', or 'docker')", c.DeploymentProvider)
	}

	return nil
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getEnvAsDuration(key, defaultValue string) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	if duration, err := time.ParseDuration(defaultValue); err == nil {
		return duration
	}
	return 60 * time.Second // default fallback
}
