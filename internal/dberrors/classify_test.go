package dberrors

import (
	"context"
	"database/sql"
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
	// The wrapped error must still satisfy errors.Is(err, gorm.ErrRecordNotFound)
	// so that callers (e.g. api/project_handlers.go DeleteProject) can branch on
	// the original GORM sentinel after Wrap's double-%w wrap. If Wrap ever
	// stopped preserving this identity, this assertion is what would catch it.
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

// TestClassify_SqlErrNoRows pins the central fix for #581 finding 1a/1d:
// sql.ErrNoRows is a distinct value from gorm.ErrRecordNotFound (errors.Is
// does not bridge them), and prior to this fix it fell through Classify
// unclassified — every call site that finishes a query with a raw
// *sql.Row/*sql.Rows Scan (rather than a GORM model query) would see a bare,
// unrecognized error, which HandleRequestError turns into an undocumented
// 500 instead of a 404. Classifying it centrally closes the gap for every
// current and future call site, not just the one that surfaced it
// (api/optimistic_locking.go's wildcard If-Match read-back).
func TestClassify_SqlErrNoRows(t *testing.T) {
	err := Classify(sql.ErrNoRows)
	assert.True(t, errors.Is(err, ErrNotFound))
	// The wrapped error must still satisfy errors.Is(err, sql.ErrNoRows) so
	// callers that need to distinguish the original stdlib sentinel can.
	assert.True(t, errors.Is(err, sql.ErrNoRows))
}

// TestClassify_SqlErrNoRows_Wrapped verifies the fmt.Errorf-wrapped form
// (as would appear from a driver/query-layer that adds context) is also
// recognized, matching the wrapped-error coverage pattern used for
// TestClassify_PgWrappedError.
func TestClassify_SqlErrNoRows_Wrapped(t *testing.T) {
	wrapped := fmt.Errorf("scan version: %w", sql.ErrNoRows)
	err := Classify(wrapped)
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

// TestClassifyByString_OracleConstraintsBeatNotFound pins the ordering inside
// classifyByString. Oracle phrases a foreign-key violation as "... violated -
// parent key not found", so a bare "not found" check placed first classified
// an FK violation as ErrNotFound and surfaced it as a 404 (#598). Only
// reachable without the `oracle` build tag, where classifyOracleError is a
// no-op and ORA errors fall through to the string fallback.
func TestClassifyByString_OracleConstraintsBeatNotFound(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    error
		notWant error
	}{
		{
			name:    "ORA-02291 parent key not found is a foreign-key violation",
			input:   "ORA-02291: integrity constraint (TMI.FK_THREATS_TM) violated - parent key not found",
			want:    ErrForeignKey,
			notWant: ErrNotFound,
		},
		{
			name:    "ORA-02292 child record found is a foreign-key violation",
			input:   "ORA-02292: integrity constraint (TMI.FK_ASSETS_TM) violated - child record found",
			want:    ErrForeignKey,
			notWant: ErrNotFound,
		},
		{
			name:  "ORA-00001 unique constraint is a duplicate",
			input: "ORA-00001: unique constraint (TMI.UQ_USERS_EMAIL) violated",
			want:  ErrDuplicate,
		},
		{
			name:  "ORA-02290 check constraint is a constraint error",
			input: "ORA-02290: check constraint (TMI.CK_SEVERITY) violated",
			want:  ErrConstraint,
		},
		{
			// The plain case must still classify as not-found; the reorder
			// must not have cost anything.
			name:  "a plain not-found string is still ErrNotFound",
			input: "threat model not found",
			want:  ErrNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyByString(errors.New(tc.input))
			if !errors.Is(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
			if tc.notWant != nil && errors.Is(got, tc.notWant) {
				t.Fatalf("must not classify as %v: %v", tc.notWant, got)
			}
		})
	}
}

// database/sql's ErrStmtClosed reaches callers when GORM's prepared-statement
// cache evicts a statement an in-flight query already fetched (#684). It is
// not driver.ErrBadConn, so only this classification makes it retryable.
// SEM@3089515ed2d1a77325f141068e6af977fed1e15c: verify a closed prepared statement error classifies as transient
func TestClassifyByString_ClosedStatementIsTransient(t *testing.T) {
	got := classifyByString(errors.New("sql: statement is closed")) // database/sql keeps errStmtClosed unexported
	if !errors.Is(got, ErrTransient) {
		t.Fatalf("expected ErrTransient, got %v", got)
	}
}
