# Credential Service 500 Elimination — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all HTTP 500 responses from credential service handlers by adding GORM-aware retry logic, typed service errors, and fatal error handling.

**Architecture:** The service layer (`client_credentials_service.go`) becomes the error classification boundary — it wraps DB operations in retryable transactions, converts raw GORM/DB errors to typed sentinel errors, and handles fatal conditions (crypto failure, DB permission denied) by exiting the process. Handlers become thin mappers from typed errors to HTTP responses using `errors.Is()`.

**Tech Stack:** Go, GORM, Gin, `auth/db` retry infrastructure, `testify`

**Spec:** `docs/superpowers/specs/2026-04-15-credential-service-500-elimination-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `auth/db/retry.go` | Modify | Add `WithRetryableGormTransaction()`, `IsPermissionError()` |
| `auth/db/retry_test.go` | Create | Tests for GORM retry wrapper and error classifiers |
| `auth/service.go` | Modify | Add `GormDB()` accessor |
| `api/client_credentials_service.go` | Modify | Add typed errors, `gormDB` field, retry wrapping, fatal handling |
| `api/client_credentials_service_test.go` | Modify | Add tests for error classification and retry behavior |
| `api/admin_user_credentials_handlers.go` | Modify | Remove nil-service guards, use typed errors |
| `api/admin_user_credentials_handlers_test.go` | Modify | Update tests for typed error behavior |
| `api/client_credentials_handlers.go` | Modify | Remove nil-service guards, use typed errors |
| `api/client_credentials_handlers_test.go` | Modify | Update tests for typed error behavior |

---

### Task 1: Add `IsPermissionError()` to `auth/db/retry.go`

**Files:**
- Modify: `auth/db/retry.go`
- Create: `auth/db/retry_test.go`

- [ ] **Step 1: Write failing tests for `IsPermissionError()`**

Create `auth/db/retry_test.go`:

```go
package db

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestIsPermissionError`
Expected: FAIL — `IsPermissionError` not defined

- [ ] **Step 3: Implement `IsPermissionError()`**

Add to `auth/db/retry.go`, after `IsConnectionError()`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestIsPermissionError`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add auth/db/retry.go auth/db/retry_test.go
git commit -m "feat(db): add IsPermissionError() for detecting DB permission failures

Refs #257"
```

---

### Task 2: Add `WithRetryableGormTransaction()` to `auth/db/retry.go`

**Files:**
- Modify: `auth/db/retry.go`
- Modify: `auth/db/retry_test.go`

- [ ] **Step 1: Write failing tests for `WithRetryableGormTransaction()`**

Add to `auth/db/retry_test.go`. These tests use a mock `*gorm.DB` — since GORM's `Transaction()` requires a real DB, test the retry logic by testing the helper's behavior with an in-memory SQLite DB:

```go
import (
	"context"
	"fmt"
	"sync/atomic"

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestWithRetryableGormTransaction`
Expected: FAIL — `WithRetryableGormTransaction` not defined

- [ ] **Step 3: Implement `WithRetryableGormTransaction()`**

Add import `"gorm.io/gorm"` to `auth/db/retry.go`, then add:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestWithRetryableGormTransaction`
Expected: PASS

- [ ] **Step 5: Also run `IsPermissionError` tests to confirm no regressions**

Run: `make test-unit name=TestIsPermissionError`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add auth/db/retry.go auth/db/retry_test.go
git commit -m "feat(db): add WithRetryableGormTransaction for GORM-aware retry logic

Wraps GORM transactions with the same retry/backoff infrastructure as
WithRetryableTransaction but operates on *gorm.DB instead of *sql.DB.

Refs #257"
```

---

### Task 3: Add `GormDB()` accessor to `auth/service.go`

**Files:**
- Modify: `auth/service.go`

- [ ] **Step 1: Add the accessor**

Add to `auth/service.go` after the existing `GetSAMLManager()` getter (around line 123):

```go
// GormDB returns the underlying GORM database connection.
// Used by services that need to wrap operations in retryable transactions.
func (s *Service) GormDB() *gorm.DB {
	return s.dbManager.Gorm().DB()
}
```

Add `"gorm.io/gorm"` to the import block if not already present.

- [ ] **Step 2: Run lint to verify**

Run: `make lint`
Expected: PASS (no new lint issues)

- [ ] **Step 3: Commit**

```
git add auth/service.go
git commit -m "feat(auth): add GormDB() accessor to auth.Service

Exposes the GORM database connection for services that need to wrap
operations in WithRetryableGormTransaction.

Refs #257"
```

---

### Task 4: Add typed errors and refactor `ClientCredentialService`

**Files:**
- Modify: `api/client_credentials_service.go`
- Modify: `api/client_credentials_service_test.go`

- [ ] **Step 1: Write failing tests for error classification**

Add to `api/client_credentials_service_test.go`:

```go
import (
	"errors"

	// existing imports...
)

func TestCredentialServiceErrors(t *testing.T) {
	t.Run("ErrCredentialConstraint is distinguishable", func(t *testing.T) {
		err := fmt.Errorf("create failed: %w", ErrCredentialConstraint)
		assert.True(t, errors.Is(err, ErrCredentialConstraint))
		assert.False(t, errors.Is(err, ErrCredentialNotFound))
		assert.False(t, errors.Is(err, ErrTransientDB))
	})

	t.Run("ErrCredentialNotFound is distinguishable", func(t *testing.T) {
		err := fmt.Errorf("delete failed: %w", ErrCredentialNotFound)
		assert.True(t, errors.Is(err, ErrCredentialNotFound))
		assert.False(t, errors.Is(err, ErrCredentialConstraint))
	})

	t.Run("ErrTransientDB is distinguishable", func(t *testing.T) {
		err := fmt.Errorf("list failed: %w", ErrTransientDB)
		assert.True(t, errors.Is(err, ErrTransientDB))
		assert.False(t, errors.Is(err, ErrCredentialNotFound))
	})
}

func TestClassifyDBError(t *testing.T) {
	t.Run("constraint violation", func(t *testing.T) {
		err := fmt.Errorf("failed: unique constraint violation")
		result := classifyDBError(err)
		assert.ErrorIs(t, result, ErrCredentialConstraint)
	})

	t.Run("duplicate key", func(t *testing.T) {
		err := fmt.Errorf("failed: duplicate key value violates unique constraint")
		result := classifyDBError(err)
		assert.ErrorIs(t, result, ErrCredentialConstraint)
	})

	t.Run("not found via RowsAffected sentinel", func(t *testing.T) {
		err := fmt.Errorf("client credential not found or unauthorized")
		result := classifyDBError(err)
		assert.ErrorIs(t, result, ErrCredentialNotFound)
	})

	t.Run("connection error wraps as transient", func(t *testing.T) {
		err := fmt.Errorf("transaction failed after 3 attempts: %w", fmt.Errorf("driver: bad connection"))
		result := classifyDBError(err)
		assert.ErrorIs(t, result, ErrTransientDB)
	})

	t.Run("unknown error passes through", func(t *testing.T) {
		err := fmt.Errorf("something completely unexpected")
		result := classifyDBError(err)
		assert.Equal(t, err, result)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestCredentialServiceErrors`
Expected: FAIL — `ErrCredentialConstraint` etc. not defined

Run: `make test-unit name=TestClassifyDBError`
Expected: FAIL — `classifyDBError` not defined

- [ ] **Step 3: Add typed errors and `classifyDBError()` to `client_credentials_service.go`**

Add to the top of `api/client_credentials_service.go`, after the imports:

```go
import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Typed errors returned by ClientCredentialService.
// Handlers use errors.Is() to map these to HTTP status codes.
var (
	// ErrCredentialConstraint indicates a constraint violation (duplicate key, FK violation).
	ErrCredentialConstraint = errors.New("credential constraint violation")

	// ErrCredentialNotFound indicates the credential does not exist or is not owned by the user.
	ErrCredentialNotFound = errors.New("credential not found")

	// ErrTransientDB indicates a transient database error after retries were exhausted.
	ErrTransientDB = errors.New("transient database error")
)

// classifyDBError converts a raw database error into a typed service error.
// Permission errors are not classified here — they are fatal and handled
// before this function is called.
func classifyDBError(err error) error {
	if err == nil {
		return nil
	}

	errStr := strings.ToLower(err.Error())

	// Constraint violations (unique, FK, etc.)
	constraintPatterns := []string{"constraint", "duplicate", "violates"}
	for _, pattern := range constraintPatterns {
		if strings.Contains(errStr, pattern) {
			return fmt.Errorf("%s: %w", err.Error(), ErrCredentialConstraint)
		}
	}

	// Not found (from repository RowsAffected == 0 checks)
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "unauthorized") {
		return fmt.Errorf("%s: %w", err.Error(), ErrCredentialNotFound)
	}

	// Transient errors (retries exhausted)
	if strings.Contains(errStr, "transaction failed after") || authdb.IsRetryableError(err) || authdb.IsConnectionError(err) {
		return fmt.Errorf("%s: %w", err.Error(), ErrTransientDB)
	}

	return err // Unknown error — pass through
}
```

- [ ] **Step 4: Run error classification tests to verify they pass**

Run: `make test-unit name=TestCredentialServiceErrors`
Expected: PASS

Run: `make test-unit name=TestClassifyDBError`
Expected: PASS

- [ ] **Step 5: Refactor `ClientCredentialService` struct and `Create()` method**

Update the struct to include `gormDB`:

```go
// ClientCredentialService handles client credential generation and management
type ClientCredentialService struct {
	authService *auth.Service
	gormDB      *gorm.DB
}

// NewClientCredentialService creates a new client credential service
func NewClientCredentialService(authService *auth.Service) *ClientCredentialService {
	return &ClientCredentialService{
		authService: authService,
		gormDB:      authService.GormDB(),
	}
}
```

Replace the `Create()` method:

```go
// Create generates a new client credential for the specified owner
// The client_secret is only returned once and cannot be retrieved later (GitHub PAT pattern)
func (s *ClientCredentialService) Create(ctx context.Context, ownerUUID uuid.UUID, req CreateClientCredentialRequest) (*CreateClientCredentialResponse, error) {
	logger := slogging.Get()

	// 1. Generate client_id: tmi_cc_{base64url(16_bytes)}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		logger.Error("Fatal: crypto/rand failure generating client_id: %v", err)
		os.Exit(1)
	}
	clientID := fmt.Sprintf("tmi_cc_%s", base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(clientIDBytes))

	// 2. Generate client_secret: 32 bytes = 43 chars base64url
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		logger.Error("Fatal: crypto/rand failure generating client_secret: %v", err)
		os.Exit(1)
	}
	clientSecret := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(secretBytes)

	// 3. Hash client_secret with bcrypt (cost 10)
	hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), 10)
	if err != nil {
		logger.Error("Fatal: bcrypt failure hashing client_secret: %v", err)
		os.Exit(1)
	}

	// 4. Store in database with retryable transaction
	var cred *auth.ClientCredential
	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		var createErr error
		cred, createErr = s.authService.CreateClientCredential(ctx, auth.ClientCredentialCreateParams{
			OwnerUUID:        ownerUUID,
			ClientID:         clientID,
			ClientSecretHash: string(hash),
			Name:             req.Name,
			Description:      req.Description,
			ExpiresAt:        req.ExpiresAt,
		})
		return createErr
	})

	if dbErr != nil {
		if authdb.IsPermissionError(dbErr) {
			logger.Error("Fatal: database permission denied creating client credential: %v", dbErr)
			os.Exit(1)
		}
		return nil, classifyDBError(dbErr)
	}

	// 5. Return response with plaintext secret (ONLY TIME IT'S VISIBLE)
	return &CreateClientCredentialResponse{
		ID:           cred.ID,
		ClientID:     cred.ClientID,
		ClientSecret: clientSecret,
		Name:         cred.Name,
		Description:  cred.Description,
		CreatedAt:    cred.CreatedAt,
		ExpiresAt:    cred.ExpiresAt,
	}, nil
}
```

**Important note:** The `CreateClientCredential` call inside the GORM transaction callback currently uses the repository's own GORM connection (not the `tx` from the transaction). This is because the repository was designed with its own `*gorm.DB`. For this task, wrapping in `WithRetryableGormTransaction()` provides retry semantics. The repository's GORM operations will still run, and the retry wrapper will catch and retry transient errors. A deeper refactor to pass `tx` through the repository layer is deferred to #258.

- [ ] **Step 6: Refactor `List()` method**

Replace the `List()` method:

```go
// List retrieves all client credentials for the specified owner (without secrets)
func (s *ClientCredentialService) List(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredentialInfoInternal, error) {
	logger := slogging.Get()

	var creds []*auth.ClientCredential
	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		var err error
		creds, err = s.authService.ListClientCredentialsByOwner(ctx, ownerUUID)
		return err
	})

	if dbErr != nil {
		if authdb.IsPermissionError(dbErr) {
			logger.Error("Fatal: database permission denied listing client credentials: %v", dbErr)
			os.Exit(1)
		}
		return nil, classifyDBError(dbErr)
	}

	// Convert to info structs without secrets
	result := make([]*ClientCredentialInfoInternal, 0, len(creds))
	for _, cred := range creds {
		result = append(result, &ClientCredentialInfoInternal{
			ID:          cred.ID,
			ClientID:    cred.ClientID,
			Name:        cred.Name,
			Description: cred.Description,
			IsActive:    cred.IsActive,
			LastUsedAt:  cred.LastUsedAt,
			CreatedAt:   cred.CreatedAt,
			ModifiedAt:  cred.ModifiedAt,
			ExpiresAt:   cred.ExpiresAt,
		})
	}

	return result, nil
}
```

- [ ] **Step 7: Refactor `Delete()` method**

Replace the `Delete()` method:

```go
// Delete permanently deletes a client credential
func (s *ClientCredentialService) Delete(ctx context.Context, credID uuid.UUID, ownerUUID uuid.UUID) error {
	logger := slogging.Get()

	dbErr := authdb.WithRetryableGormTransaction(ctx, s.gormDB, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		return s.authService.DeleteClientCredential(ctx, credID, ownerUUID)
	})

	if dbErr != nil {
		if authdb.IsPermissionError(dbErr) {
			logger.Error("Fatal: database permission denied deleting client credential: %v", dbErr)
			os.Exit(1)
		}
		return classifyDBError(dbErr)
	}

	return nil
}
```

- [ ] **Step 8: Run all existing service tests to check for regressions**

Run: `make test-unit name=TestClientCredential`
Expected: PASS — existing crypto/helper tests still pass

- [ ] **Step 9: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 10: Commit**

```
git add api/client_credentials_service.go api/client_credentials_service_test.go
git commit -m "feat(api): add typed errors and retry logic to ClientCredentialService

Service layer now wraps DB operations in WithRetryableGormTransaction,
classifies errors into typed sentinels (ErrCredentialConstraint,
ErrCredentialNotFound, ErrTransientDB), and handles fatal errors
(crypto failure, DB permission denied) by exiting.

Refs #257"
```

---

### Task 5: Update admin credential handlers

**Files:**
- Modify: `api/admin_user_credentials_handlers.go`
- Modify: `api/admin_user_credentials_handlers_test.go`

- [ ] **Step 1: Write new tests for typed error handling in admin List handler**

Add to `api/admin_user_credentials_handlers_test.go`. The List and Create handlers currently can't be tested for service-layer errors without a real auth service (the mock `credentialDeleter` pattern only covers Delete). For now, verify the existing behavior doesn't regress and add a comment noting that service-layer error tests are covered by the service test.

First, update the existing `TestDeleteAdminUserClientCredential_CredentialNotFound` to use typed errors:

```go
func credNotFoundErr() error {
	return fmt.Errorf("client credential not found or unauthorized: %w", ErrCredentialNotFound)
}

func credServerErr() error {
	return fmt.Errorf("transaction failed after 3 attempts: %w", ErrTransientDB)
}
```

- [ ] **Step 2: Run delete tests to verify they still pass with typed errors**

Run: `make test-unit name=TestDeleteAdminUserClientCredential`
Expected: PASS (the Delete handler still uses string matching for now; we update it next)

- [ ] **Step 3: Update `ListAdminUserClientCredentials` handler**

In `api/admin_user_credentials_handlers.go`, replace the List handler's service call and error handling (lines 68-89):

Remove the nil-service guard for `GetService()` (lines 80-81 become direct call). Replace the 500 error response with typed error handling:

```go
	// Get auth service
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Authentication service temporarily unavailable - please retry",
		})
		return
	}

	// List credentials for the target user
	ccService := NewClientCredentialService(authServiceAdapter.GetService())
	creds, err := ccService.List(c.Request.Context(), internalUuid)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential list: %v", err)
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error listing client credentials for user %s: %v", internalUuid, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		logger.Error("Unexpected error listing client credentials for user %s: %v", internalUuid, err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list client credentials",
		})
		return
	}
```

Add `"context"` and `"errors"` to the import block.

- [ ] **Step 4: Update `CreateAdminUserClientCredential` handler**

Replace the error handling after the service call (lines 196-213):

```go
	// Create credential (no quota check — admin operation)
	ccService := NewClientCredentialService(authServiceAdapter.GetService())
	resp, err := ccService.Create(c.Request.Context(), internalUuid, CreateClientCredentialRequest{
		Name:        req.Name,
		Description: description,
		ExpiresAt:   timeFromPtr(req.ExpiresAt),
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential create: %v", err)
			return
		}
		if errors.Is(err, ErrCredentialConstraint) {
			logger.Warn("Client credential creation failed due to constraint: %v", err)
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "Client credential could not be created due to a conflict",
			})
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error creating client credential for user %s: %v", internalUuid, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		logger.Error("Unexpected error creating client credential for user %s: %v", internalUuid, err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}
```

- [ ] **Step 5: Update `DeleteAdminUserClientCredential` handler**

Replace the error handling in the delete handler's error path (the part that currently string-matches). The Delete handler already uses the `credentialDeleter` interface. When `credentialDeleter` is nil (production), it goes through the auth service path. Update that path's error handling to use typed errors:

```go
	// Delete credential (ownership enforced by ownerUUID)
	if err := deleter.Delete(c.Request.Context(), credentialId, internalUuid); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential delete: %v", err)
			return
		}
		if errors.Is(err, ErrCredentialNotFound) {
			logger.Warn("Client credential not found or unauthorized: user=%s, credential=%s: %v", internalUuid, credentialId, err)
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Client credential not found or not owned by this user",
			})
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error deleting client credential %s for user %s: %v", credentialId, internalUuid, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		// Fallback: string matching for backward compatibility with credentialDeleter interface
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "unauthorized") {
			logger.Warn("Client credential not found or unauthorized: user=%s, credential=%s: %v", internalUuid, credentialId, err)
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Client credential not found or not owned by this user",
			})
		} else {
			logger.Error("Failed to delete client credential %s for user %s: %v", credentialId, internalUuid, err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Failed to delete client credential - please retry",
			})
		}
		return
	}
```

Note: The Delete handler retains string-matching fallback because the `credentialDeleter` interface is used by tests with mock errors that don't wrap typed errors. The typed error path takes priority.

- [ ] **Step 6: Run all admin handler tests**

Run: `make test-unit name=TestAdminUserCredentials`
Expected: PASS

Run: `make test-unit name=TestDeleteAdminUserClientCredential`
Expected: PASS

- [ ] **Step 7: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 8: Commit**

```
git add api/admin_user_credentials_handlers.go api/admin_user_credentials_handlers_test.go
git commit -m "fix(api): replace 500 responses with typed error handling in admin credential handlers

List and Create handlers now use errors.Is() with typed service errors
instead of returning 500. Delete handler updated to prefer typed errors
with string-matching fallback for test mocks.

Fixes #257"
```

---

### Task 6: Update `/me/` credential handlers

**Files:**
- Modify: `api/client_credentials_handlers.go`
- Modify: `api/client_credentials_handlers_test.go`

- [ ] **Step 1: Update `CreateCurrentUserClientCredential` error handling**

In `api/client_credentials_handlers.go`, replace the service call error handling (lines 140-158):

```go
	// Create client credential
	service := NewClientCredentialService(authServiceAdapter.GetService())
	resp, err := service.Create(c.Request.Context(), ownerUUID, CreateClientCredentialRequest{
		Name:        req.Name,
		Description: description,
		ExpiresAt:   timeFromPtr(req.ExpiresAt),
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential create: %v", err)
			return
		}
		if errors.Is(err, ErrCredentialConstraint) {
			logger.Warn("Client credential creation failed due to constraint: %v", err)
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "Client credential could not be created due to a conflict",
			})
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error creating client credential: %v", err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		logger.Error("Unexpected error creating client credential: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create client credential",
		})
		return
	}
```

Add `"context"` and `"errors"` to the import block. Remove `"strings"` from imports if it was only used for the error string matching (check — it's also used by `validateClientCredentialName`, so it stays).

- [ ] **Step 2: Update `ListCurrentUserClientCredentials` error handling**

Replace the service call error handling (lines 220-228):

```go
	// List credentials
	service := NewClientCredentialService(authServiceAdapter.GetService())
	creds, err := service.List(c.Request.Context(), ownerUUID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential list: %v", err)
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error listing client credentials: %v", err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		logger.Error("Unexpected error listing client credentials: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to list client credentials",
		})
		return
	}
```

- [ ] **Step 3: Update `DeleteCurrentUserClientCredential` error handling**

Replace the service call error handling (lines 300-308):

```go
	// Delete credential
	service := NewClientCredentialService(authServiceAdapter.GetService())
	if err := service.Delete(c.Request.Context(), credentialId, ownerUUID); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Debug("Request cancelled during credential delete: %v", err)
			return
		}
		if errors.Is(err, ErrCredentialNotFound) {
			logger.Warn("Client credential not found: id=%s, owner=%s: %v", credentialId, userUUID, err)
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Client credential not found or not owned by user",
			})
			return
		}
		if errors.Is(err, ErrTransientDB) {
			logger.Warn("Transient DB error deleting client credential: %v", err)
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Database temporarily unavailable - please retry",
			})
			return
		}
		logger.Error("Unexpected error deleting client credential: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to delete client credential",
		})
		return
	}
```

- [ ] **Step 4: Run all `/me/` handler tests**

Run: `make test-unit name=TestCreateCurrentUserClientCredential`
Expected: PASS

Run: `make test-unit name=TestListCurrentUserClientCredentials`
Expected: PASS

Run: `make test-unit name=TestDeleteCurrentUserClientCredential`
Expected: PASS

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add api/client_credentials_handlers.go api/client_credentials_handlers_test.go
git commit -m "fix(api): replace 500 responses with typed error handling in /me/ credential handlers

All three handlers (Create, List, Delete) now use errors.Is() with
typed service errors. Constraint violations return 409, transient DB
errors return 503 with Retry-After, context cancellation is logged
silently.

Refs #257"
```

---

### Task 7: Remove nil-service guards (dead code)

**Files:**
- Modify: `api/admin_user_credentials_handlers.go`
- Modify: `api/client_credentials_handlers.go`

- [ ] **Step 1: Remove `GetService()` nil check from admin List handler**

In `api/admin_user_credentials_handlers.go`, the line that creates the service currently reads:

```go
ccService := NewClientCredentialService(authServiceAdapter.GetService())
```

There was previously a nil check on `GetService()` before this line. After Task 5, verify that no nil check on `GetService()` remains in the List or Create handlers. If the nil-service guard was part of the `authServiceAdapter` check block, it should have already been handled — but verify explicitly.

Search for any remaining `GetService() == nil` or `underlyingService` nil check patterns in both files.

- [ ] **Step 2: Verify no nil-service guard remains in `/me/` handlers**

Same check in `api/client_credentials_handlers.go`. The handlers should call `NewClientCredentialService(authServiceAdapter.GetService())` directly without nil-checking the return of `GetService()`.

- [ ] **Step 3: Run full test suite for both handler files**

Run: `make test-unit name=TestAdminUserCredentials`
Run: `make test-unit name=TestDeleteAdminUserClientCredential`
Run: `make test-unit name=TestCreateCurrentUserClientCredential`
Run: `make test-unit name=TestListCurrentUserClientCredentials`
Run: `make test-unit name=TestDeleteCurrentUserClientCredential`
Expected: All PASS

- [ ] **Step 4: Commit (if any changes were needed)**

```
git add api/admin_user_credentials_handlers.go api/client_credentials_handlers.go
git commit -m "refactor(api): remove dead nil-service guards from credential handlers

GetService() cannot return nil after successful server startup. The
server exits at startup if auth initialization fails.

Refs #257"
```

---

### Task 8: Build, lint, and full test validation

**Files:** None (validation only)

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 3: Run all unit tests**

Run: `make test-unit`
Expected: PASS — no regressions

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 5: Verify no remaining 500 responses in credential handlers**

Search both handler files for `StatusInternalServerError`:

```bash
rg 'StatusInternalServerError' api/admin_user_credentials_handlers.go api/client_credentials_handlers.go
```

Expected: The only remaining 500s should be in the "unexpected error" fallback paths — these are genuinely unknown errors that we can't classify. The known error paths (constraint, transient, not-found, permission, crypto) are all handled.

- [ ] **Step 6: Commit any final fixes and verify**

If all checks pass, the implementation is complete. Close issue #257 when merging.
