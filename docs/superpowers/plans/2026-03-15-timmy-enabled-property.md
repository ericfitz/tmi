# `timmy_enabled` Property Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `timmy_enabled` boolean property (default: true, not nullable) to 6 sub-entity types: Asset, Document, Repository, Note, Diagram, Threat.

**Architecture:** Mirror the existing `include_in_report` pattern exactly. Add the property to 9 OpenAPI schemas, 6 GORM model structs, and 6 store conversion files. GORM AutoMigrate handles database column creation automatically.

**Tech Stack:** Go, OpenAPI 3.0.3 (JSON), GORM, oapi-codegen, Gin

**Spec:** `docs/superpowers/specs/2026-03-15-timmy-enabled-property-design.md`

---

## Chunk 1: OpenAPI Schema + Code Generation

### Task 1: Add `timmy_enabled` to OpenAPI spec

**Files:**
- Modify: `api-schema/tmi-openapi.json`

The property must be added to 9 schemas. In each schema, add `timmy_enabled` immediately after the existing `include_in_report` property to maintain consistent ordering.

- [ ] **Step 1: Add `timmy_enabled` to AssetBase**

In `api-schema/tmi-openapi.json`, find the `AssetBase` schema's `include_in_report` property (around line 3284-3288). Add the following property immediately after it:

```json
"timmy_enabled": {
  "type": "boolean",
  "description": "Whether the Timmy AI assistant is enabled for this entity",
  "default": true
}
```

Also add `"timmy_enabled": true` to the `example` block of AssetBase if one exists.

- [ ] **Step 2: Add `timmy_enabled` to DocumentBase**

Same property definition, added after `include_in_report` in the `DocumentBase` schema (around line 3347-3351).

- [ ] **Step 3: Add `timmy_enabled` to RepositoryBase**

Same property definition, added after `include_in_report` in the `RepositoryBase` schema (around line 3596-3600).

- [ ] **Step 4: Add `timmy_enabled` to NoteBase**

Same property definition, added after `include_in_report` in the `NoteBase` schema (around line 3417-3421).

- [ ] **Step 5: Add `timmy_enabled` to ThreatBase**

Same property definition, added after `include_in_report` in the `ThreatBase` schema (around line 2818-2822).

- [ ] **Step 6: Add `timmy_enabled` to BaseDiagram**

Same property definition, added after `include_in_report` in the `BaseDiagram` schema (around line 478-482).

- [ ] **Step 7: Add `timmy_enabled` to BaseDiagramInput**

**Important:** `BaseDiagramInput` does NOT inherit from `BaseDiagram` — it defines properties independently. Add the same property definition after `include_in_report` in `BaseDiagramInput` (around line 582-586).

- [ ] **Step 8: Add `timmy_enabled` to DiagramListItem**

Same property definition, added after `include_in_report` in `DiagramListItem` (around line 1678-1682).

- [ ] **Step 9: Add `timmy_enabled` to NoteListItem**

Same property definition, added after `include_in_report` in `NoteListItem` (around line 3500-3504).

- [ ] **Step 10: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: Passes with no new errors.

- [ ] **Step 11: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` is regenerated with new `TimmyEnabled *bool` fields in the generated types.

- [ ] **Step 12: Build to verify generated code compiles**

Run: `make build-server`
Expected: Build succeeds (generated types compile, but store conversion code doesn't reference new fields yet — that's fine because the fields are pointers and default to nil).

- [ ] **Step 13: Commit OpenAPI + generated code**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add timmy_enabled to OpenAPI spec and regenerate types (#183)"
```

---

## Chunk 2: Database Models

### Task 2: Add `TimmyEnabled` to GORM model structs

**Files:**
- Modify: `api/models/models.go`

Add `TimmyEnabled DBBool` field to each of the 6 entity model structs, immediately after the existing `IncludeInReport` field.

- [ ] **Step 1: Add to Diagram struct**

In `api/models/models.go`, find the `Diagram` struct (line ~163). After line 175:
```go
IncludeInReport   DBBool     `gorm:"default:1"`
```
Add:
```go
TimmyEnabled      DBBool     `gorm:"default:1"`
```

- [ ] **Step 2: Add to Asset struct**

In the `Asset` struct (line ~199). After line 208:
```go
IncludeInReport DBBool      `gorm:"default:1"`
```
Add:
```go
TimmyEnabled    DBBool      `gorm:"default:1"`
```

- [ ] **Step 3: Add to Threat struct**

In the `Threat` struct (line ~232). After line 246:
```go
IncludeInReport DBBool      `gorm:"default:1"`
```
Add:
```go
TimmyEnabled    DBBool      `gorm:"default:1"`
```

- [ ] **Step 4: Add to Document struct**

In the `Document` struct (line ~342). After line 348:
```go
IncludeInReport DBBool     `gorm:"default:1"`
```
Add:
```go
TimmyEnabled    DBBool     `gorm:"default:1"`
```

- [ ] **Step 5: Add to Note struct**

In the `Note` struct (line ~372). After line 378:
```go
IncludeInReport DBBool     `gorm:"default:1"`
```
Add:
```go
TimmyEnabled    DBBool     `gorm:"default:1"`
```

- [ ] **Step 6: Add to Repository struct**

In the `Repository` struct (line ~412). After line 420:
```go
IncludeInReport DBBool     `gorm:"default:1"`
```
Add:
```go
TimmyEnabled    DBBool     `gorm:"default:1"`
```

- [ ] **Step 7: Build to verify models compile**

Run: `make build-server`
Expected: Builds successfully.

- [ ] **Step 8: Commit model changes**

```bash
git add api/models/models.go
git commit -m "feat(api): add TimmyEnabled field to GORM entity models (#183)"
```

---

## Chunk 3: Store Conversions

### Task 3: Add `TimmyEnabled` conversion to all 6 store files

Each store file has API→DB and DB→API conversion code. Follow the exact pattern used by `IncludeInReport` in each file.

**Files:**
- Modify: `api/asset_store_gorm.go`
- Modify: `api/document_store_gorm.go`
- Modify: `api/note_store_gorm.go`
- Modify: `api/repository_store_gorm.go`
- Modify: `api/threat_store_gorm.go`
- Modify: `api/database_store_gorm.go` (diagrams)

---

#### 3a: Asset store

- [ ] **Step 1: Add API→DB conversion in `toGormModel`**

In `api/asset_store_gorm.go`, find the `toGormModel` function (line ~619). After lines 641-643:
```go
if asset.IncludeInReport != nil {
    gm.IncludeInReport = models.DBBool(*asset.IncludeInReport)
}
```
Add:
```go
if asset.TimmyEnabled != nil {
    gm.TimmyEnabled = models.DBBool(*asset.TimmyEnabled)
}
```

- [ ] **Step 2: Add API→DB conversion in `Update`**

In `api/asset_store_gorm.go`, find the `Update` function (line ~151). After line 188:
```go
if asset.Sensitivity != nil {
    existingAsset.Sensitivity = asset.Sensitivity
}
```
Add:
```go
if asset.TimmyEnabled != nil {
    existingAsset.TimmyEnabled = models.DBBool(*asset.TimmyEnabled)
}
```

Note: The asset Update function fetches the existing record and selectively overwrites fields (unlike document/note/repo which use map-based updates). This mirrors the existing pattern — `IncludeInReport` is also not updated here, but `TimmyEnabled` should be to ensure PUT requests can modify it.

- [ ] **Step 3: Add DB→API conversion in `toAPIModel`**

In the `toAPIModel` function (line ~649). After lines 673-674:
```go
includeInReport := gm.IncludeInReport.Bool()
asset.IncludeInReport = &includeInReport
```
Add:
```go
timmyEnabled := gm.TimmyEnabled.Bool()
asset.TimmyEnabled = &timmyEnabled
```

---

#### 3b: Document store

- [ ] **Step 4: Add API→DB conversion in Create**

In `api/document_store_gorm.go`, find the Create function. After lines 58-60:
```go
if document.IncludeInReport != nil {
    model.IncludeInReport = models.DBBool(*document.IncludeInReport)
}
```
Add:
```go
if document.TimmyEnabled != nil {
    model.TimmyEnabled = models.DBBool(*document.TimmyEnabled)
}
```

- [ ] **Step 5: Add API→DB conversion in Update (map-based)**

In the Update function. After lines 174-175:
```go
if document.IncludeInReport != nil {
    updates["include_in_report"] = models.DBBool(*document.IncludeInReport)
}
```
Add:
```go
if document.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*document.TimmyEnabled)
}
```

- [ ] **Step 6: Add API→DB conversion in BulkCreate**

In the BulkCreate function. After lines 390-392:
```go
if document.IncludeInReport != nil {
    model.IncludeInReport = models.DBBool(*document.IncludeInReport)
}
```
Add:
```go
if document.TimmyEnabled != nil {
    model.TimmyEnabled = models.DBBool(*document.TimmyEnabled)
}
```

- [ ] **Step 7: Add DB→API conversion in `modelToAPI`**

In the `modelToAPI` function (line ~486). After lines 488-494 where `includeInReport` is set:
```go
includeInReport := model.IncludeInReport.Bool()
```
Add after it:
```go
timmyEnabled := model.TimmyEnabled.Bool()
```
And add `TimmyEnabled: &timmyEnabled,` to the Document struct literal alongside `IncludeInReport: &includeInReport,`.

---

#### 3c: Note store

- [ ] **Step 8: Add API→DB conversion in Create**

In `api/note_store_gorm.go`, after lines 58-60 (same pattern as document Create):
```go
if note.TimmyEnabled != nil {
    model.TimmyEnabled = models.DBBool(*note.TimmyEnabled)
}
```

- [ ] **Step 9: Add API→DB conversion in Update (map-based)**

After lines 167-168:
```go
if note.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*note.TimmyEnabled)
}
```

- [ ] **Step 10: Add DB→API conversion in `modelToAPI`**

In `modelToAPI` (line ~429). After `includeInReport` conversion:
```go
timmyEnabled := model.TimmyEnabled.Bool()
```
Add `TimmyEnabled: &timmyEnabled,` to the Note struct literal.

---

#### 3d: Repository store

- [ ] **Step 11: Add API→DB conversion in Create**

In `api/repository_store_gorm.go`, after lines 79-80:
```go
if repository.TimmyEnabled != nil {
    model.TimmyEnabled = models.DBBool(*repository.TimmyEnabled)
}
```

- [ ] **Step 12: Add API→DB conversion in Update (map-based)**

After lines 209-210:
```go
if repository.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*repository.TimmyEnabled)
}
```

- [ ] **Step 13: Add API→DB conversion in BulkCreate**

After lines 444-446:
```go
if repository.TimmyEnabled != nil {
    model.TimmyEnabled = models.DBBool(*repository.TimmyEnabled)
}
```

- [ ] **Step 14: Add DB→API conversion in `modelToAPI`**

In `modelToAPI` (line ~540). After `includeInReport` conversion:
```go
timmyEnabled := model.TimmyEnabled.Bool()
```
Add `TimmyEnabled: &timmyEnabled,` to the Repository struct literal.

---

#### 3e: Threat store

- [ ] **Step 15: Add API→DB conversion in `toGormModelForCreate`**

In `api/threat_store_gorm.go`, find `toGormModelForCreate` (line ~880). After lines 903-905:
```go
if threat.IncludeInReport != nil {
    gm.IncludeInReport = models.DBBool(*threat.IncludeInReport)
}
```
Add:
```go
if threat.TimmyEnabled != nil {
    gm.TimmyEnabled = models.DBBool(*threat.TimmyEnabled)
}
```

Note: The threat `Update` function calls `toGormModel` (line ~944) which delegates to `toGormModelForCreate` (line 945), so adding the conversion to `toGormModelForCreate` covers both Create and Update paths.

- [ ] **Step 16: Add DB→API conversion in `toAPIModel`**

In `toAPIModel` (line ~956). After lines 958-963:
```go
includeInReport := gm.IncludeInReport.Bool()
```
Add:
```go
timmyEnabled := gm.TimmyEnabled.Bool()
```
Add `TimmyEnabled: &timmyEnabled,` to the Threat struct literal.

---

#### 3f: Diagram store

- [ ] **Step 17: Add API→DB conversion in CreateWithThreatModel**

In `api/database_store_gorm.go`, find diagram creation (line ~1115). After lines 1175-1177:
```go
if item.IncludeInReport != nil {
    diagram.IncludeInReport = models.DBBool(*item.IncludeInReport)
}
```
Add:
```go
if item.TimmyEnabled != nil {
    diagram.TimmyEnabled = models.DBBool(*item.TimmyEnabled)
}
```

- [ ] **Step 18: Add API→DB conversion in Update (map-based)**

In the diagram Update function. After lines 1261-1263:
```go
if item.IncludeInReport != nil {
    updates["include_in_report"] = models.DBBool(*item.IncludeInReport)
}
```
Add:
```go
if item.TimmyEnabled != nil {
    updates["timmy_enabled"] = models.DBBool(*item.TimmyEnabled)
}
```

- [ ] **Step 19: Add DB→API conversion in `dbToDfd`**

In the `Get` function's `dbToDfd` conversion (around line 1091). After:
```go
includeInReport := diagram.IncludeInReport.Bool()
```
Add:
```go
timmyEnabled := diagram.TimmyEnabled.Bool()
```
Add `TimmyEnabled: &timmyEnabled,` to the DfdDiagram struct literal alongside `IncludeInReport: &includeInReport,`.

---

#### 3g: ListItem handler conversions

The diagram and note list endpoints manually construct `DiagramListItem` and `NoteListItem` structs from the full entity models. These must also copy `TimmyEnabled`.

- [ ] **Step 20: Add `TimmyEnabled` to DiagramListItem construction**

In `api/threat_model_diagram_handlers.go`, find the `DiagramListItem` construction (around line 92-99). After line 99:
```go
IncludeInReport: d.IncludeInReport,
```
Add:
```go
TimmyEnabled: d.TimmyEnabled,
```

- [ ] **Step 21: Add `TimmyEnabled` to NoteListItem construction**

In `api/note_sub_resource_handlers.go`, find the `NoteListItem` construction (around line 94-102). After line 101:
```go
IncludeInReport: n.IncludeInReport,
```
Add:
```go
TimmyEnabled: n.TimmyEnabled,
```

---

- [ ] **Step 22: Build to verify all conversions compile**

Run: `make build-server`
Expected: Builds successfully with no errors.

- [ ] **Step 23: Run unit tests**

Run: `make test-unit`
Expected: All existing tests pass.

- [ ] **Step 24: Commit store conversions and handler updates**

```bash
git add api/asset_store_gorm.go api/document_store_gorm.go api/note_store_gorm.go api/repository_store_gorm.go api/threat_store_gorm.go api/database_store_gorm.go api/threat_model_diagram_handlers.go api/note_sub_resource_handlers.go
git commit -m "feat(api): add timmy_enabled store conversions and list item handlers (#183)"
```

---

## Chunk 4: Tests

### Task 4: Add unit tests for `timmy_enabled` default behavior

**Files:**
- Modify: `api/asset_sub_resource_handlers_test.go`
- Modify: `api/document_sub_resource_handlers_test.go`
- Modify: `api/note_sub_resource_handlers_test.go`
- Modify: `api/repository_sub_resource_handlers_test.go`
- Modify: `api/threat_sub_resource_handlers_test.go`
- Modify: `api/threat_model_diagram_handlers_test.go`

For each entity type, add tests verifying:
1. Creating an entity without `timmy_enabled` returns it as `true` (default)
2. Creating an entity with `timmy_enabled: false` preserves `false`
3. Updating an entity's `timmy_enabled` to `false` persists the change

Follow the existing test patterns in each file — they use `MockXxxStore` with `testify/mock` and make HTTP requests via `httptest.NewRecorder()`.

- [ ] **Step 1: Write tests for Asset `timmy_enabled`**

Add test functions to `api/asset_sub_resource_handlers_test.go`:
- `TestCreateAssetTimmyEnabledDefault` — POST without `timmy_enabled`, verify response has `timmy_enabled: true`
- `TestCreateAssetTimmyEnabledExplicitFalse` — POST with `timmy_enabled: false`, verify response has `timmy_enabled: false`

- [ ] **Step 2: Run asset tests**

Run: `make test-unit name=TestCreateAssetTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 3: Write tests for Document `timmy_enabled`**

Add to `api/document_sub_resource_handlers_test.go`:
- `TestCreateDocumentTimmyEnabledDefault`
- `TestCreateDocumentTimmyEnabledExplicitFalse`

- [ ] **Step 4: Run document tests**

Run: `make test-unit name=TestCreateDocumentTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 5: Write tests for Note `timmy_enabled`**

Add to `api/note_sub_resource_handlers_test.go`:
- `TestCreateNoteTimmyEnabledDefault`
- `TestCreateNoteTimmyEnabledExplicitFalse`

- [ ] **Step 6: Run note tests**

Run: `make test-unit name=TestCreateNoteTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 7: Write tests for Repository `timmy_enabled`**

Add to `api/repository_sub_resource_handlers_test.go`:
- `TestCreateRepositoryTimmyEnabledDefault`
- `TestCreateRepositoryTimmyEnabledExplicitFalse`

- [ ] **Step 8: Run repository tests**

Run: `make test-unit name=TestCreateRepositoryTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 9: Write tests for Threat `timmy_enabled`**

Add to `api/threat_sub_resource_handlers_test.go`:
- `TestCreateThreatTimmyEnabledDefault`
- `TestCreateThreatTimmyEnabledExplicitFalse`

- [ ] **Step 10: Run threat tests**

Run: `make test-unit name=TestCreateThreatTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 11: Write tests for Diagram `timmy_enabled`**

Add to `api/threat_model_diagram_handlers_test.go`:
- `TestCreateDiagramTimmyEnabledDefault`
- `TestCreateDiagramTimmyEnabledExplicitFalse`

- [ ] **Step 12: Run diagram tests**

Run: `make test-unit name=TestCreateDiagramTimmyEnabled`
Expected: Tests pass.

- [ ] **Step 13: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass, no regressions.

- [ ] **Step 14: Commit tests**

```bash
git add api/*_test.go
git commit -m "test(api): add tests for timmy_enabled property on all entity types (#183)"
```

---

## Chunk 5: Lint, Build, Final Verification

### Task 5: Final quality gates

- [ ] **Step 1: Run linter**

Run: `make lint`
Expected: No new lint errors (existing api/api.go ST1005 warnings are expected).

- [ ] **Step 2: Run full build**

Run: `make build-server`
Expected: Builds successfully.

- [ ] **Step 3: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: All tests pass, including existing CRUD tests that now round-trip the new field.

- [ ] **Step 5: Fix any issues found, commit fixes**

If any issues, fix and commit with appropriate conventional commit message.
