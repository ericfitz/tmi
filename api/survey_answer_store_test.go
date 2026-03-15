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

	var val string
	require.NoError(t, json.Unmarshal(mapped.AnswerValue, &val))
	assert.Equal(t, "My Project", val)

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

	err := store.ExtractAndSave(ctx, "resp-1", surveyJSON, map[string]any{"q1": "old"}, "draft")
	require.NoError(t, err)

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

	var arr []map[string]any
	require.NoError(t, json.Unmarshal(results[0].AnswerValue, &arr))
	assert.Len(t, arr, 2)
	assert.Equal(t, "SQL Injection", arr[0]["risk_name"])
}
