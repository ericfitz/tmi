package db

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// fastCfg keeps the jittered backoff negligible so retry tests stay quick.
func fastCfg() RetryConfig {
	return RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

// A PostgreSQL serialization_failure (SQLSTATE 40001) must be classified as
// transient and drive a retry. We use a real *pgconn.PgError so the exact
// production classification path (dberrors.classifyPgError) is exercised.
func TestWithRetryableGormTransaction_RetriesOnPgSerializationFailure(t *testing.T) {
	db := setupTestGormDB(t)
	pgSerFailure := &pgconn.PgError{Code: "40001", Message: "could not serialize access due to read/write dependencies"}

	var calls int32
	err := WithRetryableGormTransaction(context.Background(), db, fastCfg(), func(*gorm.DB) error {
		if atomic.AddInt32(&calls, 1) < 2 {
			return pgSerFailure
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "should retry once then succeed")
}

// An Oracle ORA-08177 (can't serialize access) must likewise drive a retry.
// godror.OraErr has only unexported fields and a CGO-only constructor, so it
// cannot be faked in a pure-Go `make test-unit` build. Instead we return the
// exact sentinel the production Oracle classifier yields for code 8177
// (dberrors.Wrap(err, ErrTransient)) — the mapping 8177 -> ErrTransient is
// itself proven in internal/dberrors/classify_oracle_codes_test.go. This
// asserts the wrapper retries whatever the classifier deems transient.
func TestWithRetryableGormTransaction_RetriesOnOracleSerializationFailure(t *testing.T) {
	db := setupTestGormDB(t)
	oraSerFailure := dberrors.Wrap(errors.New("ORA-08177: can't serialize access for this transaction"), dberrors.ErrTransient)

	var calls int32
	err := WithRetryableGormTransaction(context.Background(), db, fastCfg(), func(*gorm.DB) error {
		if atomic.AddInt32(&calls, 1) < 2 {
			return oraSerFailure
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "should retry once then succeed")
}

// A non-retryable error (e.g. a unique-constraint violation) must return
// immediately without consuming any retry attempts.
func TestWithRetryableGormTransaction_NonRetryableReturnsImmediately(t *testing.T) {
	db := setupTestGormDB(t)
	pgDuplicate := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}

	var calls int32
	err := WithRetryableGormTransaction(context.Background(), db, fastCfg(), func(*gorm.DB) error {
		atomic.AddInt32(&calls, 1)
		return pgDuplicate
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, pgDuplicate)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "non-retryable error must not be retried")
}
