package dbschema

import (
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shrinkDDLBackoff makes withDDLRetry's exponential backoff negligible for the
// duration of a test.
func shrinkDDLBackoff(t *testing.T) {
	t.Helper()
	original := ddlBaseRetryDelay
	ddlBaseRetryDelay = time.Millisecond
	t.Cleanup(func() { ddlBaseRetryDelay = original })
}

// TestWithDDLRetry_RetriesTransientUntilSuccess covers #734: a lock-contended
// CREATE INDEX (ORA-00054 -> ErrTransient) must be retried across the widened
// budget rather than aborting on the first failure.
func TestWithDDLRetry_RetriesTransientUntilSuccess(t *testing.T) {
	shrinkDDLBackoff(t)

	calls := 0
	err := withDDLRetry("test ddl", func() error {
		calls++
		if calls < ddlMaxAttempts {
			return dberrors.Wrap(errors.New("ORA-00054: resource busy"), dberrors.ErrTransient)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, ddlMaxAttempts, calls, "must retry a transient DDL error until it succeeds")
}

// TestWithDDLRetry_StopsAtMaxAttempts covers the bound: a permanently busy
// table must exhaust ddlMaxAttempts and return the last error, not spin.
func TestWithDDLRetry_StopsAtMaxAttempts(t *testing.T) {
	shrinkDDLBackoff(t)

	calls := 0
	sentinel := dberrors.Wrap(errors.New("ORA-00054: resource busy"), dberrors.ErrTransient)
	err := withDDLRetry("test ddl", func() error {
		calls++
		return sentinel
	})

	require.Error(t, err)
	assert.Equal(t, ddlMaxAttempts, calls, "must stop after ddlMaxAttempts")
	assert.True(t, dberrors.IsRetryable(dberrors.Classify(err)),
		"the returned error must still classify as transient so callers can apply the warn-and-continue policy")
}

// TestWithDDLRetry_DoesNotRetryPermanentErrors covers the other half: a
// permanent failure (bad SQL, missing privilege) must surface immediately
// instead of burning the whole backoff budget.
func TestWithDDLRetry_DoesNotRetryPermanentErrors(t *testing.T) {
	shrinkDDLBackoff(t)

	calls := 0
	err := withDDLRetry("test ddl", func() error {
		calls++
		return errors.New("ORA-00955: name is already used by an existing object")
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls, "a non-transient DDL error must not be retried")
}

// TestExecMigrationDDL_NonOracleUsesGorm covers the dialect split: on a
// non-Oracle dialect execMigrationDDL is a plain GORM Exec (no pinned
// session, no ALTER SESSION), and its effect must be visible afterwards.
func TestExecMigrationDDL_NonOracleUsesGorm(t *testing.T) {
	db := newSparseIndexTestDB(t)

	require.NoError(t, execMigrationDDL(db, "CREATE UNIQUE INDEX idx_exec_ddl_test ON users (email)"))

	var cnt int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_exec_ddl_test'",
	).Scan(&cnt).Error)
	assert.Equal(t, int64(1), cnt, "the DDL must have been applied")

	require.Error(t, execMigrationDDL(db, "CREATE UNIQUE INDEX idx_exec_ddl_test ON users (email)"),
		"a genuine DDL failure must still surface as an error")
}
