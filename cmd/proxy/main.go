package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
	transportgrpc "github.com/chempik1234/room-service-proxy/internal/transport/grpc"
	transportHttp "github.com/chempik1234/room-service-proxy/internal/transport/http"
	"github.com/chempik1234/super-danis-library-golang/v2/pkg/logger"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"go.uber.org/zap"
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

	// Initialize proxy service
	proxyService := transportgrpc.NewService(db, limiter, cfg)

	// Setup graceful shutdown
	setupGracefulShutdown(db, ctx)

	// Start gRPC server
	go startGRPCServer(proxyService, cfg, ctx)

	// Start admin API server (creates its own database connection and tenant service)
	startAdminAPIServer(cfg, ctx)
}

// setupGracefulShutdown handles graceful shutdown of all services
func setupGracefulShutdown(db *pgxpool.Pool, ctx context.Context) {
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go func() {
		sig := <-sigChan
		logger.GetLoggerFromCtx(ctx).Info(ctx, "Received shutdown signal", zap.String("signal", sig.String()))

		// Close database connection
		logger.GetLoggerFromCtx(ctx).Info(ctx, "Closing database connection...")
		db.Close()

		logger.GetLoggerFromCtx(ctx).Info(ctx, "Graceful shutdown complete")
		os.Exit(0)
	}()
}

// startGRPCServer starts the gRPC server
func startGRPCServer(proxyService *transportgrpc.Service, cfg *config.Config, ctx context.Context) {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.GRPCPort))
	if err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to listen", zap.Error(err))
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer(
		grpc.UnaryInterceptor(proxyService.UnaryInterceptor),
		grpc.StreamInterceptor(proxyService.StreamInterceptor),
	)

	// Register reflection for debugging
	reflection.Register(server)

	logger.GetLoggerFromCtx(ctx).Info(ctx, "gRPC server started", zap.Int("port", cfg.GRPCPort))
	if err := server.Serve(listener); err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to serve gRPC", zap.Error(err))
		log.Fatalf("Failed to serve: %v", err)
	}
}

// startAdminAPIServer starts the HTTP admin API server
func startAdminAPIServer(cfg *config.Config, ctx context.Context) {
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

	adminAPI, err := transportHttp.NewAdminAPI(cfg.DatabaseURL, cfg.AdminAPIKey, cfg.DeploymentProvider, deploymentConfig)
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
		IdleTimeout:   60 * time.Second,
	}

	logger.GetLoggerFromCtx(ctx).Info(ctx, "Admin API server started", zap.Int("port", cfg.AdminPort), zap.String("deployment_provider", cfg.DeploymentProvider))
	if err := server.ListenAndServe(); err != nil {
		logger.GetLoggerFromCtx(ctx).Error(ctx, "Failed to start admin API", zap.Error(err))
		log.Fatalf("Failed to start admin API: %v", err)
	}
}
