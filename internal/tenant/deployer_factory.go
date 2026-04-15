package tenant

import (
	"fmt"
	"os"

	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
)

// NewServiceDeployer creates a ServiceDeployer based on environment configuration
// Supports: "railway", "docker", "auto" (default: railway)
func NewServiceDeployer() (ports.ServiceDeployer, error) {
	deployerType := os.Getenv("SERVICE_DEPLOYER")

	// Default to Railway if not specified
	if deployerType == "" {
		deployerType = "railway"
	}

	switch deployerType {
	case "railway":
		return newRailwayDeployer()
	case "docker":
		return adapters.NewDockerServiceDeployer(), nil
	case "auto":
		// Auto-detect based on available environment variables
		if hasRailwayCredentials() {
			return newRailwayDeployer()
		}
		return adapters.NewDockerServiceDeployer(), nil
	default:
		return nil, fmt.Errorf("unknown deployer type: %s (supported: railway, docker, auto)", deployerType)
	}
}

// newRailwayDeployer creates a Railway deployer with environment credentials
func newRailwayDeployer() (ports.ServiceDeployer, error) {
	token := os.Getenv("RAILWAY_TOKEN")
	projectID := os.Getenv("RAILWAY_PROJECT_ID")
	environmentID := os.Getenv("RAILWAY_ENVIRONMENT_ID")

	if token == "" || projectID == "" || environmentID == "" {
		return nil, fmt.Errorf("Railway deployer requires RAILWAY_TOKEN, RAILWAY_PROJECT_ID, and RAILWAY_ENVIRONMENT_ID")
	}

	return adapters.NewRailwayServiceDeployer(token, projectID, environmentID), nil
}

// hasRailwayCredentials checks if Railway credentials are available
func hasRailwayCredentials() bool {
	return os.Getenv("RAILWAY_TOKEN") != "" &&
		os.Getenv("RAILWAY_PROJECT_ID") != "" &&
		os.Getenv("RAILWAY_ENVIRONMENT_ID") != ""
}
