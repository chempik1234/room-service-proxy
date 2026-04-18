package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter implements token bucket rate limiting
type Limiter struct {
	mu       sync.RWMutex
	limiters map[string]*tenantLimiter
	rps      int           // Requests per second
	window   time.Duration // Time window
	burst    int           // Burst size
}

// tenantLimiter holds rate limiting state for a single tenant
type tenantLimiter struct {
	tokens     int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewLimiter creates a new rate limiter
func NewLimiter(rps int, window time.Duration, burst int) *Limiter {
	return &Limiter{
		limiters: make(map[string]*tenantLimiter),
		rps:      rps,
		window:   window,
		burst:    burst,
	}
}

// Allow checks if a request from the given tenant is allowed
func (l *Limiter) Allow(tenantID string) bool {
	limiter := l.getTenantLimiter(tenantID)
	return limiter.allow(l.rps, l.window)
}

// getTenantLimiter gets or creates a limiter for the tenant
func (l *Limiter) getTenantLimiter(tenantID string) *tenantLimiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	if limiter, exists := l.limiters[tenantID]; exists {
		return limiter
	}

	limiter := &tenantLimiter{
		tokens:     l.burst,
		lastRefill: time.Now(),
	}

	l.limiters[tenantID] = limiter
	return limiter
}

// allow checks if the request is allowed using token bucket algorithm
func (tl *tenantLimiter) allow(rps int, window time.Duration) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tl.lastRefill)

	// Refill tokens based on elapsed time
	tokensToAdd := int(elapsed.Seconds()) * rps / int(window.Seconds())
	if tokensToAdd > 0 {
		tl.tokens += tokensToAdd
		tl.lastRefill = now
	}

	// Check if we have tokens available
	if tl.tokens > 0 {
		tl.tokens--
		return true
	}

	return false
}

// Reset removes the rate limiter for a tenant
func (l *Limiter) Reset(tenantID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.limiters, tenantID)
}

// GetStats returns current rate limiting stats for a tenant
func (l *Limiter) GetStats(tenantID string) (tokens int, lastRefill time.Time, exists bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	limiter, exists := l.limiters[tenantID]
	if !exists {
		return 0, time.Time{}, false
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	return limiter.tokens, limiter.lastRefill, true
}

// Cleanup removes stale limiters (haven't been used in a while)
func (l *Limiter) Cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanupStale(interval * 10) // Remove limiters not used in 10x interval
		}
	}
}

// cleanupStale removes limiters that haven't been used recently
func (l *Limiter) cleanupStale(staleDuration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for tenantID, limiter := range l.limiters {
		limiter.mu.Lock()
		if now.Sub(limiter.lastRefill) > staleDuration {
			delete(l.limiters, tenantID)
		}
		limiter.mu.Unlock()
	}
}
