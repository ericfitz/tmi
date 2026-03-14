# Survey Answers Extraction into Structured Table

**Date:** 2026-03-14
**Status:** Approved
**Issue:** #178

## Problem

The server stores survey response answers as an opaque JSON blob (`Answers` field) keyed by question name, and the survey definition as another opaque blob (`SurveyJSON` snapshot). This makes it impossible for server-side logic to:

- Look up individual answers with their question metadata
- Find answers that map to specific threat model fields (via `mapsToTmField` annotations)
- Query answers by type or mapping without parsing JSON at runtime

The `create_threat_model` endpoint (#177) needs structured access to answers and their field mappings to populate threat model fields from survey data.

## Design

### Approach: Extract on Every Answer Save

When a survey response's answers are saved, parse the `SurveyJSON` snapshot and `Answers` blob, then write structured rows to a `survey_answers` table. This is a full-replacement operation: delete all existing rows for the response, then insert fresh rows from the current state.

### Database Table: `survey_answers`

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| `id` | varchar(36) PK | no | UUID |
| `response_id` | varchar(36) FK | no | FK to `survey_responses.id` |
| `question_name` | varchar(256) | no | SurveyJS element `name` |
| `question_type` | varchar(64) | no | SurveyJS element `type` (text, radiogroup, checkbox, etc.) |
| `question_title` | varchar(1024) | yes | Human-readable question title |
| `maps_to_tm_field` | varchar(128) | yes | `mapsToTmField` annotation value (target TM field name) |
| `answer_value` | jsonb | yes | Answer as JSON; null if unanswered |
| `response_status` | varchar(30) | no | Denormalized copy of current response status for query convenience (avoids join to `survey_responses`) |
| `created_at` | timestamp | no | When this extraction row was created (auto-set) |

**Indexes:**
- `idx_sa_response_id` on `(response_id)` — primary lookup path
- `idx_sa_response_mapping` on `(response_id, maps_to_tm_field)` — field mapping lookups

**Foreign key:** `response_id` references `survey_responses(id)` with `ON DELETE CASCADE`.

### GORM Model

```go
type SurveyAnswer struct {
    ID              string    `gorm:"primaryKey;type:varchar(36)"`
    ResponseID      string    `gorm:"type:varchar(36);not null;index:idx_sa_response_id;index:idx_sa_response_mapping"`
    QuestionName    string    `gorm:"type:varchar(256);not null"`
    QuestionType    string    `gorm:"type:varchar(64);not null"`
    QuestionTitle   *string   `gorm:"type:varchar(1024)"`
    MapsToTmField   *string   `gorm:"type:varchar(128);index:idx_sa_response_mapping"`
    AnswerValue     JSONRaw   // no explicit type tag; JSONRaw.GormDBDataType handles dialect selection
    ResponseStatus  string    `gorm:"type:varchar(30);not null"`
    CreatedAt       time.Time `gorm:"not null;autoCreateTime"`
}

func (SurveyAnswer) TableName() string {
    return tableName("survey_answers")
}

func (s *SurveyAnswer) BeforeCreate(tx *gorm.DB) error {
    if s.ID == "" {
        s.ID = uuid.New().String()
    }
    return nil
}
```

### Extraction Trigger Points

Extract answers on every operation that modifies answers or status on a `SurveyResponse`:

1. **`CreateSurveyResponse`** — initial draft creation (answers likely empty, questions still extracted with null values)
2. **`UpdateSurveyResponse`** — draft saves with updated answers
3. **`PatchSurveyResponse`** — if answers are modified via JSON Patch
4. **`SubmitSurveyResponse`** — status transition to `submitted`
5. **`PatchTriageSurveyResponse`** — reviewer status changes (e.g., `needs_revision`, `ready_for_review`); re-extracts to update the denormalized `response_status` column even though answers don't change

Extraction runs in a **separate transaction** after the response save commits. This ensures extraction failure is truly non-fatal — the response save is never rolled back due to extraction errors.

### Extraction Logic

#### SurveyJS Parsing

Parse the `SurveyJSON` blob recursively to extract all leaf question elements:

1. Read `survey_json.pages` array
2. For each page, iterate `elements` array
3. For each element:
   - If `type` is `"panel"`: recurse into its `elements` array
   - If `type` is `"paneldynamic"`: recurse into its `templateElements` array, recording the parent panel name for answer matching
   - Otherwise: treat as a leaf question — extract `name`, `type`, `title`, `mapsToTmField`
4. Collect all leaf questions into a flat list

#### Answer Matching

For each extracted question:

**Regular questions (not inside paneldynamic):**
1. Look up `question.name` in the `Answers` JSON object
2. If found, store the value as JSON in `answer_value`
3. If not found, store `answer_value` as null

**Questions inside paneldynamic:**
SurveyJS stores paneldynamic answers as an array of objects under the parent panel's name. For example, a `paneldynamic` named `"risks"` with child question `"risk_name"` stores answers as:
```json
{"risks": [{"risk_name": "SQL Injection"}, {"risk_name": "XSS"}]}
```

For child questions of a paneldynamic:
1. Look up the parent panel's name in `Answers` to get the array
2. Store the **entire parent array** as the `answer_value` for the paneldynamic element itself (treated as a single question with `type: "paneldynamic"`)
3. **Do not** create separate rows for individual child questions of paneldynamic — the child question definitions are recorded in the parent row's metadata, and the answer array contains all child values

This avoids the complexity of flattening dynamic-length arrays into individual rows while still making the data queryable.

#### Full Replacement

1. Delete all rows from `survey_answers` where `response_id` matches
2. Insert new rows for every extracted question with current `response_status`
3. Both delete and insert run in a single transaction (separate from the response save transaction)

#### mapsToTmField Format

The `mapsToTmField` annotation is a simple string on a SurveyJS question element specifying the target threat model field name:

```json
{
  "type": "text",
  "name": "project_name",
  "title": "What is your project name?",
  "mapsToTmField": "name"
}
```

Valid target field values correspond to writable ThreatModel fields: `name`, `description`, `status`, `issue_uri`, `threat_model_framework`, `is_confidential`, `project_id`. Questions without this annotation have `maps_to_tm_field` set to null and their answers go to threat model metadata during `create_threat_model`.

**Duplicate field mappings** are not allowed: if two questions in the same survey map to the same TM field, the `ExtractQuestions` function returns an error listing the conflict. Survey template validation (on create/update) should also check for duplicate mappings and reject them.

### Internal Interface

```go
// SurveyAnswerStore provides operations for extracted survey answers.
type SurveyAnswerStore interface {
    // ExtractAndSave parses surveyJSON and answers, then replaces all rows for the response.
    ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, status string) error

    // GetAnswers returns all extracted answers for a response.
    GetAnswers(ctx context.Context, responseID string) ([]SurveyAnswer, error)

    // GetFieldMappings returns answers with non-null maps_to_tm_field, keyed by target field name.
    // Returns an error if duplicate mappings are found (should not happen if validated on save).
    GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswer, error)

    // DeleteByResponseID removes all extracted answers for a response.
    DeleteByResponseID(ctx context.Context, responseID string) error
}
```

### SurveyJS Question Extraction Function

Standalone, testable function for recursive question extraction. This function has no database dependency; it accepts a logger for reporting skipped elements.

```go
// SurveyQuestion represents a leaf question extracted from SurveyJS JSON.
type SurveyQuestion struct {
    Name          string
    Type          string
    Title         *string // nil if not present in the survey element
    MapsToTmField *string // nil if not annotated
}

// ExtractQuestions recursively extracts leaf questions from a SurveyJS survey_json object.
// Returns an error if duplicate mapsToTmField values are found.
func ExtractQuestions(surveyJSON map[string]any, logger *slogging.Logger) ([]SurveyQuestion, error)
```

This function handles:
- Top-level `pages[].elements[]` traversal
- `type: "panel"` → recurse into `elements`
- `type: "paneldynamic"` → emit the paneldynamic itself as a question (with its `templateElements` as context), recurse into `templateElements` only for `mapsToTmField` conflict detection
- Elements missing `name` or `type` are skipped with a warning log
- Duplicate `mapsToTmField` values across questions → return error

### Error Handling

- **Malformed SurveyJSON:** If `survey_json` cannot be parsed (missing `pages`, invalid structure), log a warning and skip extraction. The response save itself still succeeds — extraction runs in a separate transaction.
- **Extraction transaction failure:** If the delete+insert transaction fails, log an error. The response is already saved; extraction will be retried on the next save.
- **Partial answers:** Expected during draft saves. All questions are extracted; unanswered ones get null `answer_value`.
- **Duplicate field mappings:** `ExtractQuestions` returns an error; `ExtractAndSave` logs the error and skips extraction. This case should be caught earlier during survey template validation.

### Migration

GORM auto-migration creates the `survey_answers` table. No manual migration script needed. The table is created on server startup via the existing auto-migration path.

On first deployment, existing survey responses will not have extracted answers. Extraction happens on the next save of each response. For existing responses that are already in terminal states (`review_created`), a one-time backfill could be added but is not required for the initial implementation — `create_threat_model` can fall back to parsing `SurveyJSON` + `Answers` directly if no extracted rows exist.

### Impact on Existing Code

- **Survey handlers** (`survey_handlers.go`): Add `ExtractAndSave` calls after answer saves in create, update, patch, submit, and triage patch handlers
- **Models** (`survey_models.go`): Add `SurveyAnswer` struct with `TableName()` and `BeforeCreate` hooks matching existing model patterns
- **Store**: New `SurveyAnswerStoreGorm` implementing `SurveyAnswerStore`, plus in-memory implementation for tests
- **Server**: Wire the store at startup, register model for auto-migration
- **Survey template validation**: Add duplicate `mapsToTmField` check to `validateSurveyJSON`
- **No OpenAPI changes**: The answers table is internal; no new API endpoints are exposed
