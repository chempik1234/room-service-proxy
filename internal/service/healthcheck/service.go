package healthcheck

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/super-danis-library-golang/v2/pkg/logger"
	"go.uber.org/zap"
)

// Service monitors tenant health and updates status accordingly
type Service struct {
	storage       ports.TenantStorage
	deployer      ports.ServiceDeployer
	logger        *logger.Logger
	checkInterval time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	running       bool
	mu            sync.Mutex
}

// Config holds health check service configuration
type Config struct {
	CheckInterval    time.Duration // How often to check tenant health
	InitialDelay     time.Duration // Delay before first health check
	FailureThreshold int           // Number of consecutive failures before marking unhealthy
}

// DefaultConfig returns sensible defaults for health checking
func DefaultConfig() *Config {
	return &Config{
		CheckInterval:    5 * time.Minute,
		InitialDelay:     1 * time.Minute,
		FailureThreshold: 3, // Mark unhealthy after 3 consecutive failures (15 minutes)
	}
}

// NewService creates a new health check service
func NewService(storage ports.TenantStorage, deployer ports.ServiceDeployer, appLogger *logger.Logger, config *Config) (*Service, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}
	if deployer == nil {
		return nil, fmt.Errorf("deployer cannot be nil")
	}
	if appLogger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		storage:       storage,
		deployer:      deployer,
		logger:        appLogger,
		checkInterval: config.CheckInterval,
		ctx:           ctx,
		cancel:        cancel,
		running:       false,
	}, nil
}

// Start begins the health check service
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.logger.Warn(context.Background(), "Health check service already running", zap.String("service", "healthcheck"))
		return
	}

	s.running = true
	s.wg.Add(1)

	s.logger.Info(context.Background(), "Starting health check service",
		zap.Duration("check_interval", s.checkInterval),
		zap.String("service", "healthcheck"))

	go s.run()
}

// Stop gracefully stops the health check service
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.logger.Info(context.Background(), "Stopping health check service", zap.String("service", "healthcheck"))

	// Cancel context to signal goroutine to stop
	s.cancel()

	// Wait for goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info(context.Background(), "Health check service stopped gracefully", zap.String("service", "healthcheck"))
	case <-time.After(10 * time.Second):
		s.logger.Warn(context.Background(), "Health check service stop timeout", zap.String("service", "healthcheck"))
	}

	s.running = false
}

// run is the main health check loop
func (s *Service) run() {
	defer s.wg.Done()

	// Initial delay before first check
	initialDelay := 1 * time.Minute
	s.logger.Info(context.Background(), "Health check service initial delay",
		zap.Duration("delay", initialDelay),
		zap.String("service", "healthcheck"))

	select {
	case <-time.After(initialDelay):
		// Proceed to first check
	case <-s.ctx.Done():
		s.logger.Info(context.Background(), "Health check service stopped during initial delay", zap.String("service", "healthcheck"))
		return
	}

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Perform first health check immediately
	s.checkAllTenants()

	for {
		select {
		case <-ticker.C:
			s.checkAllTenants()
		case <-s.ctx.Done():
			s.logger.Info(context.Background(), "Health check service received stop signal", zap.String("service", "healthcheck"))
			return
		}
	}
}

// checkAllTenants checks health of all tenants
func (s *Service) checkAllTenants() {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	// Get all tenants
	tenants, err := s.storage.ListTenants(ctx)
	if err != nil {
		s.logger.Error(ctx, "Failed to list tenants for health check",
			zap.Error(err),
			zap.String("service", "healthcheck"))
		return
	}

	// Filter tenants that should be health checked
	// Only check active and unhealthy tenants (skip deleted, provisioning, etc.)
	var tenantsToCheck []*ports.Tenant
	for _, tenant := range tenants {
		// Only check tenants that are in active or unhealthy status
		if tenant.Status == "active" || tenant.Status == "unhealthy" {
			// And have host/port configured
			if tenant.Host != "" && tenant.Port > 0 {
				tenantsToCheck = append(tenantsToCheck, tenant)
			}
		}
	}

	if len(tenantsToCheck) == 0 {
		s.logger.Info(ctx, "No tenants to health check - no active/unhealthy tenants with host/port found",
			zap.Int("total_tenants", len(tenants)),
			zap.String("service", "healthcheck"))
		return
	}

	s.logger.Info(ctx, "Checking health of active/unhealthy tenants",
		zap.Int("total_tenants", len(tenants)),
		zap.Int("active_unhealthy_tenants", len(tenantsToCheck)),
		zap.String("service", "healthcheck"))

	// Check each tenant concurrently with a limit on concurrent checks
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // Limit to 10 concurrent checks

	for _, tenant := range tenantsToCheck {
		wg.Add(1)
		go func(t *ports.Tenant) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			s.checkTenantHealth(ctx, t)
		}(tenant)
	}

	wg.Wait()
}

// tenantHealthStatus tracks health check results for a tenant
type tenantHealthStatus struct {
	consecutiveFailures int
	lastCheckTime       time.Time
}

// healthStatusMap tracks health status across checks
var healthStatusMap = struct {
	sync.RWMutex
	status map[string]*tenantHealthStatus
}{
	status: make(map[string]*tenantHealthStatus),
}

// checkTenantHealth checks health of a single tenant and updates status if needed
func (s *Service) checkTenantHealth(ctx context.Context, tenant *ports.Tenant) {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	healthy, err := s.deployer.CheckHealth(checkCtx, tenant.ID)
	now := time.Now()

	// Get current health status
	healthStatusMap.RLock()
	status, exists := healthStatusMap.status[tenant.ID]
	healthStatusMap.RUnlock()

	if !exists {
		healthStatusMap.Lock()
		status = &tenantHealthStatus{
			consecutiveFailures: 0,
			lastCheckTime:       now,
		}
		healthStatusMap.status[tenant.ID] = status
		healthStatusMap.Unlock()
	}

	if err != nil {
		s.logger.Error(ctx, "Health check error for tenant",
			zap.String("tenant_id", tenant.ID),
			zap.String("tenant_name", tenant.Name),
			zap.String("current_status", tenant.Status),
			zap.String("error_details", err.Error()),
			zap.String("service", "healthcheck"))

		// Increment failure counter
		healthStatusMap.Lock()
		status.consecutiveFailures++
		status.lastCheckTime = now
		consecutiveFailures := status.consecutiveFailures
		healthStatusMap.Unlock()

		// Mark as unhealthy after threshold consecutive failures
		if consecutiveFailures >= 3 { // Hardcoded to 3 for now
			s.markTenantUnhealthy(ctx, tenant, err)
		}

		return
	}

	if !healthy {
		// Get current failure count for logging
		currentFailures := status.consecutiveFailures

		s.logger.Warn(ctx, "❌ Tenant health check returned unhealthy",
			zap.String("tenant_id", tenant.ID),
			zap.String("tenant_name", tenant.Name),
			zap.String("current_status", tenant.Status),
			zap.Int("consecutive_failures", currentFailures),
			zap.Int("new_count", currentFailures+1),
			zap.String("service", "healthcheck"))

		// Increment failure counter
		healthStatusMap.Lock()
		status.consecutiveFailures++
		status.lastCheckTime = now
		consecutiveFailures := status.consecutiveFailures
		healthStatusMap.Unlock()

		// Mark as unhealthy after threshold consecutive failures
		if consecutiveFailures >= 3 {
			s.markTenantUnhealthy(ctx, tenant, fmt.Errorf("health check failed"))
		}

		return
	}

	// Tenant is healthy
	s.logger.Info(ctx, "✅ Tenant health check passed",
		zap.String("tenant_id", tenant.ID),
		zap.String("tenant_name", tenant.Name),
		zap.String("previous_status", tenant.Status),
		zap.String("service", "healthcheck"))

	// Reset failure counter and update status if it was unhealthy
	healthStatusMap.Lock()
	status.consecutiveFailures = 0
	status.lastCheckTime = now
	healthStatusMap.Unlock()

	// If tenant was marked unhealthy, mark them back to healthy
	if tenant.Status == "unhealthy" {
		s.markTenantHealthy(ctx, tenant)
	}
}

// markTenantUnhealthy marks a tenant as unhealthy
func (s *Service) markTenantUnhealthy(ctx context.Context, tenant *ports.Tenant, reason error) {
	if tenant.Status == "unhealthy" {
		return // Already marked
	}

	previousStatus := tenant.Status
	log.Printf("⚠️  Marking tenant %s (%s) as UNHEALTHY: %v (was: %s)", tenant.ID, tenant.Name, reason, previousStatus)

	tenant.Status = "unhealthy"
	if err := s.storage.UpdateTenant(ctx, tenant); err != nil {
		s.logger.Error(ctx, "Failed to mark tenant as unhealthy",
			zap.String("tenant_id", tenant.ID),
			zap.Error(err),
			zap.String("service", "healthcheck"))
		return
	}

	s.logger.Warn(ctx, "🚨 Tenant marked as UNHEALTHY",
		zap.String("tenant_id", tenant.ID),
		zap.String("tenant_name", tenant.Name),
		zap.String("previous_status", previousStatus),
		zap.String("reason", reason.Error()),
		zap.String("service", "healthcheck"))
}

// markTenantHealthy marks a tenant as healthy/active
func (s *Service) markTenantHealthy(ctx context.Context, tenant *ports.Tenant) {
	previousStatus := tenant.Status
	log.Printf("✅ Marking tenant %s (%s) as HEALTHY/ACTIVE (was: %s)", tenant.ID, tenant.Name, previousStatus)

	tenant.Status = "active"
	if err := s.storage.UpdateTenant(ctx, tenant); err != nil {
		s.logger.Error(ctx, "Failed to mark tenant as healthy",
			zap.String("tenant_id", tenant.ID),
			zap.Error(err),
			zap.String("service", "healthcheck"))
		return
	}

	s.logger.Info(ctx, "✅ Tenant marked as HEALTHY/ACTIVE",
		zap.String("tenant_id", tenant.ID),
		zap.String("tenant_name", tenant.Name),
		zap.String("previous_status", previousStatus),
		zap.String("service", "healthcheck"))
}

// IsRunning returns whether the health check service is currently running
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
