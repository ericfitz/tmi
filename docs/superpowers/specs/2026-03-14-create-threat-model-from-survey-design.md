# Design: Create Threat Model from Survey Response (Issue #177)

## Summary

Implement the `POST /triage/survey_responses/{survey_response_id}/create_threat_model` endpoint, which currently returns 501 Not Implemented. This endpoint creates a threat model from an approved survey response by mapping survey answers to threat model fields, creating associated sub-resources (assets, documents, repositories), and transitioning the survey response status.

## Context

The triage workflow allows security reviewers to approve survey responses and create threat models from them. Recent commits added the foundational infrastructure:
- `SurveyAnswerStore` with `GetFieldMappings()` and `GetAnswers()`
- `ExtractQuestions` for recursive SurveyJS parsing
- `mapsToTmField` validation on survey templates

The endpoint is defined in the OpenAPI spec and the generated response type `CreateThreatModelFromSurveyResponse` already exists in `api/api.go`.

## Design

### Architecture: Handler + Service Function (Approach B)

The handler stays thin (HTTP concerns, precondition checks, webhook emission). Business logic lives in a service function `createThreatModelFromResponse` in the same file.

### Handler Flow (`CreateThreatModelFromSurveyResponse`)

Location: `api/survey_handlers.go` (replaces existing 501 stub)

1. **Load survey response** via `GlobalSurveyResponseStore.Get(ctx, surveyResponseId)`
   - 404 if not found
2. **Extract user identity** — two methods needed:
   - `getUserUUID(c)` → `userInternalUUID` for access checks
   - Context values (`userIdP`, `userDisplayName`, `userEmail`, `providerID`) for building TM owner/authorization (same pattern as `CreateThreatModel` in `threat_model_handlers.go`)
3. **Check access** via `GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID, AuthorizationRoleOwner)` — consistent with `PatchTriageSurveyResponse`
   - 403 if denied
4. **Validate preconditions:**
   - `response.Owner` must not be nil → 500 if nil (user may have been deleted)
   - Status must be `ready_for_review` → 409 Conflict otherwise
   - `CreatedThreatModelId` must be nil → 409 Conflict if already set (prevents duplicate TM creation)
5. **Call service function** `createThreatModelFromResponse(ctx, response)` → returns `(ThreatModel, error)`
6. **Update survey response:**
   - Transition status to `review_created` and set `CreatedThreatModelId` via `GlobalSurveyResponseStore.SetCreatedThreatModel(ctx, id, tmID)` (new store method — see Store Changes below)
7. **Record audit** via `RecordAuditCreate(c, createdTM.Id.String(), "threat_model", createdTM.Id.String(), createdTM)`
8. **Broadcast notification** via `BroadcastThreatModelCreated(userEmail, createdTM.Id.String(), createdTM.Name)`
9. **Emit webhooks** via `GlobalEventEmitter` (using `c.Request.Context()` for consistency with existing handlers):
   - `threat_model.created`:
     ```go
     EventPayload{
         EventType:     EventThreatModelCreated,
         ThreatModelID: createdTM.Id.String(),
         ResourceID:    createdTM.Id.String(),
         ResourceType:  "threat_model",
         OwnerID:       GetOwnerInternalUUID(c.Request.Context(), createdTM.Owner.Provider, createdTM.Owner.ProviderId),
         Data: map[string]any{
             "name":        createdTM.Name,
             "description": createdTM.Description,
         },
     }
     ```
   - `survey_response.updated`:
     ```go
     EventPayload{
         EventType:    EventSurveyResponseUpdated,
         ResourceID:   surveyResponseId.String(),
         ResourceType: "survey_response",
         Data: map[string]any{
             "survey_id": response.SurveyId.String(),
         },
     }
     ```
10. **Set Location header**: `c.Header("Location", "/threat_models/"+createdTM.Id.String())`
11. **Return 201** with `CreateThreatModelFromSurveyResponse{ThreatModelId, SurveyResponseId}`

### Service Function (`createThreatModelFromResponse`)

Location: `api/survey_handlers.go`

#### Step 1: Gather Data

- `GlobalSurveyAnswerStore.GetAnswers(ctx, responseID)` → all answer rows for the response

#### Step 2: Process Mapped Fields

Iterate answers and dispatch by `mapsToTmField` value:

**Scalar TM fields** (each `mapsToTmField` value is unique per `ExtractQuestions` validation):

| `mapsToTmField` Value | Action |
|------------------------|--------|
| `name` | Set as TM name (flatten + sanitize) |
| `description` | Set as TM description (flatten + sanitize) |
| `issue_uri` | Set as TM issue_uri (flatten + sanitize) |
| `metadata.{key}` | Extract key from directive, add `Metadata{Key: key, Value: flattened}` |

**Collection TM fields** via paneldynamic questions:

| `mapsToTmField` Value | Action |
|------------------------|--------|
| `assets` | Parse array-of-objects answer into asset sub-resources |
| `documents` | Parse array-of-objects answer into document sub-resources |
| `repositories` | Parse array-of-objects answer into repository sub-resources |

**Unrecognized**: Log warning, add as metadata: `{key: tmFieldValue, value: flattened}`

#### Collection Processing (via paneldynamic)

Collection sub-resources are modeled as `paneldynamic` SurveyJS questions. A paneldynamic question with `mapsToTmField: "repositories"` produces an array-of-objects answer like:

```json
[
  {"name": "frontend", "uri": "https://github.com/org/frontend"},
  {"name": "backend", "uri": "https://github.com/org/backend"}
]
```

Each object in the array is parsed into a sub-resource. The object keys correspond to sub-resource fields:

| Collection | Required Fields | Optional Fields |
|------------|----------------|-----------------|
| `assets` | `name`, `type` | `description` |
| `documents` | `name`, `uri` | |
| `repositories` | `name`, `uri` | |

**Validation per object**:
- If required fields are present → create the sub-resource
- If required fields are missing → log warning, add each provided field as metadata with `key="{collection}.{field}"` (e.g., `"repositories.name"`), `value=flattened answer`. No data is discarded.

All string values from paneldynamic answers are flattened and sanitized before use.

#### Step 3: Build Metadata from Unmapped Answers

Iterate all answers. If `MapsToTmField` is nil, add `Metadata{Key: questionName, Value: flattenedAnswer}`.

After assembling the full metadata slice (from `metadata.{key}` mappings, unmapped answers, and fallback entries), validate with `SanitizeMetadataSlice` for consistency with `CreateThreatModel`.

#### Step 4: Build TM Name

Priority:
1. If a field maps to `name` → use that flattened answer
2. Fallback template:
   - Load survey template via `GlobalSurveyStore.Get()` for template name
   - If survey response has `ProjectId` → load project, use `"{template_name}: {project_name} - {date}"`
   - If no `ProjectId` → `"{template_name} - {date}"`
   - Date format: ISO 8601 (`2006-01-02`)

#### Step 5: Build Authorization & Access Control

- **Owner**: survey response's `Owner` user (dereferenced from `*User`), with `role: owner` as first authorization entry
- **Is Confidential**: copy from survey response's `IsConfidential` field
- **Security Reviewer**: if `ReviewedBy` is set on the response, set as TM `SecurityReviewer`
- **Apply security reviewer rule**: call existing `ApplySecurityReviewerRule()` from `auth_utils.go`

#### Step 6: Create Threat Model

Call `ThreatModelStore.Create()` with the assembled ThreatModel struct, using the same `idSetter` pattern as the existing `CreateThreatModel` handler. Initialize `Threats` to `&[]Threat{}` (empty slice, matching `CreateThreatModel` pattern).

#### Step 7: Create Sub-Resources

Create validated assets, documents, and repositories via their respective stores, associated with the new TM.

**Failure handling**: Sub-resource creation failures are logged as warnings but do not fail the overall operation. The threat model is still created successfully, and users can add missing sub-resources manually. This avoids the complexity of cross-store transactions while ensuring the core TM creation is not blocked by secondary failures.

### Answer Flattening & Sanitization

New file: `api/answer_flattener.go`

Function `flattenAndSanitize(value json.RawMessage) string`:

| JSON Type | Flattening Rule | Example |
|-----------|----------------|---------|
| String | Use directly | `"hello"` → `hello` |
| Number | String representation | `42` → `42` |
| Boolean | `"true"` / `"false"` | `true` → `true` |
| Array of strings | Comma-separated | `["a","b"]` → `a, b` |
| Array of other | JSON string | `[1,2]` → `[1,2]` |
| Object | JSON string | `{"k":"v"}` → `{"k":"v"}` |
| Null | Empty string | `null` → `` |

After flattening, sanitize via bluemonday using `SanitizePlainText` for consistency with existing sanitization in `threat_model_handlers.go`.

### Store Changes

#### New method on `SurveyResponseStore` interface

```go
SetCreatedThreatModel(ctx context.Context, id uuid.UUID, threatModelID string) error
```

Atomically sets `created_threat_model_id` and transitions status to `review_created` in a single update. This is necessary because:
- `Update()` does not handle `created_threat_model_id` or status transitions
- `UpdateStatus()` does not handle `created_threat_model_id`
- These two fields must be set together to maintain consistency

Implementation: single GORM `Updates()` call setting both `created_threat_model_id` and `status` fields, with a `WHERE` clause ensuring status is still `ready_for_review` (optimistic concurrency guard against race conditions).

Must be implemented in both GORM and in-memory store implementations, plus any mock stores used in tests.

### Error Responses

| Condition | HTTP Status | Error Code |
|-----------|------------|------------|
| Malformed survey_response_id | 400 | `invalid_input` (handled by OpenAPI validation middleware) |
| Unauthorized (no/invalid JWT) | 401 | Handled by JWT middleware |
| Access denied | 403 | `forbidden` |
| Response not found | 404 | `not_found` |
| Status not `ready_for_review` | 409 | `conflict` |
| `CreatedThreatModelId` already set | 409 | `conflict` |
| Owner is nil (user deleted) | 500 | `server_error` |
| Internal error (store failure) | 500 | `server_error` |

## File Changes

### Modified
- `api/survey_handlers.go` — Replace 501 stub with handler + service function
- `api/survey_response_store_gorm.go` — Add `SetCreatedThreatModel` to interface + GORM implementation + in-memory implementation

### New Files
- `api/answer_flattener.go` — `flattenAnswerValue` and `flattenAndSanitize` utilities
- `api/answer_flattener_test.go` — Unit tests for flattening/sanitization
- `api/create_threat_model_from_survey_test.go` — Unit tests for handler and service function

### Not Modified
- `api-schema/tmi-openapi.json` — Endpoint already defined
- `api/api.go` — Response type already generated

## Test Plan

### Answer Flattener Tests
- All JSON types (string, number, boolean, array, object, null)
- HTML/script injection sanitized
- Mixed-type arrays
- Nested objects

### Service Function Tests
- Scalar field mapping (`name`, `description`, `issue_uri`)
- `metadata.{key}` mapping
- Paneldynamic collection parsing (assets, documents, repositories) — single and multiple items
- Incomplete collection objects → metadata fallback with warning
- Unrecognized `mapsToTmField` values → metadata fallback with warning
- Unmapped answers → metadata with question name as key
- Name fallback: with project, without project, with mapped name
- Authorization: owner, reviewer, confidentiality, security reviewer rule
- Owner nil → error
- Threats initialized to empty slice

### Handler Tests
- 400 for malformed UUID (via OpenAPI middleware)
- 403 for access denied
- 404 for missing response
- 409 for wrong status (draft, submitted, needs_revision, review_created)
- 409 for duplicate (CreatedThreatModelId already set)
- 500 for nil owner
- 201 success with correct response body and Location header
- Audit recording
- Broadcast notification
- Webhook emission (both events)
- Survey response updated with CreatedThreatModelId and status transition

### Store Tests
- `SetCreatedThreatModel` success: sets both fields atomically
- `SetCreatedThreatModel` race condition: fails if status changed concurrently
- In-memory implementation correctness

## Decisions Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Non-string answer flattening | Arrays→comma-sep, booleans/numbers→string | Most human-readable for reviewers |
| Status enforcement | Strict: only `ready_for_review`, block if TM already created | Enforces workflow, prevents duplicates |
| Authorization | Owner + reviewer + inherit confidentiality + security reviewer rule | Consistent with directly-created TMs |
| TM name source | Mapped `name` field, fallback to template pattern | Flexible, always produces a name |
| Date format | ISO 8601 universally | Go lacks robust locale date formatting; name is editable |
| Incomplete collections | Log warning, add to metadata | No data loss; metadata serves as breadcrumb |
| Unrecognized mappings | Log warning, add to metadata | Forward-compatible, no data loss |
| Webhook events | Both `threat_model.created` and `survey_response.updated` | Full visibility for integrators |
| Architecture | Handler + service function (same file) | Testable separation without over-abstraction |
| Sanitization | Bluemonday via existing `SanitizePlainText` + `SanitizeMetadataSlice` | Consistent with codebase, prevents injection |
| Collection modeling | Paneldynamic questions with `mapsToTmField` on the paneldynamic itself | Works within `ExtractQuestions` uniqueness constraint; paneldynamic naturally produces array-of-objects |
| Sub-resource failures | Log warning, don't fail operation | Avoids cross-store transaction complexity; user can add manually |
| Store update for status+TM ID | New `SetCreatedThreatModel` method with optimistic concurrency | Atomic update prevents inconsistent state |
