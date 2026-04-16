# GORM Store to Repository Pattern Migration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate three GORM store files to the repository pattern with typed dberrors, retry logic, and updated handlers (#261).

**Architecture:** New repository interfaces and implementations replace legacy store files in the `api` package. All string-based error matching is replaced with typed error sentinels from `internal/dberrors/`. Write operations are wrapped in `WithRetryableGormTransaction`. Handlers use `errors.Is()` and `StoreErrorToRequestError()` instead of string matching.

**Tech Stack:** Go, GORM, internal/dberrors, auth/db (retry), gin

**Design spec:** `docs/superpowers/specs/2026-04-15-gorm-store-repository-migration-design.md`

---

## File Map

### New files to create

| File | Purpose |
|------|---------|
| `api/repository_interfaces.go` | Repository interfaces, typed error sentinels, MetadataConflictError |
| `api/group_repository.go` | GormGroupRepository implementation |
| `api/metadata_repository.go` | GormMetadataRepository implementation |
| `api/group_member_repository.go` | GormGroupMemberRepository implementation |

### Files to modify

| File | Changes |
|------|---------|
| `api/store.go` | Rename globals, update InitializeGormStores |
| `api/admin_group_handlers.go` | Use GlobalGroupRepository, typed error handling |
| `api/admin_group_member_handlers.go` | Use GlobalGroupRepository/GlobalGroupMemberRepository, rewrite handleGroupMemberError |
| `api/my_group_handlers.go` | Use GlobalGroupRepository/GlobalGroupMemberRepository |
| `api/metadata_handlers.go` | Use MetadataConflictError instead of *ErrMetadataKeyExists |
| `api/request_utils.go` | Remove string-fallback from StoreErrorToRequestError |
| `api/auth_utils.go` | Remove ErrMsgGroupNotFound (keep ErrMsgUserNotFound — still used by user_store) |
| `api/admin_group_handlers_test.go` | Rename GlobalGroupStore → GlobalGroupRepository, update mock interfaces |
| `api/my_group_handlers_test.go` | Same |
| `api/admin_group_member_handlers.go` test references | Same |
| Various test files referencing GlobalGroupMemberStore/GlobalMetadataStore | Rename to repository |

### Files to delete

| File | Replaced by |
|------|-------------|
| `api/group_store_gorm.go` | `api/group_repository.go` |
| `api/metadata_store_gorm.go` | `api/metadata_repository.go` |
| `api/group_member_store_gorm.go` | `api/group_member_repository.go` |
| `api/group_store.go` | `api/repository_interfaces.go` |
| `api/metadata_store.go` | `api/repository_interfaces.go` |

---

## Task 1: Create repository interfaces and error sentinels

**Files:**
- Create: `api/repository_interfaces.go`

This is the foundation — all other tasks depend on these interface definitions and error sentinels.

- [ ] **Step 1: Create `api/repository_interfaces.go`**

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
)

// Repository error sentinels.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	// Not-found errors
	ErrGroupNotFound       = fmt.Errorf("group: %w", dberrors.ErrNotFound)
	ErrMetadataNotFound    = fmt.Errorf("metadata: %w", dberrors.ErrNotFound)
	ErrGroupMemberNotFound = fmt.Errorf("group member: %w", dberrors.ErrNotFound)

	// Duplicate/conflict errors
	ErrGroupDuplicate       = fmt.Errorf("group: %w", dberrors.ErrDuplicate)
	ErrGroupMemberDuplicate = fmt.Errorf("group member: %w", dberrors.ErrDuplicate)
	ErrMetadataKeyExists  = fmt.Errorf("metadata: %w", dberrors.ErrDuplicate)

	// Business logic errors (not DB errors)
	ErrSelfMembership = errors.New("a group cannot be a member of itself")
	ErrEveryoneGroup  = errors.New("cannot modify the everyone group")
)

// MetadataConflictError carries conflicting key names while wrapping ErrMetadataKeyExists.
// Use errors.Is(err, ErrMetadataKeyExists) for detection, errors.As for key extraction.
type MetadataConflictError struct {
	ConflictingKeys []string
}

func (e *MetadataConflictError) Error() string {
	return fmt.Sprintf("metadata key(s) already exist: %s", strings.Join(e.ConflictingKeys, ", "))
}

func (e *MetadataConflictError) Unwrap() error {
	return ErrMetadataKeyExists
}

// GroupRepository defines the interface for group storage operations.
// Delete is excluded — it delegates to auth.Service.DeleteGroupAndData() at the handler level.
type GroupRepository interface {
	List(ctx context.Context, filter GroupFilter) ([]Group, error)
	Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error)
	GetByProviderAndName(ctx context.Context, provider string, groupName string) (*Group, error)
	Create(ctx context.Context, group Group) error
	Update(ctx context.Context, group Group) error
	Count(ctx context.Context, filter GroupFilter) (int, error)
	EnrichGroups(ctx context.Context, groups []Group) ([]Group, error)
	GetGroupsForProvider(ctx context.Context, provider string) ([]Group, error)
}

// MetadataRepository defines the interface for metadata operations with caching support.
type MetadataRepository interface {
	Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error)
	Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Delete(ctx context.Context, entityType, entityID, key string) error
	List(ctx context.Context, entityType, entityID string) ([]Metadata, error)
	Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkReplace(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error
	GetByKey(ctx context.Context, key string) ([]Metadata, error)
	ListKeys(ctx context.Context, entityType, entityID string) ([]string, error)
	InvalidateCache(ctx context.Context, entityType, entityID string) error
	WarmCache(ctx context.Context, entityType, entityID string) error
}

// GroupMemberRepository defines the interface for group membership storage operations.
type GroupMemberRepository interface {
	ListMembers(ctx context.Context, filter GroupMemberFilter) ([]GroupMember, error)
	CountMembers(ctx context.Context, groupInternalUUID uuid.UUID) (int, error)
	AddMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error)
	RemoveMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) error
	IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error)
	AddGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error)
	RemoveGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID) error
	IsEffectiveMember(ctx context.Context, groupInternalUUID uuid.UUID, userInternalUUID uuid.UUID, userGroupUUIDs []uuid.UUID) (bool, error)
	HasAnyMembers(ctx context.Context, groupInternalUUID uuid.UUID) (bool, error)
	GetGroupsForUser(ctx context.Context, userInternalUUID uuid.UUID) ([]Group, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Expected: PASS (new file only adds types, no consumers yet)

- [ ] **Step 3: Commit**

```bash
git add api/repository_interfaces.go
git commit -m "refactor(api): add repository interfaces and typed error sentinels (#261)

Define GroupRepository, MetadataRepository, and GroupMemberRepository
interfaces with typed error sentinels wrapping dberrors categories."
```

---

## Task 2: Create GroupRepository implementation

**Files:**
- Create: `api/group_repository.go`
- Reference: `api/group_store_gorm.go` (source to migrate)

Migrate all methods from `GormGroupStore` except `Delete` (handled at handler level). Replace string-based error matching with `dberrors.Classify()`. Wrap `Create` and `Update` in `WithRetryableGormTransaction`.

- [ ] **Step 1: Create `api/group_repository.go`**

Copy the contents of `api/group_store_gorm.go` into `api/group_repository.go` and apply these changes:

1. Rename `GormGroupStore` → `GormGroupRepository` throughout
2. Remove the `authService *auth.Service` field and constructor parameter
3. Add `logger *slogging.Logger` field (already exists)
4. Rename constructor `NewGormGroupStore` → `NewGormGroupRepository(db *gorm.DB) *GormGroupRepository`
5. Remove the `Delete` method entirely (will be handled at handler level)
6. Update `Get` and `GetByProviderAndName`: replace `errors.New(ErrMsgGroupNotFound)` with `ErrGroupNotFound`
7. Update `Create`: replace the string-matching duplicate check with `dberrors.Classify()`, return `ErrGroupDuplicate` for duplicates
8. Update `Update`: replace `errors.New(ErrMsgGroupNotFound)` with `ErrGroupNotFound`
9. Wrap `Create` and `Update` in `db.WithRetryableGormTransaction`
10. For `List`, `Count`, `EnrichGroups`, `GetGroupsForProvider`: classify errors via `dberrors.Classify()` instead of raw `fmt.Errorf` wrapping
11. Keep `UpsertGroup` as a concrete method (not on interface — used for JWT group sync)
12. Keep `convertToGroup`, `convertFromGroup` methods; rename receiver from `s` to `r`
13. Add import for `"github.com/ericfitz/tmi/auth/db"` and `"github.com/ericfitz/tmi/internal/dberrors"`
14. Remove import for `"github.com/ericfitz/tmi/auth"` (no longer needed)

Key error handling transformations:

**Get / GetByProviderAndName (not-found):**
```go
// Old
if errors.Is(result.Error, gorm.ErrRecordNotFound) {
    return nil, errors.New(ErrMsgGroupNotFound)
}
return nil, fmt.Errorf("failed to get group: %w", result.Error)

// New
if errors.Is(result.Error, gorm.ErrRecordNotFound) {
    return nil, ErrGroupNotFound
}
return nil, dberrors.Classify(result.Error)
```

**Create (duplicate check):**
```go
// Old
errStr := result.Error.Error()
if strings.Contains(errStr, "duplicate key") || ...
    return fmt.Errorf("group already exists for provider")

// New
classified := dberrors.Classify(result.Error)
if errors.Is(classified, dberrors.ErrDuplicate) {
    return ErrGroupDuplicate
}
return classified
```

**Create — retry wrapping:**
```go
func (r *GormGroupRepository) Create(ctx context.Context, group Group) error {
    // Set defaults...
    gormGroup := r.convertFromGroup(&group)

    return db.WithRetryableGormTransaction(ctx, r.db, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
        result := tx.Create(gormGroup)
        if result.Error != nil {
            classified := dberrors.Classify(result.Error)
            if errors.Is(classified, dberrors.ErrDuplicate) {
                return ErrGroupDuplicate
            }
            return classified
        }
        return nil
    })
}
```

**Update — retry wrapping:**
```go
func (r *GormGroupRepository) Update(ctx context.Context, group Group) error {
    return db.WithRetryableGormTransaction(ctx, r.db, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
        result := tx.Model(&models.Group{}).
            Where("internal_uuid = ?", group.InternalUUID.String()).
            Updates(map[string]any{...})

        if result.Error != nil {
            return dberrors.Classify(result.Error)
        }
        if result.RowsAffected == 0 {
            return ErrGroupNotFound
        }
        return nil
    })
}
```

**List / Count / EnrichGroups — classify on error:**
```go
// Old
return nil, fmt.Errorf("failed to query groups: %w", err)

// New
return nil, dberrors.Classify(err)
```

Note: `EnrichGroups` currently logs warnings and continues on individual enrichment failures — preserve that behavior (don't classify individual enrichment errors as fatal).

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Expected: PASS (new file, no consumers yet, old file still exists)

- [ ] **Step 3: Commit**

```bash
git add api/group_repository.go
git commit -m "refactor(api): create GormGroupRepository with typed errors and retry (#261)

Migrate group CRUD from GormGroupStore to GormGroupRepository.
Replace string-based error matching with dberrors.Classify().
Wrap Create and Update in WithRetryableGormTransaction."
```

---

## Task 3: Create MetadataRepository implementation

**Files:**
- Create: `api/metadata_repository.go`
- Reference: `api/metadata_store_gorm.go` (source to migrate)

Migrate all methods from `GormMetadataStore`. Replace `*ErrMetadataKeyExists` with `*MetadataConflictError`. Replace string-based duplicate detection with `dberrors.Classify()`. Wrap write operations in `WithRetryableGormTransaction`. Keep cache integration inline.

- [ ] **Step 1: Create `api/metadata_repository.go`**

Copy `api/metadata_store_gorm.go` into `api/metadata_repository.go` and apply:

1. Rename `GormMetadataStore` → `GormMetadataRepository` throughout
2. Add `logger *slogging.Logger` field to struct
3. Rename constructor `NewGormMetadataStore` → `NewGormMetadataRepository`; add `logger: slogging.Get()` to constructor
4. Rename receiver from `s` to `r` throughout
5. Replace all `slogging.Get()` calls inside methods with `r.logger`
6. Replace `&ErrMetadataKeyExists{ConflictingKeys: ...}` with `&MetadataConflictError{ConflictingKeys: ...}`
7. Replace all string-matching duplicate checks with `dberrors.Classify()`:

**Single Create (duplicate check):**
```go
// Old
errMsg := strings.ToLower(result.Error.Error())
if strings.Contains(errMsg, "duplicate key") || ... {
    return &ErrMetadataKeyExists{ConflictingKeys: []string{metadata.Key}}
}

// New
classified := dberrors.Classify(result.Error)
if errors.Is(classified, dberrors.ErrDuplicate) {
    return &MetadataConflictError{ConflictingKeys: []string{metadata.Key}}
}
return classified
```

8. Replace not-found errors:
```go
// Old
return nil, fmt.Errorf("metadata key not found: %s", key)

// New
return nil, ErrMetadataNotFound
```

9. For `Update` and `Delete` (RowsAffected == 0):
```go
// Old
return fmt.Errorf("metadata key not found: %s", metadata.Key)

// New
return ErrMetadataNotFound
```

10. Wrap write methods in `WithRetryableGormTransaction`:
    - `Create`: Wrap the DB create operation (inside the existing mutex lock)
    - `Update`: Wrap the DB update operation
    - `Delete`: Wrap the DB delete operation
    - `BulkCreate`, `BulkUpdate`, `BulkReplace`, `BulkDelete`: These already use `s.db.Transaction()`. Replace with `db.WithRetryableGormTransaction()`. The retry function receives `tx *gorm.DB` — use that instead of `s.db` inside the transaction.

**BulkCreate transformation:**
```go
// Old
return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { ... })

// New
return db.WithRetryableGormTransaction(ctx, r.db, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
    // ... same logic but using dberrors.Classify for errors
    // ... &MetadataConflictError instead of &ErrMetadataKeyExists
})
```

11. For `Post`: it delegates to `Create` — no changes needed beyond the rename
12. Keep `validateEntityType`, cache integration, mutex, all helper calls unchanged
13. Add imports: `"github.com/ericfitz/tmi/auth/db"`, `"github.com/ericfitz/tmi/internal/dberrors"`
14. Remove `"strings"` import if no longer needed (string matching removed)

Note on mutex + retry: The mutex protects cache consistency. `WithRetryableGormTransaction` handles DB retries. These are orthogonal — the mutex stays outside the retry loop (as it is today).

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add api/metadata_repository.go
git commit -m "refactor(api): create GormMetadataRepository with typed errors and retry (#261)

Migrate metadata CRUD from GormMetadataStore to GormMetadataRepository.
Replace ErrMetadataKeyExists with MetadataConflictError wrapping dberrors.
Wrap all write operations in WithRetryableGormTransaction."
```

---

## Task 4: Create GroupMemberRepository implementation

**Files:**
- Create: `api/group_member_repository.go`
- Reference: `api/group_member_store_gorm.go` (source to migrate)

Migrate all methods. Replace `isDuplicateKeyError()` with `dberrors.Classify()`. Replace string error messages with typed sentinels. Wrap write operations in retry logic.

- [ ] **Step 1: Create `api/group_member_repository.go`**

Copy `api/group_member_store_gorm.go` into `api/group_member_repository.go` and apply:

1. Rename `GormGroupMemberStore` → `GormGroupMemberRepository`
2. Add `logger *slogging.Logger` field; update constructor to set `logger: slogging.Get()`
3. Rename constructor `NewGormGroupMemberStore` → `NewGormGroupMemberRepository`
4. Rename receiver from `s` to `r`
5. Replace all `slogging.Get()` calls with `r.logger`
6. **Remove** the `isDuplicateKeyError` method entirely
7. Keep `normalizeNullString` and `safeEmail` as package-level helpers (they already are)

**Error handling transformations:**

**AddMember — "everyone" check:**
```go
// Old
return nil, fmt.Errorf("cannot add members to the 'everyone' pseudo-group")

// New
return nil, ErrEveryoneGroup
```

**AddMember — group not found:**
```go
// Old
return nil, errors.New(ErrMsgGroupNotFound)

// New — import auth/repository for ErrUserNotFound
return nil, ErrGroupNotFound
```

**AddMember — user not found:**
```go
// Old
return nil, errors.New(ErrMsgUserNotFound)

// New — use auth/repository.ErrUserNotFound
import repository "github.com/ericfitz/tmi/auth/repository"
return nil, repository.ErrUserNotFound
```

**AddMember — duplicate check:**
```go
// Old
if s.isDuplicateKeyError(err) {
    return nil, fmt.Errorf("user is already a member of this group")
}

// New
classified := dberrors.Classify(err)
if errors.Is(classified, dberrors.ErrDuplicate) {
    return nil, ErrGroupMemberDuplicate
}
return nil, classified
```

**RemoveMember — not found:**
```go
// Old
return fmt.Errorf("membership not found")

// New
return ErrGroupMemberNotFound
```

**RemoveMember — "everyone" check:**
```go
// Old
return fmt.Errorf("cannot remove members from the 'everyone' pseudo-group")

// New
return ErrEveryoneGroup
```

**AddGroupMember — self-membership:**
```go
// Old
return nil, fmt.Errorf("a group cannot be a member of itself")

// New
return nil, ErrSelfMembership
```

**AddGroupMember — member group not found:**
```go
// Old
return nil, fmt.Errorf("member group not found")

// New
return nil, ErrGroupNotFound
```

**AddGroupMember — duplicate check:**
```go
// Old
if s.isDuplicateKeyError(err) {
    return nil, fmt.Errorf("group is already a member of this group")
}

// New
classified := dberrors.Classify(err)
if errors.Is(classified, dberrors.ErrDuplicate) {
    return nil, ErrGroupMemberDuplicate
}
return nil, classified
```

**RemoveGroupMember — not found:**
```go
// Old
return fmt.Errorf("group membership not found")

// New
return ErrGroupMemberNotFound
```

8. Wrap write methods in `db.WithRetryableGormTransaction`:
   - `AddMember`: The group/user existence checks should stay OUTSIDE the retry (they're reads). The Create operation should be inside:
   ```go
   // After existence checks...
   err := db.WithRetryableGormTransaction(ctx, r.db, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
       if err := tx.Create(&model).Error; err != nil {
           classified := dberrors.Classify(err)
           if errors.Is(classified, dberrors.ErrDuplicate) {
               return ErrGroupMemberDuplicate
           }
           return classified
       }
       return nil
   })
   if err != nil {
       return nil, err
   }
   // Fetch the complete member record...
   ```
   - `RemoveMember`, `RemoveGroupMember`: Wrap the delete + RowsAffected check
   - `AddGroupMember`: Same pattern as AddMember — existence checks outside, create inside retry

9. For read methods (`ListMembers`, `CountMembers`, `IsMember`, `IsEffectiveMember`, `HasAnyMembers`, `GetGroupsForUser`): Replace `fmt.Errorf("failed to ...: %w", err)` with `dberrors.Classify(err)`

10. Add imports: `"github.com/ericfitz/tmi/auth/db"`, `"github.com/ericfitz/tmi/internal/dberrors"`, `repository "github.com/ericfitz/tmi/auth/repository"`
11. Remove import for `"strings"` (string matching removed with isDuplicateKeyError)

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add api/group_member_repository.go
git commit -m "refactor(api): create GormGroupMemberRepository with typed errors and retry (#261)

Migrate group membership ops from GormGroupMemberStore to GormGroupMemberRepository.
Replace isDuplicateKeyError string matching with dberrors.Classify().
Wrap AddMember, RemoveMember, AddGroupMember, RemoveGroupMember in
WithRetryableGormTransaction."
```

---

## Task 5: Wire up globals and initialization

**Files:**
- Modify: `api/store.go:77,93,109,133,123,155-159`

Replace the three global store variables and their initialization with the new repository types.

- [ ] **Step 1: Update global variables in `api/store.go`**

In `api/store.go`, make these changes:

1. Remove line 93 (`var GlobalMetadataStore MetadataStore`) — it moves to `repository_interfaces.go`'s global section
2. Add three new global variables. Add them near the existing store globals (around lines 86-109):

```go
// Repository globals (replacing legacy store globals)
var GlobalGroupRepository GroupRepository
var GlobalMetadataRepository MetadataRepository
var GlobalGroupMemberRepository GroupMemberRepository
```

3. In `InitializeGormStores` (line 109), update the three initialization lines:

```go
// Old (line 123)
GlobalMetadataStore = NewGormMetadataStore(db, cache, invalidator)
// New
GlobalMetadataRepository = NewGormMetadataRepository(db, cache, invalidator)

// Old (line 133)
GlobalGroupMemberStore = NewGormGroupMemberStore(db)
// New
GlobalGroupMemberRepository = NewGormGroupMemberRepository(db)

// Old (lines 157-158, inside authService != nil block)
GlobalGroupStore = NewGormGroupStore(db, svc.GetService())
// New
GlobalGroupRepository = NewGormGroupRepository(db)
```

Note: `GlobalGroupRepository = NewGormGroupRepository(db)` no longer needs the auth service, so move it OUTSIDE the `if authService != nil` block, alongside `GlobalGroupMemberRepository`.

4. Keep the old globals (`GlobalGroupStore`, `GlobalMetadataStore`, `GlobalGroupMemberStore`) temporarily — they're still referenced by handlers and tests. They'll be removed in the cleanup task.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Expected: PASS (old and new globals coexist temporarily)

- [ ] **Step 3: Commit**

```bash
git add api/store.go
git commit -m "refactor(api): add repository globals and wiring in InitializeGormStores (#261)

Add GlobalGroupRepository, GlobalMetadataRepository, and
GlobalGroupMemberRepository. Wire them in InitializeGormStores.
Old store globals kept temporarily during migration."
```

---

## Task 6: Update admin group handlers

**Files:**
- Modify: `api/admin_group_handlers.go`

Switch from `GlobalGroupStore` to `GlobalGroupRepository`. Replace all `err.Error() == ErrMsgGroupNotFound` checks with `errors.Is()` or `StoreErrorToRequestError()`.

- [ ] **Step 1: Update `api/admin_group_handlers.go`**

Add import: `"errors"`, `"github.com/ericfitz/tmi/internal/dberrors"`

**ListAdminGroups (line 101):** `GlobalGroupStore` → `GlobalGroupRepository` (3 occurrences: List, Count, EnrichGroups)

**GetAdminGroup (lines 151-167):**
```go
// Old
group, err := GlobalGroupStore.Get(c.Request.Context(), internalUUID)
if err != nil {
    if err.Error() == ErrMsgGroupNotFound {
        HandleRequestError(c, &RequestError{Status: http.StatusNotFound, ...})
    } else {
        HandleRequestError(c, &RequestError{Status: http.StatusInternalServerError, ...})
    }
    return
}

// New
group, err := GlobalGroupRepository.Get(c.Request.Context(), internalUUID)
if err != nil {
    HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
    return
}
```

Also update the `EnrichGroups` call on line 171: `GlobalGroupStore` → `GlobalGroupRepository`

**CreateAdminGroup (line 234):**
```go
// Old
err := GlobalGroupStore.Create(c.Request.Context(), group)
if err != nil {
    switch {
    case err.Error() == "group already exists for provider":
        // 409 Conflict
    case isDBValidationError(err):
        // 400 Bad Request
    default:
        // 500
    }
}

// New
err := GlobalGroupRepository.Create(c.Request.Context(), group)
if err != nil {
    switch {
    case errors.Is(err, ErrGroupDuplicate):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusConflict,
            Code:    "duplicate_group",
            Message: "Group already exists for this provider",
        })
    case isDBValidationError(err):
        logger.Warn("Group creation failed due to validation error: %v", err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusBadRequest,
            Code:    "validation_error",
            Message: "Field value exceeds maximum allowed length or contains invalid characters",
        })
    default:
        logger.Error("Failed to create group: %v", err)
        HandleRequestError(c, &RequestError{
            Status:  http.StatusInternalServerError,
            Code:    "server_error",
            Message: "Failed to create group",
        })
    }
    return
}
```

**UpdateAdminGroup (lines 305, 360):**
```go
// Get group — same pattern as GetAdminGroup
group, err := GlobalGroupRepository.Get(c.Request.Context(), internalUUID)
if err != nil {
    HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
    return
}

// Update (line 360)
err = GlobalGroupRepository.Update(c.Request.Context(), *group)
if err != nil {
    switch {
    case errors.Is(err, ErrGroupNotFound):
        HandleRequestError(c, NotFoundError("Group not found"))
    case errors.Is(err, dberrors.ErrConstraint):
        // Protected group errors come through as constraint errors
        HandleRequestError(c, &RequestError{
            Status:  http.StatusForbidden,
            Code:    "protected_group",
            Message: "Built-in groups cannot be renamed or have their description changed.",
        })
    default:
        logger.Error("Failed to update group: %v", err)
        HandleRequestError(c, ServerError("Failed to update group"))
    }
    return
}
```

Note: The "cannot rename built-in group" / "cannot clear the display name" errors from the old `Update` method currently use string matching. In the new repository, these validation errors should be preserved as-is. The repository's `Update` method should return these errors wrapped through `dberrors.Classify()` which will classify them as generic errors (not specifically ErrConstraint). Keep the existing string matching for these protected-group checks OR create new sentinel errors. Since these are business rules (not DB errors), the simplest approach is to keep the string matching in the handler for now — the repository returns these errors as-is (they don't come from the DB, they come from business logic in the update method if it exists there). Looking at the source: the Update method in `group_store_gorm.go` does NOT check for protected groups — that logic is elsewhere. So the Update only returns ErrGroupNotFound (RowsAffected == 0) or classified DB errors. The protected-group strings in the handler error switch came from somewhere else. Let me check...

Actually, reviewing the code: the handler calls `GlobalGroupStore.Update()` which only does the DB update. The "cannot rename built-in group" errors must come from middleware or validators that aren't in the store. Since the new repository's `Update` only returns `ErrGroupNotFound` or classified DB errors, simplify the handler error handling:

```go
err = GlobalGroupRepository.Update(c.Request.Context(), *group)
if err != nil {
    if errors.Is(err, ErrGroupNotFound) {
        HandleRequestError(c, NotFoundError("Group not found"))
    } else {
        logger.Error("Failed to update group: %v", err)
        HandleRequestError(c, ServerError("Failed to update group"))
    }
    return
}
```

Wait — the handler string matching for "cannot rename built-in group" etc. suggests these errors DO come from the store. But the `GormGroupStore.Update()` I read doesn't have that logic. These error strings must be produced by business logic outside the store. Since we're migrating the store, the handler still needs to handle these if they come from elsewhere. Preserve the `strings.Contains` checks for protected group errors — they don't come from the repository:

```go
err = GlobalGroupRepository.Update(c.Request.Context(), *group)
if err != nil {
    errMsg := err.Error()
    switch {
    case errors.Is(err, ErrGroupNotFound):
        HandleRequestError(c, NotFoundError("Group not found"))
    case strings.Contains(errMsg, "cannot rename built-in group") ||
        strings.Contains(errMsg, "cannot clear the display name of built-in group") ||
        strings.Contains(errMsg, "cannot clear the description of built-in group") ||
        strings.Contains(errMsg, "cannot change the description of built-in group"):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusForbidden,
            Code:    "protected_group",
            Message: "Built-in groups cannot be renamed or have their description changed.",
        })
    default:
        logger.Error("Failed to update group: %v", err)
        HandleRequestError(c, ServerError("Failed to update group"))
    }
    return
}
```

**DeleteAdminGroup (line 412):** This method calls `GlobalGroupStore.Delete()`. Since Delete is removed from the repository, call `auth.Service.DeleteGroupAndData()` directly. The server has access to the auth service via `s.authService`:

```go
// Old
stats, err := GlobalGroupStore.Delete(c.Request.Context(), internalUuid.String())

// New — call auth service directly
// The server struct already has authService. Check how it's accessed.
```

Check: the Server struct needs access to auth service for delete. Look at how `Server` is defined and whether it has an auth service reference. If not, use a similar approach to how the old store got it. Since `GormGroupStore` held `authService *auth.Service`, and the Server uses `GlobalGroupStore.Delete()`, the Server must be able to get the auth service somehow. The simplest approach: the handler already has access to the gin context, and auth service may be available via the server struct. Check the Server struct fields. If the server doesn't have a direct auth service reference, we may need to add one, OR keep a package-level reference.

For now, add a `deletionRepo` or use the auth service via the existing `DeletionRepository` in auth. Looking at `auth/repository/interfaces.go`, there's a `DeletionRepository` with `DeleteGroupAndData`. The auth `Service` exposes this. The simplest approach: add a package-level `GlobalDeletionService` or pass it through the server. Since this is a single handler, use a minimal approach — store a reference when initializing stores.

Actually, the cleanest approach: In `InitializeGormStores`, save a reference to `auth.Service.DeleteGroupAndData` that the handler can use. Or just keep the auth service reference in a package-level variable:

In `api/store.go`, in `InitializeGormStores`, after the auth service check:
```go
if authService != nil {
    if svc, ok := authService.(AuthServiceGetter); ok {
        GlobalUserStore = NewGormUserStore(db, svc.GetService())
        globalAuthService = svc.GetService() // new package-level var
    }
}
```

Then in the delete handler:
```go
result, err := globalAuthService.DeleteGroupAndData(ctx, internalUuid.String())
```

And map the result to `GroupDeletionStats` as the old Delete method did.

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: Tests may fail due to test file references to `GlobalGroupStore` — those are fixed in Task 9.

- [ ] **Step 4: Commit**

```bash
git add api/admin_group_handlers.go api/store.go
git commit -m "refactor(api): update admin group handlers to use GroupRepository (#261)

Switch from GlobalGroupStore to GlobalGroupRepository with typed error
handling. Delete handler now calls auth service directly."
```

---

## Task 7: Update group member and my-group handlers

**Files:**
- Modify: `api/admin_group_member_handlers.go`
- Modify: `api/my_group_handlers.go`

- [ ] **Step 1: Update `api/admin_group_member_handlers.go`**

Add import: `"errors"`, `repository "github.com/ericfitz/tmi/auth/repository"`

**ListGroupMembers (line 29):** `GlobalGroupStore.Get` → `GlobalGroupRepository.Get`, update error handling:
```go
_, err = GlobalGroupRepository.Get(c.Request.Context(), groupUUID)
if err != nil {
    HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
    return
}
```

**ListGroupMembers (lines 83, 95):** `GlobalGroupMemberStore` → `GlobalGroupMemberRepository`

**AddGroupMember (lines 180, 208):** `GlobalGroupMemberStore` → `GlobalGroupMemberRepository`

**RemoveGroupMember (lines 258, 260):** `GlobalGroupMemberStore` → `GlobalGroupMemberRepository`

**RemoveGroupMember error handling (lines 264-284):** Replace string switch with typed errors:
```go
if err != nil {
    switch {
    case errors.Is(err, ErrGroupMemberNotFound):
        HandleRequestError(c, NotFoundError("Membership not found"))
    case errors.Is(err, ErrEveryoneGroup):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusForbidden,
            Code:    "forbidden",
            Message: "Cannot remove members from the 'everyone' pseudo-group",
        })
    default:
        logger.Error("Failed to remove group member: %v", err)
        HandleRequestError(c, ServerError("Failed to remove group member"))
    }
    return
}
```

**handleGroupMemberError (lines 297-343):** Replace entire function:
```go
func (s *Server) handleGroupMemberError(c *gin.Context, logger *slogging.ContextLogger, err error) {
    switch {
    case errors.Is(err, ErrGroupNotFound):
        HandleRequestError(c, NotFoundError("Group not found"))
    case errors.Is(err, repository.ErrUserNotFound):
        HandleRequestError(c, NotFoundError("User not found"))
    case errors.Is(err, ErrGroupMemberDuplicate):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusConflict,
            Code:    "duplicate_membership",
            Message: "Already a member of this group",
        })
    case errors.Is(err, ErrEveryoneGroup):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusForbidden,
            Code:    "forbidden",
            Message: "Cannot add members to the 'everyone' pseudo-group",
        })
    case errors.Is(err, ErrSelfMembership):
        HandleRequestError(c, &RequestError{
            Status:  http.StatusBadRequest,
            Code:    "invalid_request",
            Message: "A group cannot be a member of itself",
        })
    default:
        logger.Error("Failed to add group member: %v", err)
        HandleRequestError(c, ServerError("Failed to add group member"))
    }
}
```

- [ ] **Step 2: Update `api/my_group_handlers.go`**

**ListMyGroups (line 38):** `GlobalGroupMemberStore` → `GlobalGroupMemberRepository`

**ListMyGroupMembers (line 86):** `GlobalGroupStore.Get` → `GlobalGroupRepository.Get`, update error handling:
```go
_, err = GlobalGroupRepository.Get(c.Request.Context(), groupUUID)
if err != nil {
    HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
    return
}
```

**ListMyGroupMembers (lines 119, 174, 186):** `GlobalGroupMemberStore` → `GlobalGroupMemberRepository`

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`

- [ ] **Step 4: Commit**

```bash
git add api/admin_group_member_handlers.go api/my_group_handlers.go
git commit -m "refactor(api): update group member and my-group handlers to use repositories (#261)

Switch from GlobalGroupStore/GlobalGroupMemberStore to repository
equivalents. Rewrite handleGroupMemberError with typed error checks."
```

---

## Task 8: Update metadata handlers

**Files:**
- Modify: `api/metadata_handlers.go`

The `GenericMetadataHandler` currently holds a `MetadataStore` field and uses `*ErrMetadataKeyExists`. Update to use `MetadataRepository` and `*MetadataConflictError`.

- [ ] **Step 1: Update `api/metadata_handlers.go`**

1. Change the `GenericMetadataHandler` struct field (line 66):
```go
// Old
metadataStore   MetadataStore

// New
metadataStore   MetadataRepository
```

2. Update `NewGenericMetadataHandler` signature (line 79):
```go
// Old
func NewGenericMetadataHandler(metadataStore MetadataStore, ...) *GenericMetadataHandler

// New
func NewGenericMetadataHandler(metadataStore MetadataRepository, ...) *GenericMetadataHandler
```

3. Update conflict error checks in `Create` (line 225):
```go
// Old
var conflictErr *ErrMetadataKeyExists
if errors.As(err, &conflictErr) {

// New
var conflictErr *MetadataConflictError
if errors.As(err, &conflictErr) {
```

4. Update conflict error checks in `BulkCreate` (line 418):
```go
// Old
var conflictErr *ErrMetadataKeyExists
if errors.As(err, &conflictErr) {

// New
var conflictErr *MetadataConflictError
if errors.As(err, &conflictErr) {
```

5. Find where `NewGenericMetadataHandler` is called with `GlobalMetadataStore` and update to `GlobalMetadataRepository`. Search for all callers.

- [ ] **Step 2: Update callers of NewGenericMetadataHandler**

Search for `NewGenericMetadataHandler` and `GlobalMetadataStore` in the codebase and update all references to use `GlobalMetadataRepository`.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`

- [ ] **Step 4: Commit**

```bash
git add -u
git commit -m "refactor(api): update metadata handlers to use MetadataRepository (#261)

Switch GenericMetadataHandler from MetadataStore to MetadataRepository.
Replace *ErrMetadataKeyExists with *MetadataConflictError."
```

---

## Task 9: Update test files

**Files:**
- Modify: `api/admin_group_handlers_test.go`
- Modify: `api/my_group_handlers_test.go`
- Modify: All test files referencing `GlobalGroupMemberStore` or `GlobalMetadataStore`

Test files create mock stores and assign them to the global variables. These need to implement the new interfaces and use the new global names.

- [ ] **Step 1: Update test mock interfaces**

In each test file that references `GlobalGroupStore`:
- Change `GlobalGroupStore = groupStore` → `GlobalGroupRepository = groupStore`
- Change `origGroupStore := GlobalGroupStore` → `origGroupRepo := GlobalGroupRepository`
- Change deferred restore: `GlobalGroupStore = origGroupStore` → `GlobalGroupRepository = origGroupRepo`
- Ensure mock structs implement `GroupRepository` (not `GroupStore`)
- If the mock has a `Delete` method, remove it (no longer in the interface)

In each test file that references `GlobalGroupMemberStore`:
- Same rename pattern to `GlobalGroupMemberRepository`
- Ensure mock structs implement `GroupMemberRepository`

In each test file that references `GlobalMetadataStore`:
- Same rename pattern to `GlobalMetadataRepository`
- Ensure mock structs implement `MetadataRepository`
- Update any `*ErrMetadataKeyExists` references to `*MetadataConflictError`

For error assertions in tests:
- Replace `errors.New(ErrMsgGroupNotFound)` with `ErrGroupNotFound`
- Replace `errors.New(ErrMsgUserNotFound)` with `repository.ErrUserNotFound` (from auth/repository)
- Replace `fmt.Errorf("user is already a member of this group")` with `ErrGroupMemberDuplicate`
- Replace other string-based error returns in mocks with the corresponding typed sentinels

- [ ] **Step 2: Run unit tests to identify remaining failures**

Run: `make test-unit`
Fix any remaining compilation errors or test failures from the migration.

- [ ] **Step 3: Commit**

```bash
git add -u
git commit -m "test(api): update test mocks for repository interfaces (#261)

Rename mock store variables and interfaces to match new repository
types. Update error assertions to use typed sentinels."
```

---

## Task 10: Cleanup — remove old files and string constants

**Files:**
- Delete: `api/group_store_gorm.go`, `api/metadata_store_gorm.go`, `api/group_member_store_gorm.go`
- Delete: `api/group_store.go`, `api/metadata_store.go`
- Modify: `api/store.go` — remove old global variables
- Modify: `api/auth_utils.go` — remove `ErrMsgGroupNotFound`
- Modify: `api/request_utils.go` — remove string-fallback from `StoreErrorToRequestError`

- [ ] **Step 1: Delete old implementation files**

```bash
cd /Users/efitz/Projects/tmi
git rm api/group_store_gorm.go api/metadata_store_gorm.go api/group_member_store_gorm.go
```

- [ ] **Step 2: Delete old interface files**

```bash
git rm api/group_store.go api/metadata_store.go
```

Note: `group_store.go` contained `GroupStore`, `GroupMemberStore` interfaces, `Group`, `GroupFilter`, `GroupMemberFilter`, `GroupDeletionStats` types, and the `GlobalGroupStore`, `GlobalGroupMemberStore` globals. The types (`Group`, `GroupFilter`, etc.) must be preserved somewhere. Since they're used by both the repository interfaces and handlers, they should be in a standalone types file or in `repository_interfaces.go`. Add them to `repository_interfaces.go` if they aren't already defined elsewhere.

**IMPORTANT:** Before deleting, verify which types from `group_store.go` are NOT already defined in `repository_interfaces.go`:
- `GroupDeletionStats` — needed by delete handler, add to `repository_interfaces.go` or a separate file
- `Group` struct — needed everywhere, add to `repository_interfaces.go`
- `GroupFilter` struct — needed by GroupRepository, add to `repository_interfaces.go`
- `GroupMemberFilter` — needed by GroupMemberRepository, add to `repository_interfaces.go`
- `GroupMember` struct — check where it's defined (likely in api.go generated types)

Move these types to `repository_interfaces.go` before deleting the old files.

Similarly, `metadata_store.go` contained:
- `ErrMetadataKeyExists` struct — replaced by `MetadataConflictError` in `repository_interfaces.go`
- `MetadataStore` interface — replaced by `MetadataRepository` in `repository_interfaces.go`
- The `Metadata` type is likely defined elsewhere (api.go generated types)

- [ ] **Step 3: Remove old globals from `api/store.go`**

Remove these lines:
```go
var GlobalMetadataStore MetadataStore   // line 93 — already replaced
var GlobalGroupStore GroupStore         // (in group_store.go, now deleted)
var GlobalGroupMemberStore GroupMemberStore  // (in group_store.go, now deleted)
```

If the old globals were in the now-deleted files, they're already gone. If `GlobalMetadataStore` was in `store.go`, remove it from there.

- [ ] **Step 4: Remove `ErrMsgGroupNotFound` from `api/auth_utils.go`**

In `api/auth_utils.go` line 484, remove `ErrMsgGroupNotFound = "group not found"`. Keep `ErrMsgUserNotFound` — it's still used by `user_store_gorm.go` and `admin_user_handlers.go`.

- [ ] **Step 5: Remove string fallback from `StoreErrorToRequestError`**

In `api/request_utils.go` lines 561-569, remove the string fallback block:
```go
// Remove this block:
// String fallback for GORM stores not yet migrated to dberrors (#261)
errMsg := strings.ToLower(err.Error())
if strings.Contains(errMsg, "not found") {
    return NotFoundError(notFoundMsg)
}
if strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "validation") {
    return InvalidInputError(err.Error())
}
```

Keep the final `return ServerError(serverErrorMsg)`.

- [ ] **Step 6: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./api/...`
Fix any missing type references.

- [ ] **Step 7: Run full test suite**

Run: `make test-unit`

- [ ] **Step 8: Commit**

```bash
git add -u
git commit -m "refactor(api): remove legacy store files and string-based error handling (#261)

Delete group_store_gorm.go, metadata_store_gorm.go, group_member_store_gorm.go,
group_store.go, metadata_store.go. Remove ErrMsgGroupNotFound constant and
string fallback from StoreErrorToRequestError."
```

---

## Task 11: Lint, build, and integration test

**Files:** None (verification only)

- [ ] **Step 1: Run linter**

Run: `make lint`
Fix any issues.

- [ ] **Step 2: Run build**

Run: `make build-server`

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
All tests must pass.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
All tests must pass. This validates that `dberrors.Classify()` correctly handles real PostgreSQL error codes.

- [ ] **Step 5: Final commit if any fixes were needed**

```bash
git add -u
git commit -m "fix(api): address lint and test issues from repository migration (#261)"
```
