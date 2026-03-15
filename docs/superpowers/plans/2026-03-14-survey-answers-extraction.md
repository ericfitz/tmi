# Survey Answers Extraction Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract survey response answers into a structured `survey_answers` table on every answer save, enabling server-side field mapping for `create_threat_model`.

**Architecture:** Add a `SurveyAnswer` GORM model, a pure `ExtractQuestions` function for recursive SurveyJS parsing, a `SurveyAnswerStore` interface with GORM implementation, and wire `ExtractAndSave` calls into all survey response handlers that modify answers or status.

**Tech Stack:** Go, GORM, PostgreSQL, Gin, testify

**Spec:** `docs/superpowers/specs/2026-03-14-survey-answers-extraction-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `api/models/survey_models.go` | Modify | Add `SurveyAnswer` struct |
| `api/models/models.go` | Modify | Register `SurveyAnswer` in `AllModels()` |
| `api/survey_question_extractor.go` | Create | Pure `ExtractQuestions` function |
| `api/survey_question_extractor_test.go` | Create | Tests for question extraction |
| `api/survey_answer_store.go` | Create | `SurveyAnswerStore` interface + GORM impl |
| `api/survey_answer_store_test.go` | Create | Tests for store |
| `api/store.go` | Modify | Add `GlobalSurveyAnswerStore` var + init |
| `api/survey_handlers.go` | Modify | Wire `ExtractAndSave` into handlers |
| `api/survey_handlers_test.go` | Modify | Add mock store + extraction tests |
| `test/testdb/testdb.go` | Modify | Add `SurveyAnswer` to test migration |

---

## Task 1: Add SurveyAnswer GORM Model

**Files:**
- Modify: `api/models/survey_models.go` (append after `SurveyResponseAccess`)
- Modify: `api/models/models.go:804` (add to `AllModels`)
- Modify: `test/testdb/testdb.go:110` (add to test migration)

- [ ] **Step 1: Add SurveyAnswer struct to survey_models.go**

Append after the `SurveyResponseAccess` model (after line 183):

```go
// SurveyAnswer represents an extracted answer from a survey response.
// Rows are fully replaced on every response save for consistency.
type SurveyAnswer struct {
	ID             string    `gorm:"primaryKey;type:varchar(36)"`
	ResponseID     string    `gorm:"type:varchar(36);not null;index:idx_sa_response_id;index:idx_sa_response_mapping"`
	QuestionName   string    `gorm:"type:varchar(256);not null"`
	QuestionType   string    `gorm:"type:varchar(64);not null"`
	QuestionTitle  *string   `gorm:"type:varchar(1024)"`
	MapsToTmField  *string   `gorm:"type:varchar(128);index:idx_sa_response_mapping"`
	AnswerValue    JSONRaw   `gorm:""`
	ResponseStatus string    `gorm:"type:varchar(30);not null"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	SurveyResponse SurveyResponse `gorm:"foreignKey:ResponseID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for SurveyAnswer
func (SurveyAnswer) TableName() string {
	return tableName("survey_answers")
}

// BeforeCreate generates a UUID if not set
func (s *SurveyAnswer) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}
```

- [ ] **Step 2: Register in AllModels**

In `api/models/models.go`, add `&SurveyAnswer{}` after `&TriageNote{}` (line 805):

```go
		&TriageNote{},
		&SurveyAnswer{},
```

- [ ] **Step 3: Add to test DB migration**

In `test/testdb/testdb.go`, add `&models.SurveyAnswer{}` after `&models.TriageNote{}` (line 110):

```go
		&models.TriageNote{},
		&models.SurveyAnswer{},
```

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add api/models/survey_models.go api/models/models.go test/testdb/testdb.go
git commit -m "feat(models): add SurveyAnswer GORM model for extracted survey answers

Adds survey_answers table with question metadata, field mapping
annotations, and denormalized response status. Part of #178."
```

---

## Task 2: Implement ExtractQuestions Function

**Files:**
- Create: `api/survey_question_extractor.go`
- Create: `api/survey_question_extractor_test.go`

- [ ] **Step 1: Write failing tests for ExtractQuestions**

Create `api/survey_question_extractor_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractQuestions_FlatSurvey(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":  "text",
						"name":  "project_name",
						"title": "Project Name",
					},
					map[string]any{
						"type":  "comment",
						"name":  "description",
						"title": "Description",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	assert.Len(t, questions, 2)
	assert.Equal(t, "project_name", questions[0].Name)
	assert.Equal(t, "text", questions[0].Type)
	assert.Equal(t, "Project Name", *questions[0].Title)
	assert.Nil(t, questions[0].MapsToTmField)
	assert.Equal(t, "description", questions[1].Name)
}

func TestExtractQuestions_WithFieldMapping(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":           "text",
						"name":           "project_name",
						"title":          "Project Name",
						"mapsToTmField":  "name",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	require.Len(t, questions, 1)
	require.NotNil(t, questions[0].MapsToTmField)
	assert.Equal(t, "name", *questions[0].MapsToTmField)
}

func TestExtractQuestions_NestedPanels(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "panel",
						"name": "details_panel",
						"elements": []any{
							map[string]any{
								"type":  "text",
								"name":  "detail_field",
								"title": "Detail",
							},
						},
					},
					map[string]any{
						"type":  "text",
						"name":  "top_level",
						"title": "Top Level",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	assert.Len(t, questions, 2)
	// Panel child extracted as leaf question
	assert.Equal(t, "detail_field", questions[0].Name)
	assert.Equal(t, "top_level", questions[1].Name)
}

func TestExtractQuestions_PanelDynamic(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "paneldynamic",
						"name": "risks",
						"templateElements": []any{
							map[string]any{
								"type":  "text",
								"name":  "risk_name",
								"title": "Risk Name",
							},
						},
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	// paneldynamic itself is emitted as a question
	require.Len(t, questions, 1)
	assert.Equal(t, "risks", questions[0].Name)
	assert.Equal(t, "paneldynamic", questions[0].Type)
}

func TestExtractQuestions_DeeplyNestedPanels(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "panel",
						"name": "outer",
						"elements": []any{
							map[string]any{
								"type": "panel",
								"name": "inner",
								"elements": []any{
									map[string]any{
										"type":  "text",
										"name":  "deep_field",
										"title": "Deep Field",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	assert.Len(t, questions, 1)
	assert.Equal(t, "deep_field", questions[0].Name)
}

func TestExtractQuestions_DuplicateFieldMapping(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":          "text",
						"name":          "field_a",
						"mapsToTmField": "name",
					},
					map[string]any{
						"type":          "text",
						"name":          "field_b",
						"mapsToTmField": "name",
					},
				},
			},
		},
	}

	_, err := ExtractQuestions(surveyJSON, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate mapsToTmField")
	assert.Contains(t, err.Error(), "name")
}

func TestExtractQuestions_MissingPages(t *testing.T) {
	surveyJSON := map[string]any{
		"title": "No pages here",
	}

	_, err := ExtractQuestions(surveyJSON, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pages")
}

func TestExtractQuestions_SkipsElementsWithoutName(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "html",
						// no name
					},
					map[string]any{
						"type":  "text",
						"name":  "valid_field",
						"title": "Valid",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	assert.Len(t, questions, 1)
	assert.Equal(t, "valid_field", questions[0].Name)
}

func TestExtractQuestions_MultiplePages(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "text",
						"name": "q1",
					},
				},
			},
			map[string]any{
				"name": "page2",
				"elements": []any{
					map[string]any{
						"type": "text",
						"name": "q2",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	assert.Len(t, questions, 2)
}

func TestExtractQuestions_NoTitle(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "text",
						"name": "no_title_field",
					},
				},
			},
		},
	}

	questions, err := ExtractQuestions(surveyJSON, nil)
	require.NoError(t, err)
	require.Len(t, questions, 1)
	assert.Nil(t, questions[0].Title)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestExtractQuestions`
Expected: FAIL (function not defined)

- [ ] **Step 3: Implement ExtractQuestions**

Create `api/survey_question_extractor.go`:

```go
package api

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SurveyQuestion represents a leaf question extracted from SurveyJS JSON.
type SurveyQuestion struct {
	Name          string
	Type          string
	Title         *string
	MapsToTmField *string
}

// ExtractQuestions recursively extracts leaf questions from a SurveyJS survey_json object.
// Returns an error if the JSON structure is invalid or duplicate mapsToTmField values are found.
// Pass nil for logger to suppress warnings about skipped elements.
func ExtractQuestions(surveyJSON map[string]any, logger *slogging.Logger) ([]SurveyQuestion, error) {
	pagesRaw, ok := surveyJSON["pages"]
	if !ok {
		return nil, fmt.Errorf("survey_json must contain a 'pages' field")
	}
	pages, ok := pagesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("survey_json 'pages' must be an array")
	}

	var questions []SurveyQuestion
	for _, pageRaw := range pages {
		page, ok := pageRaw.(map[string]any)
		if !ok {
			continue
		}
		elementsRaw, ok := page["elements"]
		if !ok {
			continue
		}
		elements, ok := elementsRaw.([]any)
		if !ok {
			continue
		}
		extracted := extractFromElements(elements, logger)
		questions = append(questions, extracted...)
	}

	// Check for duplicate mapsToTmField values
	seen := make(map[string]string) // mapsToTmField -> question name
	for _, q := range questions {
		if q.MapsToTmField != nil {
			if prev, exists := seen[*q.MapsToTmField]; exists {
				return nil, fmt.Errorf("duplicate mapsToTmField %q: questions %q and %q both map to the same field", *q.MapsToTmField, prev, q.Name)
			}
			seen[*q.MapsToTmField] = q.Name
		}
	}

	return questions, nil
}

// extractFromElements recursively extracts questions from a SurveyJS elements array.
func extractFromElements(elements []any, logger *slogging.Logger) []SurveyQuestion {
	var questions []SurveyQuestion
	for _, elemRaw := range elements {
		elem, ok := elemRaw.(map[string]any)
		if !ok {
			continue
		}

		elemType, _ := elem["type"].(string)
		elemName, _ := elem["name"].(string)

		// Skip elements without name or type
		if elemName == "" || elemType == "" {
			if logger != nil {
				logger.Debug("skipping survey element without name or type: %v", elem)
			}
			continue
		}

		switch elemType {
		case "panel":
			// Recurse into panel's elements
			if childElementsRaw, ok := elem["elements"]; ok {
				if childElements, ok := childElementsRaw.([]any); ok {
					questions = append(questions, extractFromElements(childElements, logger)...)
				}
			}
		case "paneldynamic":
			// Emit the paneldynamic itself as a question (answer is the full array)
			q := makeQuestion(elem)
			questions = append(questions, q)

			// Scan templateElements for mapsToTmField conflict detection only.
			// Child questions are not emitted as separate rows, but their
			// mapsToTmField values are collected on the parent for dedup checking.
			if teRaw, ok := elem["templateElements"]; ok {
				if te, ok := teRaw.([]any); ok {
					for _, childRaw := range te {
						child, ok := childRaw.(map[string]any)
						if !ok {
							continue
						}
						if mapping, ok := child["mapsToTmField"].(string); ok && mapping != "" {
							childName, _ := child["name"].(string)
							if logger != nil {
								logger.Warn("mapsToTmField %q on paneldynamic child %q is not supported (dynamic panels produce arrays, not scalar values)", mapping, childName)
							}
						}
					}
				}
			}
		default:
			// Leaf question
			q := makeQuestion(elem)
			questions = append(questions, q)
		}
	}
	return questions
}

// makeQuestion creates a SurveyQuestion from a SurveyJS element map.
func makeQuestion(elem map[string]any) SurveyQuestion {
	q := SurveyQuestion{
		Name: elem["name"].(string),
		Type: elem["type"].(string),
	}
	if title, ok := elem["title"].(string); ok && title != "" {
		q.Title = &title
	}
	if mapping, ok := elem["mapsToTmField"].(string); ok && mapping != "" {
		q.MapsToTmField = &mapping
	}
	return q
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestExtractQuestions`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add api/survey_question_extractor.go api/survey_question_extractor_test.go
git commit -m "feat(api): add ExtractQuestions for recursive SurveyJS parsing

Pure function that recursively extracts leaf questions from SurveyJS
survey_json, handling panels, paneldynamic, and mapsToTmField
annotations. Validates no duplicate field mappings. Part of #178."
```

---

## Task 3: Implement SurveyAnswerStore

**Files:**
- Create: `api/survey_answer_store.go`
- Create: `api/survey_answer_store_test.go`
- Modify: `api/store.go:101` (add global var) and `api/store.go:141` (add init)

- [ ] **Step 1: Write failing tests for the store interface**

Create `api/survey_answer_store_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSurveyAnswerStore_ExtractAndSave(t *testing.T) {
	store := newInMemorySurveyAnswerStore()
	ctx := context.Background()

	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":          "text",
						"name":          "project_name",
						"title":         "Project Name",
						"mapsToTmField": "name",
					},
					map[string]any{
						"type":  "comment",
						"name":  "notes",
						"title": "Notes",
					},
				},
			},
		},
	}

	answers := map[string]any{
		"project_name": "My Project",
	}

	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, answers, "draft")
	require.NoError(t, err)

	results, err := store.GetAnswers(ctx, "resp-1")
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Find the mapped answer
	var mapped, unmapped *SurveyAnswerRow
	for i := range results {
		if results[i].QuestionName == "project_name" {
			mapped = &results[i]
		} else {
			unmapped = &results[i]
		}
	}

	require.NotNil(t, mapped)
	require.NotNil(t, mapped.MapsToTmField)
	assert.Equal(t, "name", *mapped.MapsToTmField)
	assert.Equal(t, "draft", mapped.ResponseStatus)

	// Check answer value
	var val string
	require.NoError(t, json.Unmarshal(mapped.AnswerValue, &val))
	assert.Equal(t, "My Project", val)

	// Unmapped question with no answer
	require.NotNil(t, unmapped)
	assert.Nil(t, unmapped.MapsToTmField)
	assert.Nil(t, unmapped.AnswerValue)
}

func TestSurveyAnswerStore_FullReplacement(t *testing.T) {
	store := newInMemorySurveyAnswerStore()
	ctx := context.Background()

	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "text",
						"name": "q1",
					},
				},
			},
		},
	}

	// First save
	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, map[string]any{"q1": "old"}, "draft")
	require.NoError(t, err)

	// Second save replaces
	err = store.ExtractAndSave(ctx, "resp-1", surveyJSON, map[string]any{"q1": "new"}, "submitted")
	require.NoError(t, err)

	results, err := store.GetAnswers(ctx, "resp-1")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "submitted", results[0].ResponseStatus)

	var val string
	require.NoError(t, json.Unmarshal(results[0].AnswerValue, &val))
	assert.Equal(t, "new", val)
}

func TestSurveyAnswerStore_GetFieldMappings(t *testing.T) {
	store := newInMemorySurveyAnswerStore()
	ctx := context.Background()

	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":          "text",
						"name":          "project_name",
						"mapsToTmField": "name",
					},
					map[string]any{
						"type":          "text",
						"name":          "desc",
						"mapsToTmField": "description",
					},
					map[string]any{
						"type": "text",
						"name": "unmapped",
					},
				},
			},
		},
	}

	answers := map[string]any{
		"project_name": "Test",
		"desc":         "A description",
		"unmapped":     "value",
	}

	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, answers, "submitted")
	require.NoError(t, err)

	mappings, err := store.GetFieldMappings(ctx, "resp-1")
	require.NoError(t, err)
	assert.Len(t, mappings, 2)
	assert.Contains(t, mappings, "name")
	assert.Contains(t, mappings, "description")
	assert.NotContains(t, mappings, "unmapped")
}

func TestSurveyAnswerStore_DeleteByResponseID(t *testing.T) {
	store := newInMemorySurveyAnswerStore()
	ctx := context.Background()

	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "text",
						"name": "q1",
					},
				},
			},
		},
	}

	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, map[string]any{}, "draft")
	require.NoError(t, err)

	err = store.DeleteByResponseID(ctx, "resp-1")
	require.NoError(t, err)

	results, err := store.GetAnswers(ctx, "resp-1")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSurveyAnswerStore_PanelDynamicAnswer(t *testing.T) {
	store := newInMemorySurveyAnswerStore()
	ctx := context.Background()

	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type": "paneldynamic",
						"name": "risks",
						"templateElements": []any{
							map[string]any{
								"type": "text",
								"name": "risk_name",
							},
						},
					},
				},
			},
		},
	}

	answers := map[string]any{
		"risks": []any{
			map[string]any{"risk_name": "SQL Injection"},
			map[string]any{"risk_name": "XSS"},
		},
	}

	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, answers, "draft")
	require.NoError(t, err)

	results, err := store.GetAnswers(ctx, "resp-1")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "risks", results[0].QuestionName)
	assert.Equal(t, "paneldynamic", results[0].QuestionType)

	// Answer should be the full array
	var arr []map[string]any
	require.NoError(t, json.Unmarshal(results[0].AnswerValue, &arr))
	assert.Len(t, arr, 2)
	assert.Equal(t, "SQL Injection", arr[0]["risk_name"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSurveyAnswerStore`
Expected: FAIL (types not defined)

- [ ] **Step 3: Implement the store**

Create `api/survey_answer_store.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SurveyAnswerRow represents a row in the survey_answers table for use outside the models package.
type SurveyAnswerRow struct {
	ID             string
	ResponseID     string
	QuestionName   string
	QuestionType   string
	QuestionTitle  *string
	MapsToTmField  *string
	AnswerValue    json.RawMessage // nil if unanswered
	ResponseStatus string
}

// SurveyAnswerStore provides operations for extracted survey answers.
type SurveyAnswerStore interface {
	// ExtractAndSave parses surveyJSON and answers, then replaces all rows for the response.
	ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, status string) error

	// GetAnswers returns all extracted answers for a response.
	GetAnswers(ctx context.Context, responseID string) ([]SurveyAnswerRow, error)

	// GetFieldMappings returns answers with non-null maps_to_tm_field, keyed by target field name.
	GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswerRow, error)

	// DeleteByResponseID removes all extracted answers for a response.
	DeleteByResponseID(ctx context.Context, responseID string) error
}

// --- GORM Implementation ---

// GormSurveyAnswerStore implements SurveyAnswerStore using GORM.
type GormSurveyAnswerStore struct {
	db *gorm.DB
}

// NewGormSurveyAnswerStore creates a new GORM-backed survey answer store.
func NewGormSurveyAnswerStore(db *gorm.DB) *GormSurveyAnswerStore {
	return &GormSurveyAnswerStore{db: db}
}

func (s *GormSurveyAnswerStore) ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, status string) error {
	logger := slogging.Get()

	questions, err := ExtractQuestions(surveyJSON, logger)
	if err != nil {
		logger.Warn("failed to extract questions for response %s: %v", responseID, err)
		return err
	}

	rows := buildAnswerRows(responseID, questions, answers, status)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete existing rows
		if err := tx.Where("response_id = ?", responseID).Delete(&models.SurveyAnswer{}).Error; err != nil {
			return fmt.Errorf("failed to delete existing answers: %w", err)
		}
		// Insert new rows
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return fmt.Errorf("failed to insert answers: %w", err)
			}
		}
		return nil
	})
}

func (s *GormSurveyAnswerStore) GetAnswers(ctx context.Context, responseID string) ([]SurveyAnswerRow, error) {
	var dbRows []models.SurveyAnswer
	if err := s.db.WithContext(ctx).Where("response_id = ?", responseID).Find(&dbRows).Error; err != nil {
		return nil, err
	}
	return toAnswerRows(dbRows), nil
}

func (s *GormSurveyAnswerStore) GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswerRow, error) {
	var dbRows []models.SurveyAnswer
	if err := s.db.WithContext(ctx).Where("response_id = ? AND maps_to_tm_field IS NOT NULL", responseID).Find(&dbRows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]SurveyAnswerRow, len(dbRows))
	for _, row := range dbRows {
		if row.MapsToTmField != nil {
			if _, exists := result[*row.MapsToTmField]; exists {
				return nil, fmt.Errorf("duplicate mapsToTmField %q in stored answers for response %s", *row.MapsToTmField, responseID)
			}
			result[*row.MapsToTmField] = toAnswerRow(row)
		}
	}
	return result, nil
}

func (s *GormSurveyAnswerStore) DeleteByResponseID(ctx context.Context, responseID string) error {
	return s.db.WithContext(ctx).Where("response_id = ?", responseID).Delete(&models.SurveyAnswer{}).Error
}

// --- In-Memory Implementation (for tests) ---

type inMemorySurveyAnswerStore struct {
	rows map[string][]SurveyAnswerRow // keyed by responseID
}

func newInMemorySurveyAnswerStore() *inMemorySurveyAnswerStore {
	return &inMemorySurveyAnswerStore{rows: make(map[string][]SurveyAnswerRow)}
}

func (s *inMemorySurveyAnswerStore) ExtractAndSave(_ context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, status string) error {
	questions, err := ExtractQuestions(surveyJSON, nil)
	if err != nil {
		return err
	}

	modelRows := buildAnswerRows(responseID, questions, answers, status)
	apiRows := toAnswerRows(modelRows)
	s.rows[responseID] = apiRows
	return nil
}

func (s *inMemorySurveyAnswerStore) GetAnswers(_ context.Context, responseID string) ([]SurveyAnswerRow, error) {
	return s.rows[responseID], nil
}

func (s *inMemorySurveyAnswerStore) GetFieldMappings(_ context.Context, responseID string) (map[string]SurveyAnswerRow, error) {
	result := make(map[string]SurveyAnswerRow)
	for _, row := range s.rows[responseID] {
		if row.MapsToTmField != nil {
			result[*row.MapsToTmField] = row
		}
	}
	return result, nil
}

func (s *inMemorySurveyAnswerStore) DeleteByResponseID(_ context.Context, responseID string) error {
	delete(s.rows, responseID)
	return nil
}

// --- Helpers ---

// buildAnswerRows creates model rows from extracted questions and answers.
func buildAnswerRows(responseID string, questions []SurveyQuestion, answers map[string]any, status string) []models.SurveyAnswer {
	rows := make([]models.SurveyAnswer, 0, len(questions))
	for _, q := range questions {
		row := models.SurveyAnswer{
			ID:             uuid.New().String(),
			ResponseID:     responseID,
			QuestionName:   q.Name,
			QuestionType:   q.Type,
			MapsToTmField:  q.MapsToTmField,
			ResponseStatus: status,
		}
		if q.Title != nil {
			row.QuestionTitle = q.Title
		}

		// Look up answer
		if val, ok := answers[q.Name]; ok {
			jsonVal, err := json.Marshal(val)
			if err == nil {
				row.AnswerValue = jsonVal
			}
		}

		rows = append(rows, row)
	}
	return rows
}

func toAnswerRows(dbRows []models.SurveyAnswer) []SurveyAnswerRow {
	rows := make([]SurveyAnswerRow, len(dbRows))
	for i, r := range dbRows {
		rows[i] = toAnswerRow(r)
	}
	return rows
}

func toAnswerRow(r models.SurveyAnswer) SurveyAnswerRow {
	row := SurveyAnswerRow{
		ID:             r.ID,
		ResponseID:     r.ResponseID,
		QuestionName:   r.QuestionName,
		QuestionType:   r.QuestionType,
		QuestionTitle:  r.QuestionTitle,
		MapsToTmField:  r.MapsToTmField,
		ResponseStatus: r.ResponseStatus,
	}
	if r.AnswerValue != nil {
		row.AnswerValue = json.RawMessage(r.AnswerValue)
	}
	return row
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestSurveyAnswerStore`
Expected: ALL PASS

- [ ] **Step 5: Add global variable and init wiring**

In `api/store.go`, add after `GlobalTriageNoteStore` (line 102):

```go
var GlobalSurveyAnswerStore SurveyAnswerStore
```

In `InitializeGormStores`, add after `GlobalTriageNoteStore = NewGormTriageNoteStore(db)` (line 141):

```go
	GlobalSurveyAnswerStore = NewGormSurveyAnswerStore(db)
```

- [ ] **Step 6: Verify build**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 7: Commit**

```bash
git add api/survey_answer_store.go api/survey_answer_store_test.go api/store.go
git commit -m "feat(api): add SurveyAnswerStore with GORM and in-memory implementations

Interface with ExtractAndSave, GetAnswers, GetFieldMappings, and
DeleteByResponseID. GORM impl uses transactions for atomic
replace. In-memory impl for unit tests. Part of #178."
```

---

## Task 4: Wire ExtractAndSave into Survey Response Handlers

**Note on trigger points:** The spec lists 5 trigger points. There is no separate `SubmitSurveyResponse` handler — submission is a status transition handled by `PatchIntakeSurveyResponse` (draft→submitted). So the 4 handlers below cover all 5 spec trigger points.

**Note on transactions:** The `extractSurveyAnswers` helper is called **after** the response store operation returns (i.e., the response save has already committed). The `GormSurveyAnswerStore.ExtractAndSave` opens its own separate transaction via `s.db.Transaction()` using the store's own `*gorm.DB`, so extraction failure cannot roll back the response save. This satisfies the spec requirement for separate transactions.

**Note on types:** The spec defines the interface returning `[]SurveyAnswer` (GORM model). The plan uses `SurveyAnswerRow` (API-layer type) instead, to decouple the store interface from the GORM model package. This is an intentional refinement.

**Note on logging:** The `slogging.Logger` methods (`Warn`, `Error`, `Debug`, `Info`) use printf-style formatting: `func (l *Logger) Warn(format string, args ...any)`. All logging calls in this plan use this format correctly.

**Files:**
- Modify: `api/survey_handlers.go` (4 handler functions + helper)
- Modify: `api/survey_handlers_test.go` (add mock + tests)

- [ ] **Step 1: Add extraction helper to survey_handlers.go**

Add a helper function near the other helpers (before `validateSurveyJSON`, around line 1338):

```go
// extractSurveyAnswers extracts answers from a survey response into the survey_answers table.
// This is non-fatal: errors are logged but do not fail the response save.
func extractSurveyAnswers(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, status string) {
	if GlobalSurveyAnswerStore == nil {
		return
	}
	logger := slogging.Get()
	if err := GlobalSurveyAnswerStore.ExtractAndSave(ctx, responseID, surveyJSON, answers, status); err != nil {
		logger.Warn("failed to extract survey answers for response %s: %v", responseID, err)
	}
}
```

- [ ] **Step 2: Wire into CreateIntakeSurveyResponse**

After the successful `GlobalSurveyResponseStore.Create` call (line 652, before the webhook emit), add:

```go
	// Extract answers into structured table
	if response.SurveyJson != nil && response.Id != nil {
		answersMap := make(map[string]any)
		if response.Answers != nil {
			_ = json.Unmarshal([]byte(*response.Answers), &answersMap)
		}
		surveyJSONMap := make(map[string]any)
		_ = json.Unmarshal([]byte(*response.SurveyJson), &surveyJSONMap)
		status := ResponseStatusDraft
		if response.Status != nil {
			status = *response.Status
		}
		extractSurveyAnswers(ctx, response.Id.String(), surveyJSONMap, answersMap, status)
	}
```

Note: The exact field access patterns (e.g., `response.Answers`, `response.SurveyJson`) depend on the generated API types. The implementer should check the actual response object returned from the store and adjust field access accordingly. The `SurveyResponse` API type may use pointer fields (`*string`, `*map[string]any`) vs the GORM model's `JSONRaw`. Parse as needed.

- [ ] **Step 3: Wire into UpdateIntakeSurveyResponse**

After the successful `GlobalSurveyResponseStore.Update` call and re-fetch (line 818, after getting `updated`), add the same extraction pattern using `updated` (the freshly fetched response that has the full survey_json snapshot).

- [ ] **Step 4: Wire into PatchIntakeSurveyResponse**

After the successful update and re-fetch (line 973, after getting `updated`), add the same extraction pattern.

- [ ] **Step 5: Wire into PatchTriageSurveyResponse**

After the status update and re-fetch (line 1292, after getting `updated`), add the same extraction pattern. This updates the denormalized `response_status` even though answers may not have changed.

- [ ] **Step 6: Add mock SurveyAnswerStore to test file**

In `api/survey_handlers_test.go`, add a mock and wire it in the test setup:

```go
type mockSurveyAnswerStore struct {
	extractCalls []string // tracks responseIDs passed to ExtractAndSave
	err          error
}

func (m *mockSurveyAnswerStore) ExtractAndSave(_ context.Context, responseID string, _ map[string]any, _ map[string]any, _ string) error {
	m.extractCalls = append(m.extractCalls, responseID)
	return m.err
}

func (m *mockSurveyAnswerStore) GetAnswers(_ context.Context, _ string) ([]SurveyAnswerRow, error) {
	return nil, nil
}

func (m *mockSurveyAnswerStore) GetFieldMappings(_ context.Context, _ string) (map[string]SurveyAnswerRow, error) {
	return nil, nil
}

func (m *mockSurveyAnswerStore) DeleteByResponseID(_ context.Context, _ string) error {
	return nil
}
```

Wire `GlobalSurveyAnswerStore` in the existing test setup function alongside the other store mocks.

- [ ] **Step 7: Add test for extraction on create**

Add a test verifying that `ExtractAndSave` is called when creating a survey response:

```go
func TestCreateIntakeSurveyResponse_ExtractsAnswers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	surveyStore := newMockSurveyStore()
	responseStore := newMockSurveyResponseStore()
	answerStore := &mockSurveyAnswerStore{}

	// Create a survey template with survey_json
	surveyID := uuid.New()
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{"type": "text", "name": "q1"},
				},
			},
		},
	}
	surveyJSONBytes, _ := json.Marshal(surveyJSON)
	surveyJSONRaw := json.RawMessage(surveyJSONBytes)
	surveyStore.surveys[surveyID] = &Survey{
		Id:        &surveyID,
		Name:      "Test Survey",
		Version:   "v1",
		Status:    stringPtr("active"),
		SurveyJson: &surveyJSONRaw,
	}

	saveSurveyStores(t, surveyStore, responseStore)
	origAnswerStore := GlobalSurveyAnswerStore
	GlobalSurveyAnswerStore = answerStore
	t.Cleanup(func() { GlobalSurveyAnswerStore = origAnswerStore })

	body := fmt.Sprintf(`{"survey_id": "%s"}`, surveyID)
	c, w := CreateTestGinContext("POST", "/intake/survey_responses")
	c.Request.Body = io.NopCloser(strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetTestUserContext(c, "test-user-uuid")

	server.CreateIntakeSurveyResponse(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Len(t, answerStore.extractCalls, 1, "ExtractAndSave should be called once")
}
```

Note: The exact test setup (e.g., `SetTestUserContext`, field names on `Survey`) must match the existing test helpers in `survey_handlers_test.go`. The implementer should adapt field names and helpers to match the actual codebase patterns.

- [ ] **Step 8: Run all unit tests**

Run: `make test-unit`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add api/survey_handlers.go api/survey_handlers_test.go
git commit -m "feat(api): wire answer extraction into survey response handlers

Calls ExtractAndSave after every response create, update, patch,
and triage patch. Extraction is non-fatal: errors are logged but
don't fail the response save. Closes #178."
```

---

## Task 5: Add Duplicate mapsToTmField Validation to Survey Templates

**Files:**
- Modify: `api/survey_handlers.go` (`validateSurveyJSON` function)
- Modify: `api/survey_handlers_test.go` (add validation test)

- [ ] **Step 1: Write failing test for duplicate mapping validation**

```go
func TestValidateSurveyJSON_DuplicateFieldMapping(t *testing.T) {
	surveyJSON := map[string]any{
		"pages": []any{
			map[string]any{
				"name": "page1",
				"elements": []any{
					map[string]any{
						"type":          "text",
						"name":          "q1",
						"mapsToTmField": "name",
					},
					map[string]any{
						"type":          "text",
						"name":          "q2",
						"mapsToTmField": "name",
					},
				},
			},
		},
	}

	err := validateSurveyJSON(surveyJSON)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate mapsToTmField")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestValidateSurveyJSON_DuplicateFieldMapping`
Expected: FAIL (validation doesn't check mappings yet)

- [ ] **Step 3: Update validateSurveyJSON**

Modify `validateSurveyJSON` in `api/survey_handlers.go` (line 1339) to also check for duplicate field mappings:

```go
func validateSurveyJSON(surveyJSON map[string]any) error {
	if surveyJSON == nil {
		return fmt.Errorf("survey_json is required")
	}
	pages, ok := surveyJSON["pages"]
	if !ok {
		return fmt.Errorf("survey_json must contain a 'pages' field")
	}
	if _, ok := pages.([]any); !ok {
		return fmt.Errorf("survey_json 'pages' must be an array")
	}

	// Validate no duplicate mapsToTmField annotations
	if _, err := ExtractQuestions(surveyJSON, nil); err != nil {
		return err
	}

	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestValidateSurveyJSON`
Expected: ALL PASS

- [ ] **Step 5: Run full test suite**

Run: `make test-unit`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add api/survey_handlers.go api/survey_handlers_test.go
git commit -m "feat(api): validate duplicate mapsToTmField in survey templates

Rejects survey create/update/patch when multiple questions map to
the same threat model field. Uses ExtractQuestions for validation.
Part of #178."
```

---

## Task 6: Final Verification

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: 0 issues

- [ ] **Step 2: Run full build**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 3: Run full unit test suite**

Run: `make test-unit`
Expected: ALL PASS

- [ ] **Step 4: Push**

```bash
git pull --rebase
git push
git status
```

Expected: `up to date with origin`
