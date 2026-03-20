# Nullable Fields & PUT Replacement Semantics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable clearing optional fields via PUT by adding `nullable: true` to OpenAPI schema fields and converting store Update methods to unconditional map-based writes.

**Architecture:** Three-layer fix: (1) OpenAPI schema adds `nullable: true` to 18 fields, regenerate Go types; (2) Store Update methods switch to map-based `Updates()` with all fields included unconditionally; (3) UpdateThreatModel handler removes merge logic so PUT does full replacement.

**Tech Stack:** Go, GORM, OpenAPI 3.0.3, oapi-codegen, Gin, PostgreSQL, Oracle ADB

**Spec:** `docs/superpowers/specs/2026-03-20-nullable-fields-put-semantics-design.md`
**Issues:** #200, #208

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `api-schema/tmi-openapi.json` | Modify | Add `nullable: true` to 18 fields |
| `api/api.go` | Regenerate | Auto-generated types from schema |
| `api/threat_model_handlers.go` | Modify | Remove merge logic from UpdateThreatModel |
| `api/threat_store_gorm.go` | Modify | Convert Update to map-based |
| `api/asset_store_gorm.go` | Modify | Convert Update to map-based |
| `api/document_store_gorm.go` | Modify | Make all fields unconditional |
| `api/repository_store_gorm.go` | Modify | Make all fields unconditional |
| `api/note_store_gorm.go` | Modify | Make all fields unconditional |
| `api/survey_response_store_gorm.go` | Modify | Make all fields unconditional, add missing field |
| `api/project_store_gorm.go` | Modify | Make description/uri unconditional |
| `api/database_store_gorm.go` | Modify | Make alias unconditional |

---

## Task 1: OpenAPI Schema — Add `nullable: true`

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This task adds `nullable: true` to 18 optional string fields across 7 schemas. The file is large (~200KB), so use `jq` for surgical edits.

- [ ] **Step 1: Add nullable to ThreatBase fields**

Use jq to add `"nullable": true` to these ThreatBase fields: `description`, `mitigation`, `issue_uri`, `severity`, `priority`, `status`.

```bash
jq '
  .components.schemas.ThreatBase.properties.description.nullable = true |
  .components.schemas.ThreatBase.properties.mitigation.nullable = true |
  .components.schemas.ThreatBase.properties.issue_uri.nullable = true |
  .components.schemas.ThreatBase.properties.severity.nullable = true |
  .components.schemas.ThreatBase.properties.priority.nullable = true |
  .components.schemas.ThreatBase.properties.status.nullable = true
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add nullable to ThreatModelBase, ThreatModelInput fields**

```bash
jq '
  .components.schemas.ThreatModelBase.properties.description.nullable = true |
  .components.schemas.ThreatModelBase.properties.issue_uri.nullable = true |
  .components.schemas.ThreatModelInput.properties.threat_model_framework.nullable = true
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Add nullable to ProjectBase, RepositoryBase fields**

```bash
jq '
  .components.schemas.ProjectBase.properties.description.nullable = true |
  .components.schemas.ProjectBase.properties.uri.nullable = true |
  .components.schemas.RepositoryBase.properties.name.nullable = true |
  .components.schemas.RepositoryBase.properties.type.nullable = true
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 4: Add nullable to TeamBase, SurveyBase fields**

```bash
jq '
  .components.schemas.TeamBase.properties.description.nullable = true |
  .components.schemas.TeamBase.properties.uri.nullable = true |
  .components.schemas.TeamBase.properties.email_address.nullable = true |
  .components.schemas.SurveyBase.properties.description.nullable = true |
  .components.schemas.SurveyBase.properties.status.nullable = true
' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 5: Validate schema and regenerate API code**

```bash
make validate-openapi
make generate-api
```

Expected: Both commands succeed. The generated `api/api.go` may have type changes for newly-nullable fields (some `string` fields may become `*string`).

- [ ] **Step 6: Build and fix any compilation errors from type changes**

```bash
make build-server
```

Expected: Build may fail if newly-nullable fields changed from `string` to `*string` in generated code. Fix all compilation errors before proceeding:
- If a handler passes `field` where `*field` is now expected: take the address
- If a handler dereferences `*field` where `field` is now a pointer: add nil check
- If a comparison uses `==` on a now-pointer field: compare via dereference

Re-run `make build-server` until it succeeds.

- [ ] **Step 7: Run unit tests**

```bash
make test-unit
```

Fix any test failures caused by type changes.

- [ ] **Step 8: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "fix(api): add nullable:true to 18 optional string fields in OpenAPI schema

Add nullable:true to optional string fields that users should be able to
clear once set. Affects ThreatBase, ThreatModelBase, ThreatModelInput,
ProjectBase, RepositoryBase, TeamBase, and SurveyBase schemas.

Refs #200"
```

---

## Task 2: Fix GormThreatStore — Convert to Map-Based Updates

**Files:**
- Modify: `api/threat_store_gorm.go:147-206` (Update method)
- Modify: `api/threat_store_gorm.go:703-740` (BulkUpdate method — same struct-based pattern)

The threat store currently uses struct-based `Updates()` via `toGormModel()`, which skips nil/zero-value fields. Convert both `Update` and `BulkUpdate` to map-based updates.

- [ ] **Step 1: Replace struct-based Update with map-based**

In `api/threat_store_gorm.go`, replace the Update method body. The key change: instead of calling `toGormModel()` and passing the struct to `Updates()`, build an explicit map that includes all fields unconditionally.

The map must handle custom types explicitly since map-based `Updates()` bypasses GORM custom type `Value()` methods:
- `models.StringArray` for `threat_type` and `cwe_id`
- `models.CVSSArray` for `cvss`
- `models.DBBool` for boolean fields
- UUID string conversion for `diagram_id`, `cell_id`, `asset_id`

```go
func (s *GormThreatStore) Update(ctx context.Context, threat *Threat) error {
	logger := slogging.Get()
	logger.Debug("Updating threat: %s", threat.Id)

	// Update modified timestamp
	now := time.Now().UTC()
	threat.ModifiedAt = &now

	// Normalize severity
	if threat.Severity != nil {
		normalized := normalizeSeverity(*threat.Severity)
		threat.Severity = &normalized
	}

	// Build update map with all fields included unconditionally.
	// Using map-based Updates() ensures nil values write NULL to database.
	updates := map[string]any{
		"name":              threat.Name,
		"description":       threat.Description,
		"severity":          threat.Severity,
		"mitigation":        threat.Mitigation,
		"status":            threat.Status,
		"priority":          threat.Priority,
		"issue_uri":         threat.IssueUri,
		"score":             s.convertScore(threat.Score),
		"modified_at":       now,
	}

	// Custom types: serialize explicitly since map-based Updates bypasses Value()
	updates["threat_type"] = models.StringArray(threat.ThreatType)

	// Boolean fields
	if threat.Mitigated != nil {
		updates["mitigated"] = models.DBBool(*threat.Mitigated)
	} else {
		updates["mitigated"] = models.DBBool(false)
	}
	if threat.IncludeInReport != nil {
		updates["include_in_report"] = models.DBBool(*threat.IncludeInReport)
	} else {
		updates["include_in_report"] = models.DBBool(false)
	}
	if threat.TimmyEnabled != nil {
		updates["timmy_enabled"] = models.DBBool(*threat.TimmyEnabled)
	} else {
		updates["timmy_enabled"] = models.DBBool(false)
	}

	// UUID reference fields — convert to string or nil
	if threat.DiagramId != nil {
		diagID := threat.DiagramId.String()
		updates["diagram_id"] = &diagID
	} else {
		updates["diagram_id"] = nil
	}
	if threat.CellId != nil {
		cellID := threat.CellId.String()
		updates["cell_id"] = &cellID
	} else {
		updates["cell_id"] = nil
	}
	if threat.AssetId != nil {
		assetID := threat.AssetId.String()
		updates["asset_id"] = &assetID
	} else {
		updates["asset_id"] = nil
	}

	// Array custom types
	if threat.CweId != nil && len(*threat.CweId) > 0 {
		updates["cwe_id"] = models.StringArray(*threat.CweId)
	} else {
		updates["cwe_id"] = models.StringArray{}
	}
	if threat.Cvss != nil && len(*threat.Cvss) > 0 {
		cvssArray := make(models.CVSSArray, len(*threat.Cvss))
		for i, c := range *threat.Cvss {
			cvssArray[i] = models.CVSSScore{
				Vector: c.Vector,
				Score:  float64(c.Score),
			}
		}
		updates["cvss"] = cvssArray
	} else {
		updates["cvss"] = models.CVSSArray{}
	}

	result := s.db.WithContext(ctx).Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update threat in database: %v", result.Error)
		return fmt.Errorf("failed to update threat: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("threat not found: %s", threat.Id)
	}

	// Save metadata to separate table
	if err := s.saveMetadata(ctx, threat.Id.String(), threat.Metadata); err != nil {
		logger.Error("Failed to save metadata for threat %s: %v", threat.Id, err)
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheThreat(ctx, threat); cacheErr != nil {
			logger.Error("Failed to update threat cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "threat",
			EntityID:      threat.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threat.ThreatModelId.String(),
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after threat update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated threat: %s", threat.Id)
	return nil
}
```

Also add a helper method for score conversion:

```go
func (s *GormThreatStore) convertScore(score *float32) *float64 {
	if score == nil {
		return nil
	}
	s64 := float64(*score)
	return &s64
}
```

Extract map-building into a helper method `buildThreatUpdateMap(threat *Threat, now time.Time) map[string]any` so both `Update` and `BulkUpdate` can use it.

- [ ] **Step 2: Update BulkUpdate to use map-based updates**

In `BulkUpdate` (line ~729), replace:
```go
gormThreat := s.toGormModel(threat)
if err := tx.Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(gormThreat).Error; err != nil {
```

With:
```go
updates := s.buildThreatUpdateMap(threat, now)
if err := tx.Model(&models.Threat{}).Where("id = ?", threat.Id.String()).Updates(updates).Error; err != nil {
```

- [ ] **Step 3: Build and run unit tests**

```bash
make build-server
make test-unit
```

Expected: Build succeeds, all unit tests pass.

- [ ] **Step 4: Commit**

```bash
git add api/threat_store_gorm.go
git commit -m "fix(api): convert GormThreatStore Update/BulkUpdate to map-based GORM Updates

Replace struct-based Updates() with map-based Updates() to ensure nil
fields are written as NULL to the database. Custom types (StringArray,
CVSSArray, DBBool) are serialized explicitly in the map. Extract shared
buildThreatUpdateMap helper for both Update and BulkUpdate.

Refs #200, #208"
```

---

## Task 3: Fix GormAssetStore — Convert to Map-Based Updates

**Files:**
- Modify: `api/asset_store_gorm.go:151-231` (Update method)

The asset store fetches the existing record and selectively merges non-nil fields, then calls `Save()`. Convert to map-based `Updates()`.

- [ ] **Step 1: Read current Update method**

Read `api/asset_store_gorm.go` lines 151-231 to understand the full current implementation including all fields and cache handling.

- [ ] **Step 2: Replace with map-based update**

Replace the fetch-then-merge pattern with a map that includes all fields unconditionally. Remove the fetch of the existing asset (no longer needed for merging — only the update itself is needed).

All nullable string fields (`description`, `criticality`, `sensitivity`, `type`) should be included unconditionally. Array fields (`classification`) and boolean fields (`include_in_report`, `timmy_enabled`) should also be included unconditionally.

- [ ] **Step 3: Build and run unit tests**

```bash
make build-server
make test-unit
```

- [ ] **Step 4: Commit**

```bash
git add api/asset_store_gorm.go
git commit -m "fix(api): convert GormAssetStore.Update to map-based GORM Updates

Refs #200, #208"
```

---

## Task 4: Fix Conditional Map Stores (Document, Repository, Note)

**Files:**
- Modify: `api/document_store_gorm.go:162-230`
- Modify: `api/repository_store_gorm.go:178-262`
- Modify: `api/note_store_gorm.go:157-221`

These three stores already use map-based `Updates()` but conditionally include `IncludeInReport` and `TimmyEnabled`. Change to unconditional inclusion.

- [ ] **Step 1: Read current implementations**

Read all three files to understand the current conditional patterns.

- [ ] **Step 2: Fix document_store_gorm.go**

Change the conditional blocks to unconditional. Replace:

```go
if document.IncludeInReport != nil {
    updates["include_in_report"] = models.DBBool(*document.IncludeInReport)
}
if document.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*document.TimmyEnabled)
}
```

With:

```go
if document.IncludeInReport != nil {
    updates["include_in_report"] = models.DBBool(*document.IncludeInReport)
} else {
    updates["include_in_report"] = models.DBBool(false)
}
if document.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*document.TimmyEnabled)
} else {
    updates["timmy_enabled"] = models.DBBool(false)
}
```

- [ ] **Step 3: Fix repository_store_gorm.go**

Apply the same pattern — make all conditional fields unconditional.

- [ ] **Step 4: Fix note_store_gorm.go**

Apply the same pattern — make all conditional fields unconditional.

- [ ] **Step 5: Build and run unit tests**

```bash
make build-server
make test-unit
```

- [ ] **Step 6: Commit**

```bash
git add api/document_store_gorm.go api/repository_store_gorm.go api/note_store_gorm.go
git commit -m "fix(api): make document/repository/note store updates unconditional

Include IncludeInReport and TimmyEnabled fields unconditionally in
update maps so PUT can clear them. Default to false when nil.

Refs #200, #208"
```

---

## Task 5: Fix GormSurveyResponseStore, GormProjectStore, GormThreatModelStore

**Files:**
- Modify: `api/survey_response_store_gorm.go:379-454`
- Modify: `api/project_store_gorm.go:264-280`
- Modify: `api/database_store_gorm.go:936-966`

- [ ] **Step 1: Read current implementations**

Read all three files at the specified line ranges.

- [ ] **Step 2: Fix survey_response_store_gorm.go**

The update map starts empty and only adds non-nil fields. Change to include all updatable fields unconditionally. Also add `linked_threat_model_id` which is currently missing entirely.

Replace the conditional pattern:
```go
updates := map[string]any{}
if response.Answers != nil { ... }
if response.UiState != nil { ... }
if response.ProjectId != nil { ... }
```

With unconditional inclusion of all updatable fields. `answers` and `ui_state` are JSON-serialized, so they need marshaling. `project_id` and `linked_threat_model_id` are UUID fields.

- [ ] **Step 3: Fix project_store_gorm.go**

Change conditional `description` and `uri` to unconditional:

Replace:
```go
if project.Description != nil {
    updates["description"] = *project.Description
}
if project.Uri != nil {
    updates["uri"] = *project.Uri
}
```

With:
```go
updates["description"] = project.Description
updates["uri"] = project.Uri
```

Note: Store the pointer directly (not dereferenced) so nil writes NULL.

- [ ] **Step 4: Fix database_store_gorm.go (ThreatModelStore)**

Change the conditional `alias` to unconditional. Replace:

```go
var aliasValue any
if item.Alias != nil {
    aliasValue = models.StringArray(*item.Alias)
}
// ... later:
if aliasValue != nil {
    updates["alias"] = aliasValue
}
```

With unconditional inclusion:

```go
if item.Alias != nil {
    updates["alias"] = models.StringArray(*item.Alias)
} else {
    updates["alias"] = models.StringArray{}
}
```

- [ ] **Step 5: Build and run unit tests**

```bash
make build-server
make test-unit
```

- [ ] **Step 6: Commit**

```bash
git add api/survey_response_store_gorm.go api/project_store_gorm.go api/database_store_gorm.go
git commit -m "fix(api): make survey response/project/threat model store updates unconditional

Include all nullable fields unconditionally in update maps. Add missing
linked_threat_model_id to survey response updates.

Refs #200, #208"
```

---

## Task 6: Remove Merge Logic from UpdateThreatModel Handler

**Files:**
- Modify: `api/threat_model_handlers.go:418-476`

- [ ] **Step 1: Read the current handler**

Read `api/threat_model_handlers.go` lines 340-540 to understand the full handler context.

- [ ] **Step 2: Remove merge logic**

Replace the merge block (lines 418-476) that does:
```go
owner := tm.Owner
if request.Owner != nil { ... }
framework := tm.ThreatModelFramework
if request.ThreatModelFramework != nil { ... }
authorization := tm.Authorization
if request.Authorization != nil { ... }
metadata := tm.Metadata
if request.Metadata != nil { ... }
securityReviewer := tm.SecurityReviewer
if request.SecurityReviewer != nil { ... }
```

With direct construction from the request. The `updatedTM` struct should use request fields directly instead of the merged variables. Server-managed fields (`CreatedAt`, `CreatedBy`, `IsConfidential`, sub-entities) must still be preserved from the existing `tm`.

The `Owner` field requires special handling since the request sends a string identifier that needs conversion to a `User` object. If `request.Owner` is nil or empty, pass a zero-value `User{}`.

- [ ] **Step 3: Build and run unit tests**

```bash
make build-server
make test-unit
```

- [ ] **Step 4: Commit**

```bash
git add api/threat_model_handlers.go
git commit -m "fix(api): remove merge logic from UpdateThreatModel for true PUT semantics

PUT now does full resource replacement. Omitted optional fields are
cleared instead of preserved. Server-managed fields (created_at,
created_by, is_confidential, sub-entities) still preserved.

Refs #208"
```

---

## Task 7: Lint and Final Verification

**Files:**
- All modified files

- [ ] **Step 1: Run linter**

```bash
make lint
```

Fix any lint issues in modified files.

- [ ] **Step 2: Run full unit test suite**

```bash
make test-unit
```

All tests must pass.

- [ ] **Step 3: Run integration tests**

```bash
make test-integration
```

All integration tests must pass. Pay attention to PUT-related test failures which may indicate that test expectations need updating for the new replacement semantics.

- [ ] **Step 4: Fix any test failures**

If integration tests fail because they relied on merge semantics (sending partial PUT bodies and expecting fields to be preserved), update those tests to send complete resources.

- [ ] **Step 5: Commit any fixes**

```bash
git add -u
git commit -m "fix(api): resolve lint and test issues from nullable/PUT changes

Refs #200, #208"
```

---

## Task 8: Push and Close Issues

- [ ] **Step 1: Push all commits**

```bash
git pull --rebase
git push
git status
```

Expected: `git status` shows "up to date with origin".

- [ ] **Step 2: Close issues**

```bash
gh issue close 200 --repo ericfitz/tmi --comment "Fixed in release/1.3.0: added nullable:true to 18 optional string fields and converted store Update methods to unconditional map-based writes."
gh issue close 208 --repo ericfitz/tmi --comment "Fixed in release/1.3.0: removed merge logic from UpdateThreatModel handler; PUT now does full resource replacement."
```
