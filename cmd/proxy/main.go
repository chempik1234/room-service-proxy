package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters"
	"github.com/chempik1234/room-service-proxy/internal/ports/adapters/postgres"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
	"github.com/chempik1234/room-service-proxy/internal/service/healthcheck"
	transportgrpc "github.com/chempik1234/room-service-proxy/internal/transport/grpc"
	transportHttp "github.com/chempik1234/room-service-proxy/internal/transport/http"
	"github.com/chempik1234/super-danis-library-golang/v2/pkg/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	ctx, _ := logger.New(context.Background())

	logger.GetLoggerFromCtx(ctx).Info(ctx, "Configuration loaded successfully")
	logger.GetLoggerFromCtx(ctx).Info(ctx, "Initializing database connection")

	// Connect to database
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to connect to database", zap.Error(err))
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	logger.GetLoggerFromCtx(ctx).Info(ctx, "Database connected successfully")

	// Initialize rate limiter
	limiter := ratelimit.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitWindow, cfg.RateLimitBurst)
	logger.GetLoggerFromCtx(ctx).Info(ctx, "Rate limiter initialized",
		zap.Int("rps", cfg.RateLimitRPS),
		zap.Duration("window", cfg.RateLimitWindow),
		zap.Int("burst", cfg.RateLimitBurst))

	// Repositories - Initialize all storage repositories here in main.go
	// following dependency injection pattern. All repositories are created
	// with the same database connection pool and passed to services that need them.
	storageRepo, err := postgres.NewPostgresTenantStorage(db)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to init storage repo", zap.Error(err))
		log.Fatalf("Failed to init storage repo: %v", err)
	}

	userStorage, err := postgres.NewPostgresUserStorage(db)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to init user storage", zap.Error(err))
		log.Fatalf("Failed to init user storage: %v", err)
	}

	// Auth token storage for session management (login/logout tokens)
	authTokenStorage, err := postgres.NewPostgresAuthTokenStorage(db)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to init auth token storage", zap.Error(err))
		log.Fatalf("Failed to init auth token storage: %v", err)
	}

	// Request log storage for analytics and monitoring
	requestLogStorage, err := postgres.NewPostgresRequestLogStorage(db)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to init request log storage", zap.Error(err))
		log.Fatalf("Failed to init request log storage: %v", err)
	}

	// Initialize proxy service
	appLogger := logger.GetLoggerFromCtx(ctx)
	proxyService := transportgrpc.NewService(storageRepo, limiter, cfg, appLogger)

	// Create deployment provider and health check service
	deployer, err := createDeployer(cfg)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to create deployer", zap.Error(err))
		log.Fatalf("Failed to create deployer: %v", err)
	}

	healthCheckService, err := healthcheck.NewService(storageRepo, deployer, appLogger, healthcheck.DefaultConfig())
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to create health check service", zap.Error(err))
		log.Fatalf("Failed to create health check service: %v", err)
	}

	// Setup graceful shutdown with health check service
	setupGracefulShutdown(ctx, db, healthCheckService)

	// Start health check service
	healthCheckService.Start()

	// Start gRPC server
	go startGRPCServer(ctx, proxyService, cfg)

	// Start admin API server with all repositories
	// All repositories are passed as parameters following dependency injection pattern
	startAdminAPIServer(ctx, cfg, db, storageRepo, userStorage, authTokenStorage, requestLogStorage, deployer)
}

// setupGracefulShutdown handles graceful shutdown of all services
func setupGracefulShutdown(ctx context.Context, db *pgxpool.Pool, healthCheckService *healthcheck.Service) {
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go func() {
		sig := <-sigChan
		logger.GetLoggerFromCtx(ctx).Info(ctx, "Received shutdown signal", zap.String("signal", sig.String()))

		// Stop health check service
		logger.GetLoggerFromCtx(ctx).Info(ctx, "Stopping health check service...")
		healthCheckService.Stop()

		// Close database connection
		logger.GetLoggerFromCtx(ctx).Info(ctx, "Closing database connection...")
		db.Close()

		logger.GetLoggerFromCtx(ctx).Info(ctx, "Graceful shutdown complete")
		os.Exit(0)
	}()
}

// startGRPCServer starts the gRPC server
func startGRPCServer(ctx context.Context, proxyService *transportgrpc.Service, cfg *config.Config) {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.GRPCPort))
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to listen", zap.Error(err))
		log.Fatalf("Failed to listen: %v", err)
	}

	// Use the new proxy server with proper grpc-proxy library
	server := proxyService.GetProxyServer()

	// Register reflection for debugging
	reflection.Register(server)

	logger.GetLoggerFromCtx(ctx).Info(ctx, "gRPC proxy server started with mwitkow/grpc-proxy", zap.Int("port", cfg.GRPCPort))
	if err := server.Serve(listener); err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to serve gRPC", zap.Error(err))
		log.Fatalf("Failed to serve: %v", err)
	}
}

// createDeployer creates the appropriate deployer based on configuration
func createDeployer(cfg *config.Config) (ports.ServiceDeployer, error) {
	switch cfg.DeploymentProvider {
	case "railway":
		railwayToken := cfg.RailwayToken
		railwayProjectID := cfg.RailwayProjectID
		railwayEnvironmentID := cfg.RailwayEnvironmentID
		deployer := adapters.NewRailwayServiceDeployer(railwayToken, railwayProjectID, railwayEnvironmentID)
		return deployer, nil
	case "yandex":
		yandexFolderID := cfg.YandexFolderID
		yandexZone := cfg.YandexZone
		yandexSubnetID := cfg.YandexSubnetID
		yandexServiceAccountKey := cfg.YandexServiceAccountKey
		yandexSSHKeyPath := cfg.YandexSSHKeyPath
		deployer, err := adapters.NewYandexServiceDeployer(yandexFolderID, yandexZone, yandexSubnetID, yandexServiceAccountKey, yandexSSHKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create yandex deployer: %w", err)
		}
		return deployer, nil
	case "docker":
		return nil, fmt.Errorf("docker adapter is currently disabled; please use Railway deployment instead")
	default:
		return nil, fmt.Errorf("unknown deployment provider: %s", cfg.DeploymentProvider)
	}
}

// startAdminAPIServer starts the HTTP admin API server
//
// This function initializes the admin API with all required storage repositories.
// The repositories are created in main.go and passed here following dependency injection.
//
// Args:
//   - ctx: Application context for logging
//   - cfg: Application configuration
//   - db: Database connection pool (legacy, will be removed)
//   - storageRepo: Tenant storage for multi-tenant operations
//   - userStorage: User storage for authentication and user management
//   - authTokenStorage: Session token storage for login/logout
//   - requestLogStorage: Analytics storage for request tracking
//   - deployer: Service deployer for tenant management
func startAdminAPIServer(ctx context.Context, cfg *config.Config, db *pgxpool.Pool, storageRepo ports.TenantStorage, userStorage ports.UserStorage, authTokenStorage ports.AuthTokenStorage, requestLogStorage ports.RequestLogStorage, deployer ports.ServiceDeployer) {
	// Prepare deployment configuration
	deploymentConfig := make(map[string]string)

	switch cfg.DeploymentProvider {
	case "railway":
		deploymentConfig["railway_token"] = cfg.RailwayToken
		deploymentConfig["railway_project_id"] = cfg.RailwayProjectID
		deploymentConfig["railway_environment_id"] = cfg.RailwayEnvironmentID
	case "yandex":
		deploymentConfig["yandex_folder_id"] = cfg.YandexFolderID
		deploymentConfig["yandex_zone"] = cfg.YandexZone
		deploymentConfig["yandex_subnet_id"] = cfg.YandexSubnetID
		deploymentConfig["yandex_service_account_key"] = cfg.YandexServiceAccountKey
		deploymentConfig["yandex_ssh_key_path"] = cfg.YandexSSHKeyPath
	}

	adminAPI, err := transportHttp.NewAdminAPI(db, cfg.AdminAPIKey, storageRepo, userStorage, authTokenStorage, requestLogStorage, cfg.DeploymentProvider, deploymentConfig)
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to create admin API", zap.Error(err))
		log.Fatalf("Failed to create admin API: %v", err)
	}

	router := transportHttp.SetupRoutes(adminAPI)

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.AdminPort),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.GetLoggerFromCtx(ctx).Info(ctx, "Admin API server started", zap.Int("port", cfg.AdminPort), zap.String("deployment_provider", cfg.DeploymentProvider))
	if err := server.ListenAndServe(); err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to start admin API", zap.Error(err))
		log.Fatalf("Failed to start admin API: %v", err)
	}
}
