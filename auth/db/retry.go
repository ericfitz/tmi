package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
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
// Delegates to dberrors.Classify for driver-specific error detection.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	return dberrors.IsRetryable(dberrors.Classify(err))
}

// IsConnectionError is a convenience function that checks specifically for connection errors.
// This is equivalent to IsRetryableError — both check for transient conditions.
// Kept for backward compatibility.
func IsConnectionError(err error) bool {
	return IsRetryableError(err)
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
	return dberrors.IsFatal(dberrors.Classify(err))
}
