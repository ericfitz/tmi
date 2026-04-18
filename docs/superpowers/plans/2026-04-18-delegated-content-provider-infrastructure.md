# Delegated Content Provider Infrastructure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the per-user OAuth token infrastructure (encrypted `user_content_tokens` table, `/me/content_tokens/*` and `/admin/users/{id}/content_tokens/*` endpoints, a `DelegatedSource` helper, and a build-tagged `MockDelegatedSource`) that future delegated content providers (Confluence, Google Workspace) will layer on top of.

**Architecture:** Mirrors TMI's existing service-provider content pipeline (#232) but consumes per-user OAuth tokens instead of operator-scoped credentials. A new `content_oauth` config section + `ContentOAuthProviderRegistry` lives in parallel to `auth.ProviderRegistry`. Tokens are AES-256-GCM encrypted at rest. Refresh is lazy + serialized via `SELECT … FOR UPDATE`. OAuth state is Redis-backed with a 10-minute TTL.

**Tech Stack:** Go 1.22+, Gin, GORM (AutoMigrate), PostgreSQL, Redis (go-redis), `golang.org/x/oauth2`, AES-GCM from `crypto/aes`+`crypto/cipher`, oapi-codegen v2 (Gin server).

**Spec:** `docs/superpowers/specs/2026-04-18-delegated-content-provider-infrastructure-design.md`

---

## Reference files to read before starting

Every subagent assigned a task MUST read these first to calibrate on project style:

- `docs/superpowers/specs/2026-04-18-delegated-content-provider-infrastructure-design.md` — this sub-project's spec
- `docs/superpowers/specs/2026-04-08-content-providers-design.md` — the broader content-provider architecture (#232)
- `api/content_source.go` — existing source interfaces
- `api/content_source_http.go`, `api/content_source_google_drive.go` — existing concrete sources
- `api/models/models.go` — GORM model conventions (study `ClientCredential` at line 86 and its `AllModels()` registration at line 752)
- `api/repository_interfaces.go` — repository interface + typed error idiom (first ~100 lines)
- `api/group_repository.go` — concrete GORM repository implementation (pattern to follow)
- `api/metadata_repository.go` — another repository example
- `internal/config/config.go` lines 120-200 — `OAuthConfig` / `OAuthProviderConfig` structs + env-override mechanism (`overrideOAuthProviders`)
- `internal/envutil/` — `DiscoverProviders` helper
- `auth/provider.go` — existing `Provider` interface, `OAuthProviderConfig`, `BaseProvider`, `TokenResponse`
- `auth/handlers_oauth.go` — existing user-auth OAuth flow (state storage, callback pattern)
- `api/server.go` — where routes get registered
- `CLAUDE.md` — project conventions; use `make lint`, `make build-server`, `make test-unit`, etc. Never `go test` directly.

---

## File Structure

### Files to create

| Path | Responsibility |
|------|---------------|
| `api/models/user_content_token.go` | GORM `UserContentToken` model |
| `api/content_token_encryption.go` | AES-256-GCM encrypt/decrypt with `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` |
| `api/content_token_encryption_test.go` | Encryption unit tests |
| `api/content_token_repository.go` | `ContentToken` domain type, `ContentTokenRepository` interface, typed errors, GORM impl, `RefreshWithLock` |
| `api/content_token_repository_test.go` | Repository unit tests |
| `internal/config/content_oauth.go` | `ContentOAuthConfig`, `ContentOAuthProviderConfig`, env-var overrides, startup validation |
| `internal/config/content_oauth_test.go` | Config parsing + override tests |
| `api/content_oauth_provider.go` | `ContentOAuthProvider` interface + `BaseContentOAuthProvider` implementation |
| `api/content_oauth_provider_test.go` | Provider unit tests |
| `api/content_oauth_registry.go` | `ContentOAuthProviderRegistry` |
| `api/content_oauth_registry_test.go` | Registry unit tests |
| `api/content_oauth_state.go` | Redis-backed state store with TTL |
| `api/content_oauth_state_test.go` | State store unit tests (miniredis) |
| `api/content_oauth_pkce.go` | PKCE S256 verifier/challenge helpers |
| `api/content_oauth_pkce_test.go` | PKCE unit tests |
| `api/content_oauth_callbacks.go` | `client_callback` allow-list matcher |
| `api/content_oauth_callbacks_test.go` | Allow-list unit tests |
| `api/content_oauth_handlers.go` | `/me/content_tokens/*` and `/oauth2/content_callback` handlers |
| `api/content_oauth_handlers_test.go` | Handler unit tests (mocked repository) |
| `api/content_oauth_admin_handlers.go` | `/admin/users/{id}/content_tokens/*` handlers |
| `api/content_oauth_admin_handlers_test.go` | Admin handler unit tests |
| `api/content_source_delegated.go` | `DelegatedSource` helper (token lookup + lazy refresh + concurrent-safe) |
| `api/content_source_delegated_test.go` | Unit tests with mocked repository + stub provider |
| `api/content_source_mock_delegated.go` | Build-tagged (`dev`/`test`) `MockDelegatedSource` |
| `api/content_source_mock_delegated_test.go` | Integration tests using `MockDelegatedSource` |
| `api/testhelpers/stub_oauth_provider.go` | Build-tagged stub OAuth HTTP server for tests |
| `api/content_oauth_integration_test.go` | Full end-to-end flow integration test |

### Files to modify

| Path | Change |
|------|--------|
| `api/models/models.go` | Register `UserContentToken` in `AllModels()` |
| `internal/config/config.go` | Add `ContentOAuth ContentOAuthConfig` to `Config` struct; wire env overrides |
| `api/server.go` | Instantiate registry + state store + token repository; register routes |
| `auth/handlers_user.go` **or** `auth/repository/user_repository.go` (wherever user deletion lives) | Add pre-delete cascade hook that sweeps content-token revocations |
| `api-schema/tmi-openapi.json` | Add 6 new operations + 2 new schemas (`ContentTokenInfo`, `ContentAuthorizationURL`) |
| `api/api.go` | Regenerated by `make generate-api` (do NOT hand-edit) |

---

## Execution notes for subagents

- **TDD:** Every task with production code has a test written first. Run the test, see it fail with a specific message, then write the minimal implementation to make it pass, then re-run and see it pass, then commit.
- **Commits:** One commit per task, using conventional commits (see CLAUDE.md). Commit messages end with the `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` trailer per project convention.
- **Test commands:** Always use the Makefile. `make test-unit name=TestSpecificTest` runs a single test. `make test-integration` runs the PostgreSQL integration suite. Never invoke `go test` directly.
- **Lint gate:** `make lint` must pass before commit. Fix issues in the code (don't disable rules).
- **`make build-server` gate:** must pass before commit whenever Go code changes.
- **OpenAPI:** after editing `api-schema/tmi-openapi.json`, always run `make validate-openapi` then `make generate-api`, then `make build-server`, then `make test-unit`.
- **Do NOT edit `api/api.go` by hand** — it's regenerated by oapi-codegen.
- **Logging:** Use `slogging.Get()` / `slogging.Get().WithContext(c)`. Never `fmt.Println`, never the stdlib `log` package.
- **Ripgrep:** Use `rg` (the Grep tool), never `grep`.
- **Structured error pattern:** Follow `api/repository_interfaces.go` — typed sentinels wrapping `dberrors.ErrNotFound` / `dberrors.ErrDuplicate` / etc.

---

# Phase 1 — Schema + Repository

## Task 1.1: Add `UserContentToken` GORM model

**Files:**
- Create: `api/models/user_content_token.go`
- Modify: `api/models/models.go` (register in `AllModels()`)
- Test: `api/models/user_content_token_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/models/user_content_token_test.go`:

```go
package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserContentToken_AutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(&UserContentToken{})
	require.NoError(t, err)
	assert.True(t, db.Migrator().HasTable(&UserContentToken{}))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "user_id"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "provider_id"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "access_token"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "refresh_token"))
	assert.True(t, db.Migrator().HasColumn(&UserContentToken{}, "status"))
}

func TestUserContentToken_TableName(t *testing.T) {
	assert.Equal(t, tableName("user_content_tokens"), UserContentToken{}.TableName())
}

func TestUserContentToken_BeforeCreate_GeneratesUUID(t *testing.T) {
	tok := &UserContentToken{}
	err := tok.BeforeCreate(nil)
	require.NoError(t, err)
	_, err = uuid.Parse(tok.ID)
	assert.NoError(t, err)
}

func TestUserContentToken_BeforeCreate_PreservesExistingID(t *testing.T) {
	id := uuid.New().String()
	tok := &UserContentToken{ID: id, CreatedAt: time.Now()}
	err := tok.BeforeCreate(nil)
	require.NoError(t, err)
	assert.Equal(t, id, tok.ID)
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestUserContentToken_AutoMigrate`
Expected: FAIL — `UserContentToken` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/models/user_content_token.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserContentToken is a per-user OAuth token used by delegated content providers.
// access_token and refresh_token are AES-256-GCM ciphertexts (nonce prepended).
type UserContentToken struct {
	ID                   string     `gorm:"primaryKey;type:varchar(36)"`
	UserID               string     `gorm:"type:varchar(36);not null;index:idx_uct_user;uniqueIndex:uq_uct_user_provider,priority:1"`
	ProviderID           string     `gorm:"type:varchar(64);not null;uniqueIndex:uq_uct_user_provider,priority:2"`
	AccessToken          []byte     `gorm:"type:bytea;not null"`
	RefreshToken         []byte     `gorm:"type:bytea"`
	Scopes               string     `gorm:"type:text"`
	ExpiresAt            *time.Time
	Status               string     `gorm:"type:varchar(16);default:active;index:idx_uct_status_expires,priority:1"`
	LastRefreshAt        *time.Time `gorm:"index:idx_uct_status_expires,priority:2"`
	LastError            string     `gorm:"type:text"`
	ProviderAccountID    *string    `gorm:"type:varchar(255)"`
	ProviderAccountLabel *string    `gorm:"type:varchar(255)"`
	CreatedAt            time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt           time.Time  `gorm:"not null;autoUpdateTime"`

	Owner User `gorm:"foreignKey:UserID;references:InternalUUID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for UserContentToken
func (UserContentToken) TableName() string {
	return tableName("user_content_tokens")
}

// BeforeCreate generates a UUID if not set
func (u *UserContentToken) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}
```

- [ ] **Step 4: Register in `AllModels()`**

In `api/models/models.go`, find the `AllModels()` function (around line 752) and add `&UserContentToken{}` to the returned slice alongside the other models. Locate the slice via `rg "&ClientCredential\{\}" api/models/models.go -n` and add the new entry immediately after it, keeping the trailing comma.

- [ ] **Step 5: Run the tests**

Run: `make test-unit name=TestUserContentToken`
Expected: PASS.

Also run: `make test-unit name=TestAllModels_MigratesSuccessfully`
Expected: PASS (AutoMigrate with the new model succeeds).

- [ ] **Step 6: Lint + build**

Run: `make lint && make build-server`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add api/models/user_content_token.go api/models/user_content_token_test.go api/models/models.go
git commit -m "$(cat <<'EOF'
feat(api): add UserContentToken GORM model for delegated content providers (#249)

First sub-project of #249: introduces the user_content_tokens table (per-user
OAuth tokens, AES-256-GCM encrypted at rest) via GORM AutoMigrate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 1.2: AES-256-GCM encryption helper

**Files:**
- Create: `api/content_token_encryption.go`
- Test: `api/content_token_encryption_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/content_token_encryption_test.go`:

```go
package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentTokenEncryption_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	enc, err := NewContentTokenEncryptor(hex.EncodeToString(key))
	require.NoError(t, err)

	plaintext := []byte("ya29.a0AfH6SMDe-example-access-token")
	ct, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ct)

	pt, err := enc.Decrypt(ct)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(plaintext, pt))
}

func TestContentTokenEncryption_UniqueNonces(t *testing.T) {
	enc := mustNewTestEncryptor(t)
	pt := []byte("same-plaintext")
	ct1, err := enc.Encrypt(pt)
	require.NoError(t, err)
	ct2, err := enc.Encrypt(pt)
	require.NoError(t, err)
	assert.False(t, bytes.Equal(ct1, ct2), "identical plaintext must produce different ciphertext")
}

func TestContentTokenEncryption_RejectsShortKey(t *testing.T) {
	_, err := NewContentTokenEncryptor("deadbeef")
	assert.Error(t, err)
}

func TestContentTokenEncryption_RejectsInvalidHex(t *testing.T) {
	invalidHex := "ZZ" + strings.Repeat("00", 31)
	_, err := NewContentTokenEncryptor(invalidHex)
	assert.Error(t, err)
}

func TestContentTokenEncryption_TamperedCiphertextFailsDecrypt(t *testing.T) {
	enc := mustNewTestEncryptor(t)
	ct, err := enc.Encrypt([]byte("payload"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xFF
	_, err = enc.Decrypt(ct)
	assert.Error(t, err)
}

func mustNewTestEncryptor(t *testing.T) *ContentTokenEncryptor {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	enc, err := NewContentTokenEncryptor(hex.EncodeToString(key))
	require.NoError(t, err)
	return enc
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestContentTokenEncryption_RoundTrip`
Expected: FAIL — `ContentTokenEncryptor` / `NewContentTokenEncryptor` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/content_token_encryption.go`:

```go
package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const contentTokenKeyLen = 32 // AES-256

// ContentTokenEncryptor performs AES-256-GCM encryption for per-user content
// OAuth tokens. The nonce is prepended to the ciphertext.
type ContentTokenEncryptor struct {
	aead cipher.AEAD
}

// NewContentTokenEncryptor constructs an encryptor from a hex-encoded 32-byte key.
func NewContentTokenEncryptor(hexKey string) (*ContentTokenEncryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("content token encryption key is not valid hex: %w", err)
	}
	if len(key) != contentTokenKeyLen {
		return nil, fmt.Errorf("content token encryption key must be %d bytes (got %d)", contentTokenKeyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &ContentTokenEncryptor{aead: aead}, nil
}

// Encrypt returns nonce || ciphertext.
func (e *ContentTokenEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt parses nonce || ciphertext and returns the plaintext.
func (e *ContentTokenEncryptor) Decrypt(nonceAndCiphertext []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(nonceAndCiphertext) < ns {
		return nil, errors.New("content token ciphertext too short")
	}
	nonce := nonceAndCiphertext[:ns]
	ciphertext := nonceAndCiphertext[ns:]
	return e.aead.Open(nil, nonce, ciphertext, nil)
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestContentTokenEncryption`
Expected: PASS.

- [ ] **Step 5: Lint + build**

Run: `make lint && make build-server`

- [ ] **Step 6: Commit**

```bash
git add api/content_token_encryption.go api/content_token_encryption_test.go
git commit -m "feat(api): add AES-256-GCM encryptor for content OAuth tokens (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 1.3: `ContentToken` domain type + typed errors

**Files:**
- Create: `api/content_token_repository.go` (domain type + interface + typed errors stubbed)
- Test: `api/content_token_repository_test.go` (errors only)

- [ ] **Step 1: Write the failing test**

Create `api/content_token_repository_test.go` with error-sentinel assertions:

```go
package api

import (
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
)

func TestContentTokenErrors_WrapDBErrors(t *testing.T) {
	assert.True(t, errors.Is(ErrContentTokenNotFound, dberrors.ErrNotFound))
	assert.True(t, errors.Is(ErrContentTokenDuplicate, dberrors.ErrDuplicate))
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestContentTokenErrors_WrapDBErrors`
Expected: FAIL — errors undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/content_token_repository.go`:

```go
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
)

// ContentToken is the domain representation of a per-user OAuth token used
// by delegated content providers. Access/refresh tokens are plaintext here;
// the repository handles encryption at rest.
type ContentToken struct {
	ID                   string
	UserID               string
	ProviderID           string
	AccessToken          string
	RefreshToken         string
	Scopes               string
	ExpiresAt            *time.Time
	Status               string // ContentTokenStatus*
	LastRefreshAt        *time.Time
	LastError            string
	ProviderAccountID    string
	ProviderAccountLabel string
	CreatedAt            time.Time
	ModifiedAt           time.Time
}

const (
	ContentTokenStatusActive         = "active"
	ContentTokenStatusFailedRefresh  = "failed_refresh"
)

// Typed errors for content-token repository operations.
var (
	ErrContentTokenNotFound  = fmt.Errorf("content token: %w", dberrors.ErrNotFound)
	ErrContentTokenDuplicate = fmt.Errorf("content token: %w", dberrors.ErrDuplicate)
)

// ContentTokenRepository is the repository abstraction over user_content_tokens.
// All methods return typed errors from internal/dberrors.
type ContentTokenRepository interface {
	GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)
	ListByUser(ctx context.Context, userID string) ([]ContentToken, error)
	Upsert(ctx context.Context, token *ContentToken) error
	UpdateStatus(ctx context.Context, id, status, lastError string) error
	Delete(ctx context.Context, id string) error
	DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)
	// RefreshWithLock opens a transaction, SELECT ... FOR UPDATE on the row,
	// invokes fn with the current decrypted token, and persists the returned
	// token. Returns the updated token or the fn error.
	RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error)
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestContentTokenErrors`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/content_token_repository.go api/content_token_repository_test.go
git commit -m "feat(api): add ContentToken domain type and repository interface (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 1.4: GORM implementation of `ContentTokenRepository`

**Files:**
- Modify: `api/content_token_repository.go` (append `GormContentTokenRepository`)
- Test: `api/content_token_repository_test.go` (append GORM-backed unit tests)

- [ ] **Step 1: Write the failing tests**

Follow the pattern in `api/group_repository.go` and its test file. Append to `api/content_token_repository_test.go`:

```go
// Helpers

func newTestContentTokenRepo(t *testing.T) (ContentTokenRepository, *ContentTokenEncryptor, *gorm.DB) {
	t.Helper()
	db := newInMemoryTestDB(t) // existing helper in the package; see api/repository_store_gorm.go tests
	enc := mustNewTestEncryptor(t)
	return NewGormContentTokenRepository(db, enc), enc, db
}

func TestContentTokenRepo_UpsertThenGet(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()

	tok := &ContentToken{
		UserID: "user-1", ProviderID: "mock",
		AccessToken: "at-1", RefreshToken: "rt-1",
		Scopes: "read", Status: ContentTokenStatusActive,
	}
	require.NoError(t, repo.Upsert(ctx, tok))

	got, err := repo.GetByUserAndProvider(ctx, "user-1", "mock")
	require.NoError(t, err)
	assert.Equal(t, "at-1", got.AccessToken)
	assert.Equal(t, "rt-1", got.RefreshToken)
	assert.Equal(t, ContentTokenStatusActive, got.Status)
}

func TestContentTokenRepo_Get_NotFound(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	_, err := repo.GetByUserAndProvider(context.Background(), "missing", "mock")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_Upsert_IsIdempotent(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "v1", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))
	tok.AccessToken = "v2"
	require.NoError(t, repo.Upsert(ctx, tok))
	got, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	assert.Equal(t, "v2", got.AccessToken)
}

func TestContentTokenRepo_ListByUser(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "a", AccessToken: "x", Status: "active"}))
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "b", AccessToken: "y", Status: "active"}))
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u2", ProviderID: "a", AccessToken: "z", Status: "active"}))

	list, err := repo.ListByUser(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestContentTokenRepo_DeleteByUserAndProvider_ReturnsRowThenGone(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "x", Status: "active"}))

	deleted, err := repo.DeleteByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	require.NotNil(t, deleted)
	assert.Equal(t, "x", deleted.AccessToken)

	_, err = repo.GetByUserAndProvider(ctx, "u1", "p")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_DeleteByUserAndProvider_NotFoundError(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	_, err := repo.DeleteByUserAndProvider(context.Background(), "nope", "nope")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_UpdateStatus(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "x", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))

	// Fetch to get the generated ID
	fetched, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)

	require.NoError(t, repo.UpdateStatus(ctx, fetched.ID, ContentTokenStatusFailedRefresh, "invalid_grant"))
	got, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	assert.Equal(t, ContentTokenStatusFailedRefresh, got.Status)
	assert.Equal(t, "invalid_grant", got.LastError)
}

func TestContentTokenRepo_RefreshWithLock_UpdatesToken(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "old", RefreshToken: "rt", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))
	fetched, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)

	updated, err := repo.RefreshWithLock(ctx, fetched.ID, func(current *ContentToken) (*ContentToken, error) {
		current.AccessToken = "new"
		now := time.Now().Add(3600 * time.Second)
		current.ExpiresAt = &now
		return current, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "new", updated.AccessToken)
	got, _ := repo.GetByUserAndProvider(ctx, "u1", "p")
	assert.Equal(t, "new", got.AccessToken)
}
```

Also add necessary imports (`errors`, `context`, `time`, `testify/assert|require`, `gorm.io/gorm`).

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestContentTokenRepo_UpsertThenGet`
Expected: FAIL — `NewGormContentTokenRepository` undefined.

- [ ] **Step 3: Write implementation**

Append to `api/content_token_repository.go`:

```go
import (
	// existing imports...
	"errors"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormContentTokenRepository persists ContentToken records via GORM.
// AccessToken and RefreshToken are encrypted at rest.
type GormContentTokenRepository struct {
	db  *gorm.DB
	enc *ContentTokenEncryptor
}

func NewGormContentTokenRepository(db *gorm.DB, enc *ContentTokenEncryptor) *GormContentTokenRepository {
	return &GormContentTokenRepository{db: db, enc: enc}
}

func (r *GormContentTokenRepository) GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	var row models.UserContentToken
	err := r.db.WithContext(ctx).Where("user_id = ? AND provider_id = ?", userID, providerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContentTokenNotFound
	}
	if err != nil {
		return nil, dberrors.Classify(err)
	}
	return r.decode(&row)
}

func (r *GormContentTokenRepository) ListByUser(ctx context.Context, userID string) ([]ContentToken, error) {
	var rows []models.UserContentToken
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	out := make([]ContentToken, 0, len(rows))
	for i := range rows {
		t, err := r.decode(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, nil
}

func (r *GormContentTokenRepository) Upsert(ctx context.Context, token *ContentToken) error {
	row, err := r.encode(token)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "provider_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"access_token", "refresh_token", "scopes", "expires_at", "status", "last_refresh_at", "last_error", "provider_account_id", "provider_account_label", "modified_at"}),
	}).Create(row)
	if res.Error != nil {
		return dberrors.Classify(res.Error)
	}
	token.ID = row.ID
	return nil
}

func (r *GormContentTokenRepository) UpdateStatus(ctx context.Context, id, status, lastError string) error {
	res := r.db.WithContext(ctx).Model(&models.UserContentToken{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "last_error": lastError})
	if res.Error != nil {
		return dberrors.Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContentTokenNotFound
	}
	return nil
}

func (r *GormContentTokenRepository) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Delete(&models.UserContentToken{ID: id})
	if res.Error != nil {
		return dberrors.Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContentTokenNotFound
	}
	return nil
}

func (r *GormContentTokenRepository) DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	var deleted *ContentToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.UserContentToken
		if err := tx.Where("user_id = ? AND provider_id = ?", userID, providerID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentTokenNotFound
			}
			return dberrors.Classify(err)
		}
		dec, err := r.decode(&row)
		if err != nil {
			return err
		}
		deleted = dec
		if err := tx.Delete(&models.UserContentToken{ID: row.ID}).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

func (r *GormContentTokenRepository) RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
	var updated *ContentToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.UserContentToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentTokenNotFound
			}
			return dberrors.Classify(err)
		}
		current, err := r.decode(&row)
		if err != nil {
			return err
		}
		next, err := fn(current)
		if err != nil {
			return err
		}
		nextRow, err := r.encode(next)
		if err != nil {
			return err
		}
		nextRow.ID = row.ID
		if err := tx.Save(nextRow).Error; err != nil {
			return dberrors.Classify(err)
		}
		decoded, err := r.decode(nextRow)
		if err != nil {
			return err
		}
		updated = decoded
		return nil
	})
	return updated, err
}

func (r *GormContentTokenRepository) encode(t *ContentToken) (*models.UserContentToken, error) {
	atCipher, err := r.enc.Encrypt([]byte(t.AccessToken))
	if err != nil {
		return nil, err
	}
	var rtCipher []byte
	if t.RefreshToken != "" {
		rtCipher, err = r.enc.Encrypt([]byte(t.RefreshToken))
		if err != nil {
			return nil, err
		}
	}
	var accountID, accountLabel *string
	if t.ProviderAccountID != "" {
		v := t.ProviderAccountID
		accountID = &v
	}
	if t.ProviderAccountLabel != "" {
		v := t.ProviderAccountLabel
		accountLabel = &v
	}
	status := t.Status
	if status == "" {
		status = ContentTokenStatusActive
	}
	return &models.UserContentToken{
		ID:                   t.ID,
		UserID:               t.UserID,
		ProviderID:           t.ProviderID,
		AccessToken:          atCipher,
		RefreshToken:         rtCipher,
		Scopes:               t.Scopes,
		ExpiresAt:            t.ExpiresAt,
		Status:               status,
		LastRefreshAt:        t.LastRefreshAt,
		LastError:            t.LastError,
		ProviderAccountID:    accountID,
		ProviderAccountLabel: accountLabel,
	}, nil
}

func (r *GormContentTokenRepository) decode(row *models.UserContentToken) (*ContentToken, error) {
	at, err := r.enc.Decrypt(row.AccessToken)
	if err != nil {
		return nil, err
	}
	var rt []byte
	if len(row.RefreshToken) > 0 {
		rt, err = r.enc.Decrypt(row.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	out := &ContentToken{
		ID:            row.ID,
		UserID:        row.UserID,
		ProviderID:    row.ProviderID,
		AccessToken:   string(at),
		RefreshToken:  string(rt),
		Scopes:        row.Scopes,
		ExpiresAt:     row.ExpiresAt,
		Status:        row.Status,
		LastRefreshAt: row.LastRefreshAt,
		LastError:     row.LastError,
		CreatedAt:     row.CreatedAt,
		ModifiedAt:    row.ModifiedAt,
	}
	if row.ProviderAccountID != nil {
		out.ProviderAccountID = *row.ProviderAccountID
	}
	if row.ProviderAccountLabel != nil {
		out.ProviderAccountLabel = *row.ProviderAccountLabel
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestContentTokenRepo`
Expected: All PASS.

- [ ] **Step 5: Integration test against PostgreSQL**

Run: `make test-integration name=TestContentTokenRepo` to ensure GORM `OnConflict` and `SELECT ... FOR UPDATE` work against PostgreSQL (SQLite tolerates different SQL).
Expected: PASS. If any test fails (likely the concurrent-refresh style), the subagent should investigate the failure — do not mark the task complete until all pass.

- [ ] **Step 6: Lint + commit**

```bash
git add api/content_token_repository.go api/content_token_repository_test.go
git commit -m "feat(api): add GORM ContentTokenRepository with encrypted storage and FOR UPDATE refresh (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

# Phase 2 — Config + Provider Registry

## Task 2.1: `ContentOAuthConfig` + `ContentOAuthProviderConfig` structs

**Files:**
- Create: `internal/config/content_oauth.go`
- Test: `internal/config/content_oauth_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/config/content_oauth_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestContentOAuthConfig_YAML_Decode(t *testing.T) {
	raw := `
callback_url: "http://localhost:8080/oauth2/content_callback"
allowed_client_callbacks:
  - "http://localhost:8079/"
  - "http://localhost:4200/*"
providers:
  confluence:
    enabled: true
    client_id: "cid"
    client_secret: "sec"
    auth_url: "https://auth.example.com/authorize"
    token_url: "https://auth.example.com/token"
    userinfo_url: "https://api.example.com/me"
    revocation_url: "https://auth.example.com/revoke"
    required_scopes: ["read:a", "read:b"]
`
	var c ContentOAuthConfig
	require.NoError(t, yaml.Unmarshal([]byte(raw), &c))
	assert.Equal(t, "http://localhost:8080/oauth2/content_callback", c.CallbackURL)
	assert.Len(t, c.AllowedClientCallbacks, 2)
	p := c.Providers["confluence"]
	assert.True(t, p.Enabled)
	assert.Equal(t, []string{"read:a", "read:b"}, p.RequiredScopes)
}

func TestContentOAuthConfig_Validate_RequiresKeyWhenEnabled(t *testing.T) {
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"confluence": {Enabled: true, ClientID: "c", ClientSecret: "s",
				AuthURL: "https://a", TokenURL: "https://t", RequiredScopes: []string{"read"}},
		},
	}
	err := c.Validate("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TMI_CONTENT_TOKEN_ENCRYPTION_KEY")
}

func TestContentOAuthConfig_Validate_AcceptsKeyWhenEnabled(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"confluence": {Enabled: true, ClientID: "c", ClientSecret: "s",
				AuthURL: "https://a", TokenURL: "https://t", RequiredScopes: []string{"read"}},
		},
	}
	assert.NoError(t, c.Validate(key))
}

func TestContentOAuthConfig_Validate_DisabledIsFine(t *testing.T) {
	c := ContentOAuthConfig{Providers: map[string]ContentOAuthProviderConfig{
		"confluence": {Enabled: false},
	}}
	assert.NoError(t, c.Validate(""))
}

func TestContentOAuthProvider_RequiresAuthAndTokenURLs(t *testing.T) {
	c := ContentOAuthConfig{
		Providers: map[string]ContentOAuthProviderConfig{
			"bad": {Enabled: true, ClientID: "c"},
		},
	}
	err := c.Validate("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.Error(t, err)
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestContentOAuthConfig_YAML_Decode`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/config/content_oauth.go`:

```go
package config

import (
	"fmt"
)

// ContentOAuthConfig holds configuration for delegated content OAuth providers.
type ContentOAuthConfig struct {
	CallbackURL            string                               `yaml:"callback_url" env:"TMI_CONTENT_OAUTH_CALLBACK_URL"`
	AllowedClientCallbacks []string                             `yaml:"allowed_client_callbacks"`
	Providers              map[string]ContentOAuthProviderConfig `yaml:"providers"`
}

// ContentOAuthProviderConfig is one entry under content_oauth.providers.*
type ContentOAuthProviderConfig struct {
	Enabled        bool     `yaml:"enabled"`
	ClientID       string   `yaml:"client_id"`
	ClientSecret   string   `yaml:"client_secret"`
	AuthURL        string   `yaml:"auth_url"`
	TokenURL       string   `yaml:"token_url"`
	UserinfoURL    string   `yaml:"userinfo_url"`
	RevocationURL  string   `yaml:"revocation_url"`
	RequiredScopes []string `yaml:"required_scopes"`
}

// Validate returns an error if any enabled provider is missing required fields,
// or if at least one provider is enabled but the encryption key is empty/invalid.
func (c *ContentOAuthConfig) Validate(encryptionKey string) error {
	anyEnabled := false
	for id, p := range c.Providers {
		if !p.Enabled {
			continue
		}
		anyEnabled = true
		if p.ClientID == "" {
			return fmt.Errorf("content_oauth.providers.%s: client_id is required when enabled", id)
		}
		if p.AuthURL == "" {
			return fmt.Errorf("content_oauth.providers.%s: auth_url is required when enabled", id)
		}
		if p.TokenURL == "" {
			return fmt.Errorf("content_oauth.providers.%s: token_url is required when enabled", id)
		}
	}
	if anyEnabled && encryptionKey == "" {
		return fmt.Errorf("at least one content OAuth provider is enabled but TMI_CONTENT_TOKEN_ENCRYPTION_KEY is not set")
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestContentOAuthConfig`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/content_oauth.go internal/config/content_oauth_test.go
git commit -m "feat(config): add ContentOAuthConfig for delegated content providers (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2.2: Wire `ContentOAuth` into top-level `Config` + env overrides + startup validation

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write failing test** — append to `internal/config/config_test.go`:

```go
func TestConfig_ContentOAuth_EnvOverride_DiscoversProviders(t *testing.T) {
	t.Setenv("TMI_CONTENT_OAUTH_CALLBACK_URL", "http://localhost:8080/cc")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_ENABLED", "true")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_ID", "cid")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_SECRET", "sec")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_AUTH_URL", "http://a")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_TOKEN_URL", "http://t")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_REQUIRED_SCOPES", "read write")
	t.Setenv("TMI_CONTENT_TOKEN_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	cfg, err := Load("")
	require.NoError(t, err)
	require.NotNil(t, cfg.ContentOAuth.Providers["mock"])
	m := cfg.ContentOAuth.Providers["mock"]
	assert.True(t, m.Enabled)
	assert.Equal(t, "cid", m.ClientID)
	assert.Equal(t, []string{"read", "write"}, m.RequiredScopes)
}

func TestConfig_ContentOAuth_Validation_FailsWithoutKey(t *testing.T) {
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_ENABLED", "true")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_CLIENT_ID", "c")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_AUTH_URL", "http://a")
	t.Setenv("TMI_CONTENT_OAUTH_PROVIDERS_MOCK_TOKEN_URL", "http://t")
	// NOTE: TMI_CONTENT_TOKEN_ENCRYPTION_KEY intentionally unset
	_, err := Load("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TMI_CONTENT_TOKEN_ENCRYPTION_KEY")
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestConfig_ContentOAuth_EnvOverride_DiscoversProviders`
Expected: FAIL.

- [ ] **Step 3: Implement changes in `internal/config/config.go`**

1. Add `ContentOAuth ContentOAuthConfig` to the `Config` struct (near the existing `OAuth OAuthConfig` field).
2. Add `ContentTokenEncryptionKey string` to `Config` (or to a nested security struct, matching how other encryption keys are stored). Read from `TMI_CONTENT_TOKEN_ENCRYPTION_KEY`.
3. In `overrideFromEnvironment` (the existing reflection-based helper that dispatches by parent struct name), add a case for `ContentOAuthConfig` that mirrors the `OAuthConfig` branch but calls a new `overrideContentOAuthProviders(field)` function.
4. Implement `overrideContentOAuthProviders` by copying the body of `overrideOAuthProviders` (study it at line 488) and adjusting:
   - Env prefix: `CONTENT_OAUTH_PROVIDERS_`
   - Provider fields correspond to `ContentOAuthProviderConfig`
5. In the main `Validate` method (locate via `rg "func .*Config.*Validate" internal/config/config.go`), add `if err := c.ContentOAuth.Validate(c.ContentTokenEncryptionKey); err != nil { return err }`.

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestConfig_ContentOAuth`
Expected: PASS.

Also run: `make test-unit` to ensure no existing config tests regressed.

- [ ] **Step 5: Lint + build**

Run: `make lint && make build-server`

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): wire content_oauth into Config with env overrides and startup validation (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2.3: `ContentOAuthProvider` interface + `BaseContentOAuthProvider`

**Files:**
- Create: `api/content_oauth_provider.go`
- Test: `api/content_oauth_provider_test.go`

**Design note:** `BaseContentOAuthProvider` wraps `golang.org/x/oauth2.Config` and provides `ExchangeCode`, `Refresh`, `Revoke`, `AuthorizationURL(state, pkceChallenge, redirectURI)`, and `FetchAccountInfo(accessToken)`. It does NOT implement `auth.Provider` (which is specifically for user-authentication OAuth).

- [ ] **Step 1: Write the failing test**

In `api/content_oauth_provider_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentOAuthProvider_AuthorizationURL(t *testing.T) {
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", AuthURL: "https://auth.example/authorize",
		TokenURL: "https://auth.example/token",
		RequiredScopes: []string{"read:a", "read:b"},
	})
	u := p.AuthorizationURL("state-123", "challenge-xyz", "https://tmi/cb")
	assert.Contains(t, u, "client_id=cid")
	assert.Contains(t, u, "state=state-123")
	assert.Contains(t, u, "code_challenge=challenge-xyz")
	assert.Contains(t, u, "code_challenge_method=S256")
	assert.Contains(t, u, "scope=read%3Aa+read%3Ab")
	assert.Contains(t, u, "redirect_uri=https%3A%2F%2Ftmi%2Fcb")
	assert.Contains(t, u, "response_type=code")
}

func TestContentOAuthProvider_ExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "code-1", r.FormValue("code"))
		assert.Equal(t, "pkce-v", r.FormValue("code_verifier"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "read:a",
		})
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token",
	})
	tok, err := p.ExchangeCode(context.Background(), "code-1", "pkce-v", srv.URL+"/cb")
	require.NoError(t, err)
	assert.Equal(t, "at-1", tok.AccessToken)
	assert.Equal(t, "rt-1", tok.RefreshToken)
	assert.Equal(t, 3600, tok.ExpiresIn)
}

func TestContentOAuthProvider_Refresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
		assert.Equal(t, "rt-old", r.FormValue("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-new",
			"refresh_token": "rt-new",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", TokenURL: srv.URL,
	})
	tok, err := p.Refresh(context.Background(), "rt-old")
	require.NoError(t, err)
	assert.Equal(t, "at-new", tok.AccessToken)
	assert.Equal(t, "rt-new", tok.RefreshToken)
}

func TestContentOAuthProvider_Refresh_InvalidGrantIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", TokenURL: srv.URL,
	})
	_, err := p.Refresh(context.Background(), "bad")
	require.Error(t, err)
	assert.True(t, IsContentOAuthPermanentFailure(err))
}

func TestContentOAuthProvider_Revoke_Succeeds(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "tok-1", r.FormValue("token"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", RevocationURL: srv.URL,
	})
	err := p.Revoke(context.Background(), "tok-1")
	require.NoError(t, err)
	assert.True(t, called)
}

func TestContentOAuthProvider_Revoke_NoURLIsNoop(t *testing.T) {
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{})
	assert.NoError(t, p.Revoke(context.Background(), "tok-1"))
}
```

- [ ] **Step 2: Verify test fails**

Run: `make test-unit name=TestContentOAuthProvider_AuthorizationURL`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `api/content_oauth_provider.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentOAuthProvider is the interface each delegated content provider implements.
type ContentOAuthProvider interface {
	ID() string
	AuthorizationURL(state, pkceChallenge, redirectURI string) string
	ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error)
	Revoke(ctx context.Context, token string) error
	RequiredScopes() []string
	// FetchAccountInfo is provider-specific; if UserinfoURL is configured, it
	// returns the external account id + label. Returns empty values if unavailable.
	FetchAccountInfo(ctx context.Context, accessToken string) (accountID, label string, err error)
}

// ContentOAuthTokenResponse is the token payload returned by exchange/refresh.
type ContentOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// ExpiresAt returns the computed absolute expiry time, or nil if ExpiresIn is zero.
func (r *ContentOAuthTokenResponse) ExpiresAt() *time.Time {
	if r.ExpiresIn <= 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	return &t
}

// errContentOAuthPermanent marks a failure as non-retryable (e.g., invalid_grant).
type errContentOAuthPermanent struct{ msg string }

func (e *errContentOAuthPermanent) Error() string { return e.msg }

func IsContentOAuthPermanentFailure(err error) bool {
	var e *errContentOAuthPermanent
	return errors.As(err, &e)
}

// BaseContentOAuthProvider is the default implementation; providers with
// provider-specific userinfo / scope semantics can wrap it.
type BaseContentOAuthProvider struct {
	id         string
	cfg        config.ContentOAuthProviderConfig
	httpClient *http.Client
}

func NewBaseContentOAuthProvider(id string, cfg config.ContentOAuthProviderConfig) *BaseContentOAuthProvider {
	return &BaseContentOAuthProvider{id: id, cfg: cfg, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (p *BaseContentOAuthProvider) ID() string               { return p.id }
func (p *BaseContentOAuthProvider) RequiredScopes() []string { return p.cfg.RequiredScopes }

func (p *BaseContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge)
	q.Set("code_challenge_method", "S256")
	if len(p.cfg.RequiredScopes) > 0 {
		q.Set("scope", strings.Join(p.cfg.RequiredScopes, " "))
	}
	sep := "?"
	if strings.Contains(p.cfg.AuthURL, "?") {
		sep = "&"
	}
	return p.cfg.AuthURL + sep + q.Encode()
}

func (p *BaseContentOAuthProvider) ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code_verifier", pkceVerifier)
	return p.postToken(ctx, form, false)
}

func (p *BaseContentOAuthProvider) Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	return p.postToken(ctx, form, true)
}

func (p *BaseContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	if p.cfg.RevocationURL == "" {
		slogging.Get().Info("content_oauth provider %q has no revocation_url; skipping provider-side revoke", p.id)
		return nil
	}
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.RevocationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("content_oauth revoke failed: status=%d body=%s", resp.StatusCode, string(body))
}

func (p *BaseContentOAuthProvider) FetchAccountInfo(ctx context.Context, accessToken string) (string, string, error) {
	if p.cfg.UserinfoURL == "" {
		return "", "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserinfoURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	return stringField(payload, "sub", "id", "account_id"),
		stringField(payload, "email", "username", "name"),
		nil
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (p *BaseContentOAuthProvider) postToken(ctx context.Context, form url.Values, isRefresh bool) (*ContentOAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		msg := fmt.Sprintf("content_oauth token call failed: status=%d body=%s", resp.StatusCode, string(body))
		if isRefresh {
			// Refresh 4xx errors are treated as permanent (token revoked or invalid).
			return nil, &errContentOAuthPermanent{msg: msg}
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("content_oauth token call returned 5xx: status=%d body=%s", resp.StatusCode, string(body))
	}
	var out ContentOAuthTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("content_oauth token response decode: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("content_oauth token response missing access_token")
	}
	return &out, nil
}
```

- [ ] **Step 4: Run tests + commit**

```bash
make test-unit name=TestContentOAuthProvider && \
make lint && make build-server && \
git add api/content_oauth_provider.go api/content_oauth_provider_test.go && \
git commit -m "feat(api): add BaseContentOAuthProvider for delegated content OAuth (#249)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2.4: `ContentOAuthProviderRegistry`

**Files:**
- Create: `api/content_oauth_registry.go`
- Test: `api/content_oauth_registry_test.go`

- [ ] **Step 1: Write failing test**

```go
package api

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestContentOAuthRegistry_RegisterAndLookup(t *testing.T) {
	r := NewContentOAuthProviderRegistry()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{})
	r.Register(p)
	got, ok := r.Get("mock")
	assert.True(t, ok)
	assert.Equal(t, "mock", got.ID())
	_, ok = r.Get("missing")
	assert.False(t, ok)
}

func TestContentOAuthRegistry_LoadFromConfig_OnlyEnabled(t *testing.T) {
	cfg := config.ContentOAuthConfig{
		Providers: map[string]config.ContentOAuthProviderConfig{
			"on":  {Enabled: true, ClientID: "c", AuthURL: "http://a", TokenURL: "http://t"},
			"off": {Enabled: false},
		},
	}
	r, err := LoadContentOAuthRegistryFromConfig(cfg)
	assert.NoError(t, err)
	_, ok := r.Get("on")
	assert.True(t, ok)
	_, ok = r.Get("off")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Verify fails, implement, run, commit.**

Implementation:

```go
package api

import (
	"sync"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

type ContentOAuthProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]ContentOAuthProvider
}

func NewContentOAuthProviderRegistry() *ContentOAuthProviderRegistry {
	return &ContentOAuthProviderRegistry{providers: map[string]ContentOAuthProvider{}}
}

func (r *ContentOAuthProviderRegistry) Register(p ContentOAuthProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID()] = p
}

func (r *ContentOAuthProviderRegistry) Get(id string) (ContentOAuthProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *ContentOAuthProviderRegistry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// LoadContentOAuthRegistryFromConfig registers a BaseContentOAuthProvider for
// every enabled entry. Disabled entries are skipped.
func LoadContentOAuthRegistryFromConfig(cfg config.ContentOAuthConfig) (*ContentOAuthProviderRegistry, error) {
	r := NewContentOAuthProviderRegistry()
	for id, p := range cfg.Providers {
		if !p.Enabled {
			continue
		}
		r.Register(NewBaseContentOAuthProvider(id, p))
		slogging.Get().Info("registered content OAuth provider id=%s", id)
	}
	return r, nil
}
```

Commit message:

```
feat(api): add ContentOAuthProviderRegistry (#249)
```

---

# Phase 3 — Account-linking endpoints + OAuth callback

## Task 3.1: Redis-backed OAuth state store

**Files:**
- Create: `api/content_oauth_state.go`
- Test: `api/content_oauth_state_test.go` (use `github.com/alicebob/miniredis/v2` if available in the codebase; otherwise use an embedded miniredis server or mock through the redis client — inspect existing tests for how they spin up a Redis in unit tests: `rg "miniredis\|redis.Client" api --files-with-matches`)

- [ ] **Step 1: Write failing tests**

```go
func TestContentOAuthStateStore_PutAndConsume(t *testing.T) {
	store := newTestStateStore(t) // helper: miniredis-backed
	p := ContentOAuthStatePayload{
		UserID: "u", ProviderID: "mock", ClientCallback: "http://c",
		PKCECodeVerifier: "v", CreatedAt: time.Now(),
	}
	nonce, err := store.Put(context.Background(), p, 10*time.Minute)
	require.NoError(t, err)
	assert.Len(t, nonce, 43) // base64(32 bytes) w/o padding

	got, err := store.Consume(context.Background(), nonce)
	require.NoError(t, err)
	assert.Equal(t, "u", got.UserID)

	// Consume is single-use
	_, err = store.Consume(context.Background(), nonce)
	assert.True(t, errors.Is(err, ErrContentOAuthStateNotFound))
}

func TestContentOAuthStateStore_ExpiredEntry(t *testing.T) {
	mr, store := newTestStateStoreWithServer(t)
	nonce, err := store.Put(context.Background(), ContentOAuthStatePayload{UserID: "u"}, 1*time.Second)
	require.NoError(t, err)
	mr.FastForward(2 * time.Second)
	_, err = store.Consume(context.Background(), nonce)
	assert.True(t, errors.Is(err, ErrContentOAuthStateNotFound))
}
```

- [ ] **Step 2–4: Implement + test + commit**

```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrContentOAuthStateNotFound = errors.New("content oauth state not found or expired")

type ContentOAuthStatePayload struct {
	UserID           string    `json:"user_id"`
	ProviderID       string    `json:"provider_id"`
	ClientCallback   string    `json:"client_callback"`
	PKCECodeVerifier string    `json:"pkce_code_verifier"`
	CreatedAt        time.Time `json:"created_at"`
}

type ContentOAuthStateStore struct {
	rdb       redis.UniversalClient
	keyPrefix string
}

func NewContentOAuthStateStore(rdb redis.UniversalClient) *ContentOAuthStateStore {
	return &ContentOAuthStateStore{rdb: rdb, keyPrefix: "content_oauth_state:"}
}

func (s *ContentOAuthStateStore) Put(ctx context.Context, p ContentOAuthStatePayload, ttl time.Duration) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf[:])
	payload, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := s.rdb.Set(ctx, s.keyPrefix+nonce, payload, ttl).Err(); err != nil {
		return "", fmt.Errorf("put state: %w", err)
	}
	return nonce, nil
}

func (s *ContentOAuthStateStore) Consume(ctx context.Context, nonce string) (*ContentOAuthStatePayload, error) {
	key := s.keyPrefix + nonce
	val, err := s.rdb.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrContentOAuthStateNotFound
	}
	if err != nil {
		return nil, err
	}
	var out ContentOAuthStatePayload
	if err := json.Unmarshal(val, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

Commit: `feat(api): add Redis-backed content OAuth state store (#249)`.

---

## Task 3.2: PKCE helper

**Files:**
- Create: `api/content_oauth_pkce.go`
- Test: `api/content_oauth_pkce_test.go`

Test first. Implement:

```go
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewPKCEVerifier returns a 43-character code verifier (RFC 7636 §4.1).
func NewPKCEVerifier() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// PKCES256Challenge returns base64url(SHA256(verifier)) without padding.
func PKCES256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```

Tests verify: verifier is 43 chars, unique on each call, `PKCES256Challenge` of a known verifier produces expected output (pick a fixed input and precompute the expected output in the test).

Commit: `feat(api): add PKCE helpers for content OAuth (#249)`.

---

## Task 3.3: `client_callback` allow-list

**Files:**
- Create: `api/content_oauth_callbacks.go`
- Test: `api/content_oauth_callbacks_test.go`

Test cases:
- Exact match — accepts.
- Glob with `*` suffix — accepts matching prefix.
- Non-matching URL — rejects.
- Empty allow-list — rejects everything.

Implementation:

```go
package api

import "strings"

type ClientCallbackAllowList struct {
	patterns []string
}

func NewClientCallbackAllowList(patterns []string) *ClientCallbackAllowList {
	return &ClientCallbackAllowList{patterns: patterns}
}

func (a *ClientCallbackAllowList) Allowed(url string) bool {
	for _, p := range a.patterns {
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(url, strings.TrimSuffix(p, "*")) {
				return true
			}
		} else if p == url {
			return true
		}
	}
	return false
}
```

Commit: `feat(api): add client_callback allow-list matcher (#249)`.

---

## Task 3.4: `/me/content_tokens` list handler + `/me/content_tokens/{provider_id}` DELETE handler + `POST .../authorize`

**Files:**
- Create: `api/content_oauth_handlers.go`
- Test: `api/content_oauth_handlers_test.go`

Study `api/handlers_users.go` or similar for how handlers are written with Gin + JWT auth middleware + user context extraction. Use `GetUserFromContext` or the equivalent helper in the auth package (locate via `rg "GetUserFromContext\|c\.Get\(\"user\"" api auth`).

**Handler contract (per spec section 3):**

### `listMyContentTokens(c *gin.Context)`

- Auth: JWT middleware sets user context.
- 200: `{ "content_tokens": [ ContentTokenInfo ] }` or `[]ContentTokenInfo` per OpenAPI decision (see Phase 6).
- 401 if no user (handled by middleware upstream).

### `authorizeContentToken(c *gin.Context)`

Path: `/me/content_tokens/{provider_id}/authorize`.
Body: `{ "client_callback": "https://..." }`.
Steps:
1. Look up provider in `ContentOAuthProviderRegistry`. If missing: 422 `content_token_provider_not_configured`.
2. Validate `client_callback` against allow-list. If not allowed: 400 `client_callback_not_allowed`.
3. Generate PKCE verifier + challenge.
4. `Put` state in store, TTL 10m, get nonce.
5. Build `authorization_url` via `provider.AuthorizationURL(nonce, challenge, cfg.CallbackURL)`.
6. Return `{ authorization_url, expires_at }`.

### `deleteMyContentToken(c *gin.Context)`

Path: `/me/content_tokens/{provider_id}`.
Steps:
1. `DeleteByUserAndProvider(ctx, userID, providerID)`.
2. If `ErrContentTokenNotFound`: return 204 (idempotent).
3. Otherwise decrypt access token, call `provider.Revoke(ctx, accessToken)`. Log warn on failure, continue.
4. Return 204.

Write unit tests with mocked `ContentTokenRepository` and a stub `ContentOAuthProvider` (implements the interface, records calls).

Test cases covering: happy path for each handler; provider not registered (422); cross-user access attempt should not reach these handlers (auth middleware owns that — but add a smoke test that the handler reads `userID` from context).

Implementation skeleton (stub; full code is straightforward Gin + repository plumbing):

```go
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

type ContentOAuthHandlers struct {
	Cfg        config.ContentOAuthConfig
	Registry   *ContentOAuthProviderRegistry
	StateStore *ContentOAuthStateStore
	Tokens     ContentTokenRepository
	CallbackAllow *ClientCallbackAllowList
	// Users is needed to look up the current user's internal UUID (see server wiring task)
	UserLookup func(c *gin.Context) (userID string, ok bool)
}

type authorizeRequest struct {
	ClientCallback string `json:"client_callback"`
}

type authorizeResponse struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type contentTokenInfo struct {
	ProviderID           string     `json:"provider_id"`
	ProviderAccountID    string     `json:"provider_account_id,omitempty"`
	ProviderAccountLabel string     `json:"provider_account_label,omitempty"`
	Scopes               []string   `json:"scopes"`
	Status               string     `json:"status"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt        *time.Time `json:"last_refresh_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

func (h *ContentOAuthHandlers) List(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	toks, err := h.Tokens.ListByUser(c.Request.Context(), userID)
	if err != nil {
		slogging.Get().WithContext(c).Error("list content tokens: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	out := make([]contentTokenInfo, 0, len(toks))
	for _, t := range toks {
		out = append(out, toContentTokenInfo(t))
	}
	c.JSON(http.StatusOK, gin.H{"content_tokens": out})
}

func (h *ContentOAuthHandlers) Authorize(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	providerID := c.Param("provider_id")
	provider, ok := h.Registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "content_token_provider_not_configured", "provider_id": providerID})
		return
	}
	var req authorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ClientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_callback_required"})
		return
	}
	if !h.CallbackAllow.Allowed(req.ClientCallback) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_callback_not_allowed"})
		return
	}
	verifier, err := NewPKCEVerifier()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	payload := ContentOAuthStatePayload{
		UserID: userID, ProviderID: providerID,
		ClientCallback: req.ClientCallback, PKCECodeVerifier: verifier,
		CreatedAt: time.Now(),
	}
	ttl := 10 * time.Minute
	nonce, err := h.StateStore.Put(c.Request.Context(), payload, ttl)
	if err != nil {
		slogging.Get().WithContext(c).Error("put content oauth state: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	challenge := PKCES256Challenge(verifier)
	authURL := provider.AuthorizationURL(nonce, challenge, h.Cfg.CallbackURL)
	c.JSON(http.StatusOK, authorizeResponse{
		AuthorizationURL: authURL,
		ExpiresAt:        time.Now().Add(ttl),
	})
}

func (h *ContentOAuthHandlers) Delete(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	providerID := c.Param("provider_id")
	tok, err := h.Tokens.DeleteByUserAndProvider(c.Request.Context(), userID, providerID)
	if err != nil {
		if err == ErrContentTokenNotFound || errorIsNotFound(err) {
			c.Status(http.StatusNoContent)
			return
		}
		slogging.Get().WithContext(c).Error("delete content token: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if provider, ok := h.Registry.Get(providerID); ok {
		if revErr := provider.Revoke(c.Request.Context(), tok.AccessToken); revErr != nil {
			slogging.Get().WithContext(c).Warn("content oauth provider revoke failed: %v", revErr)
		}
	}
	c.Status(http.StatusNoContent)
}

func toContentTokenInfo(t ContentToken) contentTokenInfo {
	return contentTokenInfo{
		ProviderID:           t.ProviderID,
		ProviderAccountID:    t.ProviderAccountID,
		ProviderAccountLabel: t.ProviderAccountLabel,
		Scopes:               splitScopes(t.Scopes),
		Status:               t.Status,
		ExpiresAt:            t.ExpiresAt,
		LastRefreshAt:        t.LastRefreshAt,
		CreatedAt:            t.CreatedAt,
	}
}

func splitScopes(s string) []string {
	out := []string{}
	for _, v := range splitFields(s) {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// If strings.Fields is preferred, use it — locally define splitFields as a thin alias.
func splitFields(s string) []string { return strings.Fields(s) }

// errorIsNotFound wraps errors.Is checks for ErrContentTokenNotFound.
func errorIsNotFound(err error) bool {
	return errors.Is(err, ErrContentTokenNotFound)
}
```

(Add missing imports — `errors`, `strings`, etc. — based on the final code.)

Unit tests must use a mock `ContentTokenRepository` and a stub `ContentOAuthProvider` so they don't require Redis/Postgres.

Commit: `feat(api): add /me/content_tokens list, authorize, delete handlers (#249)`.

---

## Task 3.5: `/oauth2/content_callback` handler

**Files:**
- Modify: `api/content_oauth_handlers.go` (append callback handler)
- Test: `api/content_oauth_handlers_test.go` (append callback tests)

Write tests first (with `httptest`-based stub provider) covering:
- Happy path: valid state, successful exchange → 302 to `{client_callback}?status=success&provider_id=...`; row stored.
- Missing state: renders a minimal error page (HTML) with 400; nothing stored.
- Provider error param (`?error=access_denied`): 302 to `{client_callback}?status=error&error=access_denied&provider_id=...`; nothing stored.
- Token exchange failure at provider: 302 to `{client_callback}?status=error&error=token_exchange_failed&...`; nothing stored.

Implementation appended to `api/content_oauth_handlers.go`:

```go
func (h *ContentOAuthHandlers) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	logger := slogging.Get().WithContext(c)

	nonce := c.Query("state")
	if nonce == "" {
		renderCallbackError(c, "missing_state")
		return
	}
	state, err := h.StateStore.Consume(ctx, nonce)
	if err != nil {
		logger.Warn("content oauth callback: invalid/expired state: %v", err)
		renderCallbackError(c, "invalid_state")
		return
	}
	// Provider-reported error
	if perr := c.Query("error"); perr != "" {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", perr)
		return
	}
	code := c.Query("code")
	if code == "" {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "missing_code")
		return
	}
	provider, ok := h.Registry.Get(state.ProviderID)
	if !ok {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "provider_not_configured")
		return
	}
	tok, err := provider.ExchangeCode(ctx, code, state.PKCECodeVerifier, h.Cfg.CallbackURL)
	if err != nil {
		logger.Error("content oauth exchange: %v", err)
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "token_exchange_failed")
		return
	}
	accountID, label, _ := provider.FetchAccountInfo(ctx, tok.AccessToken)

	stored := &ContentToken{
		UserID:               state.UserID,
		ProviderID:           state.ProviderID,
		AccessToken:          tok.AccessToken,
		RefreshToken:         tok.RefreshToken,
		Scopes:               tok.Scope,
		ExpiresAt:            tok.ExpiresAt(),
		Status:               ContentTokenStatusActive,
		ProviderAccountID:    accountID,
		ProviderAccountLabel: label,
	}
	if err := h.Tokens.Upsert(ctx, stored); err != nil {
		logger.Error("content oauth upsert: %v", err)
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "persist_failed")
		return
	}
	redirectClientCallback(c, state.ClientCallback, state.ProviderID, "success", "")
}

func renderCallbackError(c *gin.Context, code string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusBadRequest, "<!doctype html><title>Content link error</title><p>An error occurred completing the content-provider link: %s.</p>", code)
}

func redirectClientCallback(c *gin.Context, cb, providerID, status, errCode string) {
	q := url.Values{}
	q.Set("status", status)
	q.Set("provider_id", providerID)
	if errCode != "" {
		q.Set("error", errCode)
	}
	sep := "?"
	if strings.Contains(cb, "?") {
		sep = "&"
	}
	c.Redirect(http.StatusFound, cb+sep+q.Encode())
}
```

Commit: `feat(api): add /oauth2/content_callback handler (#249)`.

---

## Task 3.6: Wire handlers into `api/server.go`

**Files:**
- Modify: `api/server.go`

- [ ] **Step 1: Locate the route registration function** via `rg "RegisterRoutes\|SetupRoutes" api -n`. Find the point where the other `/me/*` routes are attached and where the JWT middleware group is defined.
- [ ] **Step 2: Add instantiation**
  - Build `ContentTokenEncryptor` using `cfg.ContentTokenEncryptionKey`. If enabled set is empty, still construct a nil encryptor and skip repository/handler wiring.
  - Build `ContentTokenRepository` from the global GORM DB.
  - Build `ContentOAuthProviderRegistry` via `LoadContentOAuthRegistryFromConfig`.
  - Build `ContentOAuthStateStore` from the existing Redis client (`rg "redis.UniversalClient\|redis.NewClient" api -n`).
  - Build `ClientCallbackAllowList` from `cfg.ContentOAuth.AllowedClientCallbacks`.
- [ ] **Step 3: Register the routes**
  - `r.GET("/me/content_tokens", jwtMW, h.List)`
  - `r.POST("/me/content_tokens/:provider_id/authorize", jwtMW, h.Authorize)`
  - `r.DELETE("/me/content_tokens/:provider_id", jwtMW, h.Delete)`
  - `r.GET("/oauth2/content_callback", h.Callback)` — NO auth middleware (public).
- [ ] **Step 4: Compile — fix any missing imports.**
- [ ] **Step 5: Run `make test-unit` and `make test-integration` to ensure no regressions.**
- [ ] **Step 6: Commit.**

Commit: `feat(api): register /me/content_tokens and /oauth2/content_callback routes (#249)`.

---

# Phase 4 — `DelegatedSource` helper + `MockDelegatedSource` + stub provider + integration tests

## Task 4.1: `DelegatedSource` helper

**Files:**
- Create: `api/content_source_delegated.go`
- Test: `api/content_source_delegated_test.go`

See spec section "DelegatedSource helper" for the full contract. Minimal API:

```go
package api

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ErrAuthRequired indicates the caller has no valid token for this provider.
var ErrAuthRequired = errors.New("delegated source: authentication required")

// ErrTransient indicates a temporary failure (5xx / network) during refresh.
var ErrTransient = errors.New("delegated source: transient refresh failure")

// DelegatedSourceDoFetch is the callback concrete delegated sources implement.
type DelegatedSourceDoFetch func(ctx context.Context, accessToken, uri string) (data []byte, contentType string, err error)

// DelegatedSource composes repository + provider with a lazy-refresh policy.
// Concrete sources embed this and invoke FetchForUser from their ContentSource
// Fetch method (which MUST have a user_id available in the ctx).
type DelegatedSource struct {
	ProviderID string
	Tokens     ContentTokenRepository
	Registry   *ContentOAuthProviderRegistry
	DoFetch    DelegatedSourceDoFetch
	Skew       time.Duration // e.g. 30 seconds
}

// FetchForUser runs the full lookup → optional refresh → DoFetch pipeline.
func (d *DelegatedSource) FetchForUser(ctx context.Context, userID, uri string) ([]byte, string, error) {
	logger := slogging.Get()
	tok, err := d.Tokens.GetByUserAndProvider(ctx, userID, d.ProviderID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			return nil, "", ErrAuthRequired
		}
		return nil, "", err
	}
	if tok.Status == ContentTokenStatusFailedRefresh {
		return nil, "", ErrAuthRequired
	}
	if d.expired(tok) {
		tok, err = d.refresh(ctx, tok.ID)
		if err != nil {
			return nil, "", err
		}
	}
	data, ct, err := d.DoFetch(ctx, tok.AccessToken, uri)
	if err != nil {
		logger.Warn("delegated source %s DoFetch error: %v", d.ProviderID, err)
	}
	return data, ct, err
}

func (d *DelegatedSource) expired(t *ContentToken) bool {
	if t.ExpiresAt == nil {
		return false
	}
	skew := d.Skew
	if skew == 0 {
		skew = 30 * time.Second
	}
	return time.Now().Add(skew).After(*t.ExpiresAt)
}

func (d *DelegatedSource) refresh(ctx context.Context, tokenID string) (*ContentToken, error) {
	provider, ok := d.Registry.Get(d.ProviderID)
	if !ok {
		return nil, ErrAuthRequired
	}
	return d.Tokens.RefreshWithLock(ctx, tokenID, func(current *ContentToken) (*ContentToken, error) {
		// Re-check expiry inside the lock — another goroutine may have refreshed.
		if current.ExpiresAt != nil && time.Now().Before(*current.ExpiresAt) {
			return current, nil
		}
		if current.RefreshToken == "" {
			markFailed := &ContentToken{ID: current.ID, Status: ContentTokenStatusFailedRefresh, LastError: "no refresh token"}
			_ = d.Tokens.UpdateStatus(ctx, current.ID, ContentTokenStatusFailedRefresh, "no refresh token")
			_ = markFailed // for clarity; not stored via RefreshWithLock
			return nil, ErrAuthRequired
		}
		resp, err := provider.Refresh(ctx, current.RefreshToken)
		if err != nil {
			if IsContentOAuthPermanentFailure(err) {
				current.Status = ContentTokenStatusFailedRefresh
				current.LastError = truncate(err.Error(), 1024)
				return current, ErrAuthRequired
			}
			return nil, ErrTransient
		}
		current.AccessToken = resp.AccessToken
		if resp.RefreshToken != "" {
			current.RefreshToken = resp.RefreshToken
		}
		current.Scopes = resp.Scope
		current.ExpiresAt = resp.ExpiresAt()
		now := time.Now()
		current.LastRefreshAt = &now
		current.LastError = ""
		current.Status = ContentTokenStatusActive
		return current, nil
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
```

Unit tests (fully mocked):
- Token missing → `ErrAuthRequired`.
- Token present + valid → `DoFetch` called with plaintext `accessToken`.
- Token expired + refresh succeeds → `DoFetch` called with *new* token.
- Token expired + refresh 400-class → token flipped to `failed_refresh`, returns `ErrAuthRequired`.
- Token expired + refresh 5xx → returns `ErrTransient`.
- Token already marked `failed_refresh` → `ErrAuthRequired` without any provider call.
- Concurrent FetchForUser calls both expired → only one provider.Refresh call happens (use a mock provider with a call counter and a tight goroutine + channel test).

Commit: `feat(api): add DelegatedSource with lazy refresh and FOR UPDATE serialization (#249)`.

---

## Task 4.2: Stub OAuth provider HTTP test harness

**Files:**
- Create: `api/testhelpers/stub_oauth_provider.go` (build-tagged `//go:build dev || test`)

Speaks minimum OAuth 2.0 for tests:

- `/authorize` — 302 redirect to configured redirect URI with `code=fixed&state={state}` (skips end-user consent).
- `/token` — handles `grant_type=authorization_code` (verifies PKCE) and `grant_type=refresh_token`. Response JSON populated from test-configurable struct.
- `/revoke` — records revocation calls.
- `/userinfo` — returns fake account info.

Knobs (fields on `StubOAuthProvider`):
- `AccessTokenLifetime time.Duration`
- `RefreshSucceeds bool`, `RotateRefreshToken bool`, `RefreshStatus int` (override for forcing 400)
- `RevokeSucceeds bool`
- `RevocationCalls int` (atomic counter, exposed via accessor)
- `RefreshCalls int` (atomic counter)

Written idiomatically — one `.go` file, ~150 lines. Tested implicitly through Task 4.3 and 4.4.

Commit: `test(api): add stub OAuth provider harness for content-source tests (#249)`.

---

## Task 4.3: `MockDelegatedSource`

**Files:**
- Create: `api/content_source_mock_delegated.go` (build tag `//go:build dev || test`)
- Test: `api/content_source_mock_delegated_test.go` (same build tag)

```go
//go:build dev || test

package api

import (
	"context"
	"fmt"
	"strings"
)

// MockDelegatedSource is a test-only delegated content source. It handles
// URIs of the form "mock://doc/{id}" and returns canned bytes.
type MockDelegatedSource struct {
	*DelegatedSource
	Contents map[string][]byte // id → bytes
}

func NewMockDelegatedSource(tokens ContentTokenRepository, registry *ContentOAuthProviderRegistry) *MockDelegatedSource {
	m := &MockDelegatedSource{
		Contents: map[string][]byte{},
	}
	m.DelegatedSource = &DelegatedSource{
		ProviderID: "mock",
		Tokens:     tokens,
		Registry:   registry,
		DoFetch:    m.doFetch,
	}
	return m
}

func (m *MockDelegatedSource) Name() string { return "mock" }

func (m *MockDelegatedSource) CanHandle(_ context.Context, uri string) bool {
	return strings.HasPrefix(uri, "mock://doc/")
}

// Fetch requires a user_id in the context; use FetchForUser directly when calling
// from a handler that knows the user.
func (m *MockDelegatedSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx) // add this helper to api/content_source.go: reads "user_id" key set by JWT middleware
	if !ok {
		return nil, "", fmt.Errorf("mock delegated source: no user in context")
	}
	return m.FetchForUser(ctx, userID, uri)
}

func (m *MockDelegatedSource) doFetch(_ context.Context, _ string, uri string) ([]byte, string, error) {
	id := strings.TrimPrefix(uri, "mock://doc/")
	data, ok := m.Contents[id]
	if !ok {
		return nil, "", fmt.Errorf("mock doc %q not found", id)
	}
	return data, "text/plain", nil
}
```

Also add to `api/content_source.go`:

```go
// UserIDFromContext reads the JWT-middleware-established user identifier from ctx.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v := ctx.Value(userIDContextKey{})
	s, ok := v.(string)
	return s, ok && s != ""
}

type userIDContextKey struct{}

// WithUserID is used by middleware and tests to attach a user id to the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}
```

The subagent should check whether an equivalent helper already exists before introducing this — if it does, use the existing one.

Unit tests drive a known doc id through the full pipeline using fake tokens (happy path).

Commit: `feat(api): add MockDelegatedSource for content-infra integration tests (#249)`.

---

## Task 4.4: End-to-end integration test

**Files:**
- Create: `api/content_oauth_integration_test.go` (build tag `//go:build integration`)

Flow driven in one test:

```
1. Start stub OAuth provider httptest.Server (short access-token TTL, rotates refresh, supports revoke).
2. Build ContentOAuthProviderRegistry with a BaseContentOAuthProvider whose AuthURL/TokenURL/RevocationURL/UserinfoURL point at the stub.
3. Build ContentTokenRepository against test Postgres.
4. Build ContentOAuthStateStore against miniredis (or real Redis per integration convention).
5. Register a MockDelegatedSource that uses this repository + registry, with Contents["doc1"] = []byte("hello").
6. Register handlers onto a test Gin router (mirroring the production route shape).
7. Issue HTTP:
   a. POST /me/content_tokens/mock/authorize with client_callback=http://localhost/cb → grab authorization_url.
   b. Follow authorization_url manually (stub returns 302 immediately); extract code+state.
   c. GET /oauth2/content_callback?code=...&state=... → expect 302 to http://localhost/cb?status=success&provider_id=mock.
   d. Assert user_content_tokens row exists for (user, "mock"); status=active.
   e. Call MockDelegatedSource.FetchForUser(ctx, user, "mock://doc/doc1") → expect []byte("hello").
   f. Fast-forward the stub's clock so the access token is expired.
   g. Call FetchForUser again → expect success; assert stub recorded exactly one Refresh call; row's access_token changed.
   h. Launch N goroutines, all expired, call FetchForUser concurrently. Assert stub recorded only ONE additional Refresh call.
   i. DELETE /me/content_tokens/mock → expect 204; stub recorded a Revoke call; row is gone.
8. Separately: configure stub to return 400 on refresh; force expiry; FetchForUser returns ErrAuthRequired, row flipped to failed_refresh.
```

Integration test is substantial. Split into 3-4 sub-tests within the same file if clarity benefits.

Commit: `test(api): add end-to-end delegated content provider integration tests (#249)`.

---

# Phase 5 — Admin endpoints + user-delete cascade

## Task 5.1: `/admin/users/{user_id}/content_tokens` list + DELETE

**Files:**
- Create: `api/content_oauth_admin_handlers.go`
- Test: `api/content_oauth_admin_handlers_test.go`

Mirrors `List` and `Delete` from Task 3.4 but:
- Reads `user_id` from path parameter instead of context.
- Requires admin middleware (`rg "adminRequired\|RequireAdmin" api auth -n` to find the helper).
- Uses the exact same repository calls + revocation path as the me-version (factor shared helper if it reduces duplication).

Unit tests: admin token succeeds; non-admin token → 403; target user not found → empty list / 404 as appropriate; delete on non-existent → 204.

Commit: `feat(api): add /admin/users/{user_id}/content_tokens handlers (#249)`.

---

## Task 5.2: Wire admin routes in `api/server.go`

- [ ] Add:
  - `adminGroup.GET("/users/:user_id/content_tokens", h.AdminList)`
  - `adminGroup.DELETE("/users/:user_id/content_tokens/:provider_id", h.AdminDelete)`

Use whatever admin-route grouping already exists (follow `rg "/admin/" api/server.go -n`).

Commit: `feat(api): register admin content-token routes (#249)`.

---

## Task 5.3: User-delete cascade revocation sweep

**Files:**
- Modify: the existing user-deletion code path. Locate via: `rg "DeleteUser\|user_delete\|func .*User.*Delete" auth api -n` and pick the right entry point. Typically in `auth/handlers_user.go` or `auth/repository/user_repository.go`.
- Test: add unit or integration test confirming revocation is attempted before FK cascade deletes rows.

Changes:
1. Before the existing delete step: fetch the user's content-token rows.
2. For each, invoke the same revocation path used by the me-delete handler (decrypt, call provider.Revoke, log warn on failure). Wrap in a helper so `AdminDelete` and the cascade share it.
3. Continue with the existing user deletion — FK cascade then removes the rows.

Test: stub provider recording revocation calls; delete the user; assert provider received the expected number of revoke calls; assert rows gone.

Commit: `feat(auth): sweep content-token revocations on user delete (#249)`.

---

# Phase 6 — OpenAPI spec, code regeneration, documentation

## Task 6.1: Add schemas + operations to `api-schema/tmi-openapi.json`

**Files:**
- Modify: `api-schema/tmi-openapi.json` (large JSON; >100 KB — use jq surgical updates where possible)

Schemas to add:

```json
"ContentTokenInfo": {
  "type": "object",
  "required": ["provider_id", "status", "scopes", "created_at"],
  "properties": {
    "provider_id": { "type": "string", "description": "Content OAuth provider id (e.g., 'confluence')." },
    "provider_account_id": { "type": "string", "description": "External account identifier reported by the provider. May be empty if the provider has no stable id." },
    "provider_account_label": { "type": "string", "description": "Human-readable account label (email or username)." },
    "scopes": { "type": "array", "items": { "type": "string" } },
    "status": { "type": "string", "enum": ["active", "failed_refresh"] },
    "expires_at": { "type": "string", "format": "date-time" },
    "last_refresh_at": { "type": "string", "format": "date-time" },
    "created_at": { "type": "string", "format": "date-time" }
  }
},
"ContentAuthorizationURL": {
  "type": "object",
  "required": ["authorization_url", "expires_at"],
  "properties": {
    "authorization_url": { "type": "string", "format": "uri" },
    "expires_at": { "type": "string", "format": "date-time" }
  }
}
```

New operations under `paths`:

| Path | Method | operationId | Tag | Security |
|------|--------|-------------|-----|----------|
| `/me/content_tokens` | GET | `listMyContentTokens` | `Me` | bearer |
| `/me/content_tokens/{provider_id}/authorize` | POST | `authorizeContentToken` | `Me` | bearer |
| `/me/content_tokens/{provider_id}` | DELETE | `deleteMyContentToken` | `Me` | bearer |
| `/oauth2/content_callback` | GET | `contentOAuthCallback` | `OAuth` | none (public) |
| `/admin/users/{user_id}/content_tokens` | GET | `adminListUserContentTokens` | `Admin` | bearer + admin |
| `/admin/users/{user_id}/content_tokens/{provider_id}` | DELETE | `adminDeleteUserContentToken` | `Admin` | bearer + admin |

Mark the callback with `"x-public-endpoint": true` and `"x-cacheable-endpoint": false`.

Use `jq` for surgical insertion. Before editing, run `cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.$(date +%Y%m%d_%H%M%S).backup`.

Run after edits:

```
make validate-openapi
```

Fix any schema errors reported by Vacuum/OWASP (must pass before commit).

- [ ] **Commit:** `feat(openapi): add content token management operations (#249)`.

---

## Task 6.2: Regenerate API code

- [ ] **Step 1:** `make generate-api`
- [ ] **Step 2:** `make build-server`

The regenerated `api/api.go` should now include handler-interface methods for the six new operations. Wire them to the handlers built in Tasks 3.4 / 3.5 / 5.1.

If there's drift between the handler signatures and the interface — adapt the handlers so that their signatures match the generated interface exactly (follow the pattern used by recent regenerations).

- [ ] **Step 3:** `make test-unit`
- [ ] **Step 4:** `make test-integration`
- [ ] **Step 5:** `make lint`

All must pass.

- [ ] **Commit:** `chore(api): regenerate api.go for content-token operations (#249)`.

---

## Task 6.3: Update the GitHub wiki

Per CLAUDE.md, project documentation lives in the GitHub wiki — not `docs/`.

Add a new wiki page "Delegated Content Providers" that covers:
- Purpose and relation to service content providers.
- Required env vars (`TMI_CONTENT_TOKEN_ENCRYPTION_KEY`, `TMI_CONTENT_OAUTH_*`).
- Endpoint summaries.
- Operator checklist for enabling a new delegated provider.

Do NOT add content to `docs/` (deprecated).

The subagent may not have write access to the wiki; if that is the case, produce the markdown content and place it in the `tmi-content-delegated.md` file at the repo root as a patch with a commit message `docs: add content for new wiki page on delegated content providers (#249)`. The operator will copy it into the wiki.

- [ ] **Commit:** `docs: prepare wiki content for delegated content providers (#249)`.

---

# Final verification

Before declaring the plan complete, the orchestrating subagent must:

1. Run `make lint` — PASS.
2. Run `make build-server` — PASS.
3. Run `make test-unit` — PASS.
4. Run `make test-integration` — PASS.
5. Run `make validate-openapi` — PASS.
6. Confirm `git status` is clean.
7. Execute the task-completion workflow from CLAUDE.md (push to remote, etc.).
8. Post a summary comment on issue #249 linking the spec, the plan, and the commit range; note that this closes sub-project 1 of 5.

# Out of scope

Do not add:
- Confluence, Google Workspace, OneDrive provider implementations.
- OOXML extractors.
- UI work.
- Background refresh worker.
- Key-rotation tooling.
- Incremental scope upgrade flows.

These are separate sub-projects of #249 (see spec).
