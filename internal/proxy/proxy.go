package proxy

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/credentials/insecure"

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
func NewService(db *pgxpool.Pool, limiter *ratelimit.Limiter) *Service {
	return &Service{
		db:      db,
		limiter: limiter,
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
func (s *Service) forwardUnary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, tenant *tenant.Tenant, handler grpc.UnaryHandler) (interface{}, error) {
	// Create connection to tenant instance
	tenantAddr := fmt.Sprintf("%s:%d", tenant.Host, tenant.Port)

	conn, err := grpc.DialContext(ctx, tenantAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to connect to tenant: %v", err))
	}
	defer conn.Close()

	// Create client and forward request
	// Note: This is a simplified version. In reality, you'd need to handle
	// the specific gRPC service methods you want to proxy.

	return handler(ctx, req)
}

// forwardStream forwards a streaming request to the tenant instance
func (s *Service) forwardStream(ctx context.Context, srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, tenant *tenant.Tenant, handler grpc.StreamHandler) error {
	// Create connection to tenant instance
	tenantAddr := fmt.Sprintf("%s:%d", tenant.Host, tenant.Port)

	conn, err := grpc.DialContext(ctx, tenantAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return status.Error(codes.Unavailable, fmt.Sprintf("Failed to connect to tenant: %v", err))
	}
	defer conn.Close()

	// Forward stream
	return handler(srv, ss)
}

// logRequest logs request information to database
func (s *Service) logRequest(ctx context.Context, tenantID, method, requestType string, latencyMs int, err error) {
	// Log to database asynchronously
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		statusCode := 200
		if err != nil {
			if st, ok := status.FromError(err); ok {
				statusCode = int(st.Code())
			} else {
				statusCode = 500
			}
		}

		query := `
			INSERT INTO request_logs (tenant_id, method, path, status_code, latency_ms)
			VALUES ($1, $2, $3, $4, $5)
		`

		_, dbErr := s.db.Exec(ctx, query, tenantID, method, requestType, statusCode, latencyMs)
		if dbErr != nil {
			// Log to console if database logging fails
			fmt.Printf("Failed to log request: %v\n", dbErr)
		}
	}()
}

// getClientIP extracts client IP from context
func (s *Service) getClientIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}

	if tcpAddr, ok := p.Addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}

	return p.Addr.String()
}
