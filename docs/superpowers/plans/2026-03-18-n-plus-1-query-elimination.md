# N+1 Query Elimination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate N+1 query patterns in threat model store operations, reducing Heroku page load times from 10-20s to 1-2s.

**Architecture:** Two-phase approach. Phase 1 optimizes the list endpoint and middleware with batch queries and auth caching (zero API changes). Phase 2 eliminates N+1 patterns in the Get path with batch loading and response caching. All batch queries use `WHERE ... IN (?)` for Oracle compatibility.

**Tech Stack:** Go, GORM, Redis, Gin, PostgreSQL (Oracle-compatible)

**Spec:** `docs/superpowers/specs/2026-03-18-n-plus-1-query-elimination-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `api/store.go` | Modify | Add `GetAuthorization`, `GetAuthorizationIncludingDeleted`, `GetBatch` to interfaces |
| `api/database_store_gorm.go` | Modify | Batch methods: `loadAuthorizationBatch`, `batchCounts`, `convertToListItem`, `GetAuthorization`, `GetBatch` |
| `api/middleware.go` | Modify | Lightweight auth-only middleware query with caching |
| `api/cache_service.go` | Modify | Add `CacheThreatModel`, `GetCachedThreatModel`, `InvalidateThreatModel` methods |
| `api/cache_invalidation.go` | Modify | Add threat model response cache invalidation to `invalidateImmediately` |
| `api/threat_model_handlers.go` | Modify | Response cache invalidation on writes, refactored list filter |
| `api/threat_sub_resource_handlers.go` | Modify | Response cache invalidation on sub-resource writes |
| `api/document_sub_resource_handlers.go` | Modify | Response cache invalidation on sub-resource writes |
| `api/note_sub_resource_handlers.go` | Modify | Response cache invalidation on sub-resource writes |
| `api/asset_sub_resource_handlers.go` | Modify | Response cache invalidation on sub-resource writes |
| `api/repository_sub_resource_handlers.go` | Modify | Response cache invalidation on sub-resource writes |
| `api/threat_model_diagram_handlers.go` | Modify | Response cache invalidation on sub-resource writes |

---

## Important Notes

**Spec deviations (intentional simplifications):**
- The spec defines `GetOwnerForThreatModel` and `GetAuthorizationCached` as separate interface methods. This plan bundles the owner into the `GetAuthorization` return tuple `([]Authorization, User, error)` and handles caching in the middleware layer rather than the store interface. This avoids adding methods that would only be called from one place.
- There are no separate in-memory store implementations — only GORM stores. New interface methods only need GORM implementations.

**Global CacheService access:** Handlers use dependency-injected `cache *CacheService` fields. The middleware uses global store variables. For middleware cache access, add a package-level variable `var GlobalCacheService *CacheService` in `api/cache_service.go`, set during server initialization alongside the existing store globals.

**Type reference (TMListItem from api/api.go):**
- `TMListItem.Description` is `*string`
- `TMListItem.ThreatModelFramework` is `string` (not pointer)
- `TMListItem.CreatedAt` / `ModifiedAt` are `time.Time` (not pointers)
- `models.ThreatModel.Description` is `*string`

**Cache invalidation parent type:** Use `string(CreateAddonRequestObjectsThreatModel)` (evaluates to `"threat_model"`) — matches existing pattern in `cache_invalidation.go`.

---

## Phase 1: Zero-API-Change Backend Optimizations

### Task 1: Batch User/Group Lookup in `loadAuthorization`

**Files:**
- Modify: `api/database_store_gorm.go:698-749`

This is the foundation — it refactors the N+1 user/group resolution into batch queries that other tasks build on.

- [ ] **Step 1: Write the batch user/group resolution helper**

Add a private helper `resolveUsersAndGroupsBatch` that takes slices of UUIDs and returns lookup maps.

In `api/database_store_gorm.go`, add after the existing `loadAuthorization` method (after line 749):

```go
// resolveUsersAndGroupsBatch loads users and groups by internal UUIDs in batch.
// Returns lookup maps keyed by internal_uuid. Oracle-compatible (chunks IN clauses at 999).
func (s *GormThreatModelStore) resolveUsersAndGroupsBatch(userUUIDs, groupUUIDs []string) (map[string]models.User, map[string]models.Group) {
	userMap := make(map[string]models.User, len(userUUIDs))
	groupMap := make(map[string]models.Group, len(groupUUIDs))

	if len(userUUIDs) > 0 {
		for _, chunk := range chunkStrings(userUUIDs, 999) {
			var users []models.User
			s.db.Where("internal_uuid IN ?", chunk).Find(&users)
			for _, u := range users {
				userMap[u.InternalUUID] = u
			}
		}
	}

	if len(groupUUIDs) > 0 {
		for _, chunk := range chunkStrings(groupUUIDs, 999) {
			var groups []models.Group
			s.db.Where("internal_uuid IN ?", chunk).Find(&groups)
			for _, g := range groups {
				groupMap[g.InternalUUID] = g
			}
		}
	}

	return userMap, groupMap
}

// chunkStrings splits a slice into chunks of at most size n.
func chunkStrings(s []string, n int) [][]string {
	if len(s) <= n {
		return [][]string{s}
	}
	var chunks [][]string
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}
```

- [ ] **Step 2: Refactor `loadAuthorization` to use batch lookup**

Replace the per-entry user/group queries in `loadAuthorization` (lines 698-749) with batch resolution:

```go
func (s *GormThreatModelStore) loadAuthorization(threatModelID string) ([]Authorization, error) {
	logger := slogging.Get()
	var accessEntries []models.ThreatModelAccess
	result := s.db.Where("threat_model_id = ?", threatModelID).
		Order("role DESC").
		Find(&accessEntries)
	if result.Error != nil {
		return nil, result.Error
	}

	logger.Debug("[GORM-STORE] loadAuthorization: Found %d access entries for threat model %s", len(accessEntries), threatModelID)

	// Collect unique UUIDs for batch resolution
	userUUIDSet := make(map[string]bool)
	groupUUIDSet := make(map[string]bool)
	for _, entry := range accessEntries {
		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			userUUIDSet[*entry.UserInternalUUID] = true
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			groupUUIDSet[*entry.GroupInternalUUID] = true
		}
	}

	userUUIDs := make([]string, 0, len(userUUIDSet))
	for uuid := range userUUIDSet {
		userUUIDs = append(userUUIDs, uuid)
	}
	groupUUIDs := make([]string, 0, len(groupUUIDSet))
	for uuid := range groupUUIDSet {
		groupUUIDs = append(groupUUIDs, uuid)
	}

	userMap, groupMap := s.resolveUsersAndGroupsBatch(userUUIDs, groupUUIDs)

	// Build authorization entries from maps
	authorization := []Authorization{}
	for i, entry := range accessEntries {
		role := AuthorizationRole(entry.Role)
		logger.Debug("[GORM-STORE] loadAuthorization: Entry %d - SubjectType=%s, UserUUID=%v, GroupUUID=%v, Role=%s",
			i, entry.SubjectType, entry.UserInternalUUID, entry.GroupInternalUUID, entry.Role)

		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			if user, ok := userMap[*entry.UserInternalUUID]; ok {
				auth := Authorization{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      user.Provider,
					ProviderId:    strFromPtr(user.ProviderUserID),
					DisplayName:   &user.Name,
					Email:         (*openapi_types.Email)(&user.Email),
					Role:          role,
				}
				authorization = append(authorization, auth)
			}
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			if group, ok := groupMap[*entry.GroupInternalUUID]; ok {
				auth := Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      group.Provider,
					ProviderId:    group.GroupName,
					DisplayName:   group.Name,
					Role:          role,
				}
				authorization = append(authorization, auth)
			}
		}
	}

	return authorization, nil
}
```

- [ ] **Step 3: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: All existing tests pass — this is a pure internal refactor with identical behavior.

- [ ] **Step 4: Commit**

```bash
git add api/database_store_gorm.go
git commit -m "refactor(api): batch user/group lookup in loadAuthorization

Replace per-entry DB queries with batch IN queries for Oracle-compatible
N+1 elimination. Adds chunkStrings helper for Oracle 1000-element limit.

Refs #190"
```

---

### Task 2: `GetAuthorization` Interface Method and GORM Implementation

**Files:**
- Modify: `api/store.go:61-74`
- Modify: `api/database_store_gorm.go`

Add lightweight authorization-only query to the store interface.

- [ ] **Step 1: Add `GetAuthorization` and `GetAuthorizationIncludingDeleted` to interface**

In `api/store.go`, add to `ThreatModelStoreInterface` (after `GetIncludingDeleted`):

```go
GetAuthorization(id string) ([]Authorization, User, error)
GetAuthorizationIncludingDeleted(id string) ([]Authorization, User, error)
```

Note: Returns `([]Authorization, User, error)` — the `User` is the owner, needed by `GetUserRole` to build `AuthorizationData`.

- [ ] **Step 2: Implement on `GormThreatModelStore`**

In `api/database_store_gorm.go`, add after the `Get` method:

```go
// GetAuthorization loads only authorization entries and owner for a threat model.
// Used by middleware to check access without loading the full model.
func (s *GormThreatModelStore) GetAuthorization(id string) ([]Authorization, User, error) {
	return s.getAuthorizationInternal(id, false)
}

// GetAuthorizationIncludingDeleted loads authorization for a potentially soft-deleted threat model.
func (s *GormThreatModelStore) GetAuthorizationIncludingDeleted(id string) ([]Authorization, User, error) {
	return s.getAuthorizationInternal(id, true)
}

func (s *GormThreatModelStore) getAuthorizationInternal(id string, includeDeleted bool) ([]Authorization, User, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if _, err := uuid.Parse(id); err != nil {
		return nil, User{}, fmt.Errorf("invalid UUID format: %w", err)
	}

	// Verify threat model exists and load owner
	var tm models.ThreatModel
	query := s.db.Preload("Owner").Select("id, owner_internal_uuid").Where("id = ?", id)
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.First(&tm).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, User{}, fmt.Errorf("threat model with ID %s not found", id)
		}
		return nil, User{}, fmt.Errorf("failed to get threat model: %w", err)
	}

	owner := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      tm.Owner.Provider,
		ProviderId:    strFromPtr(tm.Owner.ProviderUserID),
		DisplayName:   tm.Owner.Name,
		Email:         openapi_types.Email(tm.Owner.Email),
	}

	authorization, err := s.loadAuthorization(id)
	if err != nil {
		return nil, User{}, fmt.Errorf("failed to load authorization: %w", err)
	}

	return authorization, owner, nil
}
```

- [ ] **Step 3: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS — new methods added, no callers yet.

- [ ] **Step 4: Commit**

```bash
git add api/store.go api/database_store_gorm.go
git commit -m "feat(api): add GetAuthorization to ThreatModelStoreInterface

Lightweight query loads only authorization entries and owner for access
checks without loading full threat model with all sub-resources.

Refs #190"
```

---

### Task 3: Lightweight Middleware Authorization with Caching

**Files:**
- Modify: `api/middleware.go:310-402`
- Modify: `api/cache_service.go`

Replace full `ThreatModelStore.Get(id)` in middleware with `GetAuthorization` + existing Redis caching.

- [ ] **Step 1: Add GlobalCacheService variable**

In `api/cache_service.go`, add a package-level variable:

```go
// GlobalCacheService is the package-level cache service instance, set during server initialization.
// Used by middleware and store methods that don't have dependency-injected cache references.
// Nil-safe: all callers check for nil before use.
var GlobalCacheService *CacheService
```

Set this during server initialization (in the same place where `ThreatModelStore`, `DiagramStore`, etc. are set — likely in `api/store.go:InitializeGormStores` or `cmd/server/main.go`). Find where `NewCacheService` is called and add `GlobalCacheService = cacheService` after it.

- [ ] **Step 2: Add middleware auth caching helper**

The cache service already has `CacheAuthData`/`GetCachedAuthData`/`InvalidateAuthData` (lines 368-521 of `cache_service.go`). However, these cache `AuthorizationData` which doesn't include the owner's full `User` data needed for `GetUserRole`. Add a new struct and cache methods to `api/cache_service.go`:

```go
// MiddlewareAuthData holds authorization data needed by middleware.
// Cached separately from full threat model to avoid loading sub-resources.
type MiddlewareAuthData struct {
	Owner         User            `json:"owner"`
	Authorization []Authorization `json:"authorization"`
}

// CacheMiddlewareAuth caches lightweight auth data for middleware
func (cs *CacheService) CacheMiddlewareAuth(ctx context.Context, threatModelID string, data MiddlewareAuthData) error {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"

	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Error("Failed to marshal middleware auth data: %v", err)
		return fmt.Errorf("failed to marshal middleware auth data: %w", err)
	}

	err = cs.redis.Set(ctx, key, jsonData, AuthCacheTTL)
	if err != nil {
		logger.Error("Failed to cache middleware auth data for %s: %v", threatModelID, err)
		return fmt.Errorf("failed to cache middleware auth data: %w", err)
	}

	logger.Debug("Cached middleware auth data for %s with TTL %v", threatModelID, AuthCacheTTL)
	return nil
}

// GetCachedMiddlewareAuth retrieves cached middleware auth data
func (cs *CacheService) GetCachedMiddlewareAuth(ctx context.Context, threatModelID string) (*MiddlewareAuthData, error) {
	logger := slogging.Get()
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Cache miss
		}
		return nil, fmt.Errorf("failed to get cached middleware auth data: %w", err)
	}

	var authData MiddlewareAuthData
	if err := json.Unmarshal([]byte(data), &authData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached middleware auth data: %w", err)
	}
	return &authData, nil
}

// InvalidateMiddlewareAuth invalidates middleware auth cache for a threat model
func (cs *CacheService) InvalidateMiddlewareAuth(ctx context.Context, threatModelID string) error {
	key := cs.builder.CacheAuthKey(threatModelID) + ":mw"
	return cs.redis.Del(ctx, key)
}
```

- [ ] **Step 3: Refactor ThreatModelMiddleware**

In `api/middleware.go`, replace the full model load (lines ~322-338) with lightweight auth:

Replace:
```go
var threatModel ThreatModel
var err error
if isRestoreRoute {
    threatModel, err = ThreatModelStore.GetIncludingDeleted(id)
} else {
    threatModel, err = ThreatModelStore.Get(id)
}
```

With:
```go
var owner User
var authorization []Authorization
var err error

// Try cache first (non-restore routes only)
if !isRestoreRoute && GlobalCacheService != nil {
    cached, cacheErr := GlobalCacheService.GetCachedMiddlewareAuth(c.Request.Context(), id)
    if cacheErr == nil && cached != nil {
        owner = cached.Owner
        authorization = cached.Authorization
        logger.Debug("ThreatModelMiddleware cache hit for %s", id)
    }
}

// Cache miss or restore route — load from store
if authorization == nil {
    if isRestoreRoute {
        authorization, owner, err = ThreatModelStore.GetAuthorizationIncludingDeleted(id)
    } else {
        authorization, owner, err = ThreatModelStore.GetAuthorization(id)
    }
    if err != nil {
        logger.Debug("Threat model not found: %s, error: %v", id, err)
        c.AbortWithStatusJSON(http.StatusNotFound, Error{
            Error:            "not_found",
            ErrorDescription: "Threat model not found",
        })
        return
    }

    // Cache on miss (non-restore only)
    if !isRestoreRoute && GlobalCacheService != nil {
        _ = GlobalCacheService.CacheMiddlewareAuth(c.Request.Context(), id, MiddlewareAuthData{
            Owner:         owner,
            Authorization: authorization,
        })
    }
}
```

Then replace the `GetUserRole` call (line ~394) and the `c.Set("threatModel", ...)` (line ~397):

Replace:
```go
userRole := GetUserRole(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModel)
c.Set("userRole", userRole)
c.Set("threatModel", threatModel)
```

With:
```go
// Build auth data from lightweight query
authData := AuthorizationData{
    Type:          AuthTypeTMI10,
    Owner:         owner,
    Authorization: authorization,
}
// Determine user role using same logic as GetUserRole but with lightweight data
var userRole Role
if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleOwner, authData) {
    userRole = RoleOwner
} else if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleWriter, authData) {
    userRole = RoleWriter
} else if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleReader, authData) {
    userRole = RoleReader
}
c.Set("userRole", userRole)
// Note: "threatModel" is no longer set in context. Handlers that need the full
// model (PatchThreatModel, UpdateThreatModel) call ThreatModelStore.Get() directly
// via getExistingThreatModel(), which already has a fallback path for this case.
```

- [ ] **Step 4: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS. Verify `getExistingThreatModel` fallback works (it already calls `ThreatModelStore.Get(id)` when context miss).

- [ ] **Step 5: Run integration tests**

Run: `make test-integration`
Expected: PASS — middleware behavior is identical, just fewer DB queries.

- [ ] **Step 6: Commit**

```bash
git add api/middleware.go api/cache_service.go
git commit -m "perf(api): lightweight auth-only middleware with Redis caching

Replace full ThreatModelStore.Get() in ThreatModelMiddleware with
GetAuthorization() that loads only access entries and owner. Cache in
Redis with 15-minute TTL. Eliminates ~20-30 DB queries per sub-resource
request.

Refs #190"
```

---

### Task 4: `convertToListItem` and Batch Counts for List Endpoint

**Files:**
- Modify: `api/database_store_gorm.go:297-402`
- Modify: `api/threat_model_handlers.go:72-90`

Replace full `convertToAPIModel` + per-item COUNT queries in list path.

- [ ] **Step 1: Add `entityCounts` struct and `batchCounts` method**

In `api/database_store_gorm.go`, add after the `calculateCount` method (after line 402):

```go
// entityCounts holds count data for a single threat model
type entityCounts struct {
	DocumentCount int
	SourceCount   int
	DiagramCount  int
	ThreatCount   int
	NoteCount     int
	AssetCount    int
}

// batchCounts loads counts for multiple threat models in 6 batch queries (one per table).
func (s *GormThreatModelStore) batchCounts(ids []string) map[string]entityCounts {
	result := make(map[string]entityCounts, len(ids))
	if len(ids) == 0 {
		return result
	}

	tables := []struct {
		name  string
		field string // which field in entityCounts to set
	}{
		{"documents", "document"},
		{"repositories", "source"},
		{"diagrams", "diagram"},
		{"threats", "threat"},
		{"notes", "note"},
		{"assets", "asset"},
	}

	type countRow struct {
		ThreatModelID string
		Count         int64
	}

	for _, t := range tables {
		for _, chunk := range chunkStrings(ids, 999) {
			var rows []countRow
			s.db.Table(t.name).
				Select("threat_model_id, COUNT(*) as count").
				Where("threat_model_id IN ? AND deleted_at IS NULL", chunk).
				Group("threat_model_id").
				Find(&rows)

			for _, row := range rows {
				ec := result[row.ThreatModelID]
				switch t.field {
				case "document":
					ec.DocumentCount = int(row.Count)
				case "source":
					ec.SourceCount = int(row.Count)
				case "diagram":
					ec.DiagramCount = int(row.Count)
				case "threat":
					ec.ThreatCount = int(row.Count)
				case "note":
					ec.NoteCount = int(row.Count)
				case "asset":
					ec.AssetCount = int(row.Count)
				}
				result[row.ThreatModelID] = ec
			}
		}
	}

	return result
}
```

- [ ] **Step 2: Add `convertToListItem` method**

In `api/database_store_gorm.go`, add after `convertToAPIModel`:

```go
// convertToListItem converts a GORM ThreatModel to a lightweight list item.
// No sub-resource queries — only uses preloaded Owner, CreatedBy, SecurityReviewer.
func (s *GormThreatModelStore) convertToListItem(tm *models.ThreatModel) TMListItem {
	tmUUID, _ := uuid.Parse(tm.ID)

	owner := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      tm.Owner.Provider,
		ProviderId:    strFromPtr(tm.Owner.ProviderUserID),
		DisplayName:   tm.Owner.Name,
		Email:         openapi_types.Email(tm.Owner.Email),
	}

	var createdBy User
	if tm.CreatedByInternalUUID != "" {
		createdBy = User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.CreatedBy.Provider,
			ProviderId:    strFromPtr(tm.CreatedBy.ProviderUserID),
			DisplayName:   tm.CreatedBy.Name,
			Email:         openapi_types.Email(tm.CreatedBy.Email),
		}
	}

	var securityReviewer *User
	if tm.SecurityReviewerInternalUUID != nil && *tm.SecurityReviewerInternalUUID != "" && tm.SecurityReviewer != nil {
		securityReviewer = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.SecurityReviewer.Provider,
			ProviderId:    strFromPtr(tm.SecurityReviewer.ProviderUserID),
			DisplayName:   tm.SecurityReviewer.Name,
			Email:         openapi_types.Email(tm.SecurityReviewer.Email),
		}
	}

	framework := tm.ThreatModelFramework
	if framework == "" {
		framework = DefaultThreatModelFramework
	}

	return TMListItem{
		Id:                   &tmUUID,
		Name:                 tm.Name,
		Description:          tm.Description, // *string -> *string (both are pointers)
		CreatedAt:            tm.CreatedAt,
		ModifiedAt:           tm.ModifiedAt,
		Owner:                owner,
		CreatedBy:            createdBy,
		SecurityReviewer:     securityReviewer,
		ThreatModelFramework: framework, // string, not pointer
		IssueUri:             tm.IssueURI,
		Status:               tm.Status,
		StatusUpdated:        tm.StatusUpdated,
		DeletedAt:            tm.DeletedAt,
	}
}
```

- [ ] **Step 3: Add `batchLoadAuthorizationLightweight` for list filtering**

In `api/database_store_gorm.go`, add:

```go
// authWithOwner holds authorization data plus owner for access checking.
type authWithOwner struct {
	Owner         User
	Authorization []Authorization
}

// batchLoadAuthorizationLightweight loads authorization entries for multiple threat models
// in batch, with batch user/group resolution. Used by ListWithCounts for auth filtering.
func (s *GormThreatModelStore) batchLoadAuthorizationLightweight(ids []string, ownerMap map[string]User) map[string]authWithOwner {
	result := make(map[string]authWithOwner, len(ids))
	if len(ids) == 0 {
		return result
	}

	// Initialize with owners
	for _, id := range ids {
		result[id] = authWithOwner{Owner: ownerMap[id]}
	}

	// Load all access entries in batch
	var accessEntries []models.ThreatModelAccess
	for _, chunk := range chunkStrings(ids, 999) {
		var entries []models.ThreatModelAccess
		s.db.Where("threat_model_id IN ?", chunk).Order("role DESC").Find(&entries)
		accessEntries = append(accessEntries, entries...)
	}

	// Collect unique UUIDs for batch resolution
	userUUIDSet := make(map[string]bool)
	groupUUIDSet := make(map[string]bool)
	for _, entry := range accessEntries {
		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			userUUIDSet[*entry.UserInternalUUID] = true
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			groupUUIDSet[*entry.GroupInternalUUID] = true
		}
	}

	userUUIDs := make([]string, 0, len(userUUIDSet))
	for u := range userUUIDSet {
		userUUIDs = append(userUUIDs, u)
	}
	groupUUIDs := make([]string, 0, len(groupUUIDSet))
	for g := range groupUUIDSet {
		groupUUIDs = append(groupUUIDs, g)
	}

	userMap, groupMap := s.resolveUsersAndGroupsBatch(userUUIDs, groupUUIDs)

	// Build authorization entries grouped by threat model ID
	for _, entry := range accessEntries {
		awo := result[entry.ThreatModelID]
		role := AuthorizationRole(entry.Role)

		if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeUser) && entry.UserInternalUUID != nil {
			if user, ok := userMap[*entry.UserInternalUUID]; ok {
				awo.Authorization = append(awo.Authorization, Authorization{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      user.Provider,
					ProviderId:    strFromPtr(user.ProviderUserID),
					DisplayName:   &user.Name,
					Email:         (*openapi_types.Email)(&user.Email),
					Role:          role,
				})
			}
		} else if entry.SubjectType == string(AddGroupMemberRequestSubjectTypeGroup) && entry.GroupInternalUUID != nil {
			if group, ok := groupMap[*entry.GroupInternalUUID]; ok {
				awo.Authorization = append(awo.Authorization, Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      group.Provider,
					ProviderId:    group.GroupName,
					DisplayName:   group.Name,
					Role:          role,
				})
			}
		}
		result[entry.ThreatModelID] = awo
	}

	return result
}
```

- [ ] **Step 4: Refactor `ListWithCounts`**

Replace the body of `ListWithCounts` (`database_store_gorm.go:297-379`). Keep the query building and filter logic the same, but replace the conversion and counting:

```go
func (s *GormThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]ThreatModelWithCounts, int) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModelWithCounts

	// Build query with database-level filters (UNCHANGED — keep existing filter logic)
	query := s.db.Model(&models.ThreatModel{}).Where("threat_models.deleted_at IS NULL")
	if filters != nil && filters.IncludeDeleted {
		query = s.db.Model(&models.ThreatModel{})
	}

	// Apply database-level filters if provided (UNCHANGED — keep all existing filter clauses)
	if filters != nil {
		// ... (keep all existing filter clauses exactly as-is from lines 312-352)
		if filters.Name != nil && *filters.Name != "" {
			query = query.Where("LOWER(threat_models.name) LIKE LOWER(?)", "%"+*filters.Name+"%")
		}
		if filters.Description != nil && *filters.Description != "" {
			query = query.Where("LOWER(threat_models.description) LIKE LOWER(?)", "%"+*filters.Description+"%")
		}
		if filters.IssueUri != nil && *filters.IssueUri != "" {
			query = query.Where("LOWER(threat_models.issue_uri) LIKE LOWER(?)", "%"+*filters.IssueUri+"%")
		}
		if filters.Owner != nil && *filters.Owner != "" {
			query = query.Joins("LEFT JOIN users AS owner_filter ON threat_models.owner_internal_uuid = owner_filter.internal_uuid").
				Where("LOWER(owner_filter.email) LIKE LOWER(?) OR LOWER(owner_filter.name) LIKE LOWER(?)",
					"%"+*filters.Owner+"%", "%"+*filters.Owner+"%")
		}
		if filters.CreatedAfter != nil {
			query = query.Where("threat_models.created_at >= ?", *filters.CreatedAfter)
		}
		if filters.CreatedBefore != nil {
			query = query.Where("threat_models.created_at <= ?", *filters.CreatedBefore)
		}
		if filters.ModifiedAfter != nil {
			query = query.Where("threat_models.modified_at >= ?", *filters.ModifiedAfter)
		}
		if filters.ModifiedBefore != nil {
			query = query.Where("threat_models.modified_at <= ?", *filters.ModifiedBefore)
		}
		if len(filters.Status) > 0 {
			lowered := make([]string, len(filters.Status))
			for i, s := range filters.Status {
				lowered[i] = strings.ToLower(s)
			}
			query = query.Where("LOWER(threat_models.status) IN ?", lowered)
		}
		if filters.StatusUpdatedAfter != nil {
			query = query.Where("threat_models.status_updated >= ?", *filters.StatusUpdatedAfter)
		}
		if filters.StatusUpdatedBefore != nil {
			query = query.Where("threat_models.status_updated <= ?", *filters.StatusUpdatedBefore)
		}
	}

	var tmModels []models.ThreatModel
	result := query.Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer").Order("threat_models.created_at DESC").Find(&tmModels)
	if result.Error != nil {
		return results, 0
	}

	// Build owner map for authorization filtering
	allIDs := make([]string, len(tmModels))
	ownerMap := make(map[string]User, len(tmModels))
	for i, tm := range tmModels {
		allIDs[i] = tm.ID
		ownerMap[tm.ID] = User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      tm.Owner.Provider,
			ProviderId:    strFromPtr(tm.Owner.ProviderUserID),
			DisplayName:   tm.Owner.Name,
			Email:         openapi_types.Email(tm.Owner.Email),
		}
	}

	// Batch load authorization for filtering
	var authMap map[string]authWithOwner
	if filter != nil {
		authMap = s.batchLoadAuthorizationLightweight(allIDs, ownerMap)
	}

	// Filter and convert to list items
	type filteredItem struct {
		item TMListItem
		id   string
	}
	var filtered []filteredItem
	for _, tm := range tmModels {
		if filter != nil {
			awo := authMap[tm.ID]
			// Build a minimal ThreatModel with just auth data for the filter
			filterTM := ThreatModel{
				Owner:         awo.Owner,
				Authorization: awo.Authorization,
			}
			if !filter(filterTM) {
				continue
			}
		}
		filtered = append(filtered, filteredItem{
			item: s.convertToListItem(&tm),
			id:   tm.ID,
		})
	}

	total := len(filtered)

	// Apply pagination
	if offset >= len(filtered) {
		return []ThreatModelWithCounts{}, total
	}
	end := offset + limit
	if end > len(filtered) || limit <= 0 {
		end = len(filtered)
	}
	paginated := filtered[offset:end]

	// Batch counts for paginated results only
	paginatedIDs := make([]string, len(paginated))
	for i, f := range paginated {
		paginatedIDs[i] = f.id
	}
	counts := s.batchCounts(paginatedIDs)

	// Build final results — we still return ThreatModelWithCounts for interface compatibility.
	// The handler already converts these to TMListItem, but the conversion is now cheap
	// since convertToListItem doesn't load sub-resources.
	for _, f := range paginated {
		ec := counts[f.id]
		createdBy := f.item.CreatedBy
		createdAt := f.item.CreatedAt
		modifiedAt := f.item.ModifiedAt
		// Build a minimal ThreatModel from the list item for interface compatibility
		results = append(results, ThreatModelWithCounts{
			ThreatModel: ThreatModel{
				Id:                   f.item.Id,
				Name:                 f.item.Name,
				Description:          f.item.Description,
				Owner:                f.item.Owner,
				CreatedBy:            &createdBy,
				SecurityReviewer:     f.item.SecurityReviewer,
				ThreatModelFramework: f.item.ThreatModelFramework, // string, not pointer
				IssueUri:             f.item.IssueUri,
				Status:               f.item.Status,
				StatusUpdated:        f.item.StatusUpdated,
				DeletedAt:            f.item.DeletedAt,
				CreatedAt:            &createdAt,
				ModifiedAt:           &modifiedAt,
			},
			DocumentCount: ec.DocumentCount,
			SourceCount:   ec.SourceCount,
			DiagramCount:  ec.DiagramCount,
			ThreatCount:   ec.ThreatCount,
			NoteCount:     ec.NoteCount,
			AssetCount:    ec.AssetCount,
		})
	}

	return results, total
}
```

- [ ] **Step 5: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 6: Run integration tests**

Run: `make test-integration`
Expected: PASS — list endpoint returns identical JSON response.

- [ ] **Step 7: Commit**

```bash
git add api/database_store_gorm.go
git commit -m "perf(api): batch counts and lightweight list conversion

Replace per-item convertToAPIModel() + 6×N COUNT queries in ListWithCounts
with convertToListItem() (no sub-resource queries) and batchCounts()
(6 GROUP BY queries total). Adds batchLoadAuthorizationLightweight for
auth filtering without full model load.

Refs #190"
```

---

## Phase 2: N+1 Elimination in Get Path

### Task 5: Batch Threat Metadata Loading

**Files:**
- Modify: `api/database_store_gorm.go:757-846`

- [ ] **Step 1: Add `batchLoadThreatMetadata` helper**

In `api/database_store_gorm.go`, add after `loadThreatMetadata` (after line 846):

```go
// batchLoadThreatMetadata loads metadata for multiple threats in a single query.
func (s *GormThreatModelStore) batchLoadThreatMetadata(threatIDs []string) map[string][]Metadata {
	result := make(map[string][]Metadata, len(threatIDs))
	if len(threatIDs) == 0 {
		return result
	}

	var metadataEntries []models.Metadata
	for _, chunk := range chunkStrings(threatIDs, 999) {
		var entries []models.Metadata
		s.db.Where("entity_type = ? AND entity_id IN ?", "threat", chunk).
			Order("key ASC").
			Find(&entries)
		metadataEntries = append(metadataEntries, entries...)
	}

	for _, entry := range metadataEntries {
		result[entry.EntityID] = append(result[entry.EntityID], Metadata{
			Key:   entry.Key,
			Value: entry.Value,
		})
	}

	return result
}
```

- [ ] **Step 2: Refactor `loadThreats` to use batch metadata**

In `loadThreats` (line 797), replace the per-threat `loadThreatMetadata` call:

Replace:
```go
// Load threat metadata
threatMetadata, _ := s.loadThreatMetadata(tm.ID)
metadata := &threatMetadata
```

With:
```go
// Metadata is populated from batch map below
```

And add batch loading after the initial threats query (after `Find(&threatModels)`):

```go
// Batch load all threat metadata
threatIDs := make([]string, len(threatModels))
for i, tm := range threatModels {
    threatIDs[i] = tm.ID
}
metadataMap := s.batchLoadThreatMetadata(threatIDs)
```

Then in the per-threat loop, replace the metadata assignment:

```go
threatMeta := metadataMap[tm.ID]
if threatMeta == nil {
    threatMeta = []Metadata{}
}
metadata := &threatMeta
```

- [ ] **Step 3: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/database_store_gorm.go
git commit -m "perf(api): batch threat metadata loading

Replace per-threat loadThreatMetadata() N+1 pattern with single batch
query using WHERE entity_id IN (?).

Refs #190"
```

---

### Task 6: Batch Diagram Loading

**Files:**
- Modify: `api/store.go:76-89`
- Modify: `api/database_store_gorm.go:849-888, 993-1020`

- [ ] **Step 1: Add `GetBatch` to `DiagramStoreInterface`**

In `api/store.go`, add to `DiagramStoreInterface` (after `GetIncludingDeleted`):

```go
GetBatch(ids []string) ([]DfdDiagram, error)
```

- [ ] **Step 2: Implement `GetBatch` on `GormDiagramStore`**

In `api/database_store_gorm.go`, add after the `Get` method on `GormDiagramStore`:

```go
// GetBatch retrieves multiple diagrams by ID in a single query.
func (s *GormDiagramStore) GetBatch(ids []string) ([]DfdDiagram, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if len(ids) == 0 {
		return []DfdDiagram{}, nil
	}

	var diagrams []models.Diagram
	for _, chunk := range chunkStrings(ids, 999) {
		var batch []models.Diagram
		result := s.db.Where("id IN ? AND deleted_at IS NULL", chunk).
			Order("created_at").
			Find(&batch)
		if result.Error != nil {
			return nil, fmt.Errorf("failed to batch load diagrams: %w", result.Error)
		}
		diagrams = append(diagrams, batch...)
	}

	result := make([]DfdDiagram, 0, len(diagrams))
	for _, d := range diagrams {
		apiDiagram, err := s.convertToAPIDiagram(&d)
		if err != nil {
			continue
		}
		result = append(result, apiDiagram)
	}

	return result, nil
}
```

- [ ] **Step 3: Refactor `loadDiagramsDynamically` to use `GetBatch`**

Replace `loadDiagramsDynamically` (lines 849-888):

```go
func (s *GormThreatModelStore) loadDiagramsDynamically(threatModelID string) (*[]Diagram, error) {
	var diagramIDs []string
	result := s.db.Model(&models.Diagram{}).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at").
		Pluck("id", &diagramIDs)
	if result.Error != nil {
		return nil, result.Error
	}

	if len(diagramIDs) == 0 {
		emptySlice := []Diagram{}
		return &emptySlice, nil
	}

	// Batch load all diagrams in one query
	apiDiagrams, err := DiagramStore.GetBatch(diagramIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch load diagrams: %w", err)
	}

	diagrams := make([]Diagram, 0, len(apiDiagrams))
	for i := range apiDiagrams {
		// Ensure backward compatibility
		if apiDiagrams[i].Image == nil {
			apiDiagrams[i].Image = &struct {
				Svg          *[]byte `json:"svg,omitempty"`
				UpdateVector *int64  `json:"update_vector,omitempty"`
			}{}
		}

		var diagramUnion Diagram
		if err := diagramUnion.FromDfdDiagram(apiDiagrams[i]); err != nil {
			continue
		}
		diagrams = append(diagrams, diagramUnion)
	}

	return &diagrams, nil
}
```

- [ ] **Step 4: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/store.go api/database_store_gorm.go
git commit -m "perf(api): batch diagram loading with GetBatch

Add GetBatch to DiagramStoreInterface. Replace per-diagram Get() N+1
pattern in loadDiagramsDynamically with single batch query.

Refs #190"
```

---

### Task 7: Response Caching for GET /threat_models/{id}

**Files:**
- Modify: `api/cache_service.go`
- Modify: `api/database_store_gorm.go:122-147`
- Modify: `api/cache_invalidation.go`

- [ ] **Step 1: Add threat model response cache methods to `CacheService`**

In `api/cache_service.go`, add:

```go
// CacheThreatModelResponse caches a full threat model API response
func (cs *CacheService) CacheThreatModelResponse(ctx context.Context, id string, tm *ThreatModel) error {
	logger := slogging.Get()
	key := cs.builder.CacheThreatModelKey(id) + ":response"

	data, err := json.Marshal(tm)
	if err != nil {
		logger.Error("Failed to marshal threat model response for cache: %v", err)
		return fmt.Errorf("failed to marshal threat model response: %w", err)
	}

	err = cs.redis.Set(ctx, key, data, ThreatModelCacheTTL)
	if err != nil {
		logger.Error("Failed to cache threat model response %s: %v", id, err)
		return fmt.Errorf("failed to cache threat model response: %w", err)
	}

	logger.Debug("Cached threat model response %s with TTL %v", id, ThreatModelCacheTTL)
	return nil
}

// GetCachedThreatModelResponse retrieves a cached threat model response
func (cs *CacheService) GetCachedThreatModelResponse(ctx context.Context, id string) (*ThreatModel, error) {
	logger := slogging.Get()
	key := cs.builder.CacheThreatModelKey(id) + ":response"

	data, err := cs.redis.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Cache miss
		}
		return nil, fmt.Errorf("failed to get cached threat model response: %w", err)
	}

	var tm ThreatModel
	if err := json.Unmarshal([]byte(data), &tm); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached threat model response: %w", err)
	}
	return &tm, nil
}

// InvalidateThreatModelResponse invalidates the response cache for a threat model
func (cs *CacheService) InvalidateThreatModelResponse(ctx context.Context, id string) error {
	key := cs.builder.CacheThreatModelKey(id) + ":response"
	return cs.redis.Del(ctx, key)
}
```

- [ ] **Step 2: Add response cache check to `GormThreatModelStore.Get`**

In `api/database_store_gorm.go`, at the top of the `Get` method (after UUID validation, before DB query):

```go
// Check response cache
if GlobalCacheService != nil {
    cached, cacheErr := GlobalCacheService.GetCachedThreatModelResponse(context.Background(), id)
    if cacheErr == nil && cached != nil {
        logger.Debug("GormThreatModelStore.Get() cache hit for id=%s", id)
        return *cached, nil
    }
}
```

And after the successful `convertToAPIModel` return, cache the result:

```go
apiModel, err := s.convertToAPIModel(&tm)
if err != nil {
    return ThreatModel{}, err
}

// Cache the response
if GlobalCacheService != nil {
    _ = GlobalCacheService.CacheThreatModelResponse(context.Background(), id, &apiModel)
}

return apiModel, nil
```

- [ ] **Step 3: Add response cache invalidation to `invalidateImmediately`**

In `api/cache_invalidation.go`, inside the `invalidateImmediately` method, add after the existing entity invalidation logic:

```go
// Always invalidate the threat model response cache when any related resource changes
if event.ParentType == string(CreateAddonRequestObjectsThreatModel) && event.ParentID != "" {
    if err := ci.cache.InvalidateThreatModelResponse(ctx, event.ParentID); err != nil {
        logger.Error("Failed to invalidate threat model response cache for %s: %v", event.ParentID, err)
    }
}
```

- [ ] **Step 4: Add response cache invalidation to threat model write handlers**

In `api/threat_model_handlers.go`, after each successful update/patch/delete operation, add:

```go
// Invalidate response cache
if GlobalCacheService != nil {
    _ = GlobalCacheService.InvalidateThreatModelResponse(c.Request.Context(), id)
}
```

This applies to: `UpdateThreatModel`, `PatchThreatModel`, `DeleteThreatModel`, `SoftDeleteThreatModel`, `RestoreThreatModel`.

Also invalidate middleware auth cache when authorization changes:
```go
// Invalidate auth cache if authorization was modified
if GlobalCacheService != nil {
    _ = GlobalCacheService.InvalidateMiddlewareAuth(c.Request.Context(), id)
}
```

- [ ] **Step 5: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 6: Run integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/cache_service.go api/database_store_gorm.go api/cache_invalidation.go api/threat_model_handlers.go
git commit -m "perf(api): add response caching for GET /threat_models/{id}

Cache full threat model response in Redis with 10-minute TTL. Write-
through invalidation on any threat model or sub-resource modification.
Cold cache: ~12 queries. Warm cache: 0 queries.

Refs #190"
```

---

### Task 8: Final Verification and Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run full lint**

Run: `make lint`
Expected: PASS (no new warnings outside of expected api/api.go ones)

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Run integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 4: Run CATS fuzz tests**

Run: `make cats-fuzz && make analyze-cats-results`
Expected: No new true positive failures

- [ ] **Step 5: Final commit referencing the issue**

```bash
git add -A
git commit -m "perf(api): complete N+1 query elimination

Closes #190"
```
