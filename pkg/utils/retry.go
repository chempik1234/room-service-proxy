package utils

import (
	"context"
	"fmt"
	"time"
)

// RetryWithBackoff executes a function with exponential backoff retry
//
// Parameters:
// - ctx: Context for cancellation
// - maxRetries: Maximum number of retry attempts
// - initialDelay: Initial delay between retries (will be doubled each time)
// - operation: Function to execute
//
// Returns an error if all retries fail
func RetryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, operation func() error) error {
	var lastErr error
	delay := initialDelay

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		default:
		}

		if err := operation(); err != nil {
			lastErr = err
			fmt.Printf("Attempt %d/%d failed: %v, retrying in %v\n", attempt+1, maxRetries, err, delay)

			// Wait before retry with context cancellation
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("operation cancelled during retry: %w", ctx.Err())
			}

			// Exponential backoff
			delay *= 2
			continue
		}

		return nil // Success
	}

	return fmt.Errorf("operation failed after %d attempts: %w", maxRetries, lastErr)
}
