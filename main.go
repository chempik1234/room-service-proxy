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

	"github.com/chempik1234/room-service-proxy/internal/api"
	"github.com/chempik1234/room-service-proxy/internal/config"
	"github.com/chempik1234/room-service-proxy/internal/proxy"
	"github.com/chempik1234/room-service-proxy/internal/ratelimit"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to database
	ctx := context.Background()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Connected to database")

	// Initialize rate limiter
	limiter := ratelimit.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitWindow, cfg.RateLimitBurst)

	// Initialize proxy service
	proxyService := proxy.NewService(db, limiter, cfg)

	// Setup graceful shutdown
	setupGracefulShutdown(proxyService, db)

	// Start gRPC server
	go startGRPCServer(proxyService, cfg)

	// Start admin API server
	startAdminAPIServer(db, cfg)
}

// setupGracefulShutdown handles graceful shutdown of all services
func setupGracefulShutdown(proxyService *proxy.Service, db *pgxpool.Pool) {
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v, initiating graceful shutdown...", sig)

		// TODO: Add graceful shutdown for gRPC server
		// TODO: Close proxy service connections

		// Close database connection
		log.Println("Closing database connection...")
		db.Close()

		log.Println("Graceful shutdown complete")
		os.Exit(0)
	}()
}

func startGRPCServer(proxyService *proxy.Service, cfg *config.Config) {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.GRPCPort))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer(
		grpc.UnaryInterceptor(proxyService.UnaryInterceptor),
		grpc.StreamInterceptor(proxyService.StreamInterceptor),
	)

	// Register gRPC services here
	// room_service.RegisterRoomServiceServer(server, proxyService)

	// Register reflection for debugging
	reflection.Register(server)

	log.Printf("gRPC server started on :%d", cfg.GRPCPort)
	if err := server.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func startAdminAPIServer(db *pgxpool.Pool, cfg *config.Config) {
	adminAPI := api.NewAdminAPI(db, cfg.AdminAPIKey, cfg.RailwayToken, cfg.RailwayProjectID)

	router := api.SetupRoutes(adminAPI)

	server := &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.AdminPort),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Admin API server started on :%d", cfg.AdminPort)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start admin API: %v", err)
	}
}
