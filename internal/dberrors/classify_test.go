package dberrors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestClassify_NilError(t *testing.T) {
	assert.Nil(t, Classify(nil))
}

func TestClassify_ContextErrors(t *testing.T) {
	t.Run("context canceled", func(t *testing.T) {
		err := Classify(context.Canceled)
		assert.True(t, errors.Is(err, ErrContextDone))
	})

	t.Run("context deadline exceeded", func(t *testing.T) {
		err := Classify(context.DeadlineExceeded)
		assert.True(t, errors.Is(err, ErrContextDone))
	})
}

func TestClassify_GormRecordNotFound(t *testing.T) {
	err := Classify(gorm.ErrRecordNotFound)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestClassify_AlreadyClassified(t *testing.T) {
	original := Wrap(fmt.Errorf("already wrapped"), ErrDuplicate)
	classified := Classify(original)
	// Should return as-is, not double-wrap
	assert.Equal(t, original, classified)
}

func TestClassify_PgUniqueViolation(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrDuplicate))
	assert.True(t, errors.Is(err, ErrConstraint))
}

func TestClassify_PgForeignKeyViolation(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503", Message: "violates foreign key constraint"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrForeignKey))
	assert.True(t, errors.Is(err, ErrConstraint))
}

func TestClassify_PgOtherConstraint(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23502", Message: "not-null constraint violation"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrConstraint))
	assert.False(t, errors.Is(err, ErrDuplicate))
	assert.False(t, errors.Is(err, ErrForeignKey))
}

func TestClassify_PgSerializationFailure(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "40001", Message: "could not serialize access"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_PgDeadlock(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_PgConnectionException(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "08006", Message: "connection failure"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_PgAdminShutdown(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "57P01", Message: "terminating connection due to administrator command"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_PgInsufficientPrivilege(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table users"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrPermission))
}

func TestClassify_PgInvalidPassword(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
	err := Classify(pgErr)
	assert.True(t, errors.Is(err, ErrPermission))
}

func TestClassify_PgWrappedError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
	wrapped := fmt.Errorf("failed to create: %w", pgErr)
	err := Classify(wrapped)
	assert.True(t, errors.Is(err, ErrDuplicate))
}

func TestClassify_PgUnknownCode(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "99999", Message: "some unknown error"}
	err := Classify(pgErr)
	// Unknown PG code falls through to string fallback, then returns as-is
	assert.Equal(t, pgErr, err)
}

func TestClassify_StringFallback_ConnectionRefused(t *testing.T) {
	err := Classify(fmt.Errorf("dial tcp 127.0.0.1:5432: connection refused"))
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_StringFallback_BrokenPipe(t *testing.T) {
	err := Classify(fmt.Errorf("write: broken pipe"))
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_StringFallback_IOTimeout(t *testing.T) {
	err := Classify(fmt.Errorf("read tcp: i/o timeout"))
	assert.True(t, errors.Is(err, ErrTransient))
}

func TestClassify_StringFallback_PermissionDenied(t *testing.T) {
	err := Classify(fmt.Errorf("ERROR: permission denied for table client_credentials"))
	assert.True(t, errors.Is(err, ErrPermission))
}

func TestClassify_StringFallback_NotFound(t *testing.T) {
	err := Classify(fmt.Errorf("user not found"))
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestClassify_StringFallback_Duplicate(t *testing.T) {
	err := Classify(fmt.Errorf("duplicate key value"))
	assert.True(t, errors.Is(err, ErrDuplicate))
}

func TestClassify_StringFallback_ForeignKey(t *testing.T) {
	err := Classify(fmt.Errorf("violates foreign key constraint"))
	assert.True(t, errors.Is(err, ErrForeignKey))
}

func TestClassify_StringFallback_Constraint(t *testing.T) {
	err := Classify(fmt.Errorf("check constraint violated"))
	assert.True(t, errors.Is(err, ErrConstraint))
}

func TestClassify_UnknownError(t *testing.T) {
	original := fmt.Errorf("something completely unknown")
	err := Classify(original)
	// Returns as-is when nothing matches
	assert.Equal(t, original, err)
}
