package grpc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/chempik1234/super-danis-library-golang/v2/pkg/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mwitkow/grpc-proxy/proxy"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	tenantKey contextKey = "tenant"
	loggerKey contextKey = "logger"
)

// Service handles gRPC proxying
type Service struct {
	db      *pgxpool.Pool
	limiter *ratelimit.Limiter
	config  *config.Config
	logger  *logger.Logger // Application logger (not request-scoped)
	// Connection pool for tenant VMs
	tenantConns sync.Map // map[string]*grpc.ClientConn
}

// NewService creates a new proxy service
func NewService(db *pgxpool.Pool, limiter *ratelimit.Limiter, cfg *config.Config, appLogger *logger.Logger) *Service {
	return &Service{
		db:          db,
		limiter:     limiter,
		config:      cfg,
		logger:      appLogger,
		tenantConns: sync.Map{},
	}
}

// GetProxyServer returns a gRPC server with proper proxying
func (s *Service) GetProxyServer() *grpc.Server {
	// Create a gRPC server with proxy codec
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			s.requestIDUnaryInterceptor,
			s.authInterceptor,
			s.rateLimitInterceptor,
			s.loggingInterceptor,
		),
		grpc.ChainStreamInterceptor(
			s.requestIDStreamInterceptor,
			s.authStreamInterceptor,
			s.rateLimitStreamInterceptor,
			s.loggingStreamInterceptor,
		),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(s.getDirector())),
	)

	return server
}

// requestIDUnaryInterceptor generates request ID and injects into context
func (s *Service) requestIDUnaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Generate request ID
	requestID := uuid.New().String()

	// Store request ID in context for later use
	ctx = context.WithValue(ctx, loggerKey, requestID)

	return handler(ctx, req)
}

// requestIDStreamInterceptor generates request ID and injects into context for streams
func (s *Service) requestIDStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()

	// Generate request ID
	requestID := uuid.New().String()

	// Store request ID in context for later use
	ctx = context.WithValue(ctx, loggerKey, requestID)

	// Update context in server stream
	ss = &contextServerStream{
		ServerStream: ss,
		ctx:          ctx,
	}

	return handler(srv, ss)
}

// getRequestLoggerFromContext gets logger and request ID from context
func (s *Service) getRequestLoggerFromContext(ctx context.Context) (*logger.Logger, string) {
	// Get request ID from context
	requestID := "unknown"
	if id, ok := ctx.Value(loggerKey).(string); ok {
		requestID = id
	}

	// Return application logger and request ID
	return s.logger, requestID
}

// getDirector returns a director function that routes requests to tenant VMs
func (s *Service) getDirector() proxy.StreamDirector {
	return func(ctx context.Context, fullMethodName string) (context.Context, grpc.ClientConnInterface, error) {
		log, requestID := s.getRequestLoggerFromContext(ctx)

		// Extract tenant info from context
		tenant, err := s.extractTenantFromContext(ctx)
		if err != nil {
			log.Warn(ctx, "Routing failed: tenant not found in context",
				zap.String("request_id", requestID),
				zap.Error(err))
			return nil, nil, err
		}

		// Get or create connection to tenant VM
		conn, err := s.getTenantConnection(tenant, log)
		if err != nil {
			log.Warn(ctx, "Routing failed: connection error",
				zap.String("request_id", requestID),
				zap.String("tenant_id", tenant.ID),
				zap.Error(err))
			return nil, nil, status.Error(codes.Unavailable, fmt.Sprintf("Failed to connect to tenant VM: %v", err))
		}

		// Log successful routing with target IP
		target := fmt.Sprintf("%s:%d", tenant.Host, tenant.Port)
		log.Debug(ctx, "Routing request",
			zap.String("request_id", requestID),
			zap.String("tenant_id", tenant.ID),
			zap.String("target", target))

		// Return context with connection for proxy library to handle
		return ctx, conn, nil
	}
}

// getTenantConnection gets or creates a connection to the tenant VM
func (s *Service) getTenantConnection(tenant *ports.Tenant, log *logger.Logger) (*grpc.ClientConn, error) {
	ctx := context.Background()

	// Check if connection already exists
	if conn, ok := s.tenantConns.Load(tenant.ID); ok {
		log.Debug(ctx, "Using existing connection",
			zap.String("tenant_id", tenant.ID))
		return conn.(*grpc.ClientConn), nil
	}

	// Create new connection
	target := fmt.Sprintf("%s:%d", tenant.Host, tenant.Port)
	log.Info(ctx, "Creating new connection",
		zap.String("tenant_id", tenant.ID),
		zap.String("target", target))

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

	log.Info(ctx, "Connection created and pooled",
		zap.String("tenant_id", tenant.ID),
		zap.String("target", target))
	return conn, nil
}

// authInterceptor validates authentication
func (s *Service) authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	log, requestID := s.getRequestLoggerFromContext(ctx)

	// Log incoming request
	log.Debug(ctx, "Incoming gRPC request",
		zap.String("request_id", requestID),
		zap.String("method", info.FullMethod),
		zap.String("type", "unary"))

	if !s.config.EnableAuth {
		return handler(ctx, req)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		// Log auth error with warning
		log.Warn(ctx, "Authentication failed",
			zap.String("request_id", requestID),
			zap.Error(err))
		return nil, err
	}

	// Log successful authentication with tenant info
	log.Debug(ctx, "Authentication successful",
		zap.String("request_id", requestID),
		zap.String("tenant_id", tenant.ID))

	// Store tenant in context for director to use
	ctx = context.WithValue(ctx, tenantKey, tenant)

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
	if tenant, ok := ctx.Value(tenantKey).(*ports.Tenant); ok {
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
	log, requestID := s.getRequestLoggerFromContext(ctx)

	// Log incoming stream request
	log.Debug(ctx, "Incoming gRPC request",
		zap.String("request_id", requestID),
		zap.String("method", info.FullMethod),
		zap.String("type", "stream"))

	if !s.config.EnableAuth {
		return handler(srv, ss)
	}

	tenant, err := s.extractTenantFromContext(ctx)
	if err != nil {
		// Log auth error with warning
		log.Warn(ctx, "Authentication failed",
			zap.String("request_id", requestID),
			zap.Error(err))
		return err
	}

	// Log successful authentication with tenant info
	log.Debug(ctx, "Authentication successful",
		zap.String("request_id", requestID),
		zap.String("tenant_id", tenant.ID))

	// Store tenant in context for director to use
	ctx = context.WithValue(ctx, tenantKey, tenant)

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
	if tenant, ok := ctx.Value(tenantKey).(*ports.Tenant); ok {
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


// contextServerStream wraps ServerStream to override context
type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (css *contextServerStream) Context() context.Context {
	return css.ctx
}
