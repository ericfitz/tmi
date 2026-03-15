# Create Threat Model from Survey Response Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `POST /triage/survey_responses/{id}/create_threat_model` endpoint that creates a threat model from an approved survey response, mapping survey answers to TM fields and sub-resources.

**Architecture:** Handler + service function in `survey_handlers.go`. The handler validates preconditions and emits side effects (audit, broadcast, webhooks). The service function `createThreatModelFromResponse` handles business logic: answer processing, field mapping, TM construction. Answer flattening/sanitization lives in a dedicated `answer_flattener.go` file.

**Tech Stack:** Go, Gin, GORM, bluemonday (sanitization), oapi-codegen generated types

**Spec:** `docs/superpowers/specs/2026-03-14-create-threat-model-from-survey-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `api/answer_flattener.go` | Create | `flattenAnswerValue` and `flattenAndSanitize` utilities |
| `api/answer_flattener_test.go` | Create | Unit tests for flattening/sanitization |
| `api/survey_response_store_gorm.go` | Modify | Add `SetCreatedThreatModel` to interface + GORM impl |
| `api/survey_handlers.go` | Modify | Replace 501 stub with handler + service function |
| `api/create_threat_model_from_survey_test.go` | Create | Unit tests for service function and handler |

---

## Chunk 1: Answer Flattener

### Task 1: Answer Flattener — Tests

**Files:**
- Create: `api/answer_flattener_test.go`

- [ ] **Step 1: Write the test file**

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlattenAnswerValue_String(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`"hello world"`))
	assert.Equal(t, "hello world", result)
}

func TestFlattenAnswerValue_Number(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`42`))
	assert.Equal(t, "42", result)

	result = flattenAnswerValue(json.RawMessage(`3.14`))
	assert.Equal(t, "3.14", result)
}

func TestFlattenAnswerValue_Boolean(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`true`))
	assert.Equal(t, "true", result)

	result = flattenAnswerValue(json.RawMessage(`false`))
	assert.Equal(t, "false", result)
}

func TestFlattenAnswerValue_Null(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`null`))
	assert.Equal(t, "", result)

	result = flattenAnswerValue(nil)
	assert.Equal(t, "", result)
}

func TestFlattenAnswerValue_StringArray(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`["option1", "option2", "option3"]`))
	assert.Equal(t, "option1, option2, option3", result)
}

func TestFlattenAnswerValue_MixedArray(t *testing.T) {
	// Mixed-type arrays get JSON representation
	result := flattenAnswerValue(json.RawMessage(`[1, "two", true]`))
	assert.Equal(t, `[1, "two", true]`, result)
}

func TestFlattenAnswerValue_Object(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`{"key": "value"}`))
	assert.Equal(t, `{"key": "value"}`, result)
}

func TestFlattenAndSanitize_HTMLInjection(t *testing.T) {
	result := flattenAndSanitize(json.RawMessage(`"<script>alert('xss')</script>Hello"`))
	assert.NotContains(t, result, "<script>")
	assert.Contains(t, result, "Hello")
}

func TestFlattenAndSanitize_HTMLInArray(t *testing.T) {
	result := flattenAndSanitize(json.RawMessage(`["<b>bold</b>", "normal"]`))
	assert.NotContains(t, result, "<b>")
	assert.Contains(t, result, "bold")
	assert.Contains(t, result, "normal")
}

func TestFlattenAnswerValue_EmptyString(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`""`))
	assert.Equal(t, "", result)
}

func TestFlattenAnswerValue_EmptyArray(t *testing.T) {
	result := flattenAnswerValue(json.RawMessage(`[]`))
	assert.Equal(t, "", result)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestFlattenAnswerValue`
Expected: FAIL — `flattenAnswerValue` not defined

### Task 2: Answer Flattener — Implementation

**Files:**
- Create: `api/answer_flattener.go`

- [ ] **Step 3: Write the implementation**

```go
package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// flattenAnswerValue converts a JSON answer value to a plain string.
// Arrays of strings become comma-separated. Booleans and numbers become
// their string representations. Objects and mixed arrays become JSON strings.
// Null and nil become empty string.
func flattenAnswerValue(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}

	// Try null
	if string(value) == "null" {
		return ""
	}

	// Try string
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return s
	}

	// Try number (json.Number or float64)
	var f float64
	if err := json.Unmarshal(value, &f); err == nil {
		// Use %g to avoid trailing zeros (42.0 -> "42", 3.14 -> "3.14")
		return fmt.Sprintf("%g", f)
	}

	// Try boolean
	var b bool
	if err := json.Unmarshal(value, &b); err == nil {
		return fmt.Sprintf("%t", b)
	}

	// Try array
	var arr []json.RawMessage
	if err := json.Unmarshal(value, &arr); err == nil {
		if len(arr) == 0 {
			return ""
		}
		// Check if all elements are strings
		strs := make([]string, 0, len(arr))
		allStrings := true
		for _, elem := range arr {
			var s string
			if err := json.Unmarshal(elem, &s); err != nil {
				allStrings = false
				break
			}
			strs = append(strs, s)
		}
		if allStrings {
			return strings.Join(strs, ", ")
		}
		// Mixed array: return JSON representation
		return string(value)
	}

	// Object or anything else: return JSON representation
	return string(value)
}

// flattenAndSanitize flattens a JSON answer value to a string and sanitizes
// it via bluemonday to prevent injection attacks.
func flattenAndSanitize(value json.RawMessage) string {
	flat := flattenAnswerValue(value)
	return SanitizePlainText(flat)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestFlatten`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add api/answer_flattener.go api/answer_flattener_test.go
git commit -m "feat(api): add answer flattener for survey-to-TM field mapping"
```

---

## Chunk 2: Store Extension

### Task 3: SetCreatedThreatModel — Interface + Implementation

**Files:**
- Modify: `api/survey_response_store_gorm.go`

- [ ] **Step 6: Add method to SurveyResponseStore interface**

In `api/survey_response_store_gorm.go`, add to the `SurveyResponseStore` interface (after the `HasAccess` method):

```go
	// SetCreatedThreatModel atomically sets created_threat_model_id and transitions
	// status to review_created. Returns an error if the response is not in
	// ready_for_review status (optimistic concurrency guard).
	SetCreatedThreatModel(ctx context.Context, id uuid.UUID, threatModelID string) error
```

- [ ] **Step 7: Add GORM implementation**

Add the method to `GormSurveyResponseStore` (after the existing `HasAccess` method):

```go
// SetCreatedThreatModel atomically sets created_threat_model_id and transitions
// status to review_created with an optimistic concurrency guard.
func (s *GormSurveyResponseStore) SetCreatedThreatModel(ctx context.Context, id uuid.UUID, threatModelID string) error {
	result := s.db.WithContext(ctx).
		Model(&models.SurveyResponse{}).
		Where("id = ? AND status = ?", id.String(), ResponseStatusReadyForReview).
		Updates(map[string]any{
			"status":                  ResponseStatusReviewCreated,
			"created_threat_model_id": threatModelID,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to set created threat model for response %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("survey response %s is not in ready_for_review status or does not exist", id)
	}
	return nil
}
```

- [ ] **Step 8: Add SetCreatedThreatModel to mockSurveyResponseStore**

In `api/survey_handlers_test.go`, add a field and method to the existing `mockSurveyResponseStore` struct.

Add field to the struct (after `hasAccessErr error`):
```go
	setCreatedTMErr error
```

Add method after the existing mock methods:
```go
func (m *mockSurveyResponseStore) SetCreatedThreatModel(_ context.Context, id uuid.UUID, threatModelID string) error {
	if m.setCreatedTMErr != nil {
		return m.setCreatedTMErr
	}
	resp, exists := m.responses[id]
	if !exists {
		return fmt.Errorf("not found")
	}
	tmUUID, _ := ParseUUID(threatModelID)
	resp.CreatedThreatModelId = &tmUUID
	status := ResponseStatusReviewCreated
	resp.Status = &status
	return nil
}
```

Also check for any in-memory implementation: `grep -r "inMemorySurveyResponseStore" api/`. If found, add the method there too.

- [ ] **Step 9: Build to verify compilation**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 10: Run existing tests**

Run: `make test-unit`
Expected: All PASS

- [ ] **Step 11: Commit**

```bash
git add api/survey_response_store_gorm.go api/survey_handlers_test.go
git commit -m "feat(api): add SetCreatedThreatModel to SurveyResponseStore interface"
```

---

## Chunk 3: Service Function

### Task 5: Service Function — Collection Parser Tests

**Files:**
- Create or extend: `api/create_threat_model_from_survey_test.go`

- [ ] **Step 12: Write collection parsing tests**

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCollectionAnswer_Repositories(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "frontend", "uri": "https://github.com/org/frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 2)
	assert.Empty(t, fallback)

	repo0 := repos[0].(Repository)
	assert.Equal(t, "frontend", *repo0.Name)
	assert.Equal(t, "https://github.com/org/frontend", repo0.Uri)

	repo1 := repos[1].(Repository)
	assert.Equal(t, "backend", *repo1.Name)
	assert.Equal(t, "https://github.com/org/backend", repo1.Uri)
}

func TestParseCollectionAnswer_Documents(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Design Doc", "uri": "https://docs.example.com/design"}
	]`)
	docs, fallback := parseCollectionAnswer("documents", answer)
	assert.Len(t, docs, 1)
	assert.Empty(t, fallback)

	doc := docs[0].(Document)
	assert.Equal(t, "Design Doc", doc.Name)
	assert.Equal(t, "https://docs.example.com/design", doc.Uri)
}

func TestParseCollectionAnswer_Assets(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Database", "type": "data-store", "description": "Main PostgreSQL DB"}
	]`)
	assets, fallback := parseCollectionAnswer("assets", answer)
	assert.Len(t, assets, 1)
	assert.Empty(t, fallback)

	asset := assets[0].(Asset)
	assert.Equal(t, "Database", asset.Name)
	assert.Equal(t, AssetType("data-store"), asset.Type)
	assert.Equal(t, "Main PostgreSQL DB", *asset.Description)
}

func TestParseCollectionAnswer_IncompleteObject(t *testing.T) {
	// Repository missing uri -> falls back to metadata
	answer := json.RawMessage(`[
		{"name": "frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 1) // only complete one
	assert.Len(t, fallback, 1) // incomplete one falls back

	assert.Equal(t, "repositories.name", fallback[0].Key)
	assert.Equal(t, "frontend", fallback[0].Value)
}

func TestParseCollectionAnswer_UnrecognizedCollection(t *testing.T) {
	answer := json.RawMessage(`[{"name": "test"}]`)
	items, fallback := parseCollectionAnswer("unknowns", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1)
}

func TestParseCollectionAnswer_InvalidJSON(t *testing.T) {
	answer := json.RawMessage(`"just a string"`)
	items, fallback := parseCollectionAnswer("repositories", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1) // falls back to metadata
}
```

- [ ] **Step 13: Run tests to verify they fail**

Run: `make test-unit name=TestParseCollection`
Expected: FAIL — `parseCollectionAnswer` not defined

### Task 6: Service Function — Collection Parser Implementation

**Files:**
- Modify: `api/answer_flattener.go` (add collection parsing here since it's answer processing)

- [ ] **Step 14: Write parseCollectionAnswer**

Add to `api/answer_flattener.go`:

```go
// parseCollectionAnswer parses a paneldynamic array-of-objects answer into
// typed sub-resources (Asset, Document, Repository). Returns the successfully
// parsed items and any fallback metadata entries for incomplete objects.
// The collectionType should be "assets", "documents", or "repositories".
func parseCollectionAnswer(collectionType string, answer json.RawMessage) (items []any, fallbackMetadata []Metadata) {
	logger := slogging.Get()

	var objects []map[string]any
	if err := json.Unmarshal(answer, &objects); err != nil {
		// Not an array of objects — fall back to metadata
		logger.Warn("collection answer for %q is not an array of objects, falling back to metadata", collectionType)
		fallbackMetadata = append(fallbackMetadata, Metadata{
			Key:   collectionType,
			Value: SanitizePlainText(flattenAnswerValue(answer)),
		})
		return nil, fallbackMetadata
	}

	for _, obj := range objects {
		switch collectionType {
		case "assets":
			name, _ := obj["name"].(string)
			assetType, _ := obj["type"].(string)
			if name == "" || assetType == "" {
				logger.Warn("incomplete asset object (missing name or type), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("assets.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			asset := Asset{
				Name: SanitizePlainText(name),
				Type: AssetType(SanitizePlainText(assetType)),
			}
			if desc, ok := obj["description"].(string); ok && desc != "" {
				sanitized := SanitizePlainText(desc)
				asset.Description = &sanitized
			}
			items = append(items, asset)

		case "documents":
			name, _ := obj["name"].(string)
			uri, _ := obj["uri"].(string)
			if name == "" || uri == "" {
				logger.Warn("incomplete document object (missing name or uri), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("documents.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			doc := Document{
				Name: SanitizePlainText(name),
				Uri:  SanitizePlainText(uri),
			}
			items = append(items, doc)

		case "repositories":
			name, _ := obj["name"].(string)
			uri, _ := obj["uri"].(string)
			if name == "" || uri == "" {
				logger.Warn("incomplete repository object (missing name or uri), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("repositories.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			sanitizedName := SanitizePlainText(name)
			repo := Repository{
				Name: &sanitizedName,
				Uri:  SanitizePlainText(uri),
			}
			items = append(items, repo)

		default:
			logger.Warn("unrecognized collection type %q, falling back to metadata", collectionType)
			for k, v := range obj {
				valBytes, _ := json.Marshal(v)
				fallbackMetadata = append(fallbackMetadata, Metadata{
					Key:   fmt.Sprintf("%s.%s", collectionType, k),
					Value: SanitizePlainText(flattenAnswerValue(valBytes)),
				})
			}
		}
	}
	return items, fallbackMetadata
}
```

Also add `"github.com/ericfitz/tmi/internal/slogging"` to imports in `answer_flattener.go`.

- [ ] **Step 15: Run tests to verify they pass**

Run: `make test-unit name=TestParseCollection`
Expected: All PASS

- [ ] **Step 16: Commit**

```bash
git add api/answer_flattener.go api/answer_flattener_test.go
git commit -m "feat(api): add collection parser for paneldynamic survey answers"
```

### Task 7: Service Function — Core Logic Tests

**Files:**
- Extend: `api/create_threat_model_from_survey_test.go`

- [ ] **Step 17: Write service function tests**

Add these tests to `api/create_threat_model_from_survey_test.go`:

```go
func TestProcessMappedAnswers_ScalarFields(t *testing.T) {
	answers := []SurveyAnswerRow{
		{QuestionName: "project_name", MapsToTmField: strPtr("name"), AnswerValue: json.RawMessage(`"My Project"`)},
		{QuestionName: "project_desc", MapsToTmField: strPtr("description"), AnswerValue: json.RawMessage(`"A description"`)},
		{QuestionName: "tracker_url", MapsToTmField: strPtr("issue_uri"), AnswerValue: json.RawMessage(`"https://jira.example.com/123"`)},
	}

	result := processMappedAnswers(answers)
	assert.Equal(t, "My Project", *result.name)
	assert.Equal(t, "A description", *result.description)
	assert.Equal(t, "https://jira.example.com/123", *result.issueURI)
}

func TestProcessMappedAnswers_MetadataKey(t *testing.T) {
	answers := []SurveyAnswerRow{
		{QuestionName: "team", MapsToTmField: strPtr("metadata.team_name"), AnswerValue: json.RawMessage(`"Platform"`)},
	}

	result := processMappedAnswers(answers)
	require.Len(t, result.metadata, 1)
	assert.Equal(t, "team_name", result.metadata[0].Key)
	assert.Equal(t, "Platform", result.metadata[0].Value)
}

func TestProcessMappedAnswers_UnmappedAnswers(t *testing.T) {
	answers := []SurveyAnswerRow{
		{QuestionName: "project_name", MapsToTmField: strPtr("name"), AnswerValue: json.RawMessage(`"My Project"`)},
		{QuestionName: "extra_info", MapsToTmField: nil, AnswerValue: json.RawMessage(`"some notes"`)},
		{QuestionName: "checkboxes", MapsToTmField: nil, AnswerValue: json.RawMessage(`["opt1", "opt2"]`)},
	}

	result := processMappedAnswers(answers)
	// Unmapped answers go to metadata
	assert.Len(t, result.metadata, 2)
	assert.Equal(t, "extra_info", result.metadata[0].Key)
	assert.Equal(t, "some notes", result.metadata[0].Value)
	assert.Equal(t, "checkboxes", result.metadata[1].Key)
	assert.Equal(t, "opt1, opt2", result.metadata[1].Value)
}

func TestProcessMappedAnswers_UnrecognizedField(t *testing.T) {
	answers := []SurveyAnswerRow{
		{QuestionName: "weird", MapsToTmField: strPtr("unknown_field"), AnswerValue: json.RawMessage(`"value"`)},
	}

	result := processMappedAnswers(answers)
	// Unrecognized fields go to metadata
	require.Len(t, result.metadata, 1)
	assert.Equal(t, "unknown_field", result.metadata[0].Key)
	assert.Equal(t, "value", result.metadata[0].Value)
}

func TestProcessMappedAnswers_Collections(t *testing.T) {
	answers := []SurveyAnswerRow{
		{
			QuestionName:  "repos",
			MapsToTmField: strPtr("repositories"),
			AnswerValue: json.RawMessage(`[
				{"name": "frontend", "uri": "https://github.com/org/frontend"}
			]`),
		},
	}

	result := processMappedAnswers(answers)
	assert.Len(t, result.repositories, 1)
}

// Helper
func strPtr(s string) *string { return &s }
```

- [ ] **Step 18: Run tests to verify they fail**

Run: `make test-unit name=TestProcessMappedAnswers`
Expected: FAIL — `processMappedAnswers` not defined

### Task 8: Service Function — Core Logic Implementation

**Files:**
- Modify: `api/survey_handlers.go`

- [ ] **Step 19: Write the processMappedAnswers function and result type**

Add to `api/survey_handlers.go` (before the `CreateThreatModelFromSurveyResponse` handler):

```go
// mappedAnswerResult holds the processed results of mapping survey answers
// to threat model fields.
type mappedAnswerResult struct {
	name         *string
	description  *string
	issueURI     *string
	metadata     []Metadata
	assets       []any // []Asset after type assertion
	documents    []any // []Document after type assertion
	repositories []any // []Repository after type assertion
}

// processMappedAnswers iterates all answer rows and dispatches them to the
// appropriate TM field based on mapsToTmField. Unmapped answers become metadata.
func processMappedAnswers(answers []SurveyAnswerRow) mappedAnswerResult {
	logger := slogging.Get()
	var result mappedAnswerResult

	for _, row := range answers {
		if row.MapsToTmField == nil {
			// Unmapped answer -> metadata
			result.metadata = append(result.metadata, Metadata{
				Key:   row.QuestionName,
				Value: flattenAndSanitize(row.AnswerValue),
			})
			continue
		}

		field := *row.MapsToTmField
		switch {
		case field == "name":
			val := flattenAndSanitize(row.AnswerValue)
			result.name = &val

		case field == "description":
			val := flattenAndSanitize(row.AnswerValue)
			result.description = &val

		case field == "issue_uri":
			val := flattenAndSanitize(row.AnswerValue)
			result.issueURI = &val

		case strings.HasPrefix(field, "metadata."):
			key := strings.TrimPrefix(field, "metadata.")
			result.metadata = append(result.metadata, Metadata{
				Key:   SanitizePlainText(key),
				Value: flattenAndSanitize(row.AnswerValue),
			})

		case field == "assets" || field == "documents" || field == "repositories":
			items, fallback := parseCollectionAnswer(field, row.AnswerValue)
			result.metadata = append(result.metadata, fallback...)
			switch field {
			case "assets":
				result.assets = append(result.assets, items...)
			case "documents":
				result.documents = append(result.documents, items...)
			case "repositories":
				result.repositories = append(result.repositories, items...)
			}

		default:
			logger.Warn("unrecognized mapsToTmField %q on question %q, falling back to metadata", field, row.QuestionName)
			result.metadata = append(result.metadata, Metadata{
				Key:   field,
				Value: flattenAndSanitize(row.AnswerValue),
			})
		}
	}

	return result
}
```

- [ ] **Step 20: Run tests to verify they pass**

Run: `make test-unit name=TestProcessMappedAnswers`
Expected: All PASS

- [ ] **Step 21: Commit**

```bash
git add api/survey_handlers.go api/create_threat_model_from_survey_test.go
git commit -m "feat(api): add processMappedAnswers for survey-to-TM field dispatch"
```

---

## Chunk 4: Service Function — TM Builder + Name Fallback

### Task 9: TM Name Fallback Tests

**Files:**
- Extend: `api/create_threat_model_from_survey_test.go`

- [ ] **Step 22: Write name fallback tests**

```go
func TestBuildThreatModelName_MappedName(t *testing.T) {
	name := "My Mapped Name"
	result := buildThreatModelName(&name, "", "")
	assert.Equal(t, "My Mapped Name", result)
}

func TestBuildThreatModelName_FallbackWithProject(t *testing.T) {
	result := buildThreatModelName(nil, "Security Review", "Payment Service")
	// Format: "{template_name}: {project_name} - {date}"
	assert.Contains(t, result, "Security Review: Payment Service - ")
	// Verify date is ISO 8601
	assert.Regexp(t, `\d{4}-\d{2}-\d{2}$`, result)
}

func TestBuildThreatModelName_FallbackWithoutProject(t *testing.T) {
	result := buildThreatModelName(nil, "Security Review", "")
	// Format: "{template_name} - {date}"
	assert.Contains(t, result, "Security Review - ")
	assert.NotContains(t, result, ": ")
}

func TestBuildThreatModelName_EmptyMappedName(t *testing.T) {
	empty := ""
	result := buildThreatModelName(&empty, "Security Review", "")
	// Empty mapped name should use fallback
	assert.Contains(t, result, "Security Review - ")
}
```

- [ ] **Step 23: Run tests to verify they fail**

Run: `make test-unit name=TestBuildThreatModelName`
Expected: FAIL

### Task 10: TM Name Fallback Implementation

**Files:**
- Modify: `api/survey_handlers.go`

- [ ] **Step 24: Write buildThreatModelName**

Add to `api/survey_handlers.go`:

```go
// buildThreatModelName returns the TM name from the mapped name field, or
// constructs a fallback from template name, project name, and current date.
func buildThreatModelName(mappedName *string, templateName, projectName string) string {
	if mappedName != nil && *mappedName != "" {
		return *mappedName
	}

	date := time.Now().UTC().Format("2006-01-02")
	if projectName != "" {
		return fmt.Sprintf("%s: %s - %s", templateName, projectName, date)
	}
	return fmt.Sprintf("%s - %s", templateName, date)
}
```

Add `"time"` to the imports in `survey_handlers.go` if not already present.

- [ ] **Step 25: Run tests**

Run: `make test-unit name=TestBuildThreatModelName`
Expected: All PASS

- [ ] **Step 26: Commit**

```bash
git add api/survey_handlers.go api/create_threat_model_from_survey_test.go
git commit -m "feat(api): add buildThreatModelName with mapped name and fallback"
```

### Task 11: createThreatModelFromResponse Service Function

**Files:**
- Modify: `api/survey_handlers.go`

- [ ] **Step 27: Write the createThreatModelFromResponse function**

Add to `api/survey_handlers.go`:

```go
// createThreatModelFromResponse builds and creates a ThreatModel from a survey
// response's answers, mapping fields according to mapsToTmField directives.
// Returns the created ThreatModel and any sub-resources that failed to create.
func createThreatModelFromResponse(ctx context.Context, response *SurveyResponse) (*ThreatModel, error) {
	logger := slogging.Get()

	// Step 1: Get all answers
	answers, err := GlobalSurveyAnswerStore.GetAnswers(ctx, response.Id.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get answers for response %s: %w", response.Id.String(), err)
	}

	// Step 2: Process mapped fields
	mapped := processMappedAnswers(answers)

	// Step 3: Build TM name
	var templateName string
	if GlobalSurveyStore != nil {
		survey, err := GlobalSurveyStore.Get(ctx, response.SurveyId)
		if err != nil {
			logger.Warn("failed to load survey template %s for TM name fallback: %v", response.SurveyId.String(), err)
			templateName = "Survey"
		} else if survey == nil {
			logger.Warn("survey template %s not found for TM name fallback", response.SurveyId.String())
			templateName = "Survey"
		} else {
			templateName = survey.Name
		}
	}

	var projectName string
	if response.ProjectId != nil && GlobalProjectStore != nil {
		project, err := GlobalProjectStore.Get(ctx, response.ProjectId.String())
		if err != nil {
			logger.Warn("failed to load project %s for TM name fallback: %v", response.ProjectId.String(), err)
		} else if project != nil {
			projectName = project.Name
		}
	}

	tmName := buildThreatModelName(mapped.name, templateName, projectName)

	// Step 4: Build metadata
	metadata := &mapped.metadata
	if err := SanitizeMetadataSlice(metadata); err != nil {
		logger.Warn("metadata sanitization warning: %v", err)
	}

	// Step 5: Build owner and authorization
	owner := *response.Owner
	authorizations := []Authorization{
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      owner.Provider,
			ProviderId:    owner.ProviderId,
			Role:          RoleOwner,
		},
	}

	// Set security reviewer from the person who reviewed the response
	var securityReviewer *User
	if response.ReviewedBy != nil {
		securityReviewer = response.ReviewedBy
	}

	// Apply security reviewer rule (auto-add to authorization)
	authorizations = ApplySecurityReviewerRule(authorizations, securityReviewer)

	// Step 6: Copy confidentiality
	isConfidential := response.IsConfidential

	// Step 7: Build and create threat model
	now := time.Now().UTC()
	emptyThreats := []Threat{}

	tm := ThreatModel{
		Name:                 tmName,
		Description:          mapped.description,
		IssueUri:             mapped.issueURI,
		IsConfidential:       isConfidential,
		SecurityReviewer:     securityReviewer,
		CreatedAt:            &now,
		ModifiedAt:           &now,
		Owner:                owner,
		CreatedBy:            &owner,
		Authorization:        authorizations,
		Metadata:             metadata,
		Threats:              &emptyThreats,
	}

	// Copy project reference if set
	if response.ProjectId != nil {
		tm.ProjectId = response.ProjectId
	}

	idSetter := func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	}

	createdTM, err := ThreatModelStore.Create(tm, idSetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create threat model: %w", err)
	}

	// Step 8: Create sub-resources (non-fatal failures)
	tmID := createdTM.Id.String()

	for _, item := range mapped.assets {
		asset := item.(Asset)
		if err := GlobalAssetStore.Create(ctx, &asset, tmID); err != nil {
			logger.Warn("failed to create asset %q for TM %s: %v", asset.Name, tmID, err)
		}
	}

	for _, item := range mapped.documents {
		doc := item.(Document)
		if err := GlobalDocumentStore.Create(ctx, &doc, tmID); err != nil {
			logger.Warn("failed to create document %q for TM %s: %v", doc.Name, tmID, err)
		}
	}

	for _, item := range mapped.repositories {
		repo := item.(Repository)
		repoName := ""
		if repo.Name != nil {
			repoName = *repo.Name
		}
		if err := GlobalRepositoryStore.Create(ctx, &repo, tmID); err != nil {
			logger.Warn("failed to create repository %q for TM %s: %v", repoName, tmID, err)
		}
	}

	return &createdTM, nil
}
```

- [ ] **Step 28: Build to verify compilation**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 29: Commit**

```bash
git add api/survey_handlers.go
git commit -m "feat(api): add createThreatModelFromResponse service function"
```

---

## Chunk 5: Handler Implementation

### Task 12: Handler — Replace 501 Stub

**Files:**
- Modify: `api/survey_handlers.go`

- [ ] **Step 30: Replace the 501 stub**

Replace the existing `CreateThreatModelFromSurveyResponse` method (around line 1327-1336) with:

```go
// CreateThreatModelFromSurveyResponse creates a threat model from an approved survey response.
// POST /triage/survey_responses/{response_id}/create_threat_model
func (s *Server) CreateThreatModelFromSurveyResponse(c *gin.Context, surveyResponseId SurveyResponseId) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Step 1: Load survey response
	response, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseId)
	if err != nil {
		logger.WithContext(c).Error("failed to get survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return
	}

	// Step 2: Extract user identity
	userInternalUUID, ok := getUserUUID(c)
	if !ok {
		return // getUserUUID already wrote the error response
	}

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Step 3: Check access
	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseId, userInternalUUID, AuthorizationRoleOwner)
	if err != nil {
		logger.WithContext(c).Error("failed to check access for survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, ServerError("Failed to check access"))
		return
	}
	if !hasAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions"))
		return
	}

	// Step 4: Validate preconditions
	if response.Owner == nil {
		logger.WithContext(c).Error("survey response %s has nil owner", surveyResponseId.String())
		HandleRequestError(c, ServerError("Survey response owner not found"))
		return
	}

	if response.Status == nil || *response.Status != ResponseStatusReadyForReview {
		currentStatus := "unknown"
		if response.Status != nil {
			currentStatus = *response.Status
		}
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: fmt.Sprintf("Survey response must be in '%s' status to create a threat model (current: '%s')", ResponseStatusReadyForReview, currentStatus),
		})
		return
	}

	if response.CreatedThreatModelId != nil {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: fmt.Sprintf("A threat model has already been created from this survey response (threat_model_id: %s)", response.CreatedThreatModelId.String()),
		})
		return
	}

	// Step 5: Create threat model from response
	createdTM, err := createThreatModelFromResponse(ctx, response)
	if err != nil {
		logger.WithContext(c).Error("failed to create threat model from survey response %s: %v", surveyResponseId.String(), err)
		HandleRequestError(c, ServerError("Failed to create threat model"))
		return
	}

	// Step 6: Update survey response
	if err := GlobalSurveyResponseStore.SetCreatedThreatModel(ctx, surveyResponseId, createdTM.Id.String()); err != nil {
		logger.WithContext(c).Error("failed to update survey response %s after TM creation: %v", surveyResponseId.String(), err)
		// TM was created but response wasn't updated — log but don't fail the response
		// The TM exists and the user can see it
	}

	// Step 7: Record audit
	RecordAuditCreate(c, createdTM.Id.String(), "threat_model", createdTM.Id.String(), createdTM)

	// Step 8: Broadcast notification
	BroadcastThreatModelCreated(userEmail, createdTM.Id.String(), createdTM.Name)

	// Step 9: Emit webhooks
	if GlobalEventEmitter != nil {
		tmPayload := EventPayload{
			EventType:     EventThreatModelCreated,
			ThreatModelID: createdTM.Id.String(),
			ResourceID:    createdTM.Id.String(),
			ResourceType:  "threat_model",
			OwnerID:       GetOwnerInternalUUID(ctx, createdTM.Owner.Provider, createdTM.Owner.ProviderId),
			Data: map[string]any{
				"name":        createdTM.Name,
				"description": createdTM.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, tmPayload)

		responsePayload := EventPayload{
			EventType:    EventSurveyResponseUpdated,
			ResourceID:   surveyResponseId.String(),
			ResourceType: "survey_response",
			Data: map[string]any{
				"survey_id": response.SurveyId.String(),
			},
		}
		_ = GlobalEventEmitter.EmitEvent(ctx, responsePayload)
	}

	// Step 10: Return 201
	c.Header("Location", "/threat_models/"+createdTM.Id.String())
	c.JSON(http.StatusCreated, CreateThreatModelFromSurveyResponse{
		ThreatModelId:    *createdTM.Id,
		SurveyResponseId: surveyResponseId,
	})
}
```

- [ ] **Step 31: Build to verify compilation**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 32: Commit**

```bash
git add api/survey_handlers.go
git commit -m "feat(api): implement CreateThreatModelFromSurveyResponse handler

Replaces the 501 Not Implemented stub with a full implementation that:
- Validates preconditions (status, no duplicate TM, access check)
- Maps survey answers to TM fields via mapsToTmField directives
- Creates sub-resources (assets, documents, repositories) from paneldynamic answers
- Transitions survey response to review_created status
- Emits audit, broadcast, and webhook events

Fixes #177"
```

---

## Chunk 6: Handler Tests

### Task 13: Handler Unit Tests

**Files:**
- Extend: `api/create_threat_model_from_survey_test.go`

- [ ] **Step 33: Write handler tests**

Add comprehensive handler tests covering:

1. **404** — survey response not found
2. **403** — access denied (user doesn't have owner role)
3. **409** — wrong status (not `ready_for_review`)
4. **409** — duplicate (CreatedThreatModelId already set)
5. **500** — nil owner
6. **201** — successful creation with correct response body

These tests require mock stores. Use the existing `mockSurveyResponseStore` and `mockSurveyAnswerStore` from `survey_handlers_test.go`. You'll also need a mock `ThreatModelStoreInterface`.

Key test structure:

```go
func TestCreateThreatModelFromSurveyResponse_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{},
		getErr:    fmt.Errorf("not found"),
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+uuid.New().String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice-provider-id")

	server.CreateThreatModelFromSurveyResponse(c, uuid.New())

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_WrongStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	draftStatus := ResponseStatusDraft
	owner := &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    "alice-provider-id",
		Email:         "alice@example.com",
	}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {
				Id:     &responseID,
				Status: &draftStatus,
				Owner:  owner,
			},
		},
		accessMap: map[string]AuthorizationRole{
			responseID.String() + ":user-123": AuthorizationRoleOwner,
		},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice-provider-id")

	server.CreateThreatModelFromSurveyResponse(c, responseID)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_DuplicateTM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview
	existingTMID := uuid.New()
	owner := &User{PrincipalType: UserPrincipalTypeUser, Provider: "tmi", ProviderId: "alice"}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: owner, CreatedThreatModelId: &existingTMID},
		},
		accessMap: map[string]AuthorizationRole{responseID.String() + ":user-123": AuthorizationRoleOwner},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_NilOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: nil},
		},
		accessMap: map[string]AuthorizationRole{responseID.String() + ":user-123": AuthorizationRoleOwner},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_AccessDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview
	owner := &User{PrincipalType: UserPrincipalTypeUser, Provider: "tmi", ProviderId: "alice"}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: owner},
		},
		accessMap: map[string]AuthorizationRole{}, // No access
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

For the full 201 success test, you'll need to mock `ThreatModelStore`, `GlobalSurveyAnswerStore`, `GlobalSurveyStore`, and set up the response with `ready_for_review` status. The success test should verify:
- Response body contains `threat_model_id` and `survey_response_id`
- Location header is set
- Survey response status transitioned to `review_created`
- `CreatedThreatModelId` is set on the response

- [ ] **Step 34: Run tests**

Run: `make test-unit name=TestCreateThreatModelFromSurveyResponse`
Expected: All PASS

- [ ] **Step 35: Commit**

```bash
git add api/create_threat_model_from_survey_test.go
git commit -m "test(api): add handler tests for CreateThreatModelFromSurveyResponse"
```

---

## Chunk 7: Quality Gates

### Task 14: Lint, Build, Test

- [ ] **Step 36: Run linter**

Run: `make lint`
Expected: No new issues (existing api/api.go warnings are expected)

Fix any lint issues found.

- [ ] **Step 37: Build**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 38: Run all unit tests**

Run: `make test-unit`
Expected: All PASS

- [ ] **Step 39: Commit any lint fixes**

If lint fixes were needed:
```bash
git add -u
git commit -m "style(api): fix lint issues in create_threat_model implementation"
```

### Task 15: Integration Tests

- [ ] **Step 40: Run integration tests**

Run: `make test-integration`
Expected: All PASS (including the new endpoint working against real DB)

- [ ] **Step 41: Fix any integration issues**

If issues found, fix and commit:
```bash
git add -u
git commit -m "fix(api): resolve integration test issues for create_threat_model"
```

### Task 16: Push and Close Issue

- [ ] **Step 42: Push to remote**

```bash
git pull --rebase
git push
git status  # MUST show "up to date with origin"
```

- [ ] **Step 43: Close the issue**

```bash
gh issue close 177 --repo ericfitz/tmi --comment "Implemented in release/1.3.0 branch. The endpoint now creates threat models from approved survey responses with field mapping, collection support, and proper authorization."
```
