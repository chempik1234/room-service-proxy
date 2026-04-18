package grpc

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mwitkow/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
)

const (
	defaultRequestTimeout = 30 * time.Second
)

// Service handles gRPC proxying
type Service struct {
	db      *pgxpool.Pool
	limiter *ratelimit.Limiter
	config  *config.Config
	// Connection pool for tenant VMs
	tenantConns sync.Map // map[string]*grpc.ClientConn
}

// NewService creates a new proxy service
func NewService(db *pgxpool.Pool, limiter *ratelimit.Limiter, cfg *config.Config) *Service {
	return &Service{
		db:          db,
		limiter:     limiter,
		config:      cfg,
		tenantConns: sync.Map{},
	}
}

// GetProxyServer returns a gRPC server with proper proxying
func (s *Service) GetProxyServer() *grpc.Server {
	// Create a gRPC server with proxy codec
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			s.authInterceptor,
			s.rateLimitInterceptor,
			s.loggingInterceptor,
		),
		grpc.ChainStreamInterceptor(
			s.authStreamInterceptor,
			s.rateLimitStreamInterceptor,
			s.loggingStreamInterceptor,
		),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(s.getDirector())),
	)

	return server
}

// getDirector returns a director function that routes requests to tenant VMs
func (s *Service) getDirector() proxy.Director {
	return func(ctx context.Context, fullMethodName string) (context.Context, *grpc.ClientConn, error) {
		// Extract tenant info from context
		tenant, err := s.extractTenantFromContext(ctx)
		if err != nil {
			return nil, nil, err
		}

		// Get or create connection to tenant VM
		conn, err := s.getTenantConnection(tenant)
		if err != nil {
			return nil, nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to connect to tenant VM: %v", err))
		}

		// Return context with connection for proxy library to handle
		return ctx, conn, nil
	}
}

// getTenantConnection gets or creates a connection to the tenant VM
func (s *Service) getTenantConnection(tenant *ports.Tenant) (*grpc.ClientConn, error) {
	// Check if connection already exists
	if conn, ok := s.tenantConns.Load(tenant.ID); ok {
		return conn.(*grpc.ClientConn), nil
	}

	// Create new connection
	target := fmt.Sprintf("%s:%d", tenant.Host, tenant.Port)
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB max message size
			grpc.MaxCallSendMsgSize(4*1024*1024),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection to %s: %w", target, err)
	}

	// Store connection in pool
	s.tenantConns.Store(tenant.ID, conn)

	return conn, nil
}

// authInterceptor validates authentication
func (s *Service) authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if !s.config.EnableAuth {
		return handler(ctx, req)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Store tenant in context for director to use
	ctx = context.WithValue(ctx, "tenant", tenant)

	return handler(ctx, req)
}

// rateLimitInterceptor checks rate limits
func (s *Service) rateLimitInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if !s.config.EnableRateLimit {
		return handler(ctx, req)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if !s.limiter.Allow(tenant.ID) {
		return nil, status.Error(codes.ResourceExhausted, "Rate limit exceeded")
	}

	return handler(ctx, req)
}

// loggingInterceptor logs requests
func (s *Service) loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	startTime := time.Now()

	// Get tenant from context
	tenantID := "unknown"
	if tenant, ok := ctx.Value("tenant").(*ports.Tenant); ok {
		tenantID = tenant.ID
	}

	response, err := handler(ctx, req)

	// Log request
	latency := time.Since(startTime).Milliseconds()
	s.logRequest(ctx, tenantID, info.FullMethod, "unary", int(latency), err)

	return response, err
}

// authStreamInterceptor validates authentication for streams
func (s *Service) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()

	if !s.config.EnableAuth {
		return handler(srv, ss)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		return err
	}

	// Store tenant in context for director to use
	ctx = context.WithValue(ctx, "tenant", tenant)

	// Update context in server stream
	ss = &contextServerStream{
		ServerStream: ss,
		ctx:          ctx,
	}

	return handler(srv, ss)
}

// rateLimitStreamInterceptor checks rate limits for streams
func (s *Service) rateLimitStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()

	if !s.config.EnableRateLimit {
		return handler(srv, ss)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		return err
	}

	if !s.limiter.Allow(tenant.ID) {
		return status.Error(codes.ResourceExhausted, "Rate limit exceeded")
	}

	return handler(srv, ss)
}

// loggingStreamInterceptor logs streaming requests
func (s *Service) loggingStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	startTime := time.Now()
	ctx := ss.Context()

	// Get tenant from context
	tenantID := "unknown"
	if tenant, ok := ctx.Value("tenant").(*ports.Tenant); ok {
		tenantID = tenant.ID
	}

	err := handler(srv, ss)

	// Log request
	latency := time.Since(startTime).Milliseconds()
	s.logRequest(ctx, tenantID, info.FullMethod, "stream", int(latency), err)

	return err
}

// extractTenantFromContext extracts tenant from context metadata
func (s *Service) extractTenantFromContext(ctx context.Context) (*ports.Tenant, error) {
	if !s.config.EnableAuth {
		// Return default tenant for testing
		return &ports.Tenant{
			ID:     "default",
			Status: "active",
			MaxRPS: s.config.RateLimitRPS,
		}, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "Missing metadata")
	}

	apiKeys := md.Get("x-api-key")
	if len(apiKeys) == 0 {
		return nil, status.Error(codes.Unauthenticated, "Missing API key")
	}

	apiKey := apiKeys[0]
	if apiKey == "" {
		return nil, status.Error(codes.Unauthenticated, "Empty API key")
	}

	// Validate tenant
	return s.validateTenant(ctx, apiKey)
}

// validateTenant validates the tenant and returns tenant info
func (s *Service) validateTenant(ctx context.Context, apiKey string) (*ports.Tenant, error) {
	tenantRepo, err := adapters.NewPostgresTenantStorage(s.db)
	if err != nil {
		return nil, status.Error(codes.Internal, "Failed to create tenant repository")
	}

	tenant, err := tenantRepo.GetTenantByAPIKey(ctx, apiKey)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "Invalid API key")
	}

	if tenant.Status != "active" {
		return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("Tenant is %s", tenant.Status))
	}

	return tenant, nil
}

// logRequest logs request information to database
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
func processLogEntries() {
	for entry := range logChannel {
		processSingleLogEntry(entry)
	}
}

// processSingleLogEntry processes a single log entry
func processSingleLogEntry(entry logEntry) {
	_, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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

// contextServerStream wraps ServerStream to override context
type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (css *contextServerStream) Context() context.Context {
	return css.ctx
}
