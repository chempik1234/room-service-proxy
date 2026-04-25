package models

import "time"

// User represents a user in the system
type User struct {
	ID           string    `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"` // Never expose in JSON
	Name         string    `json:"name" db:"name"`
	Role         string    `json:"role" db:"role"` // admin, user
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// AuthToken represents an authentication token
type AuthToken struct {
	Token     string    `json:"token" db:"token"`
	UserID    string    `json:"user_id" db:"user_id"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// RequestLog represents a logged request
type RequestLog struct {
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	Method      string    `json:"method" db:"method"`
	RequestType string    `json:"request_type" db:"request_type"`
	StatusCode  int       `json:"status_code" db:"status_code"`
	LatencyMs   int       `json:"latency_ms" db:"latency_ms"`
	Timestamp   time.Time `json:"timestamp" db:"created_at"`
}
