# TeamStatus Named Schema Refactor Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the inline TeamBase status enum to a named `TeamStatus` schema with `$ref`, unifying three generated types into one.

**Architecture:** Add `TeamStatus` to OpenAPI `components/schemas`, replace inline enums in `TeamBase` and `TeamListItem` with `allOf` + `$ref` + `nullable` wrapper, regenerate API code, then update store/handler/test code to use the unified type.

**Tech Stack:** OpenAPI 3.0.3, oapi-codegen v2, Go, Gin, GORM

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `api-schema/tmi-openapi.json` | Modify | Add `TeamStatus` named schema; update `TeamBase.status` and `TeamListItem.status` to use `$ref` |
| `api/api.go` | Regenerate | Auto-generated from OpenAPI spec |
| `api/team_store_gorm.go` | Modify | Remove `stringToTeamListItemStatus`; update List method call site |
| `api/team_handlers.go` | Modify | Remove `(*TeamStatus)(req.Status)` casts in CreateTeam and UpdateTeam |
| `api/team_handlers_test.go` | Modify | Fix `TeamBaseStatus` reference; add status-specific tests |

---

## Chunk 1: OpenAPI Schema and Code Generation

### Task 1: Add TeamStatus Named Schema to OpenAPI Spec

**Files:**
- Modify: `api-schema/tmi-openapi.json:9456` (insert before closing `}` of schemas, after `ProjectStatus`)

- [ ] **Step 1: Add the TeamStatus schema**

Insert `TeamStatus` named schema in `components/schemas` right after the `ProjectStatus` schema (after line 9456). Use jq for surgical JSON modification:

```bash
jq '.components.schemas.TeamStatus = {
  "type": "string",
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to '\''active'\'' if not provided or set to null."
}' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 2: Replace TeamBase.status inline enum with $ref**

Replace the inline enum at `TeamBase.properties.status` (lines 8638-8651) with the `allOf` + `nullable` wrapper:

```bash
jq '.components.schemas.TeamBase.properties.status = {
  "nullable": true,
  "allOf": [{"$ref": "#/components/schemas/TeamStatus"}],
  "description": "Team lifecycle status. Defaults to '\''active'\'' if not provided or set to null.",
  "example": "active"
}' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 3: Replace TeamListItem.status inline enum with $ref**

Replace the inline enum at `TeamListItem.properties.status` (lines 8766-8779) with the same wrapper:

```bash
jq '.components.schemas.TeamListItem.properties.status = {
  "nullable": true,
  "allOf": [{"$ref": "#/components/schemas/TeamStatus"}],
  "description": "Team lifecycle status. Defaults to '\''active'\'' if not provided or set to null.",
  "example": "active"
}' api-schema/tmi-openapi.json > /tmp/tmi-openapi-tmp.json && mv /tmp/tmi-openapi-tmp.json api-schema/tmi-openapi.json
```

- [ ] **Step 4: Validate OpenAPI spec**

Run: `make validate-openapi`
Expected: PASS with no errors

- [ ] **Step 5: Commit OpenAPI changes**

```bash
git add api-schema/tmi-openapi.json
git commit -m "feat(api): add TeamStatus enum to OpenAPI spec and update refs (#185)"
```

### Task 2: Regenerate API Code

**Files:**
- Regenerate: `api/api.go`

- [ ] **Step 1: Regenerate API types**

Run: `make generate-api`
Expected: Successfully generates `api/api.go`

- [ ] **Step 2: Verify unified TeamStatus type**

Confirm the generated code has:
- A single `type TeamStatus string` (not `TeamBaseStatus` or `TeamListItemStatus`)
- Constants like `TeamStatusActive`, `TeamStatusOnHold`, etc.
- `TeamBase.Status` is `*TeamStatus`
- `TeamListItem.Status` is `*TeamStatus`

```bash
grep -n "type TeamStatus\|type TeamBaseStatus\|type TeamListItemStatus\|TeamStatusActive\|TeamBaseStatusActive\|TeamListItemStatusActive" api/api.go
```

Expected: Only `TeamStatus` type and `TeamStatus*` constants; no `TeamBaseStatus` or `TeamListItemStatus`.

- [ ] **Step 3: Check build compiles (expect errors)**

Run: `make build-server`
Expected: FAIL — compile errors in `team_store_gorm.go` (removed `TeamListItemStatus` type) and `team_handlers_test.go` (removed `TeamBaseStatus` type). This confirms what needs updating.

## Chunk 2: Store and Handler Updates

### Task 3: Update Store Layer

**Files:**
- Modify: `api/team_store_gorm.go:40-47` (remove `stringToTeamListItemStatus`)
- Modify: `api/team_store_gorm.go:606` (update List method call site)

- [ ] **Step 1: Remove stringToTeamListItemStatus function**

Delete the `stringToTeamListItemStatus` function (lines 40-47 of `api/team_store_gorm.go`):

```go
// DELETE THIS:
// stringToTeamListItemStatus converts a *string from GORM to *TeamListItemStatus for the API.
func stringToTeamListItemStatus(s *string) *TeamListItemStatus {
	if s == nil {
		return nil
	}
	status := TeamListItemStatus(*s)
	return &status
}
```

- [ ] **Step 2: Update List method to use stringToTeamStatus**

In the `List` method (~line 606), change:

```go
// BEFORE:
Status:       stringToTeamListItemStatus(rec.Status),

// AFTER:
Status:       stringToTeamStatus(rec.Status),
```

- [ ] **Step 3: Verify build compiles**

Run: `make build-server`
Expected: FAIL — `team_handlers_test.go` still references `TeamBaseStatus`. Store/handler code should compile.

### Task 4: Update Handlers

**Files:**
- Modify: `api/team_handlers.go:108` (CreateTeam cast)
- Modify: `api/team_handlers.go:211` (UpdateTeam cast)

- [ ] **Step 1: Remove cast in CreateTeam**

At line ~108 of `api/team_handlers.go`, change:

```go
// BEFORE:
Status:             (*TeamStatus)(req.Status),

// AFTER:
Status:             req.Status,
```

- [ ] **Step 2: Remove cast in UpdateTeam**

At line ~211 of `api/team_handlers.go`, change:

```go
// BEFORE:
Status:             (*TeamStatus)(req.Status),

// AFTER:
Status:             req.Status,
```

- [ ] **Step 3: Verify build compiles**

Run: `make build-server`
Expected: FAIL — only `team_handlers_test.go` errors remain.

## Chunk 3: Test Updates

### Task 5: Fix Existing Test and Add Status Tests

**Files:**
- Modify: `api/team_handlers_test.go:543` (fix `TeamBaseStatus` reference)
- Modify: `api/team_handlers_test.go` (add new test functions)

- [ ] **Step 1: Fix TeamBaseStatus reference in existing test**

At line 543 of `api/team_handlers_test.go`, change:

```go
// BEFORE:
status := TeamBaseStatus("forming")

// AFTER:
status := TeamStatus("forming")
```

- [ ] **Step 2: Verify build compiles**

Run: `make build-server`
Expected: PASS — all compile errors resolved.

- [ ] **Step 3: Run existing tests to verify no regressions**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Add TestCreateTeamWithStatus**

Add after the existing `TestCreateTeam` function, mirroring `TestCreateProjectWithStatus`:

```go
func TestCreateTeamWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("explicit status is preserved", func(t *testing.T) {
		store := newMockTeamStore()
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		formingStatus := TeamStatusForming
		body := TeamInput{
			Name:   "Forming Team",
			Status: &formingStatus,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateTeam(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		require.NotNil(t, created.Status)
		assert.Equal(t, TeamStatusForming, *created.Status)
	})
}
```

- [ ] **Step 5: Add TestUpdateTeamWithStatus**

```go
func TestUpdateTeamWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("update with valid status", func(t *testing.T) {
		store := newMockTeamStore()
		seedTeamInStore(store, testTeamID, "Test Team")
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		archivedStatus := TeamStatusArchived
		body := TeamInput{
			Name:   "Updated Team",
			Status: &archivedStatus,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateTeam(c, teamUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		require.NotNil(t, updated.Status)
		assert.Equal(t, TeamStatusArchived, *updated.Status)
	})
}
```

- [ ] **Step 6: Add TestListTeamsWithStatus**

```go
func TestListTeamsWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("list items include typed status", func(t *testing.T) {
		activeStatus := TeamStatusActive
		store := newMockTeamStore()
		store.listItems = []TeamListItem{
			{Name: "Team A", Status: &activeStatus},
		}
		store.listTotal = 1
		saveTeamProjectStores(t, store, nil)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/teams")
		TestUsers.Owner.SetContext(c)

		server.ListTeams(c, ListTeamsParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListTeamsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Teams, 1)
		require.NotNil(t, resp.Teams[0].Status)
		assert.Equal(t, TeamStatusActive, *resp.Teams[0].Status)
	})
}
```

- [ ] **Step 7: Add TestPatchTeamWithStatus**

```go
func TestPatchTeamWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("patch status field", func(t *testing.T) {
		activeStatus := TeamStatusActive
		store := newMockTeamStore()
		teamUUID, _ := uuid.Parse(testTeamID)
		store.teams[testTeamID] = &Team{
			Id:     &teamUUID,
			Name:   "Test Team",
			Status: &activeStatus,
		}
		saveTeamProjectStores(t, store, nil)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		patchBody := `[{"op": "replace", "path": "/status", "value": "on_hold"}]`
		c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json", []byte(patchBody))
		TestUsers.Owner.SetContext(c)

		server.PatchTeam(c, teamUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var patched Team
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
		require.NotNil(t, patched.Status)
		assert.Equal(t, TeamStatusOnHold, *patched.Status)
	})
}
```

- [ ] **Step 8: Run all unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 9: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 10: Commit store, handler, and test changes**

```bash
git add api/team_store_gorm.go api/team_handlers.go api/team_handlers_test.go api/api.go
git commit -m "feat(api): add TeamStatus store conversions and handler tests (#185)"
```
