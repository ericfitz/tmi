# Project Status Enum Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the free-text `status` field on Project with a named `ProjectStatus` enum, enforced at the API layer via OpenAPI validation.

**Architecture:** Add a `ProjectStatus` enum schema to the OpenAPI spec, reference it via `$ref` + `allOf` wrapper from `ProjectBase.status` and `ProjectListItem.status`, regenerate API code, then update the store layer with type conversion functions mirroring the Team pattern.

**Tech Stack:** OpenAPI 3.0.3, oapi-codegen, Go, GORM, jq (for spec edits)

---

## Chunk 1: OpenAPI Schema & Code Generation

### Task 1: Add ProjectStatus enum schema to OpenAPI spec

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Add ProjectStatus schema**

Use jq to add the new schema to `components.schemas`:

```bash
jq '.components.schemas.ProjectStatus = {
  "type": "string",
  "enum": [
    "active", "planning", "on_hold", "cancelled",
    "in_development", "in_review", "mvp",
    "limited_availability", "general_availability",
    "deprecated", "end_of_life", "archived"
  ],
  "description": "Project lifecycle status. Defaults to '\''active'\'' if not provided or set to null."
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Update ProjectBase.status to use $ref with allOf wrapper**

Replace the free-text status property in `ProjectBase` with a `$ref` to `ProjectStatus` using the `allOf` + `nullable` pattern required by OpenAPI 3.0.3:

```bash
jq '.components.schemas.ProjectBase.properties.status = {
  "nullable": true,
  "allOf": [{"$ref": "#/components/schemas/ProjectStatus"}],
  "description": "Project lifecycle status. Defaults to '\''active'\'' if not provided or set to null.",
  "example": "active"
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

This removes the previous `maxLength: 128` and `pattern` constraints, which are superseded by enum validation.

- [ ] **Step 3: Update ProjectListItem.status to use $ref with allOf wrapper**

Replace the free-text status property in `ProjectListItem`:

```bash
jq '.components.schemas.ProjectListItem.properties.status = {
  "nullable": true,
  "allOf": [{"$ref": "#/components/schemas/ProjectStatus"}],
  "description": "Project lifecycle status.",
  "example": "active"
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 4: Validate the OpenAPI spec**

```bash
make validate-openapi
```

Expected: Passes with no errors. The spec should be valid OpenAPI 3.0.3.

- [ ] **Step 5: Regenerate API code**

```bash
make generate-api
```

Expected: Generates `api/api.go` with:
- `type ProjectStatus string`
- Constants: `ProjectStatusActive`, `ProjectStatusPlanning`, `ProjectStatusOnHold`, `ProjectStatusCancelled`, `ProjectStatusInDevelopment`, `ProjectStatusInReview`, `ProjectStatusMvp`, `ProjectStatusLimitedAvailability`, `ProjectStatusGeneralAvailability`, `ProjectStatusDeprecated`, `ProjectStatusEndOfLife`, `ProjectStatusArchived`
- `ProjectBase.Status` field type: `*ProjectStatus`
- `ProjectListItem.Status` field type: `*ProjectStatus`

- [ ] **Step 6: Verify generated types**

```bash
grep -A 15 "Defines values for ProjectStatus" api/api.go
grep "Status.*ProjectStatus" api/api.go
```

Expected: 12 enum constants and `*ProjectStatus` fields on both `ProjectBase` and `ProjectListItem`.

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add ProjectStatus enum to OpenAPI spec and regenerate types (#184)"
```

---

## Chunk 2: Store Layer Conversions

### Task 2: Add conversion functions, update all store code, and fix compilation

All store changes are in a single task because they must all be applied together for the code to compile — `*ProjectStatus` replaces `*string` in the generated types, so every assignment between the GORM model (`*string`) and API types (`*ProjectStatus`) must be updated atomically.

**Files:**
- Modify: `api/project_store_gorm.go`
- Modify: `api/project_handlers_test.go`

- [ ] **Step 1: Write failing test for conversion functions**

Add to `api/project_handlers_test.go`:

```go
func TestProjectStatusConversions(t *testing.T) {
	t.Run("projectStatusToString with value", func(t *testing.T) {
		status := ProjectStatusActive
		result := projectStatusToString(&status)
		require.NotNil(t, result)
		assert.Equal(t, "active", *result)
	})

	t.Run("projectStatusToString with nil", func(t *testing.T) {
		result := projectStatusToString(nil)
		assert.Nil(t, result)
	})

	t.Run("stringToProjectStatus with value", func(t *testing.T) {
		s := "planning"
		result := stringToProjectStatus(&s)
		require.NotNil(t, result)
		assert.Equal(t, ProjectStatusPlanning, *result)
	})

	t.Run("stringToProjectStatus with nil", func(t *testing.T) {
		result := stringToProjectStatus(nil)
		assert.Nil(t, result)
	})
}
```

- [ ] **Step 2: Add conversion functions to project_store_gorm.go**

Add after the imports and before the `ProjectFilters` struct (line 16 of `api/project_store_gorm.go`):

```go
// projectStatusDefault is the default project lifecycle status.
const projectStatusDefault = "active"

// projectStatusToString converts a *ProjectStatus to *string for GORM storage.
func projectStatusToString(s *ProjectStatus) *string {
	if s == nil {
		return nil
	}
	str := string(*s)
	return &str
}

// stringToProjectStatus converts a *string from GORM to *ProjectStatus for the API.
func stringToProjectStatus(s *string) *ProjectStatus {
	if s == nil {
		return nil
	}
	status := ProjectStatus(*s)
	return &status
}
```

- [ ] **Step 3: Update Create method (~line 48) with defaulting and conversion**

After the relationship validation block (line 74) and before the record building (line 77), add:

```go
	// Default status to "active" if not provided
	if project.Status == nil {
		defaultStatus := ProjectStatus(projectStatusDefault)
		project.Status = &defaultStatus
	}
```

Then change line 83 from:
```go
		Status:                project.Status,
```
to:
```go
		Status:                projectStatusToString(project.Status),
```

- [ ] **Step 4: Update Update method (~line 236) with defaulting and conversion**

Replace the status handling block (lines 248-250):

From:
```go
	if project.Status != nil {
		updates["status"] = *project.Status
	}
```

To (mirroring `team_store_gorm.go` lines 324-335):
```go
	// Default status to "active" if nullified
	if project.Status == nil {
		defaultStatus := ProjectStatus(projectStatusDefault)
		project.Status = &defaultStatus
	}
	updates["status"] = projectStatusToString(project.Status)
```

- [ ] **Step 5: Update recordToAPI method (~line 763)**

Change line 763 from:
```go
		Status:      record.Status,
```
to:
```go
		Status:      stringToProjectStatus(record.Status),
```

- [ ] **Step 6: Update List method result building (~line 473)**

Change line 473 from:
```go
			Status:      r.Status,
```
to:
```go
			Status:      stringToProjectStatus(r.Status),
```

- [ ] **Step 7: Build to verify all type mismatches are resolved**

```bash
make build-server
```

Expected: PASS — all `*string` ↔ `*ProjectStatus` mismatches should be resolved.

- [ ] **Step 8: Run conversion tests**

```bash
make test-unit name=TestProjectStatusConversions
```

Expected: PASS

- [ ] **Step 9: Run full unit test suite**

```bash
make test-unit
```

Expected: PASS — all existing tests should pass. The mock store's `Create`/`Update` methods store and return whatever `*ProjectStatus` value is passed, so existing tests still work. Status defaulting is a GORM store concern tested via integration tests.

- [ ] **Step 10: Commit**

```bash
git add api/project_store_gorm.go api/project_handlers_test.go
git commit -m "feat(api): add ProjectStatus store conversions and defaulting (#184)"
```

---

## Chunk 3: Handler Tests & Validation

### Task 3: Add handler tests for ProjectStatus enum

**Files:**
- Modify: `api/project_handlers_test.go`

Note: The mock project store does not apply status defaulting (that logic lives in the GORM store). These tests verify that typed `ProjectStatus` values flow correctly through handlers. Default-status behavior is covered by integration tests.

- [ ] **Step 1: Add test for create with explicit status**

Add to `api/project_handlers_test.go`:

```go
func TestCreateProjectWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("explicit status is preserved", func(t *testing.T) {
		teamStore := newMockTeamStore()
		teamStore.isMember = true
		projectStore := newMockProjectStore()
		saveTeamProjectStores(t, teamStore, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)

		teamUUID, _ := uuid.Parse(testTeamID)
		planningStatus := ProjectStatusPlanning
		body := ProjectInput{
			Name:   "Planning Project",
			TeamId: teamUUID,
			Status: &planningStatus,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("POST", "/projects", "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.CreateProject(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		var created Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		require.NotNil(t, created.Status)
		assert.Equal(t, ProjectStatusPlanning, *created.Status)
	})
}
```

- [ ] **Step 2: Add test for update with status**

```go
func TestUpdateProjectWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("update with valid status", func(t *testing.T) {
		projectStore := newMockProjectStore()
		seedProjectInStore(projectStore, testProjectID, "Test Project", testTeamID)
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		teamUUID, _ := uuid.Parse(testTeamID)
		projectUUID, _ := uuid.Parse(testProjectID)
		deprecatedStatus := ProjectStatusDeprecated
		body := ProjectInput{
			Name:   "Updated Project",
			TeamId: teamUUID,
			Status: &deprecatedStatus,
		}
		bodyBytes, _ := json.Marshal(body)
		c, w := CreateTestGinContextWithBody("PUT", "/projects/"+testProjectID, "application/json", bodyBytes)
		TestUsers.Owner.SetContext(c)

		server.UpdateProject(c, projectUUID)

		assert.Equal(t, http.StatusOK, w.Code)
		var updated Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
		require.NotNil(t, updated.Status)
		assert.Equal(t, ProjectStatusDeprecated, *updated.Status)
	})
}
```

- [ ] **Step 3: Add test for list items with status**

```go
func TestListProjectsWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("list items include typed status", func(t *testing.T) {
		activeStatus := ProjectStatusActive
		store := newMockProjectStore()
		store.listItems = []ProjectListItem{
			{Name: "Project A", Status: &activeStatus},
		}
		store.listTotal = 1
		saveTeamProjectStores(t, nil, store)
		setupTestTeamAuthDB(t)

		c, w := CreateTestGinContext("GET", "/projects")
		TestUsers.Owner.SetContext(c)

		server.ListProjects(c, ListProjectsParams{})

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ListProjectsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Projects, 1)
		require.NotNil(t, resp.Projects[0].Status)
		assert.Equal(t, ProjectStatusActive, *resp.Projects[0].Status)
	})
}
```

- [ ] **Step 4: Add test for patch with status**

The PatchProject handler uses `ApplyPatchOperations` via JSON marshal/unmarshal. This test verifies `*ProjectStatus` deserializes correctly from JSON patch string values.

```go
func TestPatchProjectWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	t.Run("patch status field", func(t *testing.T) {
		activeStatus := ProjectStatusActive
		projectStore := newMockProjectStore()
		projectID, _ := uuid.Parse(testProjectID)
		teamID, _ := uuid.Parse(testTeamID)
		projectStore.projects[testProjectID] = &Project{
			Id:     &projectID,
			Name:   "Test Project",
			TeamId: teamID,
			Status: &activeStatus,
		}
		saveTeamProjectStores(t, nil, projectStore)

		db := setupTestTeamAuthDB(t)
		seedTeamAuthData(t, db, testTeamID, testUserUUID)
		seedProjectAuthData(t, db, testProjectID, testTeamID)

		patchBody := `[{"op": "replace", "path": "/status", "value": "deprecated"}]`
		c, w := CreateTestGinContextWithBody("PATCH", "/projects/"+testProjectID, "application/json", []byte(patchBody))
		TestUsers.Owner.SetContext(c)

		server.PatchProject(c, projectID)

		assert.Equal(t, http.StatusOK, w.Code)
		var patched Project
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
		require.NotNil(t, patched.Status)
		assert.Equal(t, ProjectStatusDeprecated, *patched.Status)
	})
}
```

- [ ] **Step 5: Run new tests**

```bash
make test-unit name=TestCreateProjectWithStatus
make test-unit name=TestUpdateProjectWithStatus
make test-unit name=TestListProjectsWithStatus
make test-unit name=TestPatchProjectWithStatus
```

Expected: PASS

- [ ] **Step 6: Run full unit test suite**

```bash
make test-unit
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/project_handlers_test.go
git commit -m "test(api): add tests for ProjectStatus enum on all handler operations (#184)"
```

### Task 4: Lint, build, and final validation

**Files:** None new — validation only.

- [ ] **Step 1: Lint**

```bash
make lint
```

Expected: PASS (ignore expected staticcheck warnings in `api/api.go`).

- [ ] **Step 2: Build**

```bash
make build-server
```

Expected: PASS

- [ ] **Step 3: Run all unit tests**

```bash
make test-unit
```

Expected: PASS

- [ ] **Step 4: Validate OpenAPI spec**

```bash
make validate-openapi
```

Expected: PASS

- [ ] **Step 5: Verify integration test fixture status values are valid enum values**

Check `test/integration/framework/fixtures.go` — the `WithStatus` helper takes a `string` and sends it over HTTP where OpenAPI validation will enforce enum values. Ensure any existing test calls to `WithStatus` use valid enum values (`"active"`, `"planning"`, etc.). The `WithStatus` method signature itself does not need to change since it builds JSON for HTTP requests.

```bash
grep -n "WithStatus" test/integration/framework/fixtures.go test/integration/*.go
```

If any test passes a status value not in the enum, update it to a valid value.
