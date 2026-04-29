# Replace Wildcard Provider "*" with "tmi" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate `"*"` as a provider value; all built-in groups and the "everyone" pseudo-group use `"tmi"` as their provider. This is a breaking change — the `"*"` wildcard was a workaround that is no longer needed.

**Architecture:** Define a `BuiltInProvider` constant set to `"tmi"`. Replace every occurrence of `"*"` as a provider with this constant. Add a data migration in the seed layer to update existing database rows. Remove the wildcard-matching logic from `checkGroupMatch` since `"tmi"` groups should only match users from the `"tmi"` provider (or match by group name for built-in groups that are provider-independent by design). Keep the provider-agnostic fallback in `EnrichAuthorizationEntry` (from issue #254) since clients may still send `"*"` during the transition.

**Tech Stack:** Go, GORM, PostgreSQL, Oracle ADB

**Closes:** #255

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `api/auth_utils.go` | Modify | Add `BuiltInProvider` constant, update `checkGroupMatch` |
| `api/seed/seed.go` | Modify | Change all group seeds from `"*"` to `BuiltInProvider`, add migration function |
| `api/database_store_gorm.go` | Modify | Change default provider in `resolveGroupToUUID` and `ensureGroupExists` |
| `api/survey_response_store_gorm.go` | Modify | Change `"*"` to `BuiltInProvider` in 3 ensure-group functions |
| `auth/repository/deletion_repository.go` | Modify | Change `"*"` to `BuiltInProvider` in `ensureSecurityReviewersGroupForDeletion` |
| `cmd/dbtool/health.go` | Modify | Change health check query from `provider = "*"` to `provider = "tmi"` |
| `api/authorization_enrichment.go` | Modify | Update wildcard fallback comment; keep fallback for backward compat |
| `api/auth_utils_test.go` | Modify | Update ~17 test fixtures from `"*"` to `BuiltInProvider` |
| `api/auth_utils_extended_test.go` | Modify | Update ~19 test fixtures from `"*"` to `BuiltInProvider` |
| `api/admin_group_handlers_test.go` | Modify | Update ~34 test fixtures from `"*"` to `BuiltInProvider` |
| `api/authorization_enrichment_test.go` | Modify | Update test for wildcard provider |
| `api/models/models_test.go` | Modify | Update 1 test fixture |
| `auth/repository/deletion_repository_test.go` | Modify | Update 1 test fixture |
| `test/integration/workflows/authorization_wildcard_test.go` | Modify | Update integration test to also verify `"tmi"` as group provider |
| `test/integration/workflows/tier2_features/cross_user_authorization_test.go` | Modify | Update everyone pseudo-group provider from `"*"` to `"tmi"` |

---

### Task 1: Add `BuiltInProvider` constant and update `checkGroupMatch`

**Files:**
- Modify: `api/auth_utils.go:400-456` (constants block) and `api/auth_utils.go:608-628` (`checkGroupMatch`)

- [ ] **Step 1: Add the `BuiltInProvider` constant**

In `api/auth_utils.go`, add to the top of the pseudo-group constants block (around line 404):

```go
const (
	// BuiltInProvider is the provider value for all TMI built-in groups and
	// the "everyone" pseudo-group. Replaces the former "*" wildcard.
	BuiltInProvider = "tmi"
)
```

- [ ] **Step 2: Update the comment on `SecurityReviewersGroup`**

Change the comment at line 420 from:
```go
// It is provider-independent (provider = "*") and auto-added to non-confidential survey responses and threat models.
```
to:
```go
// It uses the built-in provider ("tmi") and is auto-added to non-confidential survey responses and threat models.
```

- [ ] **Step 3: Update `checkGroupMatch` to remove wildcard matching**

Replace the function body of `checkGroupMatch` (lines 608-628):

```go
// checkGroupMatch checks if an authorization entry matches the user's groups.
// Returns true if the user is a member of the group, handling special pseudo-groups.
func checkGroupMatch(auth Authorization, user ResolvedUser, groups []string) bool {
	// Special handling for "everyone" pseudo-group
	if auth.ProviderId == EveryonePseudoGroup {
		logger := slogging.Get()
		logger.Debug("Access granted via 'everyone' pseudo-group with role: %s for user: %s",
			auth.Role, user.Email)
		return true
	}

	// Built-in groups (provider = "tmi") are provider-independent:
	// they match any user regardless of their identity provider.
	// External groups must match both group name AND provider.
	if auth.Provider == BuiltInProvider || auth.Provider == user.Provider {
		if slices.Contains(groups, auth.ProviderId) {
			return true
		}
	}

	return false
}
```

- [ ] **Step 4: Run lint and unit tests**

```bash
make lint
make test-unit
```

Expected: All pass. The `checkGroupMatch` behavior is unchanged because built-in groups were `"*"` (matched everything) and now are `"tmi"` (the `BuiltInProvider` constant still matches all users since `auth.Provider == BuiltInProvider` is a dedicated check).

- [ ] **Step 5: Commit**

```bash
git add api/auth_utils.go
git commit -m "refactor(api): add BuiltInProvider constant, update checkGroupMatch

Replace wildcard provider matching with explicit BuiltInProvider check.
Built-in groups now use 'tmi' as provider instead of '*'.

Refs #255"
```

---

### Task 2: Update seed data to use `BuiltInProvider` and add data migration

**Files:**
- Modify: `api/seed/seed.go`

- [ ] **Step 1: Add import for `api` package constant**

The seed package imports `api/validation` but not `api` (to avoid circular deps). Since `BuiltInProvider` is in `api/auth_utils.go`, we need to either:
- Move `BuiltInProvider` to the `validation` package (already imported by seed), or
- Define a local constant in seed.

Check for circular dependency: `seed` imports `validation`, `api` imports `seed` — if `api` imports `seed`, putting the constant in `api` would create a circular import from `seed` to `api`. Define a local constant in seed that mirrors the value.

Add at the top of `seed.go` after the import block:

```go
// builtInProvider is the provider value for all TMI built-in groups.
// Must match api.BuiltInProvider — kept as a local constant to avoid
// a circular import from seed -> api.
const builtInProvider = "tmi"
```

- [ ] **Step 2: Replace all `"*"` with `builtInProvider` in seed functions**

In each of the 6 seed functions (`seedEveryoneGroup`, `seedSecurityReviewersGroup`, `seedAdministratorsGroup`, `seedConfidentialProjectReviewersGroup`, `seedEmbeddingAutomationGroup`, `seedTMIAutomationGroup`), replace every `Provider: "*"` with `Provider: builtInProvider` and every `Provider: "*"` in the `Where` clause with `Provider: builtInProvider`.

For example, `seedEveryoneGroup` becomes:

```go
func seedEveryoneGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Everyone (Pseudo-group)"
	group := models.Group{
		InternalUUID: validation.EveryonePseudoGroupUUID,
		Provider:     builtInProvider,
		GroupName:    "everyone",
		Name:         &name,
		UsageCount:   0,
	}

	result := db.Where(&models.Group{
		Provider:  builtInProvider,
		GroupName: "everyone",
	}).FirstOrCreate(&group)
	// ... rest unchanged
```

Repeat for all 6 functions. Each has exactly 2 occurrences of `"*"`: one in the struct literal and one in the `Where` clause.

- [ ] **Step 3: Add `migrateWildcardProviderToTMI` data migration function**

Add this function to `seed.go` — it runs as part of seeding and updates any existing `"*"` provider rows to `"tmi"`:

```go
// migrateWildcardProviderToTMI updates built-in groups and their access entries
// from the legacy "*" wildcard provider to the standard "tmi" provider.
// This is idempotent — rows already set to "tmi" are unaffected.
func migrateWildcardProviderToTMI(db *gorm.DB) error {
	log := slogging.Get()

	// Update groups table
	result := db.Exec("UPDATE groups SET provider = ? WHERE provider = ?", builtInProvider, "*")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Info("Migrated %d groups from provider '*' to '%s'", result.RowsAffected, builtInProvider)
	} else {
		log.Debug("No groups with provider '*' found (already migrated)")
	}

	return nil
}
```

- [ ] **Step 4: Wire migration into `SeedDatabase`**

Add the migration call in `SeedDatabase` **before** the individual group seed calls (so `FirstOrCreate` uses the new provider for matching):

```go
func SeedDatabase(db *gorm.DB) error {
	log := slogging.Get()

	log.Info("Seeding database with required data...")

	// Migrate legacy "*" provider to "tmi" before seeding groups
	// (ensures FirstOrCreate matches on the new provider value)
	if err := migrateWildcardProviderToTMI(db); err != nil {
		log.Error("Failed to migrate wildcard provider: %v", err)
		return err
	}

	if err := seedEveryoneGroup(db); err != nil {
	// ... rest unchanged
```

- [ ] **Step 5: Run lint and unit tests**

```bash
make lint
make test-unit
```

- [ ] **Step 6: Commit**

```bash
git add api/seed/seed.go
git commit -m "refactor(seed): replace provider '*' with 'tmi' in all built-in groups

Add data migration to update existing database rows from '*' to 'tmi'.
Migration runs before seed functions to ensure FirstOrCreate matches
correctly on the new provider value.

Refs #255"
```

---

### Task 3: Update database store default provider

**Files:**
- Modify: `api/database_store_gorm.go:66-121` (`resolveGroupToUUID` and `ensureGroupExists`)

- [ ] **Step 1: Change default provider in `resolveGroupToUUID`**

Line 68: change `provider := "*"` to `provider := BuiltInProvider`

- [ ] **Step 2: Change default provider in `ensureGroupExists`**

Line 88: change `provider := "*"` to `provider := BuiltInProvider`

- [ ] **Step 3: Run lint and unit tests**

```bash
make lint
make test-unit
```

- [ ] **Step 4: Commit**

```bash
git add api/database_store_gorm.go
git commit -m "refactor(api): use BuiltInProvider as default in group resolution

resolveGroupToUUID and ensureGroupExists now default to 'tmi'
instead of '*' when no identity provider is specified.

Refs #255"
```

---

### Task 4: Update survey response store and deletion repository

**Files:**
- Modify: `api/survey_response_store_gorm.go:238-320`
- Modify: `auth/repository/deletion_repository.go:716-748`

- [ ] **Step 1: Update `ensureSecurityReviewersGroup` in survey response store**

Replace all 3 occurrences of `"*"` in `ensureSecurityReviewersGroup` (lines 241, 255, 264):
- Line 241: `"*"` → `BuiltInProvider` in WHERE clause
- Line 255: `Provider: "*"` → `Provider: BuiltInProvider` in struct
- Line 264: `"*"` → `BuiltInProvider` in race-condition WHERE clause

- [ ] **Step 2: Update `ensureConfidentialProjectReviewersGroup` in survey response store**

Replace all 3 occurrences of `"*"` (lines 276, 290, 299):
- Same pattern as step 1.

- [ ] **Step 3: Update `ensureTMIAutomationGroup` in survey response store**

Replace `"*"` in the WHERE clause (line 311) and struct literal.

- [ ] **Step 4: Update `ensureSecurityReviewersGroupForDeletion` in deletion repository**

The `auth/repository` package does not import `api`, so it can't reference `api.BuiltInProvider`. Define a local constant:

```go
// builtInProvider is the provider for TMI built-in groups.
// Must match api.BuiltInProvider.
const builtInProvider = "tmi"
```

Then replace all 3 occurrences of `"*"` in `ensureSecurityReviewersGroupForDeletion` (lines 718, 732, 741).

- [ ] **Step 5: Run lint and unit tests**

```bash
make lint
make test-unit
```

- [ ] **Step 6: Commit**

```bash
git add api/survey_response_store_gorm.go auth/repository/deletion_repository.go
git commit -m "refactor(api,auth): use 'tmi' provider in survey and deletion stores

Update ensureSecurityReviewersGroup, ensureConfidentialProjectReviewersGroup,
ensureTMIAutomationGroup, and ensureSecurityReviewersGroupForDeletion to use
the built-in 'tmi' provider instead of '*'.

Refs #255"
```

---

### Task 5: Update dbtool health check

**Files:**
- Modify: `cmd/dbtool/health.go:59-73`

- [ ] **Step 1: Update the health check query and comment**

Change line 61 comment from:
```go
// Check built-in groups (provider = "*" are built-in)
```
to:
```go
// Check built-in groups (provider = "tmi" are built-in)
```

Change line 63 from:
```go
if err := db.DB().Table("groups").Where("provider = ?", "*").Count(&groupCount).Error; err != nil {
```
to:
```go
if err := db.DB().Table("groups").Where("provider = ?", "tmi").Count(&groupCount).Error; err != nil {
```

- [ ] **Step 2: Run lint**

```bash
make lint
```

- [ ] **Step 3: Commit**

```bash
git add cmd/dbtool/health.go
git commit -m "refactor(dbtool): update health check to use 'tmi' provider

Refs #255"
```

---

### Task 6: Update authorization enrichment (backward compat)

**Files:**
- Modify: `api/authorization_enrichment.go:75-93`

The wildcard fallback from issue #254 should remain for backward compatibility during the transition period. Update the comment to document this is a legacy compatibility path.

- [ ] **Step 1: Update the fallback comment**

Change the comment at line 75 from:
```go
// Wildcard fallback: when provider="*" is used for user entries, the provider-specific
// lookup above will miss users stored under their real provider (e.g., "tmi", "google").
// Fall back to a provider-agnostic search by identifier only.
```
to:
```go
// Legacy wildcard fallback: clients may still send provider="*" for user entries
// during the transition from wildcard to explicit "tmi" provider. When the
// provider-specific lookup fails with "*", fall back to a provider-agnostic
// search by identifier only.
```

- [ ] **Step 2: Commit**

```bash
git add api/authorization_enrichment.go
git commit -m "docs(api): clarify wildcard fallback is legacy compatibility path

Refs #255"
```

---

### Task 7: Update all unit test fixtures

**Files:**
- Modify: `api/auth_utils_test.go` (~17 occurrences)
- Modify: `api/auth_utils_extended_test.go` (~19 occurrences)
- Modify: `api/admin_group_handlers_test.go` (~34 occurrences)
- Modify: `api/authorization_enrichment_test.go` (3 occurrences)
- Modify: `api/models/models_test.go` (1 occurrence)
- Modify: `auth/repository/deletion_repository_test.go` (1 occurrence)

- [ ] **Step 1: Bulk replace `Provider: "*"` with `Provider: BuiltInProvider` in api test files**

For files in the `api` package that can reference `BuiltInProvider` directly:
- `api/auth_utils_test.go`: Replace all `Provider:      "*"` and `Provider: "*"` with `Provider: BuiltInProvider`
- `api/auth_utils_extended_test.go`: Same replacement
- `api/admin_group_handlers_test.go`: Same replacement
- `api/authorization_enrichment_test.go`: Same replacement for test fixture data, but **keep** the `Provider: "*"` in the wildcard fallback test cases (those test the legacy path)
- `api/models/models_test.go`: Replace `Provider: "*"` with `Provider: "tmi"` (models package can't import api)

- [ ] **Step 2: Update comment about wildcard provider in auth_utils_test.go**

Line 1746: Change `// Test 3: Editor from different IdP gets writer (because editors group has Provider: "*")` to `// Test 3: Editor from different IdP gets writer (because built-in groups match all providers)`

- [ ] **Step 3: Update deletion repository test**

`auth/repository/deletion_repository_test.go` line 277: Replace `Provider: "*"` with `Provider: "tmi"` (can't import api package, use string literal)

- [ ] **Step 4: Run lint and unit tests**

```bash
make lint
make test-unit
```

Expected: All 1441+ tests pass. The behavioral change in `checkGroupMatch` is that `"tmi"` groups match all users (via the `auth.Provider == BuiltInProvider` check), same as `"*"` did before.

- [ ] **Step 5: Commit**

```bash
git add api/auth_utils_test.go api/auth_utils_extended_test.go api/admin_group_handlers_test.go api/authorization_enrichment_test.go api/models/models_test.go auth/repository/deletion_repository_test.go
git commit -m "test: update all test fixtures from provider '*' to 'tmi'

Refs #255"
```

---

### Task 8: Update integration tests

**Files:**
- Modify: `test/integration/workflows/authorization_wildcard_test.go`
- Modify: `test/integration/workflows/tier2_features/cross_user_authorization_test.go`

- [ ] **Step 1: Update `authorization_wildcard_test.go`**

This test verifies that `provider="*"` is accepted (legacy compat) and enriched to the actual provider. Keep the test — it validates the backward-compatibility fallback. But update the group entries from `"*"` to `"tmi"` to match the new expected behavior. The user entries should keep `"*"` to test the legacy path.

Change lines 82 and 88 (group entries) from `"provider": "*"` to `"provider": "tmi"`.

Update the test assertion at line 122-133 to verify that group entries also have a non-`"*"` provider (they should now be `"tmi"`).

- [ ] **Step 2: Update `cross_user_authorization_test.go`**

Line 597: Change `"provider": "*"` to `"provider": "tmi"` for the everyone pseudo-group.

- [ ] **Step 3: Build, restart server, and run integration tests**

```bash
make build-server
make stop-server
make start-dev
make test-integration
```

Expected: `TestPatchAuthorizationWithWildcardProvider` and `TestEveryonePseudoGroup` pass.

- [ ] **Step 4: Commit**

```bash
git add test/integration/workflows/authorization_wildcard_test.go test/integration/workflows/tier2_features/cross_user_authorization_test.go
git commit -m "test(integration): update integration tests for 'tmi' provider

Refs #255"
```

---

### Task 9: Final verification and cleanup

- [ ] **Step 1: Verify no remaining `"*"` provider references in non-test code**

```bash
grep -rn 'Provider.*"\*"\|provider.*"\*"\|"provider".*"\*"' --include='*.go' api/ auth/ cmd/ internal/ | grep -v _test.go | grep -v node_modules
```

Expected: Zero results (except possibly the wildcard fallback check `auth.Provider == "*"` in authorization_enrichment.go, which is the legacy compat path).

- [ ] **Step 2: Run the full quality gate**

```bash
make lint
make build-server
make test-unit
```

- [ ] **Step 3: Rebuild and run integration tests**

```bash
make stop-server
make start-dev
make test-integration
```

- [ ] **Step 4: Final commit referencing issue closure**

```bash
git add -A
git commit -m "refactor(api): replace provider '*' with 'tmi' for all built-in groups

All built-in groups (everyone, security-reviewers, administrators,
confidential-project-reviewers, embedding-automation, tmi-automation)
now use 'tmi' as their provider instead of the wildcard '*'.

A data migration in the seed layer updates existing database rows.
The enrichment layer retains a provider-agnostic fallback for clients
that may still send '*' during the transition.

BREAKING CHANGE: API responses for built-in group authorization entries
now return provider='tmi' instead of provider='*'. Clients that match
on provider='*' must be updated.

Closes #255"
```
