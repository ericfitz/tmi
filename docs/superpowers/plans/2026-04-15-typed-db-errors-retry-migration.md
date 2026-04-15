# Typed DB Errors and Retry Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace string-based DB error classification with typed errors backed by driver-specific error codes, and standardize retry logic across the repository layer.

**Architecture:** New `internal/dberrors/` package provides error taxonomy and classification. Repositories classify errors via `dberrors.Classify()` but don't retry. Services decide retry policy per-operation using existing `WithRetryableGormTransaction`. Handlers use `errors.Is()` for HTTP status mapping.

**Tech Stack:** Go, GORM, jackc/pgx v5 (PostgreSQL), godror (Oracle), testify

**Spec:** `docs/superpowers/specs/2026-04-15-typed-db-errors-retry-migration-design.md`

---

## Scoping Note: Handler Migration Boundary

Handlers that consume `auth/repository/` via services are fully migrated to typed errors. Handlers that consume GORM store files (`api/*_store_gorm.go`) are OUT OF SCOPE — those stores are migrated in #261. Specifically:

**In scope (consume repository-backed services):**
- `api/client_credentials_handlers.go`, `api/admin_user_credentials_handlers.go`
- `api/identity.go` (consumes UserRepository via auth service)
- `api/admin_automation_handlers.go` (consumes auth service CreateUser → UserRepository)

**Out of scope (consume GORM stores, deferred to #261):**
- `api/admin_user_handlers.go`, `api/admin_group_handlers.go`, `api/admin_group_member_handlers.go`
- `api/survey_handlers.go`, `api/document_sub_resource_handlers.go`, `api/triage_note_handlers.go`
- `api/my_group_handlers.go`, `api/admin_quota_handlers.go`

**Transitional (add typed checks as primary, keep string fallback for stores):**
- `api/request_utils.go` — `StoreErrorToRequestError()` and `isForeignKeyConstraintError()`

---

## File Structure

### New Files
- `internal/dberrors/errors.go` — Sentinel error types and sub-categories
- `internal/dberrors/classify.go` — Core classification logic, GORM checks, string fallback
- `internal/dberrors/classify_pg.go` — PostgreSQL pgconn.PgError SQLSTATE classification (default build)
- `internal/dberrors/classify_oracle.go` — Oracle godror OraErr classification (`//go:build oracle`)
- `internal/dberrors/classify_no_oracle.go` — No-op Oracle stub (`//go:build !oracle`)
- `internal/dberrors/fatal.go` — HandleFatal shutdown function
- `internal/dberrors/errors_test.go` — Tests for sentinel error hierarchy
- `internal/dberrors/classify_test.go` — Tests for Classify with pgconn.PgError and fallback

### Modified Files
- `auth/db/retry.go` — Delegate IsRetryableError/IsConnectionError/IsPermissionError to dberrors
- `auth/db/retry_test.go` — Update tests for delegated behavior
- `auth/repository/interfaces.go` — Sentinel errors wrap dberrors.ErrNotFound
- `auth/repository/client_credentials_repository.go` — Use dberrors.Classify()
- `auth/repository/user_repository.go` — Use dberrors.Classify()
- `auth/repository/deletion_repository.go` — Use dberrors.Classify() within transactions
- `api/client_credentials_service.go` — Remove classifyDBError, use dberrors
- `api/client_credentials_handlers.go` — Switch to dberrors sentinels
- `api/admin_user_credentials_handlers.go` — Switch to dberrors, remove string fallback
- `api/identity.go` — Use dberrors.ErrNotFound instead of string matching
- `api/request_utils.go` — Add typed error primary path, keep string fallback
- `api/admin_automation_handlers.go` — Use dberrors.ErrDuplicate instead of string matching

---

### Task 1: Create `internal/dberrors/` Error Taxonomy

**Files:**
- Create: `internal/dberrors/errors.go`
- Create: `internal/dberrors/fatal.go`
- Create: `internal/dberrors/errors_test.go`

- [ ] **Step 1: Create the sentinel errors and helpers**

Create `internal/dberrors/errors.go`:

```go
// Package dberrors provides typed database error classification.
// Repositories use Classify() to wrap raw driver errors with sentinel types.
// Services check IsFatal() and decide retry policy.
// Handlers use errors.Is() for HTTP status mapping.
package dberrors

import (
	"errors"
	"fmt"
)

// Top-level error categories
var (
	ErrNotFound    = errors.New("not found")
	ErrConstraint  = errors.New("constraint violation")
	ErrTransient   = errors.New("transient database error")
	ErrPermission  = errors.New("permission denied")
	ErrContextDone = errors.New("context cancelled")
)

// Constraint sub-categories (wrap ErrConstraint so errors.Is works for both)
var (
	ErrDuplicate  = fmt.Errorf("duplicate: %w", ErrConstraint)
	ErrForeignKey = fmt.Errorf("foreign key: %w", ErrConstraint)
)

// IsRetryable returns true if the error represents a transient condition
// that may succeed on retry (connection errors, serialization failures, deadlocks).
func IsRetryable(err error) bool {
	return errors.Is(err, ErrTransient)
}

// IsFatal returns true if the error indicates the server is fundamentally broken
// and should shut down (permission denied, invalid credentials).
func IsFatal(err error) bool {
	return errors.Is(err, ErrPermission)
}

// Wrap wraps a raw error with a typed sentinel, preserving the original error chain.
// Example: Wrap(rawErr, ErrDuplicate) returns an error where:
//   - errors.Is(result, ErrDuplicate) == true
//   - errors.Is(result, ErrConstraint) == true (because ErrDuplicate wraps ErrConstraint)
//   - errors.Unwrap(result) chain reaches rawErr
func Wrap(err error, sentinel error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}
```

- [ ] **Step 2: Create the fatal error handler**

Create `internal/dberrors/fatal.go`:

```go
package dberrors

import (
	"os"

	"github.com/ericfitz/tmi/internal/slogging"
)

// HandleFatal logs the error and terminates the process.
// Called by services when they detect a fatal condition (DB permission denied,
// crypto failure, etc.). Fatal errors should never reach handlers.
//
// Future improvement: #262 will add graceful shutdown before exit.
func HandleFatal(err error) {
	logger := slogging.Get()
	logger.Error("Fatal error, shutting down: %v", err)
	os.Exit(1)
}
```

- [ ] **Step 3: Write tests for the error hierarchy**

Create `internal/dberrors/errors_test.go`:

```go
package dberrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrorHierarchy(t *testing.T) {
	t.Run("ErrDuplicate is ErrConstraint", func(t *testing.T) {
		assert.True(t, errors.Is(ErrDuplicate, ErrConstraint))
	})

	t.Run("ErrForeignKey is ErrConstraint", func(t *testing.T) {
		assert.True(t, errors.Is(ErrForeignKey, ErrConstraint))
	})

	t.Run("ErrDuplicate is not ErrForeignKey", func(t *testing.T) {
		assert.False(t, errors.Is(ErrDuplicate, ErrForeignKey))
	})

	t.Run("ErrTransient is not ErrConstraint", func(t *testing.T) {
		assert.False(t, errors.Is(ErrTransient, ErrConstraint))
	})
}

func TestWrap(t *testing.T) {
	raw := fmt.Errorf("pg: unique violation on idx_email")

	t.Run("wrapped error matches sentinel", func(t *testing.T) {
		wrapped := Wrap(raw, ErrDuplicate)
		assert.True(t, errors.Is(wrapped, ErrDuplicate))
		assert.True(t, errors.Is(wrapped, ErrConstraint))
	})

	t.Run("wrapped error matches original", func(t *testing.T) {
		wrapped := Wrap(raw, ErrDuplicate)
		assert.True(t, errors.Is(wrapped, raw))
	})

	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, Wrap(nil, ErrDuplicate))
	})
}

func TestIsRetryable(t *testing.T) {
	t.Run("transient error is retryable", func(t *testing.T) {
		err := Wrap(fmt.Errorf("connection reset"), ErrTransient)
		assert.True(t, IsRetryable(err))
	})

	t.Run("constraint error is not retryable", func(t *testing.T) {
		err := Wrap(fmt.Errorf("duplicate key"), ErrDuplicate)
		assert.False(t, IsRetryable(err))
	})

	t.Run("bare sentinel is retryable", func(t *testing.T) {
		assert.True(t, IsRetryable(ErrTransient))
	})
}

func TestIsFatal(t *testing.T) {
	t.Run("permission error is fatal", func(t *testing.T) {
		err := Wrap(fmt.Errorf("insufficient privilege"), ErrPermission)
		assert.True(t, IsFatal(err))
	})

	t.Run("transient error is not fatal", func(t *testing.T) {
		err := Wrap(fmt.Errorf("connection reset"), ErrTransient)
		assert.False(t, IsFatal(err))
	})
}
```

- [ ] **Step 4: Run tests to verify**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dberrors/ -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/dberrors/errors.go internal/dberrors/fatal.go internal/dberrors/errors_test.go
git commit -m "feat(dberrors): add typed error taxonomy and sentinel hierarchy (#258)"
```

---

### Task 2: Create `internal/dberrors/` Error Classification

**Files:**
- Create: `internal/dberrors/classify.go`
- Create: `internal/dberrors/classify_pg.go`
- Create: `internal/dberrors/classify_oracle.go`
- Create: `internal/dberrors/classify_no_oracle.go`
- Create: `internal/dberrors/classify_test.go`

- [ ] **Step 1: Create the core classification logic**

Create `internal/dberrors/classify.go`:

```go
package dberrors

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Classify wraps a raw database error with the appropriate typed sentinel.
// It checks in order: context errors, GORM errors, driver-specific errors
// (PostgreSQL pgconn.PgError, Oracle godror.OraErr), then falls back to
// string matching for errors that don't carry typed driver info.
func Classify(err error) error {
	if err == nil {
		return nil
	}

	// Context cancellation
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Wrap(err, ErrContextDone)
	}

	// Already classified — don't double-wrap
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConstraint) ||
		errors.Is(err, ErrTransient) || errors.Is(err, ErrPermission) ||
		errors.Is(err, ErrContextDone) {
		return err
	}

	// GORM-specific: record not found
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Wrap(err, ErrNotFound)
	}

	// Driver-specific classification (PostgreSQL, Oracle)
	if classified := classifyPgError(err); classified != nil {
		return classified
	}
	if classified := classifyOracleError(err); classified != nil {
		return classified
	}

	// String-matching fallback for errors without typed driver info
	return classifyByString(err)
}

// classifyByString is the fallback classifier for errors that don't carry
// typed driver information (e.g., raw net.OpError, TLS errors).
// This should handle a minimal set of patterns — driver-specific checks
// cover the vast majority of cases.
func classifyByString(err error) error {
	errStr := strings.ToLower(err.Error())

	// Connection/transient errors
	transientPatterns := []string{
		"driver: bad connection",
		"connection refused",
		"connection reset by peer",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"no connection available",
		"connection timed out",
		"unexpected eof",
		"server closed",
		"ssl connection has been closed",
		"connection is shut down",
		"invalid connection",
		"connection unexpectedly closed",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return Wrap(err, ErrTransient)
		}
	}

	// Permission errors
	permissionPatterns := []string{
		"permission denied",
		"insufficient privilege",
	}
	for _, pattern := range permissionPatterns {
		if strings.Contains(errStr, pattern) {
			return Wrap(err, ErrPermission)
		}
	}

	// Not found (from RowsAffected == 0 checks that return error strings)
	if strings.Contains(errStr, "not found") {
		return Wrap(err, ErrNotFound)
	}

	// Constraint patterns (fallback for non-typed driver errors)
	if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "unique constraint") {
		return Wrap(err, ErrDuplicate)
	}
	if strings.Contains(errStr, "foreign key") {
		return Wrap(err, ErrForeignKey)
	}
	if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "violates") {
		return Wrap(err, ErrConstraint)
	}

	// Unclassified — return as-is
	return err
}
```

- [ ] **Step 2: Create PostgreSQL classifier**

Create `internal/dberrors/classify_pg.go`:

```go
package dberrors

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// classifyPgError extracts a pgconn.PgError and classifies by SQLSTATE code.
// Returns nil if the error doesn't contain a PgError.
func classifyPgError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}

	code := pgErr.Code

	// Class 23 — Integrity Constraint Violation
	if strings.HasPrefix(code, "23") {
		switch code {
		case "23505": // unique_violation
			return Wrap(err, ErrDuplicate)
		case "23503": // foreign_key_violation
			return Wrap(err, ErrForeignKey)
		default: // 23000 (integrity_constraint_violation), 23502 (not_null), 23514 (check), etc.
			return Wrap(err, ErrConstraint)
		}
	}

	// Class 40 — Transaction Rollback
	switch code {
	case "40001": // serialization_failure
		return Wrap(err, ErrTransient)
	case "40P01": // deadlock_detected
		return Wrap(err, ErrTransient)
	}

	// Class 08 — Connection Exception
	if strings.HasPrefix(code, "08") {
		return Wrap(err, ErrTransient)
	}

	// Class 57 — Operator Intervention
	switch code {
	case "57P01": // admin_shutdown
		return Wrap(err, ErrTransient)
	case "57P03": // cannot_connect_now
		return Wrap(err, ErrTransient)
	}

	// Privilege errors
	switch code {
	case "42501": // insufficient_privilege
		return Wrap(err, ErrPermission)
	case "28P01": // invalid_password
		return Wrap(err, ErrPermission)
	case "28000": // invalid_authorization_specification
		return Wrap(err, ErrPermission)
	}

	return nil
}
```

- [ ] **Step 3: Create Oracle classifier (behind build tag)**

Create `internal/dberrors/classify_oracle.go`:

```go
//go:build oracle

package dberrors

import (
	"errors"

	"github.com/godror/godror"
)

// classifyOracleError extracts a godror.OraErr and classifies by ORA- code.
// Returns nil if the error doesn't contain an OraErr.
func classifyOracleError(err error) error {
	var oraErr *godror.OraErr
	if !errors.As(err, &oraErr) {
		return nil
	}

	code := oraErr.Code()

	switch code {
	// Unique constraint violated
	case 1: // ORA-00001
		return Wrap(err, ErrDuplicate)

	// Foreign key violations
	case 2291, 2292: // ORA-02291 (parent key not found), ORA-02292 (child record found)
		return Wrap(err, ErrForeignKey)

	// Serialization / deadlock
	case 8177: // ORA-08177 (can't serialize access)
		return Wrap(err, ErrTransient)
	case 60: // ORA-00060 (deadlock detected)
		return Wrap(err, ErrTransient)

	// Connection errors
	case 3113, 3114: // ORA-03113/03114 (end-of-file on communication channel / not connected)
		return Wrap(err, ErrTransient)
	case 3135: // ORA-03135 (connection lost contact)
		return Wrap(err, ErrTransient)
	case 12170: // ORA-12170 (connect timeout)
		return Wrap(err, ErrTransient)
	case 12541, 12543: // ORA-12541/12543 (no listener / destination host unreachable)
		return Wrap(err, ErrTransient)

	// Permission / credential errors
	case 1017: // ORA-01017 (invalid username/password)
		return Wrap(err, ErrPermission)
	case 1031: // ORA-01031 (insufficient privileges)
		return Wrap(err, ErrPermission)
	}

	return nil
}
```

- [ ] **Step 4: Create no-oracle stub**

Create `internal/dberrors/classify_no_oracle.go`:

```go
//go:build !oracle

package dberrors

// classifyOracleError is a no-op when built without the oracle tag.
// The oracle build tag pulls in godror which requires CGO and Oracle Instant Client.
func classifyOracleError(_ error) error {
	return nil
}
```

- [ ] **Step 5: Write classification tests**

Create `internal/dberrors/classify_test.go`:

```go
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
```

- [ ] **Step 6: Run tests to verify**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dberrors/ -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add internal/dberrors/classify.go internal/dberrors/classify_pg.go internal/dberrors/classify_oracle.go internal/dberrors/classify_no_oracle.go internal/dberrors/classify_test.go
git commit -m "feat(dberrors): add driver-specific error classification for PostgreSQL and Oracle (#258)"
```

---

### Task 3: Migrate `auth/db/retry.go` to Use dberrors

**Files:**
- Modify: `auth/db/retry.go`
- Modify: `auth/db/retry_test.go`

- [ ] **Step 1: Update retry.go to delegate to dberrors**

In `auth/db/retry.go`, replace the string-matching implementations of `IsRetryableError`, `IsConnectionError`, and `IsPermissionError` with delegations to `dberrors.Classify`:

Replace the imports:

```go
import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)
```

Replace `IsRetryableError` (lines 95-137):

```go
// IsRetryableError determines if an error should trigger a retry.
// Delegates to dberrors.Classify for driver-specific error detection.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	return dberrors.IsRetryable(dberrors.Classify(err))
}
```

Replace `IsConnectionError` (lines 139-168):

```go
// IsConnectionError is a convenience function that checks specifically for connection errors.
// This is equivalent to IsRetryableError — both check for transient conditions.
// Kept for backward compatibility.
func IsConnectionError(err error) bool {
	return IsRetryableError(err)
}
```

Replace `IsPermissionError` (lines 211-232):

```go
// IsPermissionError checks if an error indicates a database permission or privilege failure.
// These errors are not transient and indicate server misconfiguration.
func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}
	return dberrors.IsFatal(dberrors.Classify(err))
}
```

Remove the `"strings"` import since it's no longer needed.

- [ ] **Step 2: Run existing retry tests to verify behavior is preserved**

Run: `cd /Users/efitz/Projects/tmi && go test ./auth/db/ -run TestWithRetryable -v`
Expected: All existing retry tests PASS (behavior-preserving refactor)

- [ ] **Step 3: Run permission error tests**

Run: `cd /Users/efitz/Projects/tmi && go test ./auth/db/ -run TestIsPermissionError -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add auth/db/retry.go auth/db/retry_test.go
git commit -m "refactor(auth/db): delegate retry error detection to dberrors.Classify (#258)"
```

---

### Task 4: Update Repository Interfaces and Implementations

**Files:**
- Modify: `auth/repository/interfaces.go`
- Modify: `auth/repository/client_credentials_repository.go`
- Modify: `auth/repository/user_repository.go`
- Modify: `auth/repository/deletion_repository.go`

- [ ] **Step 1: Update sentinel errors in interfaces.go to wrap dberrors types**

In `auth/repository/interfaces.go`, change the imports:

```go
import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
)
```

Replace the sentinel error declarations (lines 15-20):

```go
// Common errors returned by repositories.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	ErrUserNotFound             = fmt.Errorf("user: %w", dberrors.ErrNotFound)
	ErrClientCredentialNotFound = fmt.Errorf("client credential: %w", dberrors.ErrNotFound)
	ErrGroupNotFound            = fmt.Errorf("group: %w", dberrors.ErrNotFound)
	ErrUnauthorized             = fmt.Errorf("unauthorized: %w", dberrors.ErrNotFound)
)
```

- [ ] **Step 2: Update client_credentials_repository.go**

In `auth/repository/client_credentials_repository.go`, add the dberrors import:

```go
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)
```

Update `Create` method (line 46-49) — replace the error return:

```go
	result := r.db.WithContext(ctx).Create(gormCred)
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}
```

Update `GetByClientID` method (lines 61-66) — use Classify for non-not-found errors:

```go
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrClientCredentialNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}
```

Update `ListByOwner` method (lines 79-81):

```go
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}
```

Update `UpdateLastUsed` method (lines 97-99):

```go
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}
```

Update `Deactivate` method (lines 117-124) — replace string error with typed sentinel:

```go
	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}
```

Update `Delete` method (lines 134-140) — same pattern:

```go
	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}
```

- [ ] **Step 3: Update user_repository.go**

In `auth/repository/user_repository.go`, add the dberrors import:

```go
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)
```

For every Get* method that checks `gorm.ErrRecordNotFound` → `ErrUserNotFound`, keep that pattern but classify other errors. Example pattern for `GetByEmail` (and all similar Get methods):

```go
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}
```

Apply this to: `GetByEmail`, `GetByID`, `GetByProviderID`, `GetByProviderAndEmail`, `GetByAnyProviderID`.

For `GetProviders` and `GetPrimaryProviderID` (which return empty/nil on not-found, not error), classify the non-not-found errors:

```go
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil  // or empty slice — keep existing behavior
		}
		return nil, dberrors.Classify(result.Error)
	}
```

For `Create` — classify errors:

```go
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}
```

For `Update` — classify errors, keep RowsAffected check:

```go
	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
```

For `Delete` — same pattern as Update.

- [ ] **Step 4: Update deletion_repository.go**

In `auth/repository/deletion_repository.go`, add the dberrors import:

```go
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)
```

Inside `DeleteUserAndData` transaction (line 39-43), replace string error with typed:

```go
		if err := tx.Where("email = ?", userEmail).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return dberrors.Classify(err)
		}
```

Inside `DeleteUserByInternalUUID` transaction (line 67-71), same pattern:

```go
		if err := tx.Where("internal_uuid = ?", internalUUID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return dberrors.Classify(err)
		}
```

Inside `deleteUserCore` — classify errors from GORM operations within the transaction. The `fmt.Errorf("failed to ...: %w", err)` patterns should wrap with dberrors.Classify:

```go
		return fmt.Errorf("failed to query owned threat models: %w", dberrors.Classify(err))
```

Apply this pattern to all `fmt.Errorf("failed to ...: %w", err)` calls in deleteUserCore, DeleteGroupAndData, and TransferOwnership. Keep the `RowsAffected == 0` → `ErrUserNotFound` checks.

Inside `DeleteGroupAndData` (line 191-195):

```go
		if err := tx.Where("internal_uuid = ?", internalUUID).First(&group).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGroupNotFound
			}
			return dberrors.Classify(err)
		}
```

Inside `TransferOwnership` (line 763-767 and 772-776): These already use `ErrUserNotFound` for not-found. Classify the `fmt.Errorf` wraps for other errors.

- [ ] **Step 5: Run repository tests**

Run: `cd /Users/efitz/Projects/tmi && go test ./auth/repository/ -v`
Expected: All existing tests PASS

- [ ] **Step 6: Verify the error chain works**

Run: `cd /Users/efitz/Projects/tmi && go test ./auth/repository/ -run TestNotFound -v 2>/dev/null; go test ./internal/dberrors/ -v`
Expected: dberrors tests PASS, repository tests PASS (no regressions)

- [ ] **Step 7: Commit**

```bash
git add auth/repository/interfaces.go auth/repository/client_credentials_repository.go auth/repository/user_repository.go auth/repository/deletion_repository.go
git commit -m "refactor(repository): return typed dberrors from all repository methods (#258)"
```

---

### Task 5: Update Client Credentials Service

**Files:**
- Modify: `api/client_credentials_service.go`

- [ ] **Step 1: Remove classifyDBError and local sentinels**

In `api/client_credentials_service.go`:

Replace the imports (remove `strings`, add `dberrors`):

```go
import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)
```

Delete the local sentinel error declarations (lines 22-27):

```go
// DELETE these lines:
// var (
//     ErrCredentialConstraint = errors.New("credential constraint violation")
//     ErrCredentialNotFound   = errors.New("credential not found")
//     ErrTransientDB          = errors.New("transient database error")
// )
```

Delete the entire `classifyDBError` function (lines 29-56).

- [ ] **Step 2: Update Create method fatal/error handling**

In the `Create` method, replace `os.Exit(1)` calls for crypto failures with `dberrors.HandleFatal`:

```go
	if _, err := rand.Read(clientIDBytes); err != nil {
		dberrors.HandleFatal(fmt.Errorf("crypto/rand failure generating client_id: %w", err))
	}
```

```go
	if _, err := rand.Read(secretBytes); err != nil {
		dberrors.HandleFatal(fmt.Errorf("crypto/rand failure generating client_secret: %w", err))
	}
```

```go
	hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), 10)
	if err != nil {
		dberrors.HandleFatal(fmt.Errorf("bcrypt failure hashing client_secret: %w", err))
	}
```

Replace the post-DB error handling (lines 147-153):

```go
	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error creating client credential: %w", dbErr))
		}
		return nil, dbErr
	}
```

- [ ] **Step 3: Update List method**

Replace post-DB error handling (lines 178-184):

```go
	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error listing client credentials: %w", dbErr))
		}
		return nil, dbErr
	}
```

- [ ] **Step 4: Update Delete method**

Replace post-DB error handling (lines 212-218):

```go
	if dbErr != nil {
		if dberrors.IsFatal(dbErr) {
			dberrors.HandleFatal(fmt.Errorf("database error deleting client credential: %w", dbErr))
		}
		return dbErr
	}
```

- [ ] **Step 5: Update Deactivate method**

Keep the current simple wrapping — no retry needed (intentional):

```go
func (s *ClientCredentialService) Deactivate(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	if err := s.authService.DeactivateClientCredential(ctx, credID, ownerUUID); err != nil {
		if dberrors.IsFatal(err) {
			dberrors.HandleFatal(fmt.Errorf("database error deactivating client credential: %w", err))
		}
		return err
	}
	return nil
}
```

Remove the `"os"` and `"errors"` imports if no longer needed.

- [ ] **Step 6: Build to verify compilation**

Run: `cd /Users/efitz/Projects/tmi && make build-server`
Expected: Build succeeds. (Some handlers may fail compilation since they reference the deleted sentinels — that's expected and will be fixed in Task 6.)

If build fails due to handler references to `ErrCredentialConstraint`, `ErrCredentialNotFound`, `ErrTransientDB`, proceed to Task 6 immediately and fix those before building again.

- [ ] **Step 7: Commit**

```bash
git add api/client_credentials_service.go
git commit -m "refactor(api): remove classifyDBError, use dberrors in credential service (#258)"
```

---

### Task 6: Update Credential Handlers

**Files:**
- Modify: `api/client_credentials_handlers.go`
- Modify: `api/admin_user_credentials_handlers.go`

- [ ] **Step 1: Update client_credentials_handlers.go**

Add dberrors import:

```go
	"github.com/ericfitz/tmi/internal/dberrors"
```

Replace all error checks in `CreateCurrentUserClientCredential` (around lines 142-170):

```go
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if errors.Is(err, dberrors.ErrDuplicate) || errors.Is(err, dberrors.ErrConstraint) {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "A client credential with these details already exists",
			})
			return
		}
		if errors.Is(err, dberrors.ErrTransient) {
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable, please retry",
			})
			return
		}
		logger.Error("Failed to create client credential for user %s: %v", ownerUUID, err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}
```

Apply the same pattern to `ListCurrentUserClientCredentials` and `DeleteCurrentUserClientCredential`, using the appropriate dberrors checks:
- List: check `ErrTransient` → 503, default → 500
- Delete: check `ErrNotFound` → 404, `ErrTransient` → 503, default → 500

Remove the now-unused imports of the old sentinel errors. Remove the `"strings"` import if no longer needed.

- [ ] **Step 2: Update admin_user_credentials_handlers.go**

Add dberrors import:

```go
	"github.com/ericfitz/tmi/internal/dberrors"
```

Apply the same pattern as Step 1 to all handler methods:
- `ListAdminUserClientCredentials`: `ErrTransient` → 503, default → 500
- `CreateAdminUserClientCredential`: `ErrDuplicate`/`ErrConstraint` → 409, `ErrTransient` → 503, default → 500
- `DeleteAdminUserClientCredential`: `ErrNotFound` → 404, `ErrTransient` → 503, default → 500

**Critically**: Remove the string matching fallback in `DeleteAdminUserClientCredential` (around lines 305-320) that checks `strings.Contains(errStr, "not found")` and `strings.Contains(errStr, "unauthorized")`. Replace with:

```go
	if errors.Is(err, dberrors.ErrNotFound) {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Client credential not found",
		})
		return
	}
```

- [ ] **Step 3: Build to verify**

Run: `cd /Users/efitz/Projects/tmi && make build-server`
Expected: Build succeeds

- [ ] **Step 4: Run unit tests**

Run: `cd /Users/efitz/Projects/tmi && make test-unit`
Expected: All tests PASS. If any handler tests fail because they create errors via `errors.New(...)` that no longer match, update the test mocks to use dberrors sentinels.

- [ ] **Step 5: Commit**

```bash
git add api/client_credentials_handlers.go api/admin_user_credentials_handlers.go
git commit -m "refactor(api): migrate credential handlers to dberrors.Is() checks (#258)"
```

---

### Task 7: Update Identity Resolution and Request Utilities

**Files:**
- Modify: `api/identity.go`
- Modify: `api/request_utils.go`
- Modify: `api/admin_automation_handlers.go`

- [ ] **Step 1: Update identity.go**

Add dberrors import:

```go
	"github.com/ericfitz/tmi/internal/dberrors"
```

Replace the `isUserNotFound` function (line 38-40):

```go
// isUserNotFound returns true if the error indicates a user was not found.
func isUserNotFound(err error) bool {
	return err != nil && errors.Is(err, dberrors.ErrNotFound)
}
```

In `resolveWithoutUUID` (around lines 112 and 124), replace the compound check:

```go
		// Old: if !isUserNotFound(err) && !strings.Contains(err.Error(), "not found") {
		if !isUserNotFound(err) {
			return ResolvedUser{}, err
		}
```

Remove the `"strings"` import if no longer needed.

- [ ] **Step 2: Update request_utils.go — StoreErrorToRequestError**

Add dberrors import:

```go
	"github.com/ericfitz/tmi/internal/dberrors"
```

Rewrite `StoreErrorToRequestError` (lines 540-558) to check typed errors first, with string fallback for GORM stores not yet migrated:

```go
func StoreErrorToRequestError(err error, notFoundMsg, serverErrorMsg string) *RequestError {
	// If already a RequestError, return it directly to preserve its status code
	var reqErr *RequestError
	if errors.As(err, &reqErr) {
		return reqErr
	}

	// Typed error checks (from repositories using dberrors)
	if errors.Is(err, dberrors.ErrNotFound) {
		return NotFoundError(notFoundMsg)
	}
	if errors.Is(err, dberrors.ErrDuplicate) {
		return ConflictError(notFoundMsg) // 409
	}
	if errors.Is(err, dberrors.ErrConstraint) {
		return InvalidInputError(err.Error())
	}
	if errors.Is(err, dberrors.ErrTransient) {
		return ServerError(serverErrorMsg)
	}

	// String fallback for GORM stores not yet migrated to dberrors (#261)
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "not found") {
		return NotFoundError(notFoundMsg)
	}
	if strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "validation") {
		return InvalidInputError(err.Error())
	}

	return ServerError(serverErrorMsg)
}
```

**Note:** Check if `ConflictError` exists in request_utils.go. If not, use `&RequestError{Status: http.StatusConflict, Code: "conflict", Message: notFoundMsg}` or add a `ConflictError` helper.

- [ ] **Step 3: Update isForeignKeyConstraintError**

Rewrite `isForeignKeyConstraintError` (lines 604-641) to check typed errors first:

```go
func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}

	// Typed error check (from repositories using dberrors)
	if errors.Is(err, dberrors.ErrForeignKey) {
		return true
	}

	// String fallback for GORM stores not yet migrated to dberrors (#261)
	errorMessage := strings.ToLower(err.Error())

	// PostgreSQL patterns
	if strings.Contains(errorMessage, "foreign key constraint") ||
		strings.Contains(errorMessage, "violates foreign key constraint") ||
		strings.Contains(errorMessage, "fkey constraint") {
		return true
	}

	// Oracle patterns
	if strings.Contains(errorMessage, "ora-02291") ||
		strings.Contains(errorMessage, "ora-02292") ||
		(strings.Contains(errorMessage, "integrity constraint") && strings.Contains(errorMessage, "parent key not found")) {
		return true
	}

	// MySQL patterns
	if strings.Contains(errorMessage, "cannot add or update a child row") ||
		strings.Contains(errorMessage, "a foreign key constraint fails") {
		return true
	}

	// SQLite patterns
	if strings.Contains(errorMessage, "foreign key constraint failed") {
		return true
	}

	// Legacy pattern
	if strings.Contains(errorMessage, "constraint") && strings.Contains(errorMessage, "owner_email") {
		return true
	}

	return false
}
```

- [ ] **Step 4: Update admin_automation_handlers.go**

Add dberrors import:

```go
	"github.com/ericfitz/tmi/internal/dberrors"
```

Replace the string matching (around line 137):

```go
	// Old: if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "constraint") || strings.Contains(errStr, "UNIQUE") {
	if errors.Is(err, dberrors.ErrDuplicate) || errors.Is(err, dberrors.ErrConstraint) {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: "An account with the same email or provider ID already exists",
		})
		return
	}
```

- [ ] **Step 5: Build and test**

Run: `cd /Users/efitz/Projects/tmi && make build-server && make test-unit`
Expected: Build succeeds, all tests PASS

- [ ] **Step 6: Commit**

```bash
git add api/identity.go api/request_utils.go api/admin_automation_handlers.go
git commit -m "refactor(api): migrate identity, request utils, and automation handlers to dberrors (#258)"
```

---

### Task 8: Lint, Build, and Full Test Verification

**Files:** None (verification only)

- [ ] **Step 1: Run linter**

Run: `cd /Users/efitz/Projects/tmi && make lint`
Expected: No new lint errors (existing api/api.go warnings are expected)

Fix any lint issues found (unused imports, etc.).

- [ ] **Step 2: Run full build**

Run: `cd /Users/efitz/Projects/tmi && make build-server`
Expected: Build succeeds

- [ ] **Step 3: Run unit tests**

Run: `cd /Users/efitz/Projects/tmi && make test-unit`
Expected: All tests PASS

- [ ] **Step 4: Run integration tests**

Run: `cd /Users/efitz/Projects/tmi && make test-integration`
Expected: All tests PASS (validates typed errors work end-to-end with real PostgreSQL)

- [ ] **Step 5: Fix any failures**

If any tests fail, investigate and fix. Common issues:
- Test mocks returning `errors.New("credential constraint violation")` that no longer match `errors.Is(err, ErrCredentialConstraint)` — update mocks to use dberrors sentinels
- Unused import warnings from removed `"strings"` or `"os"` imports
- Handler tests expecting specific error message text that changed

- [ ] **Step 6: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "fix: resolve test and lint issues from typed error migration (#258)"
```
