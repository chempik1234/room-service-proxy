package dto

// DatabaseDeployment contains information about a deployed database
type DatabaseDeployment struct {
	ConnectionString string
	Host             string
	Port             int
	Username         string
	Password         string // Caller should store securely if needed
	Database         string
	Type             string // "mongodb", "postgresql", etc.
}

// CacheDeployment contains information about a deployed cache
type CacheDeployment struct {
	ConnectionString string
	Host             string
	Port             int
	Password         string // Caller should store securely if needed
	DB               int    // Redis database number
	Type             string // "redis", "memcached", etc.
}

// ApplicationDeployment contains information about a deployed application
type ApplicationDeployment struct {
	Endpoint string // gRPC endpoint, HTTP URL, etc.
	Host     string
	Port     int
	Status   string // "deploying", "healthy", "failed"
	APIKey   string // Generated API key for this tenant
}

// TenantDeployment contains information about all deployed services for a tenant
type TenantDeployment struct {
	TenantID    string
	Database    DatabaseDeployment
	Cache       CacheDeployment
	Application ApplicationDeployment
}

// ApplicationConfig contains configuration for deploying the application
type ApplicationConfig struct {
	Image       string
	Environment map[string]string
	Resources   ResourceConfig
}

// ResourceConfig defines resource limits/requests
type ResourceConfig struct {
	CPU    string
	Memory string
}

// DeploymentStatus represents the current state of tenant services
type DeploymentStatus struct {
	TenantID     string
	Healthy      bool
	Services     []ServiceStatus
	Provisioning string // "pending", "deploying", "healthy", "failed"
	CreatedAt    string
	UpdatedAt    string
}

// ServiceStatus represents the status of an individual service
type ServiceStatus struct {
	Name    string
	Type    string // "database", "cache", "application"
	Healthy bool
	Status  string
}
