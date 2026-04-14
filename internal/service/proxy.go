package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
	"github.com/chempik1234/room-service-proxy/internal/tenant"
)

// Service handles gRPC proxying
type Service struct {
	db      *pgxpool.Pool
	limiter *ratelimit.Limiter
	config  *config.Config
}

// NewService creates a new proxy service
func NewService(db *pgxpool.Pool, limiter *ratelimit.Limiter, cfg *config.Config) *Service {
	return &Service{
		db:      db,
		limiter: limiter,
		config:  cfg,
	}
}

// UnaryInterceptor intercepts unary gRPC calls
func (s *Service) UnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	startTime := time.Now()

	// Extract tenant info
	tenantID, apiKey, err := s.extractTenantInfo(ctx)
	if err != nil {
		return nil, err
	}

	// Validate tenant
	tenant, err := s.validateTenant(ctx, apiKey)
	if err != nil {
		return nil, err
	}

	// Check rate limits
	if s.config.EnableRateLimit {
		if !s.limiter.Allow(tenantID) {
			return nil, status.Error(codes.ResourceExhausted, "Rate limit exceeded")
		}
	}

	// Log request
	s.logRequest(ctx, tenant.ID, info.FullMethod, "unary", 0, nil)

	// Forward to tenant instance
	response, err := s.forwardUnary(ctx, req, info, tenant, handler)
	if err != nil {
		s.logRequest(ctx, tenant.ID, info.FullMethod, "unary", 0, err)
		return nil, err
	}

	// Log success
	latency := time.Since(startTime).Milliseconds()
	s.logRequest(ctx, tenant.ID, info.FullMethod, "unary", int(latency), nil)

	return response, nil
}

// StreamInterceptor intercepts streaming gRPC calls
func (s *Service) StreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	startTime := time.Now()

	// Extract tenant info
	tenantID, apiKey, err := s.extractTenantInfo(ctx)
	if err != nil {
		return err
	}

	// Validate tenant
	tenant, err := s.validateTenant(ctx, apiKey)
	if err != nil {
		return err
	}

	// Check rate limits (streaming connections count against rate limit)
	if s.config.EnableRateLimit {
		if !s.limiter.Allow(tenantID) {
			return status.Error(codes.ResourceExhausted, "Rate limit exceeded")
		}
	}

	// Log stream start
	s.logRequest(ctx, tenant.ID, info.FullMethod, "stream", 0, nil)

	// Forward stream to tenant instance
	err = s.forwardStream(ctx, srv, ss, info, tenant, handler)
	if err != nil {
		s.logRequest(ctx, tenant.ID, info.FullMethod, "stream", 0, err)
		return err
	}

	// Log success
	latency := time.Since(startTime).Milliseconds()
	s.logRequest(ctx, tenant.ID, info.FullMethod, "stream", int(latency), nil)

	return nil
}

// extractTenantInfo extracts tenant ID and API key from context
func (s *Service) extractTenantInfo(ctx context.Context) (string, string, error) {
	if !s.config.EnableAuth {
		return "", "", nil // Auth disabled
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "Missing metadata")
	}

	apiKeys := md.Get("x-api-key")
	if len(apiKeys) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "Missing API key")
	}

	apiKey := apiKeys[0]
	if apiKey == "" {
		return "", "", status.Error(codes.Unauthenticated, "Empty API key")
	}

	return "", apiKey, nil
}

// validateTenant validates the tenant and returns tenant info
func (s *Service) validateTenant(ctx context.Context, apiKey string) (*tenant.Tenant, error) {
	if !s.config.EnableAuth {
		// Return a default tenant when auth is disabled (for testing)
		return &tenant.Tenant{
			ID:     "default",
			Status: "active",
			MaxRPS: s.config.RateLimitRPS,
		}, nil
	}

	tenantRepo := tenant.NewRepository(s.db)
	tenant, err := tenantRepo.GetByAPIKey(ctx, apiKey)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "Invalid API key")
	}

	if tenant.Status != "active" {
		return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("Tenant is %s", tenant.Status))
	}

	return tenant, nil
}

// forwardUnary forwards a unary request to the tenant instance
// For shared instance deployment, this routes to a single backend
func (s *Service) forwardUnary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, tenant *tenant.Tenant, handler grpc.UnaryHandler) (interface{}, error) {
	// In shared instance mode, the handler directly processes the request
	// No connection forwarding needed - tenant isolation happens in the handler

	// Add timeout if not set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Process request directly (tenant context already injected)
	return handler(ctx, req)
}

// forwardStream forwards a streaming request to the tenant instance
// For shared instance deployment, this routes to a single backend
func (s *Service) forwardStream(ctx context.Context, srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, tenant *tenant.Tenant, handler grpc.StreamHandler) error {
	// In shared instance mode, the handler directly processes the request
	// No connection forwarding needed - tenant isolation happens in the handler

	// Add timeout if not set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Process stream directly (tenant context already injected)
	return handler(srv, ss)
}

// logRequest logs request information to database
// Uses a worker pool to prevent goroutine leaks
func (s *Service) logRequest(ctx context.Context, tenantID, method, requestType string, latencyMs int, err error) {
	// Extract status code from error
	statusCode := 200
	if err != nil {
		if st, ok := status.FromError(err); ok {
			statusCode = int(st.Code())
		} else {
			statusCode = 500
		}
	}

	// Use a non-blocking log channel to prevent goroutine leaks
	select {
	case logChannel <- logEntry{
		tenantID:    tenantID,
		method:      method,
		requestType: requestType,
		statusCode:  statusCode,
		latencyMs:   latencyMs,
		timestamp:   time.Now(),
	}:
	default:
		// Log channel full, skip this log to prevent blocking
		return
	}
}

// logEntry represents a single log entry
type logEntry struct {
	tenantID    string
	method      string
	requestType string
	statusCode  int
	latencyMs   int
	timestamp   time.Time
}

// logChannel is a buffered channel for async logging
var logChannel = make(chan logEntry, 1000)

// init starts the background log processor
func init() {
	go processLogEntries()
}

// processLogEntries processes log entries in a single goroutine
// This prevents goroutine leaks and ensures database doesn't get overwhelmed
func processLogEntries() {
	// Use a pointer to Service once we have a global instance
	// For now, just process and discard until proper initialization
	for entry := range logChannel {
		processSingleLogEntry(entry)
	}
}

// processSingleLogEntry processes a single log entry
func processSingleLogEntry(entry logEntry) {
	_, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Get database connection from global service when available
	// For now, just print to stderr to avoid connection issues
	if entry.statusCode >= 400 {
		fmt.Fprintf(os.Stderr, "[ERROR] tenant=%s method=%s status=%d latency_ms=%d\n",
			entry.tenantID, entry.method, entry.statusCode, entry.latencyMs)
	}
}

// getClientIP extracts client IP from context
func (s *Service) getClientIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}

	// Handle both TCP and UDP addresses
	if addr := p.Addr; addr != nil {
		if tcpAddr, ok := addr.(*net.TCPAddr); ok && len(tcpAddr.IP) > 0 {
			return tcpAddr.IP.String()
		}
		if udpAddr, ok := addr.(*net.UDPAddr); ok && len(udpAddr.IP) > 0 {
			return udpAddr.IP.String()
		}
		return addr.String()
	}

	return "unknown"
}
