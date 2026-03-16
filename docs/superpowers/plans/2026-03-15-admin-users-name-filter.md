# Admin Users Name Filter Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `name` query parameter to `GET /admin/users` for case-insensitive substring filtering and sorting by user display name.

**Architecture:** Reuse the existing `NameQueryParam` from the OpenAPI spec. Add name filtering to `UserFilter`, `GormUserStore.List()`, `GormUserStore.Count()`, and the handler. Add `name` to the `SortByQueryParam` enum. Follow the exact pattern established by the existing `email` filter.

**Tech Stack:** Go, OpenAPI 3.0.3, oapi-codegen, Gin, GORM

**Spec:** `docs/superpowers/specs/2026-03-15-admin-users-name-filter-design.md`
**Issue:** [#182](https://github.com/ericfitz/tmi/issues/182)

---

## Chunk 1: OpenAPI Spec + Code Generation

### Task 1: Update OpenAPI spec

**Files:**
- Modify: `api-schema/tmi-openapi.json:10040-10050` (NameQueryParam description)
- Modify: `api-schema/tmi-openapi.json:10448-10452` (SortByQueryParam enum)
- Modify: `api-schema/tmi-openapi.json:34357-34363` (GET /admin/users parameters)

- [ ] **Step 1: Update NameQueryParam description**

Use jq to update the description of the existing `NameQueryParam` from threat-model-specific to generic:

```bash
jq '.components.parameters.NameQueryParam.description = "Filter by name (case-insensitive substring match)"' api-schema/tmi-openapi.json > tmp.json && mv tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add `name` to SortByQueryParam enum**

```bash
jq '.components.parameters.SortByQueryParam.schema.enum += ["name"]' api-schema/tmi-openapi.json > tmp.json && mv tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Add NameQueryParam ref to GET /admin/users parameters**

Add `{ "$ref": "#/components/parameters/NameQueryParam" }` after `EmailQueryParam` in the `/admin/users` GET parameters array. The EmailQueryParam ref is at index 1 (0-indexed), so insert at index 2:

```bash
jq '.paths["/admin/users"].get.parameters |= (.[0:2] + [{"$ref": "#/components/parameters/NameQueryParam"}] + .[2:])' api-schema/tmi-openapi.json > tmp.json && mv tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 4: Validate OpenAPI spec**

Run: `make validate-openapi`
Expected: No errors.

- [ ] **Step 5: Regenerate API code**

Run: `make generate-api`
Expected: Success. `api/api.go` regenerated with `Name *string` field in `ListAdminUsersParams`.

- [ ] **Step 6: Verify generated code has Name parameter**

Run: `grep -A2 'Name.*\*string' api/api.go | head -5`
Expected: Shows `Name *string` in `ListAdminUsersParams` struct.

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add name query parameter to GET /admin/users OpenAPI spec

Add NameQueryParam reference to listAdminUsers endpoint for
case-insensitive substring filtering by user display name.
Add 'name' to SortByQueryParam enum.

Refs #182"
```

---

## Chunk 2: Backend Implementation

### Task 2: Add Name field to UserFilter

**Files:**
- Modify: `api/user_store.go:13-24`

- [ ] **Step 1: Add Name field to UserFilter struct**

In `api/user_store.go`, add `Name` field after `Email` (line 15):

```go
// Before:
Email           string // Case-insensitive ILIKE %email%

// After:
Email           string // Case-insensitive ILIKE %email%
Name            string // Case-insensitive ILIKE %name%
```

- [ ] **Step 2: Update SortBy comment**

In the same struct, update line 22:

```go
// Before:
SortBy          string // created_at, last_login, email

// After:
SortBy          string // created_at, last_login, email, name
```

### Task 3: Add name filter to GormUserStore

**Files:**
- Modify: `api/user_store_gorm.go:34-110` (List method)
- Modify: `api/user_store_gorm.go:194-228` (Count method)

- [ ] **Step 1: Add name filter to List()**

In `api/user_store_gorm.go`, add after the email filter block (after line 45):

```go
	if filter.Name != "" {
		// Use LOWER() for cross-database case-insensitive search
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+filter.Name+"%")
	}
```

- [ ] **Step 2: Add `"name"` to sortBy switch in List()**

In `api/user_store_gorm.go`, update the sortBy switch (line 67):

```go
// Before:
case "created_at", "last_login", "email":

// After:
case "created_at", "last_login", "email", "name":
```

- [ ] **Step 3: Add name filter to Count()**

In `api/user_store_gorm.go`, add after the email filter block in Count() (after line 203):

```go
	if filter.Name != "" {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+filter.Name+"%")
	}
```

### Task 4: Add name parameter extraction to handler

**Files:**
- Modify: `api/admin_user_handlers.go:15-67`

- [ ] **Step 1: Extract params.Name in ListAdminUsers handler**

In `api/admin_user_handlers.go`, add after the email extraction block (after line 53):

```go
	name := ""
	if params.Name != nil {
		name = *params.Name
	}
```

- [ ] **Step 2: Pass name to UserFilter**

In the same file, add `Name` to the filter struct literal (after `Email: email,` on line 58):

```go
	filter := UserFilter{
		Provider:        provider,
		Email:           email,
		Name:            name,
		CreatedAfter:    params.CreatedAfter,
		// ... rest unchanged
	}
```

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: Success, no compilation errors.

- [ ] **Step 4: Commit**

```bash
git add api/user_store.go api/user_store_gorm.go api/admin_user_handlers.go
git commit -m "feat(api): implement name filter and sort for admin users

Add Name field to UserFilter struct. Add LOWER(name) LIKE LOWER(?)
filtering to GormUserStore List() and Count(). Extract params.Name
in ListAdminUsers handler. Add 'name' to valid sortBy values.

Refs #182"
```

---

## Chunk 3: Tests

### Task 5: Add name filter to mock store and write tests

**Files:**
- Modify: `api/admin_user_handlers_test.go`

- [ ] **Step 1: Add name filter to mockUserStore.List()**

In `api/admin_user_handlers_test.go`, in the `List` method of `mockUserStore`, add after the email filter block (after the `continue` on line 64):

```go
		// Apply name filter (simple substring match)
		if filter.Name != "" {
			if !containsIgnoreCase(u.Name, filter.Name) {
				continue
			}
		}
```

- [ ] **Step 2: Add name filter to mockUserStore.Count()**

In the `Count` method, add after the email filter block (after the `continue` on line 145):

```go
		if filter.Name != "" {
			if !containsIgnoreCase(u.Name, filter.Name) {
				continue
			}
		}
```

- [ ] **Step 3: Add test for name substring filtering**

Add a new test case inside `TestListAdminUsers`, after the `Success_FilterByEmail` test (after line 471):

```go
	t.Run("Success_FilterByName", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice Johnson", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Bob Smith", "bob@example.com", "tmi")
		user3 := makeTestAdminUser("Charlie Johnson", "charlie@example.com", "tmi")
		mockStore.addUser(user1)
		mockStore.addUser(user2)
		mockStore.addUser(user3)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		// Filter by "johnson" - should match Alice Johnson and Charlie Johnson
		req, _ := http.NewRequest("GET", "/admin/users?name=johnson", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 2)
		assert.Equal(t, float64(2), response["total"])
	})
```

- [ ] **Step 4: Add test for case-insensitive name matching**

```go
	t.Run("Success_FilterByName_CaseInsensitive", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice Johnson", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Bob Smith", "bob@example.com", "tmi")
		mockStore.addUser(user1)
		mockStore.addUser(user2)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		// Filter by "ALICE" (uppercase) - should match "Alice Johnson"
		req, _ := http.NewRequest("GET", "/admin/users?name=ALICE", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 1)
		assert.Equal(t, float64(1), response["total"])
	})
```

- [ ] **Step 5: Add test for combined name + email filtering**

```go
	t.Run("Success_FilterByNameAndEmail", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice Johnson", "alice@example.com", "tmi")
		user2 := makeTestAdminUser("Alice Smith", "asmith@other.com", "tmi")
		user3 := makeTestAdminUser("Bob Johnson", "bob@example.com", "tmi")
		mockStore.addUser(user1)
		mockStore.addUser(user2)
		mockStore.addUser(user3)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		// Filter by name=alice AND email=example - should match only Alice Johnson
		req, _ := http.NewRequest("GET", "/admin/users?name=alice&email=example", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 1)
		assert.Equal(t, float64(1), response["total"])
	})
```

- [ ] **Step 6: Add test for name filter with no matches**

```go
	t.Run("Success_FilterByName_NoMatch", func(t *testing.T) {
		mockStore := newMockUserStore()
		GlobalUserStore = mockStore

		user1 := makeTestAdminUser("Alice", "alice@example.com", "tmi")
		mockStore.addUser(user1)

		r, _ := setupAdminUserRouter("admin@example.com", uuid.New().String())

		req, _ := http.NewRequest("GET", "/admin/users?name=nonexistent", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		users, ok := response["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 0)
		assert.Equal(t, float64(0), response["total"])
	})
```

- [ ] **Step 7: Run unit tests**

Run: `make test-unit`
Expected: All tests pass, including the 4 new test cases.

- [ ] **Step 8: Commit**

```bash
git add api/admin_user_handlers_test.go
git commit -m "test(api): add tests for name filter on GET /admin/users

Add test cases for name substring filtering, case-insensitive
matching, combined name+email filtering, and no-match scenario.
Update mock store to support name filtering.

Refs #182"
```

---

## Chunk 4: Lint, Final Verification & Issue Closure

### Task 6: Lint and final checks

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: No new warnings.

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 3: Final commit (if any lint fixes needed)**

Only if lint fixes were required in previous steps.

### Task 7: Close issue

- [ ] **Step 1: Close GitHub issue**

```bash
gh issue close 182 --repo ericfitz/tmi --reason completed --comment "Implemented name query parameter for GET /admin/users with case-insensitive substring matching and sort-by-name support."
```
