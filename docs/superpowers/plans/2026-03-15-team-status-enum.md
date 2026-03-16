# Team Status Enum Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Constrain `TeamBase.status` from a freeform string to a 7-value enum with server-side defaulting to `"active"`.

**Architecture:** Update the OpenAPI spec to add an enum constraint on `TeamBase.status` and `TeamListItem.status`, regenerate the API code, add type conversions in the store layer, and add default-to-active logic in Create and Update.

**Tech Stack:** OpenAPI 3.0.3, oapi-codegen v2, Go/Gin, GORM, jq

**Spec:** `docs/superpowers/specs/2026-03-15-team-status-enum-design.md`

---

## Chunk 1: OpenAPI Schema + Code Generation

### Task 1: Update OpenAPI spec

**Files:**
- Modify: `api-schema/tmi-openapi.json` (TeamBase.properties.status, TeamListItem.properties.status, GET /teams status param description)

- [ ] **Step 1: Update `TeamBase.properties.status`**

Use jq to replace the status property in TeamBase:

```bash
jq '.components.schemas.TeamBase.properties.status = {
  "type": "string",
  "nullable": true,
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to '\''active'\'' if not provided or set to null.",
  "example": "active"
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Update `TeamListItem.properties.status`**

```bash
jq '.components.schemas.TeamListItem.properties.status = {
  "type": "string",
  "nullable": true,
  "enum": ["active", "on_hold", "winding_down", "archived", "forming", "merging", "splitting"],
  "description": "Team lifecycle status. Defaults to '\''active'\'' if not provided or set to null.",
  "example": "active"
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Update GET /teams status query parameter description**

Update the `description` field of the `status` query parameter on `GET /teams` to document the valid enum values:

```bash
jq '(.paths["/teams"].get.parameters[] | select(.name == "status")).description = "Filter by team lifecycle status (exact match, comma-separated for multiple). Valid values: active, on_hold, winding_down, archived, forming, merging, splitting"' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 4: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: No errors (warnings about public endpoints are expected and acceptable).

- [ ] **Step 5: Regenerate API code**

Run: `make generate-api`
Expected: Generates `api/api.go` successfully. The generated code will contain new enum types for the status field.

- [ ] **Step 6: Identify the generated enum type name**

After generation, find the exact generated type name:

```bash
grep -n 'TeamBase.*Status\|TeamListItem.*Status' api/api.go | head -20
```

Note the type name (likely `TeamBaseStatus` and `TeamListItemStatus`) and the generated constants. These are needed for Tasks 2 and 3.

- [ ] **Step 7: Build to check for compilation errors**

Run: `make build-server`
Expected: Compilation errors in `api/team_store_gorm.go` due to type mismatches (`*string` vs `*EnumType`). This is expected — Task 2 fixes them.

- [ ] **Step 8: Commit the OpenAPI spec change only**

```bash
git add api-schema/tmi-openapi.json
git commit -m "fix(api): constrain TeamBase.status to enum values (#181)

Add enum constraint with values: active, on_hold, winding_down, archived,
forming, merging, splitting. Update TeamListItem.status to match.
Remove maxLength/pattern validators (enum is sufficient).

Closes #181"
```

---

## Chunk 2: Store Layer — Type Conversions + Default Logic

### Task 2: Fix type conversions in team_store_gorm.go

**Files:**
- Modify: `api/team_store_gorm.go` (Create, Update, recordToAPI, List methods)

The generated enum type (from Task 1 Step 6) replaces `*string` in the API types. The GORM model remains `*string`. Add conversion helpers and update each method.

- [ ] **Step 1: Add a helper function for enum-to-string and string-to-enum conversion**

At the top of `api/team_store_gorm.go` (after the imports/constants), add:

```go
// teamStatusDefault is the default team lifecycle status.
const teamStatusDefault = "active"

// teamStatusToString converts a *TeamBaseStatus to *string for GORM storage.
func teamStatusToString(s *TeamBaseStatus) *string {
	if s == nil {
		return nil
	}
	str := string(*s)
	return &str
}

// stringToTeamStatus converts a *string from GORM to *TeamBaseStatus for the API.
func stringToTeamStatus(s *string) *TeamBaseStatus {
	if s == nil {
		return nil
	}
	status := TeamBaseStatus(*s)
	return &status
}

// stringToTeamListItemStatus converts a *string from GORM to *TeamListItemStatus for the API.
func stringToTeamListItemStatus(s *string) *TeamListItemStatus {
	if s == nil {
		return nil
	}
	status := TeamListItemStatus(*s)
	return &status
}
```

**Note:** The actual type names (`TeamBaseStatus`, `TeamListItemStatus`) must match what oapi-codegen generated in Task 1 Step 6. Adjust if the generated names differ.

- [ ] **Step 2: Add defaulting logic in Create method**

In the `Create` method, before the `record := &models.TeamRecord{...}` block, add:

```go
// Default status to "active" if not provided
if team.Status == nil {
	defaultStatus := TeamBaseStatus(teamStatusDefault)
	team.Status = &defaultStatus
}
```

Update the record creation to use the conversion:

```go
Status: teamStatusToString(team.Status),
```

(replacing the current `Status: team.Status`)

- [ ] **Step 3: Add defaulting logic in Update method**

In the `Update` method, before the `updates := map[string]any{...}` block, add:

```go
// Default status to "active" if nullified
if team.Status == nil {
	defaultStatus := TeamBaseStatus(teamStatusDefault)
	team.Status = &defaultStatus
}
```

Update the updates map to use the conversion:

```go
"status": teamStatusToString(team.Status),
```

(replacing the current `"status": team.Status`)

- [ ] **Step 4: Fix recordToAPI method**

In `recordToAPI`, change:

```go
if record.Status != nil {
	team.Status = record.Status
}
```

to:

```go
if record.Status != nil {
	team.Status = stringToTeamStatus(record.Status)
}
```

- [ ] **Step 5: Fix List method TeamListItem population**

In the `List` method, in the loop where `TeamListItem` is constructed, change:

```go
Status: rec.Status,
```

to:

```go
Status: stringToTeamListItemStatus(rec.Status),
```

- [ ] **Step 6: Build**

Run: `make build-server`
Expected: Clean compilation with no errors.

- [ ] **Step 7: Run existing tests**

Run: `make test-unit`
Expected: All existing tests pass. The mock store tests may need fixes (Task 3).

- [ ] **Step 8: Commit**

```bash
git add api/api.go api/team_store_gorm.go
git commit -m "fix(api): add type conversions and default logic for team status enum

Add enum-to-string conversion helpers. Default status to 'active' in
Create and Update when nil. Fix recordToAPI and List type conversions."
```

---

## Chunk 3: Tests

### Task 3: Update tests for status defaulting and enum behavior

**Files:**
- Modify: `api/team_handlers_test.go` (TestCreateTeam, TestUpdateTeam, TestPatchTeam, seedTeamInStore, mock Create)

- [ ] **Step 1: Update mock store Create to apply the same defaulting**

In the `mockTeamStore.Create` method, add status defaulting to mirror real behavior:

```go
func (m *mockTeamStore) Create(_ context.Context, team *Team, _ string) (*Team, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.err != nil {
		return nil, m.err
	}
	if team.Id == nil {
		id := uuid.New()
		team.Id = &id
	}
	// Default status to "active" if not provided (mirrors GormTeamStore behavior)
	if team.Status == nil {
		defaultStatus := TeamBaseStatus("active")
		team.Status = &defaultStatus
	}
	now := time.Now().UTC()
	team.CreatedAt = &now
	team.ModifiedAt = &now
	m.teams[team.Id.String()] = team
	return team, nil
}
```

- [ ] **Step 2: Update mock store Update to apply the same defaulting**

In the `mockTeamStore.Update` method (find it near the Create method), add status defaulting:

```go
// Default status to "active" if nullified
if team.Status == nil {
	defaultStatus := TeamBaseStatus("active")
	team.Status = &defaultStatus
}
```

- [ ] **Step 3: Add test for create without status defaults to active**

Add to `TestCreateTeam`:

```go
t.Run("defaults status to active when not provided", func(t *testing.T) {
	store := newMockTeamStore()
	saveTeamProjectStores(t, store, nil)
	setupTestTeamAuthDB(t)

	body := TeamInput{
		Name: "No Status Team",
	}
	bodyBytes, _ := json.Marshal(body)
	c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)
	TestUsers.Owner.SetContext(c)

	server.CreateTeam(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	var created Team
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.NotNil(t, created.Status)
	assert.Equal(t, TeamBaseStatus("active"), *created.Status)
})
```

- [ ] **Step 4: Add test for create with explicit status**

Add to `TestCreateTeam`:

```go
t.Run("accepts explicit status value", func(t *testing.T) {
	store := newMockTeamStore()
	saveTeamProjectStores(t, store, nil)
	setupTestTeamAuthDB(t)

	status := TeamBaseStatus("forming")
	body := TeamInput{
		Name:   "Forming Team",
		Status: &status,
	}
	bodyBytes, _ := json.Marshal(body)
	c, w := CreateTestGinContextWithBody("POST", "/teams", "application/json", bodyBytes)
	TestUsers.Owner.SetContext(c)

	server.CreateTeam(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	var created Team
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.NotNil(t, created.Status)
	assert.Equal(t, TeamBaseStatus("forming"), *created.Status)
})
```

- [ ] **Step 5: Add test for update nullifying status defaults to active**

Add to `TestUpdateTeam`:

```go
t.Run("defaults status to active when nullified", func(t *testing.T) {
	store := newMockTeamStore()
	seedTeamInStore(store, testTeamID, "Status Team")
	saveTeamProjectStores(t, store, nil)

	db := setupTestTeamAuthDB(t)
	seedTeamAuthData(t, db, testTeamID, testUserUUID)

	body := TeamInput{Name: "Status Team"}
	// Status is nil (not provided)
	bodyBytes, _ := json.Marshal(body)
	teamUUID, _ := uuid.Parse(testTeamID)
	c, w := CreateTestGinContextWithBody("PUT", "/teams/"+testTeamID, "application/json", bodyBytes)
	TestUsers.Owner.SetContext(c)

	server.UpdateTeam(c, teamUUID)

	assert.Equal(t, http.StatusOK, w.Code)
	var updated Team
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.NotNil(t, updated.Status)
	assert.Equal(t, TeamBaseStatus("active"), *updated.Status)
})
```

- [ ] **Step 6: Add test for patch nullifying status defaults to active**

Add to `TestPatchTeam`:

```go
t.Run("defaults status to active when nullified via patch", func(t *testing.T) {
	store := newMockTeamStore()
	// Seed with an explicit status
	id := seedTeamInStore(store, testTeamID, "Status Team")
	formingStatus := TeamBaseStatus("forming")
	store.teams[testTeamID].Status = &formingStatus
	saveTeamProjectStores(t, store, nil)

	db := setupTestTeamAuthDB(t)
	seedTeamAuthData(t, db, testTeamID, testUserUUID)

	patch := []PatchOperation{
		{Op: "replace", Path: "/status", Value: nil},
	}
	patchBytes, _ := json.Marshal(patch)
	c, w := CreateTestGinContextWithBody("PATCH", "/teams/"+testTeamID, "application/json-patch+json", patchBytes)
	TestUsers.Owner.SetContext(c)

	server.PatchTeam(c, id)

	assert.Equal(t, http.StatusOK, w.Code)
	var patched Team
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
	assert.NotNil(t, patched.Status)
	assert.Equal(t, TeamBaseStatus("active"), *patched.Status)
})
```

- [ ] **Step 7: Run unit tests**

Run: `make test-unit`
Expected: All tests pass including the new ones.

- [ ] **Step 8: Lint**

Run: `make lint`
Expected: Clean.

- [ ] **Step 9: Commit**

```bash
git add api/team_handlers_test.go
git commit -m "test(api): add tests for team status enum defaulting

Verify status defaults to 'active' on create when omitted, on update
when nullified, and on patch when nullified. Verify explicit enum values
are preserved."
```

---

## Chunk 4: Final Verification

### Task 4: Integration test and cleanup

- [ ] **Step 1: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 2: Run integration tests**

Run: `make test-integration`
Expected: All tests pass. Enum values round-trip correctly through PostgreSQL.

- [ ] **Step 3: Run lint**

Run: `make lint`
Expected: Clean (api/api.go ST1005 warnings are expected from auto-generated code).

- [ ] **Step 4: Push**

```bash
git pull --rebase
git push
```
