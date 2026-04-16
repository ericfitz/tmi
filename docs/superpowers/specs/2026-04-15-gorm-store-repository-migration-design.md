# GORM Store to Repository Pattern Migration

**Issue:** #261  
**Date:** 2026-04-15  
**Depends on:** #258 (typed errors and retry infrastructure) — completed  
**Approach:** Big-bang migration of all three stores in a single pass

## Summary

Migrate three legacy GORM store files in `api/` to the repository pattern established by #258 in `auth/repository/`. Replace all string-based error matching with typed errors from `internal/dberrors/`, add `WithRetryableGormTransaction` retry logic to write operations, rename interfaces and globals for consistency, and update all consuming handlers.

## Files In Scope

### Store files to migrate

| File | Lines | Methods | Complexity |
|------|-------|---------|------------|
| `api/group_store_gorm.go` | ~358 | 9 public + 2 helpers | Low — standard CRUD |
| `api/metadata_store_gorm.go` | ~669 | 17 public + 1 helper | High — cache integration, bulk ops, mutex |
| `api/group_member_store_gorm.go` | ~609 | 15 public + 1 helper | Medium — complex JOINs, business logic |

### Handler files to update

| File | Current pattern |
|------|----------------|
| `api/admin_group_handlers.go` | `err.Error() == ErrMsgGroupNotFound` string matching |
| `api/admin_group_member_handlers.go` | `err.Error()` switch statement in `handleGroupMemberError` |
| `api/my_group_handlers.go` | `err.Error() == ErrMsgGroupNotFound` string matching |
| `api/metadata_handlers.go` | `*ErrMetadataKeyExists` type assertion |

### Files to remove after migration

- `api/group_store_gorm.go`
- `api/metadata_store_gorm.go`
- `api/group_member_store_gorm.go`
- `api/group_store.go` (interface definitions move to repository)
- `api/metadata_store.go` (interface definitions move to repository)

## Package Structure

Repository files stay in the `api` package (not a sub-package).

```
api/
├── repository_interfaces.go          # Interface definitions + typed error sentinels
├── group_repository.go               # GroupRepository implementation (replaces group_store_gorm.go)
├── group_repository_test.go
├── metadata_repository.go            # MetadataRepository implementation (replaces metadata_store_gorm.go)
├── metadata_repository_test.go
├── group_member_repository.go        # GroupMemberRepository implementation (replaces group_member_store_gorm.go)
└── group_member_repository_test.go
```

**Why not `api/repository/` sub-package?** The API domain types (`Group`, `GroupFilter`, `Metadata`, `GroupMember`, etc.) are defined in the `api` package. Creating `api/repository/` would cause a circular import: `api/repository/` needs `api` for types, and `api` needs `api/repository/` for constructors in `InitializeGormStores`. The `auth/repository/` pattern avoids this because its domain types are defined within the repository package itself. Moving API types to a shared package would be a larger refactor beyond #261's scope.

Keeping files in `api/` still achieves all goals: typed errors, retry logic, `dberrors.Classify()`, and interface renames. The naming convention (`*_repository.go` vs `*_store_gorm.go`) clearly distinguishes the new pattern.

## Typed Error Sentinels

Defined in `api/repository_interfaces.go`, following the pattern from `auth/repository/interfaces.go`:

```go
var (
    // Not-found errors — wrap dberrors.ErrNotFound
    ErrGroupNotFound        = fmt.Errorf("group: %w", dberrors.ErrNotFound)
    ErrMetadataNotFound     = fmt.Errorf("metadata: %w", dberrors.ErrNotFound)
    ErrGroupMemberNotFound  = fmt.Errorf("group member: %w", dberrors.ErrNotFound)
    // Duplicate errors — wrap dberrors.ErrDuplicate
    ErrGroupDuplicate       = fmt.Errorf("group: %w", dberrors.ErrDuplicate)
    ErrGroupMemberDuplicate = fmt.Errorf("group member: %w", dberrors.ErrDuplicate)
    ErrMetadataKeyExists    = fmt.Errorf("metadata: %w", dberrors.ErrDuplicate)

    // Business logic errors — plain errors, not DB errors
    ErrSelfMembership = errors.New("a group cannot be a member of itself")
    ErrEveryoneGroup  = errors.New("cannot modify the everyone group")
)

// Note: For "user not found" errors in GroupMemberRepository, import and use
// auth/repository.ErrUserNotFound (already defined in auth/repository/interfaces.go)
// rather than redefining the same conceptual error here.
```

All sentinels support both specific and generic error checking:
- `errors.Is(err, ErrGroupNotFound)` — specific entity check
- `errors.Is(err, dberrors.ErrNotFound)` — generic category check

### MetadataConflictError

The current `*ErrMetadataKeyExists` struct carries `ConflictingKeys []string`. The new design preserves this data via a struct that wraps the sentinel:

```go
type MetadataConflictError struct {
    ConflictingKeys []string
}

func (e *MetadataConflictError) Error() string {
    return fmt.Sprintf("metadata key(s) already exist: %s", strings.Join(e.ConflictingKeys, ", "))
}

func (e *MetadataConflictError) Unwrap() error {
    return ErrMetadataKeyExists
}
```

This enables:
- `errors.Is(err, ErrMetadataKeyExists)` — detect conflict
- `errors.As(err, &conflictErr)` — extract conflicting keys
- `errors.Is(err, dberrors.ErrDuplicate)` — generic duplicate check (via unwrap chain)

## Interface Definitions

Interfaces move from `api/group_store.go` and `api/metadata_store.go` to `api/repository_interfaces.go`. Names change from `*Store` to `*Repository`:

### GroupRepository

Same methods as current `GroupStore`, minus `Delete`:

- `List(ctx, filter) ([]Group, error)`
- `Get(ctx, internalUUID) (*Group, error)`
- `GetByProviderAndName(ctx, provider, groupName) (*Group, error)`
- `Create(ctx, group) error`
- `Update(ctx, group) error`
- `Count(ctx, filter) (int, error)`
- `EnrichGroups(ctx, groups) ([]Group, error)`
- `GetGroupsForProvider(ctx, provider) ([]Group, error)`

`Delete` is excluded because the current implementation delegates to `auth.Service.DeleteGroupAndData()` — an orchestration concern, not a repository concern. After migration, the delete handler calls `auth.Service.DeleteGroupAndData()` directly.

### MetadataRepository

Same methods as current `MetadataStore`:

- `Create`, `Get`, `Update`, `Delete`, `List`, `Post`
- `BulkCreate`, `BulkUpdate`, `BulkReplace`, `BulkDelete`
- `GetByKey`, `ListKeys`
- `InvalidateCache`, `WarmCache`

### GroupMemberRepository

Same methods as current `GroupMemberStore`:

- `ListMembers`, `CountMembers`
- `AddMember`, `RemoveMember`
- `AddGroupMember`, `RemoveGroupMember`
- `IsMember`, `IsEffectiveMember`, `HasAnyMembers`
- `GetGroupsForUser`

## Repository Implementations

### Constructor pattern

Matches `auth/repository/`:

```go
func NewGormGroupRepository(db *gorm.DB) *GormGroupRepository {
    return &GormGroupRepository{db: db, logger: slogging.Get()}
}
```

MetadataRepository additionally takes `cache` and `invalidator` parameters (see Cache Integration section).

### Error handling pattern

Every method follows:

1. Execute GORM operation
2. If error, classify immediately via `dberrors.Classify(result.Error)`
3. Return the appropriate typed sentinel

```go
func (r *GormGroupRepository) Get(ctx context.Context, internalUUID uuid.UUID) (*Group, error) {
    var group models.Group
    result := r.db.WithContext(ctx).Where(...).First(&group)
    if result.Error != nil {
        if errors.Is(result.Error, gorm.ErrRecordNotFound) {
            return nil, ErrGroupNotFound
        }
        return nil, dberrors.Classify(result.Error)
    }
    return convertGroup(&group), nil
}
```

No string matching in any repository method. All database-specific error detection is handled by `dberrors.Classify()` which inspects typed driver errors (`pgconn.PgError`, `godror.OraErr`) before falling back to minimal string patterns.

### Retry wrapping

Operations wrapped in `WithRetryableGormTransaction` (from `auth/db/retry.go`):

**Wrapped (single writes):**
- `GroupRepository`: `Create`, `Update`
- `MetadataRepository`: `Create`, `Update`, `Delete`, `Post`, `BulkCreate`, `BulkUpdate`, `BulkReplace`, `BulkDelete`
- `GroupMemberRepository`: `AddMember`, `RemoveMember`, `AddGroupMember`, `RemoveGroupMember`

**Not wrapped (reads):**
- All `List`, `Get`, `Count`, `IsMember`, `IsEffectiveMember`, `HasAnyMembers`, `GetGroupsForUser`, `EnrichGroups`, `GetGroupsForProvider`, `GetByKey`, `ListKeys`

Read failures are classified via `dberrors.Classify()` and bubble up for higher-level retry if needed.

Default retry config: 3 max retries, 100ms base delay, 5s max delay (exponential backoff).

### Preserved behaviors

The following existing behaviors are preserved without change:

- **Oracle column name handling**: `Col()` and `ColumnName()` functions for uppercase column names
- **Cross-database LIKE queries**: `clause.Expr` for `LOWER() LIKE` patterns
- **Model conversion**: Between `models.Group`/`models.Metadata`/`models.GroupMember` and API types
- **Complex raw queries**: LEFT JOIN queries in `GroupMemberRepository.ListMembers` with `memberRow` struct scanner
- **Oracle NULL handling**: `normalizeNullString()` helper for godror driver
- **Email validation**: `safeEmail()` wrapper for JSON marshaling
- **UUID generation**: `uuidgen.MustNewForEntity()` for metadata IDs

## Cache Integration (MetadataRepository)

Cache integration stays inline in the repository — not extracted to a separate layer. This is a scope decision: extracting caching is a meaningful architectural change that goes beyond #261's goals and risks introducing subtle bugs in cache invalidation timing.

The `MetadataRepository` constructor takes:
- `db *gorm.DB`
- `cache *CacheService`
- `invalidator *CacheInvalidator`

Preserved cache behaviors:
- `sync.RWMutex` for cache safety
- Cache check → DB fallback on reads (`Get`, `List`)
- `CacheInvalidator` calls after writes
- `InvalidateCache` and `WarmCache` methods on the interface

## Global Variables & Wiring

### Renames in `api/store.go`

```go
// Old
var GlobalGroupStore GroupStore
var GlobalMetadataStore MetadataStore
var GlobalGroupMemberStore GroupMemberStore

// New
var GlobalGroupRepository GroupRepository
var GlobalMetadataRepository MetadataRepository
var GlobalGroupMemberRepository GroupMemberRepository
```

### Initialization in `InitializeGormStores`

```go
// Old
GlobalGroupStore = NewGormGroupStore(db, svc.GetService())
GlobalMetadataStore = NewGormMetadataStore(db, cache, invalidator)
GlobalGroupMemberStore = NewGormGroupMemberStore(db)

// New
GlobalGroupRepository = NewGormGroupRepository(db)
GlobalMetadataRepository = NewGormMetadataRepository(db, cache, invalidator)
GlobalGroupMemberRepository = NewGormGroupMemberRepository(db)
```

`GroupRepository` no longer needs the auth service — that dependency was only for the `Delete` method.

### Group Delete handling

The delete handler in `admin_group_handlers.go` currently calls `GlobalGroupStore.Delete()` which delegates to `auth.Service.DeleteGroupAndData()`. After migration, the handler calls `auth.Service.DeleteGroupAndData()` directly via the server's existing auth service reference.

## Handler Migration

### Error handling pattern

Handlers switch from string matching to the existing `StoreErrorToRequestError()` utility, which already supports typed dberrors:

```go
// Old — string matching
group, err := GlobalGroupStore.Get(ctx, id)
if err != nil {
    if err.Error() == ErrMsgGroupNotFound {
        HandleRequestError(c, NotFoundError("Group not found"))
    } else {
        HandleRequestError(c, ServerError("Failed to get group"))
    }
    return
}

// New — typed errors via existing utility
group, err := GlobalGroupRepository.Get(ctx, id)
if err != nil {
    HandleRequestError(c, StoreErrorToRequestError(err, "Group not found", "Failed to get group"))
    return
}
```

### handleGroupMemberError migration

The `handleGroupMemberError` switch statement in `admin_group_member_handlers.go` currently switches on `err.Error()` string values. After migration:

```go
func (s *Server) handleGroupMemberError(c *gin.Context, logger *slogging.ContextLogger, err error) {
    switch {
    case errors.Is(err, ErrGroupNotFound):
        HandleRequestError(c, NotFoundError("Group not found"))
    case errors.Is(err, repository.ErrUserNotFound):  // from auth/repository
        HandleRequestError(c, NotFoundError("User not found"))
    case errors.Is(err, ErrGroupMemberDuplicate):
        HandleRequestError(c, ConflictError("Already a member of this group"))
    case errors.Is(err, ErrSelfMembership):
        HandleRequestError(c, InvalidInputError("A group cannot be a member of itself"))
    case errors.Is(err, ErrEveryoneGroup):
        HandleRequestError(c, InvalidInputError("Cannot modify the everyone group"))
    default:
        HandleRequestError(c, StoreErrorToRequestError(err, "Not found", "Operation failed"))
    }
}
```

### Metadata handler migration

`GenericMetadataHandler` switches from `*ErrMetadataKeyExists` type assertion to:
- `errors.Is(err, ErrMetadataKeyExists)` for simple conflict detection
- `errors.As(err, &conflictErr)` with `*MetadataConflictError` to extract conflicting keys

## Cleanup

After migration is complete and tests pass:

1. **Remove old store implementation files:**
   - `api/group_store_gorm.go`
   - `api/metadata_store_gorm.go`
   - `api/group_member_store_gorm.go`

2. **Remove old interface files** (definitions moved to `api/repository_interfaces.go`):
   - `api/group_store.go`
   - `api/metadata_store.go`

3. **Remove string error constants** from `api/auth_utils.go`:
   - `ErrMsgGroupNotFound` (verify no remaining consumers)
   - `ErrMsgUserNotFound` (verify — may still be used by `user_store_gorm.go` or `admin_user_handlers.go`)

4. **Remove string-fallback branch** from `StoreErrorToRequestError()` in `api/request_utils.go` (the `strings.Contains` fallback block, which was explicitly marked as temporary for #261)

5. **Remove old `ErrMetadataKeyExists` struct** from `api/metadata_store.go` (replaced by `MetadataConflictError`)

## Testing Strategy

### Unit tests

New test files in `api/` for each repository:

- Verify each method returns correct typed error sentinels for each failure mode:
  - Not found → entity-specific `ErrXxxNotFound`
  - Duplicate → entity-specific `ErrXxxDuplicate`
  - Business logic → `ErrSelfMembership`, `ErrEveryoneGroup`
- Verify unwrap chains: `errors.Is(err, dberrors.ErrNotFound)` works on entity-specific sentinels
- Verify `MetadataConflictError` carries `ConflictingKeys` and unwraps correctly
- Verify retry wrapping: transient errors trigger retry, non-transient errors return immediately

### Handler tests

Existing handler tests should continue to pass — HTTP behavior is unchanged. Any tests that assert on specific error message strings from stores will need updating to match the new sentinel-based flow.

### Integration tests

`make test-integration` validates the full stack with a real PostgreSQL database, confirming that `dberrors.Classify()` correctly handles actual PostgreSQL error codes.

No new handler test files — existing coverage validates the migration.
