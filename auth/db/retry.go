package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns reasonable defaults for transaction retries
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}
}

// WithRetryableTransaction executes a function within a transaction with retry logic.
// It automatically retries on connection errors and other transient failures.
// The transaction is rolled back on error and committed on success.
func WithRetryableTransaction(ctx context.Context, db *sql.DB, cfg RetryConfig, fn func(*sql.Tx) error) error {
	logger := slogging.Get()
	var lastErr error

	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff with cap
			// #nosec G115 - attempt is always in range [1, maxRetries-1] so no overflow possible
			delay := min(cfg.BaseDelay*time.Duration(1<<uint(attempt-1)), cfg.MaxDelay)
			logger.Debug("Retrying transaction in %v (attempt %d/%d)", delay, attempt+1, cfg.MaxRetries)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Begin transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			if IsRetryableError(err) {
				lastErr = err
				logger.Warn("Failed to begin transaction (attempt %d/%d): %v", attempt+1, cfg.MaxRetries, err)
				continue
			}
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Execute the function
		if err := fn(tx); err != nil {
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				logger.Error("Failed to rollback transaction: %v (original error: %v)", rollbackErr, err)
			}

			if IsRetryableError(err) {
				lastErr = err
				logger.Warn("Transaction function failed with retryable error (attempt %d/%d): %v",
					attempt+1, cfg.MaxRetries, err)
				continue
			}
			return err
		}

		// Commit
		if err := tx.Commit(); err != nil {
			if IsRetryableError(err) {
				lastErr = err
				logger.Warn("Transaction commit failed with retryable error (attempt %d/%d): %v",
					attempt+1, cfg.MaxRetries, err)
				continue
			}
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil // Success
	}

	return fmt.Errorf("transaction failed after %d attempts: %w", cfg.MaxRetries, lastErr)
}

// IsRetryableError determines if an error should trigger a retry.
// It checks for common database connection and transient errors.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Connection-related errors
	retryablePatterns := []string{
		"driver: bad connection",
		"connection refused",
		"connection reset by peer",
		"connection reset",
		"broken pipe",
		"eof",
		"i/o timeout",
		"no connection available",
		"connection timed out",
		"unexpected eof",
		"server closed",
		"ssl connection has been closed",
		"connection is shut down",
		"invalid connection",
		// PostgreSQL-specific transient errors
		"canceling statement due to conflict", // Serialization conflict
		"could not serialize access",          // Serialization failure
		"deadlock detected",                   // Deadlock
		"the database system is starting up",  // Database not ready
		"the database system is shutting down",
		"terminating connection due to administrator command",
		"connection unexpectedly closed",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsConnectionError is a convenience function that checks specifically for connection errors.
// This is a subset of IsRetryableError focused only on connection-related issues.
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	connectionPatterns := []string{
		"driver: bad connection",
		"connection refused",
		"connection reset",
		"broken pipe",
		"eof",
		"i/o timeout",
		"no connection",
		"connection timed out",
		"connection unexpectedly closed",
		"invalid connection",
	}

	for _, pattern := range connectionPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// WithRetryableGormTransaction executes a function within a GORM transaction with retry logic.
// It automatically retries on connection errors and other transient failures.
// The transaction is managed by GORM (auto-commit on nil return, auto-rollback on error).
func WithRetryableGormTransaction(ctx context.Context, gormDB *gorm.DB, cfg RetryConfig, fn func(tx *gorm.DB) error) error {
	logger := slogging.Get()
	var lastErr error

	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// #nosec G115 - attempt is always in range [1, maxRetries-1] so no overflow possible
			delay := min(cfg.BaseDelay*time.Duration(1<<uint(attempt-1)), cfg.MaxDelay)
			logger.Debug("Retrying GORM transaction in %v (attempt %d/%d)", delay, attempt+1, cfg.MaxRetries)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(tx)
		})

		if err == nil {
			return nil
		}

		if IsRetryableError(err) {
			lastErr = err
			logger.Warn("GORM transaction failed with retryable error (attempt %d/%d): %v",
				attempt+1, cfg.MaxRetries, err)
			continue
		}

		return err // Non-retryable error, return immediately
	}

	return fmt.Errorf("transaction failed after %d attempts: %w", cfg.MaxRetries, lastErr)
}

// IsPermissionError checks if an error indicates a database permission or privilege failure.
// These errors are not transient and indicate server misconfiguration.
func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	permissionPatterns := []string{
		"permission denied",
		"insufficient privilege",
	}

	for _, pattern := range permissionPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}
