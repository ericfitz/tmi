package db

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}
	return db
}

func TestWithRetryableGormTransaction_Success(t *testing.T) {
	db := setupTestGormDB(t)
	ctx := context.Background()
	cfg := DefaultRetryConfig()

	var callCount int32
	err := WithRetryableGormTransaction(ctx, db, cfg, func(tx *gorm.DB) error {
		atomic.AddInt32(&callCount, 1)
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestWithRetryableGormTransaction_NonRetryableError(t *testing.T) {
	db := setupTestGormDB(t)
	ctx := context.Background()
	cfg := DefaultRetryConfig()

	expectedErr := fmt.Errorf("constraint violation: duplicate key")
	err := WithRetryableGormTransaction(ctx, db, cfg, func(tx *gorm.DB) error {
		return expectedErr
	})

	assert.ErrorIs(t, err, expectedErr)
}

func TestWithRetryableGormTransaction_RetryableErrorExhaustsRetries(t *testing.T) {
	db := setupTestGormDB(t)
	ctx := context.Background()
	cfg := RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}

	var callCount int32
	err := WithRetryableGormTransaction(ctx, db, cfg, func(tx *gorm.DB) error {
		atomic.AddInt32(&callCount, 1)
		return fmt.Errorf("driver: bad connection")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transaction failed after 2 attempts")
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestWithRetryableGormTransaction_RetryThenSucceed(t *testing.T) {
	db := setupTestGormDB(t)
	ctx := context.Background()
	cfg := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}

	var callCount int32
	err := WithRetryableGormTransaction(ctx, db, cfg, func(tx *gorm.DB) error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 2 {
			return fmt.Errorf("driver: bad connection")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestWithRetryableGormTransaction_ContextCancelled(t *testing.T) {
	db := setupTestGormDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	cfg := RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}

	err := WithRetryableGormTransaction(ctx, db, cfg, func(tx *gorm.DB) error {
		return fmt.Errorf("driver: bad connection")
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestIsPermissionError(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, IsPermissionError(nil))
	})

	t.Run("permission denied", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("ERROR: permission denied for table client_credentials")))
	})

	t.Run("insufficient privilege", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("ERROR: insufficient privilege")))
	})

	t.Run("unrelated error returns false", func(t *testing.T) {
		assert.False(t, IsPermissionError(errors.New("connection refused")))
	})

	t.Run("case insensitive", func(t *testing.T) {
		assert.True(t, IsPermissionError(errors.New("PERMISSION DENIED for relation users")))
	})
}
