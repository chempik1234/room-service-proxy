package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chempik1234/room-service-proxy/internal/tenant"
)

// AdminAPI handles HTTP API requests for tenant management
type AdminAPI struct {
	db          *pgxpool.Pool
	adminAPIKey string
	tenantSvc   *tenant.Service
}

// NewAdminAPI creates a new admin API
func NewAdminAPI(db *pgxpool.Pool, adminAPIKey string) *AdminAPI {
	return &AdminAPI{
		db:          db,
		adminAPIKey: adminAPIKey,
		tenantSvc:   tenant.NewService(db, ""), // Railway token set separately
	}
}

// SetupRoutes configures all API routes
func SetupRoutes(api *AdminAPI) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	router.Use(api.corsMiddleware())
	router.Use(api.authMiddleware())

	// Tenant routes
	router.POST("/api/tenants", api.createTenant)
	router.GET("/api/tenants", api.listTenants)
	router.GET("/api/tenants/:id", api.getTenant)
	router.PUT("/api/tenants/:id", api.updateTenant)
	router.DELETE("/api/tenants/:id", api.deleteTenant)
	router.POST("/api/tenants/:id/regenerate-api-key", api.regenerateAPIKey)

	// Health check
	router.GET("/health", api.healthCheck)

	// Status endpoint
	router.GET("/status", api.status)

	return router
}

// authMiddleware validates admin API key
func (api *AdminAPI) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip auth for health check
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/status" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format"})
			c.Abort()
			return
		}

		if parts[1] != api.adminAPIKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// corsMiddleware handles CORS
func (api *AdminAPI) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// createTenant creates a new tenant
func (api *AdminAPI) createTenant(c *gin.Context) {
	var req tenant.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate request
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required"})
		return
	}
	if req.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required"})
		return
	}

	// Create tenant
	newTenant, err := api.tenantSvc.CreateTenantWithProvisioning(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, newTenant)
}

// listTenants lists all tenants
func (api *AdminAPI) listTenants(c *gin.Context) {
	repo := tenant.NewRepository(api.db)

	tenants, err := repo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenants": tenants,
		"count":   len(tenants),
	})
}

// getTenant gets a specific tenant
func (api *AdminAPI) getTenant(c *gin.Context) {
	id := c.Param("id")

	repo := tenant.NewRepository(api.db)
	tenant, err := repo.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tenant not found"})
		return
	}

	c.JSON(http.StatusOK, tenant)
}

// updateTenant updates a tenant
func (api *AdminAPI) updateTenant(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Status   string `json:"status"`
		Plan     string `json:"plan"`
		MaxRooms int    `json:"max_rooms"`
		MaxRPS   int    `json:"max_rps"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	repo := tenant.NewRepository(api.db)
	existing, err := repo.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tenant not found"})
		return
	}

	// Update fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Email != "" {
		existing.Email = req.Email
	}
	if req.Host != "" {
		existing.Host = req.Host
	}
	if req.Port > 0 {
		existing.Port = req.Port
	}
	if req.Status != "" {
		existing.Status = req.Status
	}
	if req.Plan != "" {
		existing.Plan = req.Plan
	}
	if req.MaxRooms > 0 {
		existing.MaxRooms = req.MaxRooms
	}
	if req.MaxRPS > 0 {
		existing.MaxRPS = req.MaxRPS
	}

	if err := repo.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, existing)
}

// deleteTenant deletes a tenant
func (api *AdminAPI) deleteTenant(c *gin.Context) {
	id := c.Param("id")

	repo := tenant.NewRepository(api.db)
	if err := repo.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tenant deleted successfully"})
}

// regenerateAPIKey regenerates the API key for a tenant
func (api *AdminAPI) regenerateAPIKey(c *gin.Context) {
	id := c.Param("id")

	repo := tenant.NewRepository(api.db)
	newAPIKey, err := repo.RegenerateAPIKey(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_key": newAPIKey,
		"message": "API key regenerated successfully",
	})
}

// healthCheck returns health status
func (api *AdminAPI) healthCheck(c *gin.Context) {
	// Check database connection
	ctx := c.Request.Context()
	if err := api.db.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "Database connection failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// status returns detailed status information
func (api *AdminAPI) status(c *gin.Context) {
	status := map[string]interface{}{
		"service": "roomservice-proxy",
		"version": "1.0.0",
	}

	// Get tenant count
	repo := tenant.NewRepository(api.db)
	tenants, err := repo.List(c.Request.Context())
	if err == nil {
		status["tenant_count"] = len(tenants)
	}

	// Get recent request count
	var requestCount int64
	err = api.db.QueryRow(c.Request.Context(),
		"SELECT COUNT(*) FROM request_logs WHERE created_at > NOW() - INTERVAL '1 hour'").
		Scan(&requestCount)
	if err == nil {
		status["requests_last_hour"] = requestCount
	}

	c.JSON(http.StatusOK, status)
}
