package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chempik1234/room-service-proxy/internal/models"
	"github.com/chempik1234/room-service-proxy/internal/ports"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// User represents a user in the system (HTTP layer DTO)
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// AuthAPI handles authentication operations
type AuthAPI struct {
	userStorage      ports.UserStorage
	authTokenStorage ports.AuthTokenStorage
}

// NewAuthAPI creates a new auth API with storage repositories
func NewAuthAPI(userStorage ports.UserStorage, authTokenStorage ports.AuthTokenStorage) *AuthAPI {
	return &AuthAPI{
		userStorage:      userStorage,
		authTokenStorage: authTokenStorage,
	}
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
	User  User   `json:"user"`
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
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
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
	_, err := auth.userStorage.GetUserByEmail(ctx, req.Email)
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
	user := &models.User{
		ID:           userID,
		Email:        req.Email,
		PasswordHash: hashedPassword,
		Name:         req.Name,
		Role:         "user",
	}

	if err := auth.userStorage.CreateUser(ctx, user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	// Generate token
	token := generateToken()

	// Store token
	authToken := &models.AuthToken{
		Token:     token,
		UserID:    userID,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	if err := auth.authTokenStorage.CreateToken(ctx, authToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Token: token,
		User: User{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
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

	// Get user by email
	user, err := auth.userStorage.GetUserByEmail(ctx, req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check password
	if !checkPassword(req.Password, user.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate token
	token := generateToken()

	// Store token
	authToken := &models.AuthToken{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}

	if err := auth.authTokenStorage.CreateToken(ctx, authToken); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		User: User{
			ID:        user.ID,
			Email:     user.Email,
			Name:      user.Name,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	})
}

// GetUserFromToken retrieves a user from their authentication token
func (auth *AuthAPI) GetUserFromToken(token string) (*User, error) {
	ctx := context.Background()

	// Get token from storage
	authToken, err := auth.authTokenStorage.GetToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired token")
	}

	// Get user details
	user, err := auth.userStorage.GetUserByID(ctx, authToken.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	return &User{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
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

	// Delete token from storage
	if err := auth.authTokenStorage.DeleteToken(context.Background(), token); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}
