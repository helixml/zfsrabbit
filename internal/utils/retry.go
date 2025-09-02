package utils

import (
	"context"
	"fmt"
	"time"
)

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
	BackoffFactor float64
}

// DefaultRetryConfig returns sensible defaults for retry operations
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		Delay:         1 * time.Second,
		BackoffFactor: 2.0,
	}
}

// RetryWithContext executes a function with retry logic and context cancellation
func RetryWithContext(ctx context.Context, config RetryConfig, operation func() error) error {
	var lastErr error
	delay := config.Delay

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = operation()
		if lastErr == nil {
			return nil // Success
		}

		// Don't sleep after the last attempt
		if attempt < config.MaxAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			
			// Apply exponential backoff
			delay = time.Duration(float64(delay) * config.BackoffFactor)
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", config.MaxAttempts, lastErr)
}

// Retry is a convenience function that uses a default timeout context
func Retry(config RetryConfig, operation func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return RetryWithContext(ctx, config, operation)
}