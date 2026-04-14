package api

import (
	"fmt"
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
	authAPI     *AuthAPI
}

// NewAdminAPI creates a new admin API
func NewAdminAPI(db *pgxpool.Pool, adminAPIKey string) *AdminAPI {
	return &AdminAPI{
		db:          db,
		adminAPIKey: adminAPIKey,
		tenantSvc:   tenant.NewService(db, ""), // Railway token set separately
		authAPI:     NewAuthAPI(db),
	}
}

// SetupRoutes configures all API routes
func SetupRoutes(api *AdminAPI) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())
	router.Use(api.corsMiddleware())

	// Public routes (authentication)
	router.POST("/api/auth/signup", api.authAPI.Signup)
	router.POST("/api/auth/login", api.authAPI.Login)
	router.POST("/api/auth/logout", api.authAPI.Logout)

	// Protected routes (require authentication)
	protected := router.Group("/api")
	protected.Use(api.authMiddleware())
	{
		// Tenant routes
		protected.POST("/tenants", api.createTenant)
		protected.GET("/tenants", api.listTenants)
		protected.GET("/tenants/:id", api.getTenant)
		protected.PUT("/tenants/:id", api.updateTenant)
		protected.DELETE("/tenants/:id", api.deleteTenant)
		protected.POST("/tenants/:id/regenerate-api-key", api.regenerateAPIKey)

		// Stats and logs
		protected.GET("/stats", api.getStats)
		protected.GET("/logs", api.getLogs)
	}

	// Health check (no auth required)
	router.GET("/health", api.healthCheck)

	// Status endpoint (no auth required)
	router.GET("/status", api.status)

	return router
}

// authMiddleware validates admin API key or user token
func (api *AdminAPI) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing authorization header"})
			c.Abort()
			return
		}

		// Check if it's a Bearer token (user auth) or raw API key (admin auth)
		if strings.HasPrefix(authHeader, "Bearer ") {
			// User authentication
			token := strings.TrimPrefix(authHeader, "Bearer ")
			user, err := api.authAPI.GetUserFromToken(token)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
				c.Abort()
				return
			}

			// Set user context
			c.Set("user", user)
			c.Set("authType", "user")
		} else {
			// Admin API key authentication
			if authHeader != api.adminAPIKey {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid admin API key"})
				c.Abort()
				return
			}

			// Set admin context
			c.Set("authType", "admin")
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

	// Check if user is admin or regular user
	authType := c.GetString("authType")
	if authType == "user" {
		// Regular user creating tenant - set their user_id
		user := c.MustGet("user").(*User)
		req.UserID = user.ID
	}

	// Create tenant
	newTenant, err := api.tenantSvc.CreateTenantWithProvisioning(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, newTenant)
}

// listTenants lists all tenants (or user's tenants if not admin)
func (api *AdminAPI) listTenants(c *gin.Context) {
	repo := tenant.NewRepository(api.db)

	// Check if user is admin or regular user
	authType := c.GetString("authType")
	if authType == "admin" {
		// Admin can see all tenants
		tenants, err := repo.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"tenants": tenants,
			"count":   len(tenants),
		})
	} else {
		// Regular user can only see their own tenants
		user := c.MustGet("user").(*User)
		tenants, err := repo.ListByUserID(c.Request.Context(), user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"tenants": tenants,
			"count":   len(tenants),
		})
	}
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

// getStats returns statistics for the dashboard
func (api *AdminAPI) getStats(c *gin.Context) {
	repo := tenant.NewRepository(api.db)

	var tenants []*tenant.Tenant
	var err error

	// Check if user is admin or regular user
	authType := c.GetString("authType")
	if authType == "admin" {
		// Admin can see stats for all tenants
		tenants, err = repo.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		// Regular user can only see stats for their own tenants
		user := c.MustGet("user").(*User)
		tenants, err = repo.ListByUserID(c.Request.Context(), user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// Calculate stats
	totalTenants := len(tenants)
	activeTenants := 0
	suspendedTenants := 0

	for _, tenant := range tenants {
		if tenant.Status == "active" {
			activeTenants++
		} else if tenant.Status == "suspended" {
			suspendedTenants++
		}
	}

	// Get total requests from logs - filter by user's tenants if not admin
	var totalRequests int64
	if authType == "admin" {
		err = api.db.QueryRow(c.Request.Context(),
			"SELECT COUNT(*) FROM request_logs").
			Scan(&totalRequests)
	} else {
		// For regular users, count only requests for their tenants
		tenantIDs := make([]string, len(tenants))
		for i, tenant := range tenants {
			tenantIDs[i] = tenant.ID
		}

		err = api.db.QueryRow(c.Request.Context(),
			"SELECT COUNT(*) FROM request_logs WHERE tenant_id = ANY($1)",
			tenantIDs).
			Scan(&totalRequests)
	}

	if err != nil {
		totalRequests = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"totalTenants":     totalTenants,
		"activeTenants":    activeTenants,
		"suspendedTenants": suspendedTenants,
		"totalRequests":    totalRequests,
	})
}

// getLogs returns recent request logs
func (api *AdminAPI) getLogs(c *gin.Context) {
	// Get query parameters
	limit := 100
	if limitParam := c.Query("limit"); limitParam != "" {
		if l, err := parseInt(limitParam); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	var rows *pgx.Rows
	var err error

	// Check if user is admin or regular user
	authType := c.GetString("authType")
	if authType == "admin" {
		// Admin can see logs for all tenants
		rows, err = api.db.Query(c.Request.Context(),
			`SELECT tenant_id, method, path, status_code, response_time, created_at
			 FROM request_logs
			 ORDER BY created_at DESC
			 LIMIT $1`, limit)
	} else {
		// Regular user can only see logs for their own tenants
		user := c.MustGet("user").(*User)

		// Get user's tenants
		repo := tenant.NewRepository(api.db)
		tenants, err := repo.ListByUserID(c.Request.Context(), user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Get tenant IDs
		tenantIDs := make([]string, len(tenants))
		for i, tenant := range tenants {
			tenantIDs[i] = tenant.ID
		}

		// Query logs only for user's tenants
		rows, err = api.db.Query(c.Request.Context(),
			`SELECT tenant_id, method, path, status_code, response_time, created_at
			 FROM request_logs
			 WHERE tenant_id = ANY($1)
			 ORDER BY created_at DESC
			 LIMIT $2`, tenantIDs, limit)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type LogEntry struct {
		TenantID     string `json:"tenantId"`
		Method       string `json:"method"`
		Path         string `json:"path"`
		StatusCode   int    `json:"statusCode"`
		ResponseTime int    `json:"responseTime"`
		Timestamp    string `json:"timestamp"`
	}

	logs := []LogEntry{}
	for rows.Next() {
		var log LogEntry
		err := rows.Scan(&log.TenantID, &log.Method, &log.Path, &log.StatusCode, &log.ResponseTime, &log.Timestamp)
		if err != nil {
			continue
		}
		logs = append(logs, log)
	}

	c.JSON(http.StatusOK, logs)
}

// parseInt safely parses a string to int
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}
