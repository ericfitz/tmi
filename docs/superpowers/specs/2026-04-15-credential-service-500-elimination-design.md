# Credential Service 500 Elimination

**Issue:** [#257](https://github.com/ericfitz/tmi/issues/257)
**Date:** 2026-04-15
**Related:** [#258](https://github.com/ericfitz/tmi/issues/258) (broader DB retry audit)

## Problem

`ListAdminUserClientCredentials` and `CreateAdminUserClientCredential` in `api/admin_user_credentials_handlers.go` return HTTP 500 on service errors. The same problem exists in the `/me/` equivalents in `api/client_credentials_handlers.go`. This violates the zero-500-error policy.

The handlers also contain nil-service guards that protect against states that cannot occur in a running server (the server exits at startup if auth initialization fails).

## Root Cause Analysis

The credential service (`api/client_credentials_service.go`) performs DB operations through GORM repositories without retry logic. When a transient DB error occurs (connection reset, pool exhaustion), the error propagates up as a generic `error`, and every handler returns 500.

The existing `WithRetryableTransaction()` in `auth/db/retry.go` provides retry logic with exponential backoff, but it operates on `*sql.DB`/`*sql.Tx`. It has never been used — no callers exist. The credential repositories use GORM's `*gorm.DB`, so the retry mechanism cannot be used directly without adaptation.

## Design

### 1. GORM-aware retry wrapper

Add `WithRetryableGormTransaction()` to `auth/db/retry.go` alongside the existing `*sql.DB` version. This wrapper:

- Accepts a `*gorm.DB` and a `func(*gorm.DB) error` callback
- Uses GORM's `gormDB.Transaction()` internally
- Applies the same retry logic and `IsRetryableError()` classification as the existing `WithRetryableTransaction()`
- Uses the same `RetryConfig` (3 retries, 100ms base, 5s max)

```go
func WithRetryableGormTransaction(ctx context.Context, db *gorm.DB, cfg RetryConfig, fn func(tx *gorm.DB) error) error
```

### 2. Typed service errors

Define typed errors in `api/client_credentials_service.go`:

```go
var (
    ErrCredentialConstraint = errors.New("credential constraint violation")
    ErrCredentialNotFound   = errors.New("credential not found")
    ErrTransientDB          = errors.New("transient database error")
)
```

The service classifies errors from the repository/GORM layer into these types. Handlers use `errors.Is()` instead of string matching.

### 3. Fatal error policy

These errors indicate the server is fundamentally broken and trigger `slogging.Get().Error()` + `os.Exit(1)`:

- **`crypto/rand.Read` failure** — OS entropy source is broken; the server cannot generate secrets, tokens, or UUIDs.
- **DB permission denied** — The DB user lost privileges; every subsequent request will also fail.

Detection: DB permission errors are identified by checking the error string for PostgreSQL permission-denied indicators (`"permission denied"`, `"insufficient privilege"`). This is string matching, but it is acceptable here because the response is fatal — there is no risk of misclassifying a non-fatal error as fatal since these strings are specific to permission errors.

### 4. Service layer changes (`api/client_credentials_service.go`)

The `ClientCredentialService` needs access to the GORM `*gorm.DB` to pass to `WithRetryableGormTransaction()`. Add it as a field alongside the existing `*auth.Service`:

```go
type ClientCredentialService struct {
    authService *auth.Service
    gormDB      *gorm.DB
}
```

The `gormDB` is obtained from `auth.Service` via a new accessor: `authService.GormDB()`, which returns `s.dbManager.Gorm().DB()`.

#### Create()

1. Generate client_id and secret (`crypto/rand` + bcrypt) — **fatal on failure**.
2. Wrap the DB insert in `WithRetryableGormTransaction()`.
3. Classify errors from the transaction:
   - Constraint/duplicate → return `ErrCredentialConstraint`
   - Retries exhausted on transient error → return `ErrTransientDB`
   - Permission denied → **fatal**

#### List()

1. Wrap the DB query in `WithRetryableGormTransaction()`.
2. Classify errors:
   - Retries exhausted → return `ErrTransientDB`
   - Permission denied → **fatal**

#### Delete()

1. Wrap the DB delete in `WithRetryableGormTransaction()`.
2. Classify errors:
   - Not found / rows affected == 0 → return `ErrCredentialNotFound`
   - Retries exhausted → return `ErrTransientDB`
   - Permission denied → **fatal**

### 5. Handler layer changes

**Both `admin_user_credentials_handlers.go` and `client_credentials_handlers.go`:**

- Remove nil-service guards on `GetService()` (dead code — server exits at startup if auth fails).
- Keep the `authServiceAdapter` type assertion (removal deferred to #258, since it exists across many handlers).
- Replace string-matching error classification with `errors.Is()`:

| Service error | Handler response |
|---|---|
| `ErrCredentialConstraint` | 409 Conflict |
| `ErrCredentialNotFound` | 404 Not Found |
| `ErrTransientDB` | 503 Service Unavailable + `Retry-After: 30` |
| `context.Canceled` / `context.DeadlineExceeded` | Log at debug level, return (client is gone) |
| Fatal errors | Never reach handler (service calls `os.Exit(1)`) |

### 6. Context cancellation handling

When the context is cancelled (client disconnected), the service returns the context error. Handlers check `errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)` and log at debug level without writing a response (the client is already gone, and writing to a closed connection is a no-op in Gin).

### 7. What's NOT in scope

- Removing `authServiceAdapter` type assertions from handlers (→ #258)
- Retrying DB operations in other services (→ #258)
- Changing the DB connection idle timeout (separate investigation)

## Files modified

| File | Changes |
|---|---|
| `auth/db/retry.go` | Add `WithRetryableGormTransaction()` |
| `auth/service.go` | Add `GormDB()` accessor |
| `api/client_credentials_service.go` | Add typed errors, retry wrapping, fatal error handling, `gormDB` field |
| `api/admin_user_credentials_handlers.go` | Remove nil-service guards, use `errors.Is()` for error classification |
| `api/client_credentials_handlers.go` | Same handler changes as admin |
| New: tests for retry behavior and error classification | |

## Error classification reference

For detecting constraint vs. permission vs. transient errors from GORM/PostgreSQL:

| Error category | Detection | Source |
|---|---|---|
| Constraint violation | `strings.Contains(lower, "unique")`, `"duplicate"`, `"violates"`, `"constraint"` | PostgreSQL error messages |
| Permission denied | `strings.Contains(lower, "permission denied")`, `"insufficient privilege"` | PostgreSQL error messages |
| Transient / connection | `IsRetryableError()` from `auth/db/retry.go` | Existing infrastructure |
| Not found | `gorm.ErrRecordNotFound` or `RowsAffected == 0` | GORM |

Note: String matching for error classification is used only at the service layer boundary where GORM/PostgreSQL errors are converted to typed service errors. Handlers never string-match — they use `errors.Is()` exclusively.
