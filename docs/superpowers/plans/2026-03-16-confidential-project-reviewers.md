# Confidential Project Reviewers Group Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a well-known "Confidential Project Reviewers" group that is auto-added to confidential survey responses and threat models, while also auto-adding "Security Reviewers" to non-confidential threat models (new behavior).

**Architecture:** Follow the existing well-known group pattern: constants in auth_utils.go, UUID in validators.go, BuiltInGroup var in group_membership.go, seed in seed.go, and auto-assignment logic in the survey response store and threat model handler layers respectively.

**Tech Stack:** Go, GORM, Gin, httptest

**Spec:** `docs/superpowers/specs/2026-03-16-confidential-project-reviewers-design.md`

---

### Task 1: Add constants and helper functions

**Files:**
- Modify: `api/auth_utils.go:416-489`
- Modify: `api/validation/validators.go:46-54,267-277`
- Modify: `api/group_membership.go:18-24`
- Test: `api/auth_utils_extended_test.go:1051-1135`

- [ ] **Step 1: Write the failing tests**

Add to `api/auth_utils_extended_test.go` after the existing `TestSecurityReviewersHelpers` function (after line 1135):

```go
func TestConfidentialProjectReviewersHelpers(t *testing.T) {
	t.Run("ConfidentialProjectReviewersAuthorization returns correct entry", func(t *testing.T) {
		auth := ConfidentialProjectReviewersAuthorization()
		assert.Equal(t, AuthorizationPrincipalTypeGroup, auth.PrincipalType)
		assert.Equal(t, "*", auth.Provider)
		assert.Equal(t, ConfidentialProjectReviewersGroup, auth.ProviderId)
		assert.Equal(t, AuthorizationRoleOwner, auth.Role)
	})

	t.Run("IsConfidentialProjectReviewersGroup identifies correct group", func(t *testing.T) {
		tests := []struct {
			name     string
			auth     Authorization
			expected bool
		}{
			{
				name: "exact confidential-project-reviewers group",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    ConfidentialProjectReviewersGroup,
					Role:          AuthorizationRoleOwner,
				},
				expected: true,
			},
			{
				name: "user not group",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeUser,
					Provider:      "*",
					ProviderId:    ConfidentialProjectReviewersGroup,
					Role:          AuthorizationRoleOwner,
				},
				expected: false,
			},
			{
				name: "security-reviewers group is not confidential",
				auth: Authorization{
					PrincipalType: AuthorizationPrincipalTypeGroup,
					Provider:      "*",
					ProviderId:    SecurityReviewersGroup,
					Role:          AuthorizationRoleOwner,
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := IsConfidentialProjectReviewersGroup(tt.auth)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestConfidentialProjectReviewersHelpers`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Add constants to `api/auth_utils.go`**

In the built-in group constants block (line 417-433), add after `AdministratorsGroupUUID`:

```go
	// ConfidentialProjectReviewersGroup is the group_name for the built-in
	// Confidential Project Reviewers group. Used for reviews of confidential
	// survey responses and threat models.
	ConfidentialProjectReviewersGroup = "confidential-project-reviewers"

	// ConfidentialProjectReviewersGroupUUID is the well-known UUID for the
	// Confidential Project Reviewers built-in group.
	ConfidentialProjectReviewersGroupUUID = "00000000-0000-0000-0000-000000000003"
```

Update the comment on `SecurityReviewersGroup` (line 418-420) to:

```go
	// SecurityReviewersGroup is a built-in group for security engineers who triage survey responses and threat models.
	// Unlike pseudo-groups, this is a regular group that can have members managed via the admin API.
	// It is provider-independent (provider = "*") and auto-added to non-confidential survey responses and threat models.
```

Update the comment on `SecurityReviewersAuthorization` (line 474-475) to:

```go
// SecurityReviewersAuthorization returns an Authorization entry for the Security Reviewers group
// with owner role. This is used to auto-add Security Reviewers to non-confidential survey responses and threat models.
```

Add the helper functions after `IsSecurityReviewersGroup` (after line 489):

```go
// ConfidentialProjectReviewersAuthorization returns an Authorization entry for the
// Confidential Project Reviewers group with owner role. This is used to auto-add
// Confidential Project Reviewers to confidential survey responses and threat models.
func ConfidentialProjectReviewersAuthorization() Authorization {
	return Authorization{
		PrincipalType: AuthorizationPrincipalTypeGroup,
		Provider:      "*",
		ProviderId:    ConfidentialProjectReviewersGroup,
		Role:          AuthorizationRoleOwner,
	}
}

// IsConfidentialProjectReviewersGroup checks if an authorization entry represents the Confidential Project Reviewers group
func IsConfidentialProjectReviewersGroup(auth Authorization) bool {
	return auth.PrincipalType == AuthorizationPrincipalTypeGroup &&
		auth.ProviderId == ConfidentialProjectReviewersGroup
}
```

- [ ] **Step 4: Add UUID to `api/validation/validators.go`**

After `AdministratorsGroupUUID` (line 53), add:

```go
	// ConfidentialProjectReviewersGroupUUID is the well-known UUID for the Confidential Project Reviewers group.
	ConfidentialProjectReviewersGroupUUID = "00000000-0000-0000-0000-000000000003"
```

Add the new UUID to `BuiltInGroupUUIDs` slice (line 268-272):

```go
var BuiltInGroupUUIDs = []string{
	EveryonePseudoGroupUUID,
	SecurityReviewersGroupUUID,
	AdministratorsGroupUUID,
	ConfidentialProjectReviewersGroupUUID,
}
```

- [ ] **Step 5: Add BuiltInGroup var to `api/group_membership.go`**

After `GroupSecurityReviewers` (line 23), add:

```go
	// GroupConfidentialProjectReviewers is the built-in Confidential Project Reviewers group.
	GroupConfidentialProjectReviewers = BuiltInGroup{Name: ConfidentialProjectReviewersGroup, UUID: uuid.MustParse(ConfidentialProjectReviewersGroupUUID)}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit name=TestConfidentialProjectReviewersHelpers`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/auth_utils.go api/auth_utils_extended_test.go api/validation/validators.go api/group_membership.go
git commit -m "feat(api): add confidential-project-reviewers constants and helpers (#187)"
```

---

### Task 2: Add seed function

**Files:**
- Modify: `api/seed/seed.go:13-42`

- [ ] **Step 1: Add `seedConfidentialProjectReviewersGroup` to `api/seed/seed.go`**

Add a call in `SeedDatabase()` after `seedAdministratorsGroup` (after line 38):

```go
	if err := seedConfidentialProjectReviewersGroup(db); err != nil {
		log.Error("Failed to seed 'confidential-project-reviewers' group: %v", err)
		return err
	}
```

Add the seed function after `seedAdministratorsGroup` (after line 141), following the same pattern:

```go
// seedConfidentialProjectReviewersGroup ensures the "confidential-project-reviewers" built-in group exists.
// This group is used for reviewers with access to confidential survey responses and threat models.
func seedConfidentialProjectReviewersGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Confidential Project Reviewers"
	group := models.Group{
		InternalUUID: validation.ConfidentialProjectReviewersGroupUUID,
		Provider:     "*",
		GroupName:    "confidential-project-reviewers",
		Name:         &name,
		UsageCount:   0,
	}

	// Use FirstOrCreate for idempotent seeding
	result := db.Where(&models.Group{
		Provider:  "*",
		GroupName: "confidential-project-reviewers",
	}).FirstOrCreate(&group)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Info("Created 'confidential-project-reviewers' group")
	} else {
		log.Debug("'confidential-project-reviewers' group already exists")
	}

	return nil
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `make build-server`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add api/seed/seed.go
git commit -m "feat(api): seed confidential-project-reviewers group on startup (#187)"
```

---

### Task 3: Auto-add reviewer group to survey responses

**Files:**
- Modify: `api/survey_response_store_gorm.go:136-156,189-214`

- [ ] **Step 1: Note on survey response testing**

The survey response store uses the GORM store layer which requires a real database (integration tests). The auto-add logic in `survey_response_store_gorm.go` will be validated via `make test-integration` after all code changes are complete. Unit tests cannot cover this path since the in-memory store doesn't use the GORM create path.

- [ ] **Step 2: Add `ensureConfidentialProjectReviewersGroup` method**

Add after `ensureSecurityReviewersGroup` in `api/survey_response_store_gorm.go` (mirroring lines 189-214):

```go
// ensureConfidentialProjectReviewersGroup ensures the confidential-project-reviewers group exists and returns its UUID.
func (s *GormSurveyResponseStore) ensureConfidentialProjectReviewersGroup(tx *gorm.DB) (string, error) {
	var group models.Group
	result := tx.Where("group_name = ? AND provider = ?", ConfidentialProjectReviewersGroup, "*").First(&group)

	if result.Error == nil {
		return group.InternalUUID, nil
	}

	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", result.Error
	}

	// Create the group
	groupName := "Confidential Project Reviewers"
	group = models.Group{
		InternalUUID: ConfidentialProjectReviewersGroupUUID,
		Provider:     "*",
		GroupName:    ConfidentialProjectReviewersGroup,
		Name:         &groupName,
		UsageCount:   1,
	}

	if err := tx.Create(&group).Error; err != nil {
		// Handle race condition - another transaction may have created it
		var existingGroup models.Group
		if tx.Where("group_name = ? AND provider = ?", ConfidentialProjectReviewersGroup, "*").First(&existingGroup).Error == nil {
			return existingGroup.InternalUUID, nil
		}
		return "", err
	}

	return group.InternalUUID, nil
}
```

- [ ] **Step 3: Add else branch to the create path**

Modify `api/survey_response_store_gorm.go` lines 136-156. Change the `if !isConfidential` block to add an `else` branch:

```go
	// Add Security Reviewers group if not confidential, or Confidential Project Reviewers if confidential
	isConfidential := response.IsConfidential != nil && *response.IsConfidential
	if !isConfidential {
		// Get or create Security Reviewers group
		groupUUID, err := s.ensureSecurityReviewersGroup(tx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to ensure security reviewers group: %w", err)
		}

		reviewersAccess := models.SurveyResponseAccess{
			SurveyResponseID:  model.ID,
			GroupInternalUUID: &groupUUID,
			SubjectType:       "group",
			Role:              string(AuthorizationRoleOwner),
		}
		if err := tx.Create(&reviewersAccess).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create security reviewers access: %w", err)
		}
	} else {
		// Get or create Confidential Project Reviewers group
		groupUUID, err := s.ensureConfidentialProjectReviewersGroup(tx)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to ensure confidential project reviewers group: %w", err)
		}

		reviewersAccess := models.SurveyResponseAccess{
			SurveyResponseID:  model.ID,
			GroupInternalUUID: &groupUUID,
			SubjectType:       "group",
			Role:              string(AuthorizationRoleOwner),
		}
		if err := tx.Create(&reviewersAccess).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create confidential project reviewers access: %w", err)
		}
	}
```

- [ ] **Step 4: Build to verify compilation**

Run: `make build-server`
Expected: SUCCESS

- [ ] **Step 5: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/survey_response_store_gorm.go
git commit -m "feat(api): auto-add confidential-project-reviewers to confidential survey responses (#187)"
```

---

### Task 4: Auto-add reviewer group to threat models

**Files:**
- Modify: `api/threat_model_handlers.go:306-309`
- Test: `api/threat_model_handlers_test.go:1187-1266`

- [ ] **Step 1: Write the failing tests**

Add to `api/threat_model_handlers_test.go` after the existing `TestIsConfidentialField` function. These tests verify the auto-assignment of reviewer groups:

```go
func TestReviewerGroupAutoAssignment(t *testing.T) {
	r := setupThreatModelRouter()

	t.Run("Non-confidential threat model gets security-reviewers group", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]any{
			"name":        "Non-Confidential Model",
			"description": "Should get security-reviewers",
		})

		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)

		// Verify security-reviewers group is in authorization
		found := false
		for _, auth := range tm.Authorization {
			if IsSecurityReviewersGroup(auth) {
				found = true
				assert.Equal(t, AuthorizationRoleOwner, auth.Role)
				break
			}
		}
		assert.True(t, found, "security-reviewers group should be auto-added to non-confidential threat model")

		// Clean up
		if tm.Id != nil {
			delReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
			delW := httptest.NewRecorder()
			r.ServeHTTP(delW, delReq)
		}
	})

	t.Run("Confidential threat model gets confidential-project-reviewers group", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]any{
			"name":            "Confidential Model",
			"description":     "Should get confidential-project-reviewers",
			"is_confidential": true,
		})

		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)

		// Verify confidential-project-reviewers group is in authorization
		found := false
		for _, auth := range tm.Authorization {
			if IsConfidentialProjectReviewersGroup(auth) {
				found = true
				assert.Equal(t, AuthorizationRoleOwner, auth.Role)
				break
			}
		}
		assert.True(t, found, "confidential-project-reviewers group should be auto-added to confidential threat model")

		// Verify security-reviewers is NOT present
		for _, auth := range tm.Authorization {
			assert.False(t, IsSecurityReviewersGroup(auth), "security-reviewers should NOT be on confidential threat model")
		}

		// Clean up
		if tm.Id != nil {
			delReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
			delW := httptest.NewRecorder()
			r.ServeHTTP(delW, delReq)
		}
	})

	t.Run("Explicitly non-confidential threat model gets security-reviewers group", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]any{
			"name":            "Explicit Non-Confidential",
			"description":     "is_confidential explicitly false",
			"is_confidential": false,
		})

		req, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var tm ThreatModel
		err := json.Unmarshal(w.Body.Bytes(), &tm)
		require.NoError(t, err)

		found := false
		for _, auth := range tm.Authorization {
			if IsSecurityReviewersGroup(auth) {
				found = true
				break
			}
		}
		assert.True(t, found, "security-reviewers group should be auto-added when is_confidential=false")

		// Clean up
		if tm.Id != nil {
			delReq, _ := http.NewRequest("DELETE", "/threat_models/"+tm.Id.String(), nil)
			delW := httptest.NewRecorder()
			r.ServeHTTP(delW, delReq)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestReviewerGroupAutoAssignment`
Expected: FAIL — security-reviewers group not found in authorization

- [ ] **Step 3: Add auto-assignment logic to `CreateThreatModel`**

Modify `api/threat_model_handlers.go` after line 308 (the `ApplySecurityReviewerRule` call). Add:

```go
	// Auto-add reviewer group based on confidentiality (skip if already present)
	if tm.IsConfidential != nil && *tm.IsConfidential {
		if !hasGroup(tm.Authorization, IsConfidentialProjectReviewersGroup) {
			tm.Authorization = append(tm.Authorization, ConfidentialProjectReviewersAuthorization())
		}
	} else {
		if !hasGroup(tm.Authorization, IsSecurityReviewersGroup) {
			tm.Authorization = append(tm.Authorization, SecurityReviewersAuthorization())
		}
	}
```

Also add the `hasGroup` helper to `api/threat_model_handlers.go` (or `api/auth_utils.go` if preferred):

```go
// hasGroup checks if any authorization entry matches the given predicate
func hasGroup(authList []Authorization, predicate func(Authorization) bool) bool {
	for _, auth := range authList {
		if predicate(auth) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestReviewerGroupAutoAssignment`
Expected: PASS

- [ ] **Step 5: Run all unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/threat_model_handlers.go api/threat_model_handlers_test.go
git commit -m "feat(api): auto-add reviewer group to threat models based on confidentiality (#187)"
```

---

### Task 5: Validation test for BuiltInGroup protection

**Files:**
- Test: `api/auth_utils_extended_test.go` (or appropriate validation test file)

- [ ] **Step 1: Write test verifying IsBuiltInGroup includes the new UUID**

Add to `api/auth_utils_extended_test.go`:

```go
func TestConfidentialProjectReviewersIsBuiltIn(t *testing.T) {
	assert.True(t, validation.IsBuiltInGroup(validation.ConfidentialProjectReviewersGroupUUID),
		"confidential-project-reviewers should be a built-in group")
}
```

Add the import `"github.com/ericfitz/tmi/api/validation"` if not already present in the import block.

- [ ] **Step 2: Run test to verify it passes**

Run: `make test-unit name=TestConfidentialProjectReviewersIsBuiltIn`
Expected: PASS (since we added the UUID to BuiltInGroupUUIDs in Task 1)

- [ ] **Step 3: Commit**

```bash
git add api/auth_utils_extended_test.go
git commit -m "test(api): verify confidential-project-reviewers is a protected built-in group (#187)"
```

---

### Task 6: Final validation

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Run build**

Run: `make build-server`
Expected: SUCCESS

- [ ] **Step 4: Final commit if any lint fixes were needed**

If lint required changes:
```bash
git add -A
git commit -m "fix(api): lint fixes for confidential-project-reviewers (#187)"
```

- [ ] **Step 5: Close issue #187**

```bash
gh issue close 187 --repo ericfitz/tmi --reason completed --comment "Implemented in release/1.3.0 branch. Closes #187."
```
