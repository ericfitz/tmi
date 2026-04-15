# Typed DB Errors and Retry Migration

**Issue:** [#258](https://github.com/ericfitz/tmi/issues/258) — Migrate all DB operations to WithRetryableTransaction and typed errors
**Date:** 2026-04-15
**Status:** Approved

## Summary

Replace string-based database error classification throughout the codebase with typed errors backed by driver-specific error code inspection. Repositories classify errors, services decide retry policy, and handlers use `errors.Is()` for HTTP status mapping. Fatal conditions (DB permission denied, crypto failures) trigger immediate server exit.

## Approach

**Approach B — Layered Error Classification** was selected after evaluating three options:

- **A (Repository retry):** Every repo method wraps in retry. Simplest handlers, but service can't opt out.
- **B (Repo classifies, service retries):** Repos return typed errors, services choose retry per-operation. Clean separation of concerns.
- **C (Default retry + opt-out):** Repo retries by default with context-based opt-out. Over-engineered for current codebase.

Approach B matches the existing `client_credentials_service.go` pattern and gives per-operation control over retry policy, which varies by service.

## Scope

**In scope:**
- New `internal/dberrors/` package with error taxonomy
- Driver-specific error detection (PostgreSQL via `pgconn.PgError`, Oracle via `godror.OraErr`)
- `auth/db/retry.go` internals migrated to typed detection
- `auth/repository/` implementations return typed errors
- Service layer error classification cleanup
- Handler-level `strings.Contains` → `errors.Is()` migration
- Fatal error policy for crypto and DB permission failures

**Out of scope (deferred to [#261](https://github.com/ericfitz/tmi/issues/261)):**
- `api/group_store_gorm.go`
- `api/metadata_store_gorm.go`
- `api/group_member_store_gorm.go`

**Related issues:**
- [#262](https://github.com/ericfitz/tmi/issues/262) — Graceful shutdown for HandleFatal (future improvement)

## Design

### 1. Error Taxonomy (`internal/dberrors/`)

A new package provides unified error types that flow from repositories through services to handlers.

**Sentinel errors:**

```go
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

// Constraint sub-categories (handlers may need to distinguish)
var (
    ErrDuplicate  = fmt.Errorf("duplicate: %w", ErrConstraint)
    ErrForeignKey = fmt.Errorf("foreign key: %w", ErrConstraint)
)
```

**Classifier function:**

```go
// Classify wraps a raw database error with the appropriate typed sentinel.
// Uses driver-specific error inspection (pgconn.PgError, godror.OraErr)
// with string-matching fallback for errors without typed info.
func Classify(err error) error
```

**Helper functions:**

```go
func IsRetryable(err error) bool  // true for ErrTransient
func IsFatal(err error) bool      // true for ErrPermission
```

### 2. Driver-Specific Error Detection

`Classify` uses `errors.As` to extract driver-specific error types and maps error codes to the taxonomy.

**PostgreSQL** (via `jackc/pgx` — `*pgconn.PgError` with SQLSTATE codes):

| SQLSTATE | Sentinel | Meaning |
|----------|----------|---------|
| `23505` | `ErrDuplicate` | Unique violation |
| `23503` | `ErrForeignKey` | FK violation |
| `23000`–`23xxx` (other) | `ErrConstraint` | Other integrity constraints |
| `40001` | `ErrTransient` | Serialization failure |
| `40P01` | `ErrTransient` | Deadlock |
| `08xxx` | `ErrTransient` | Connection exception class |
| `57P01` | `ErrTransient` | Admin shutdown |
| `57P03` | `ErrTransient` | Cannot connect now |
| `42501` | `ErrPermission` | Insufficient privilege |
| `28P01` | `ErrPermission` | Invalid password |

**Oracle** (via `godror` — `godror.OraErr` with ORA- codes):

| ORA Code | Sentinel | Meaning |
|----------|----------|---------|
| `ORA-00001` | `ErrDuplicate` | Unique constraint violated |
| `ORA-02291`, `ORA-02292` | `ErrForeignKey` | Parent/child FK violation |
| `ORA-08177` | `ErrTransient` | Serialization failure |
| `ORA-00060` | `ErrTransient` | Deadlock |
| `ORA-03113`, `ORA-03114` | `ErrTransient` | Connection lost |
| `ORA-01017` | `ErrPermission` | Invalid username/password |
| `ORA-01031` | `ErrPermission` | Insufficient privileges |

**GORM layer:** `gorm.ErrRecordNotFound` → `ErrNotFound` (checked before driver-specific inspection).

**Fallback:** For errors without typed driver info (raw `net.OpError`, TLS errors), a small set of string patterns covers connection-level failures: `connection refused`, `broken pipe`, `i/o timeout`, `connection reset`, `eof`. This fallback is minimal — typed checks handle the vast majority of cases.

**Build tags:** Oracle-specific detection lives behind the existing `//go:build oracle` tag so PG-only builds don't pull in godror dependencies.

### 3. Retry Logic Migration

**`auth/db/retry.go` changes:**

`IsRetryableError()` and `IsConnectionError()` delegate to the new typed detection internally. Public signatures are unchanged:

```go
func IsRetryableError(err error) bool {
    return dberrors.IsRetryable(dberrors.Classify(err))
}
```

`IsPermissionError()` delegates to `dberrors.IsFatal()`.

`WithRetryableTransaction` and `WithRetryableGormTransaction` remain unchanged in behavior — exponential backoff with the same defaults (3 attempts, 100ms base, 5s max). Only the retryability check is now type-based.

### 4. Repository Layer Changes

Repositories classify errors via `dberrors.Classify()` but do not retry.

**Pattern for every repository method:**

```go
func (r *GormFooRepository) GetByID(ctx context.Context, id string) (*Foo, error) {
    var model models.Foo
    result := r.db.WithContext(ctx).Where("id = ?", id).First(&model)
    if result.Error != nil {
        return nil, dberrors.Classify(result.Error)
    }
    return convertModel(&model), nil
}
```

**Files changed:**

- **`client_credentials_repository.go`** — All 6 methods: wrap `result.Error` with `dberrors.Classify()`. For not-found cases (`gorm.ErrRecordNotFound` or `RowsAffected == 0`), return `ErrClientCredentialNotFound` (which wraps `dberrors.ErrNotFound`). Replace string-based "not found or unauthorized" in `Deactivate` and `Delete`.
- **`user_repository.go`** — All methods: wrap errors with `dberrors.Classify()`. For methods that return entity-specific not-found errors, check the classified result with `errors.Is(classified, dberrors.ErrNotFound)` and return the entity-specific sentinel (e.g., `ErrUserNotFound`) instead, preserving the entity context while maintaining the `dberrors.ErrNotFound` chain.
- **`deletion_repository.go`** — Existing `db.Transaction()` calls stay (multi-step transactions). Errors within transactions classified via `dberrors.Classify()`. Services decide whether to add retry around the whole deletion call.

**Entity-specific sentinels in `interfaces.go`** wrap `dberrors` types:

```go
var (
    ErrUserNotFound             = fmt.Errorf("user: %w", dberrors.ErrNotFound)
    ErrClientCredentialNotFound = fmt.Errorf("client credential: %w", dberrors.ErrNotFound)
    ErrGroupNotFound            = fmt.Errorf("group: %w", dberrors.ErrNotFound)
    ErrUnauthorized             = fmt.Errorf("unauthorized: %w", dberrors.ErrNotFound)
)
```

This allows both `errors.Is(err, dberrors.ErrNotFound)` at the handler level and `errors.Is(err, repository.ErrUserNotFound)` for entity-specific checks.

### 5. Service Layer Changes

Services own retry policy and fatal error detection.

**Pattern:**

```go
func (s *FooService) Create(ctx context.Context, ...) (*Foo, error) {
    // Pre-DB work — detect fatal conditions directly
    if _, err := rand.Read(buf); err != nil {
        dberrors.HandleFatal(fmt.Errorf("crypto/rand failure: %w", err))
    }

    // DB operation — service decides to retry
    var result *Foo
    dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
        // repo call returns typed errors
    })

    if dbErr != nil {
        if dberrors.IsFatal(dbErr) {
            dberrors.HandleFatal(dbErr)
        }
        return nil, dbErr  // already typed, pass through
    }
    return result, nil
}
```

**Changes to `api/client_credentials_service.go`:**

- Delete `classifyDBError()` — repos now return typed errors
- Delete local sentinels `ErrCredentialConstraint`, `ErrCredentialNotFound`, `ErrTransientDB` — replaced by `dberrors.*`
- Keep `WithRetryableGormTransaction` wrapping for Create, List, Delete
- Keep Deactivate without retry (intentional)
- Fatal checks switch from `authdb.IsPermissionError()` to `dberrors.IsFatal()`

**Deletion service:** Deletion operations are already transactional internally. The service wraps the whole call in `WithRetryableGormTransaction` since user/group deletion should not fail on a transient blip.

### 6. Handler Layer Changes

Handlers use `errors.Is()` against `dberrors` types instead of string matching.

**HTTP status mapping:**

| `dberrors` type | HTTP Status | Notes |
|-----------------|-------------|-------|
| `ErrNotFound` | 404 | Includes entity-specific variants |
| `ErrDuplicate` | 409 Conflict | Subset of constraint |
| `ErrForeignKey` | 400 Bad Request | Invalid reference |
| `ErrConstraint` (other) | 400 Bad Request | Check violations, etc. |
| `ErrTransient` | 503 + Retry-After: 30 | Retries exhausted |
| `ErrPermission` | Never reaches handler | Fatal — service calls os.Exit |
| `context.Canceled` / `DeadlineExceeded` | No response | Client disconnected |
| Anything else | 500 | Unexpected |

**Handler pattern:**

```go
if err != nil {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return
    }
    if errors.Is(err, dberrors.ErrNotFound) {
        c.JSON(404, ...)
        return
    }
    if errors.Is(err, dberrors.ErrDuplicate) {
        c.JSON(409, ...)
        return
    }
    if errors.Is(err, dberrors.ErrConstraint) {
        c.JSON(400, ...)
        return
    }
    if errors.Is(err, dberrors.ErrTransient) {
        c.Header("Retry-After", "30")
        c.JSON(503, ...)
        return
    }
    c.JSON(500, ...)
}
```

**Files affected:**

- `api/client_credentials_handlers.go` — switch from local sentinels to `dberrors.*`
- `api/admin_user_credentials_handlers.go` — remove fallback string matching
- `api/admin_user_handlers.go` — replace `strings.Contains(err.Error(), ErrMsgUserNotFound)`
- `api/request_utils.go` — rewrite `StoreErrorToRequestError()` and `isForeignKeyConstraintError()`
- `api/identity.go` — replace `strings.Contains(err.Error(), "user not found")`
- `api/document_sub_resource_handlers.go` — replace string "not found" checks
- `api/triage_note_handlers.go` — replace string "not found" checks
- `api/survey_handlers.go` — replace string matching for not-found, constraint, duplicate
- `api/admin_group_handlers.go` — replace Oracle value-too-long patterns and string matching
- `api/admin_automation_handlers.go` — replace duplicate/constraint string matching
- `api/validation.go` — replace "invalid UUID" string check

### 7. Fatal Error Policy

**Fatal conditions:**

| Condition | Detection Layer | Detection Method |
|-----------|----------------|------------------|
| `crypto/rand.Read` failure | Service | Direct error check |
| DB permission denied | Repository → Service | `pgconn.PgError` SQLSTATE `42501`, godror `ORA-01031` |
| DB invalid credentials | Repository → Service | pgconn `28P01`, godror `ORA-01017` |
| bcrypt failure | Service | Direct error check |

**Shutdown behavior:**

```go
// internal/dberrors/fatal.go
func HandleFatal(err error) {
    logger := slogging.Get()
    logger.Error("Fatal error, shutting down: %v", err)
    os.Exit(1)
}
```

Log and exit. No graceful shutdown in this implementation — if DB permissions are gone or entropy is broken, a graceful drain won't produce correct responses. Graceful shutdown is tracked in [#262](https://github.com/ericfitz/tmi/issues/262).

Fatal errors never reach handlers. Services detect them and call `HandleFatal()`.

### 8. Migration Strategy

**Implementation order** (each step is independently buildable and testable):

1. Create `internal/dberrors/` — new package, no existing code affected
2. Migrate `auth/db/retry.go` — swap internals to use `dberrors.Classify`, public API unchanged
3. Update `auth/repository/interfaces.go` — sentinel errors wrap `dberrors.ErrNotFound`
4. Update repository implementations — return `dberrors.Classify(err)` instead of `fmt.Errorf` wrapping
5. Update `api/client_credentials_service.go` — delete `classifyDBError()` and local sentinels
6. Update handlers — replace `strings.Contains` with `errors.Is()`, one file at a time
7. Update `api/request_utils.go` — rewrite `StoreErrorToRequestError()` and `isForeignKeyConstraintError()`

### 9. Testing Strategy

- **`internal/dberrors/` unit tests:** Construct real `pgconn.PgError` with specific SQLSTATE codes and verify `Classify` returns the correct sentinel. Same for godror `OraErr` behind build tag. Test the string-matching fallback path.
- **`auth/db/retry.go` unit tests:** Verify `IsRetryableError` returns correct results — behavior-preserving refactors.
- **Repository tests:** Existing tests pass unchanged. Add tests verifying `errors.Is(err, dberrors.ErrNotFound)` etc.
- **Handler tests:** Existing tests pass. Add tests verifying correct HTTP status codes for each error category.
- **Integration tests:** `make test-integration` validates end-to-end with real PostgreSQL.

### 10. What Does NOT Change

- No database schema changes
- No API behavior changes (same HTTP status codes for same conditions)
- No changes to GORM store files in `api/` (deferred to #261)
- No changes to WebSocket code
- No changes to OpenAPI spec
