package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// User represents a user in the system
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

// AuthAPI handles authentication operations
type AuthAPI struct {
	db *pgxpool.Pool
}

// NewAuthAPI creates a new auth API
func NewAuthAPI(db *pgxpool.Pool) *AuthAPI {
	return &AuthAPI{db: db}
}

// SignupRequest represents a signup request
type SignupRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// AuthResponse represents an authentication response
type AuthResponse struct {
	Token string `json:"token"`
	User  User  `json:"user"`
}

// generateUserID generates a unique user ID
func generateUserID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return "usr_" + base64.URLEncoding.EncodeToString(b), nil
}

// hashPassword hashes a password using bcrypt
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPassword checks a password against a hash
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// generateToken generates a simple authentication token
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// Signup handles user registration
func (auth *AuthAPI) Signup(c *gin.Context) {
	var req SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Check if user already exists
	var existingUser string
	err := auth.db.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", req.Email).Scan(&existingUser)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Generate user ID
	userID, err := generateUserID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate user ID"})
		return
	}

	// Hash password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Create user
	_, err = auth.db.Exec(ctx,
		"INSERT INTO users (id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, 'user')",
		userID, req.Email, hashedPassword, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Generate token
	token := generateToken()

	// Store token in database (you might want to use Redis for this in production)
	_, err = auth.db.Exec(ctx,
		"INSERT INTO auth_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, userID, time.Now().Add(30*24*time.Hour))
	if err != nil {
		// If token storage fails, try to create the table first
		_, createErr := auth.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS auth_tokens (
				token TEXT PRIMARY KEY,
				user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
				expires_at TIMESTAMP NOT NULL,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`)
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to setup authentication"})
			return
		}

		// Retry storing token
		_, err = auth.db.Exec(ctx,
			"INSERT INTO auth_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)",
			token, userID, time.Now().Add(30*24*time.Hour))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
			return
		}
	}

	// Get created user
	user := User{
		ID:        userID,
		Email:     req.Email,
		Name:      req.Name,
		Role:      "user",
		CreatedAt: time.Now(),
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Token: token,
		User:  user,
	})
}

// Login handles user login
func (auth *AuthAPI) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Get user
	var userID, passwordHash, name, role string
	err := auth.db.QueryRow(ctx,
		"SELECT id, password_hash, name, role FROM users WHERE email = $1",
		req.Email).Scan(&userID, &passwordHash, &name, &role)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check password
	if !checkPassword(req.Password, passwordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate token
	token := generateToken()

	// Store token
	_, err = auth.db.Exec(ctx,
		"INSERT INTO auth_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, userID, time.Now().Add(30*24*time.Hour))
	if err != nil {
		// Create table if it doesn't exist
		_, _ = auth.db.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS auth_tokens (
				token TEXT PRIMARY KEY,
				user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
				expires_at TIMESTAMP NOT NULL,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`)
		// Retry storing token
		_, err = auth.db.Exec(ctx,
			"INSERT INTO auth_tokens (token, user_id, expires_at) VALUES ($1, $2, $3)",
			token, userID, time.Now().Add(30*24*time.Hour))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
			return
		}
	}

	user := User{
		ID:        userID,
		Email:     req.Email,
		Name:      name,
		Role:      role,
		CreatedAt: time.Now(), // You might want to fetch the actual created_at from DB
	}

	c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

// GetUserFromToken retrieves a user from their authentication token
func (auth *AuthAPI) GetUserFromToken(token string) (*User, error) {
	ctx := context.Background()

	// Get user ID from token
	var userID string
	var expiresAt time.Time
	err := auth.db.QueryRow(ctx,
		"SELECT user_id, expires_at FROM auth_tokens WHERE token = $1",
		token).Scan(&userID, &expiresAt)
	if err != nil {
		return nil, err
	}

	// Check if token is expired
	if time.Now().After(expiresAt) {
		return nil, http.ErrNoCookie
	}

	// Get user details
	var email, name, role string
	err = auth.db.QueryRow(ctx,
		"SELECT email, name, role FROM users WHERE id = $1",
		userID).Scan(&email, &name, &role)
	if err != nil {
		return nil, err
	}

	return &User{
		ID:   userID,
		Email: email,
		Name: name,
		Role: role,
	}, nil
}

// Logout handles user logout
func (auth *AuthAPI) Logout(c *gin.Context) {
	// Get token from Authorization header
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No token provided"})
		return
	}

	// Remove Bearer prefix
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	// Delete token from database
	_, err := auth.db.Exec(context.Background(),
		"DELETE FROM auth_tokens WHERE token = $1", token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}
