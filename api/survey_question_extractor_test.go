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
						"type":          "text",
						"name":          "project_name",
						"title":         "Project Name",
						"mapsToTmField": "name",
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
