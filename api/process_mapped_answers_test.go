package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessMappedAnswers_ScalarFields(t *testing.T) {
	field1 := "name"
	field2 := "description"
	field3 := "issue_uri"
	answers := []SurveyAnswerRow{
		{QuestionName: "project_name", MapsToTmField: &field1, AnswerValue: json.RawMessage(`"My Project"`)},
		{QuestionName: "project_desc", MapsToTmField: &field2, AnswerValue: json.RawMessage(`"A description"`)},
		{QuestionName: "tracker_url", MapsToTmField: &field3, AnswerValue: json.RawMessage(`"https://jira.example.com/123"`)},
	}

	result := processMappedAnswers(answers)
	assert.Equal(t, "My Project", *result.name)
	assert.Equal(t, "A description", *result.description)
	assert.Equal(t, "https://jira.example.com/123", *result.issueURI)
}

func TestProcessMappedAnswers_MetadataKey(t *testing.T) {
	field := "metadata.team_name"
	answers := []SurveyAnswerRow{
		{QuestionName: "team", MapsToTmField: &field, AnswerValue: json.RawMessage(`"Platform"`)},
	}

	result := processMappedAnswers(answers)
	require.Len(t, result.metadata, 1)
	assert.Equal(t, "team_name", result.metadata[0].Key)
	assert.Equal(t, "Platform", result.metadata[0].Value)
}

func TestProcessMappedAnswers_UnmappedAnswers(t *testing.T) {
	field := "name"
	answers := []SurveyAnswerRow{
		{QuestionName: "project_name", MapsToTmField: &field, AnswerValue: json.RawMessage(`"My Project"`)},
		{QuestionName: "extra_info", MapsToTmField: nil, AnswerValue: json.RawMessage(`"some notes"`)},
		{QuestionName: "checkboxes", MapsToTmField: nil, AnswerValue: json.RawMessage(`["opt1", "opt2"]`)},
	}

	result := processMappedAnswers(answers)
	assert.Len(t, result.metadata, 2)
	assert.Equal(t, "extra_info", result.metadata[0].Key)
	assert.Equal(t, "some notes", result.metadata[0].Value)
	assert.Equal(t, "checkboxes", result.metadata[1].Key)
	assert.Equal(t, "opt1, opt2", result.metadata[1].Value)
}

func TestProcessMappedAnswers_UnrecognizedField(t *testing.T) {
	field := "unknown_field"
	answers := []SurveyAnswerRow{
		{QuestionName: "weird", MapsToTmField: &field, AnswerValue: json.RawMessage(`"value"`)},
	}

	result := processMappedAnswers(answers)
	require.Len(t, result.metadata, 1)
	assert.Equal(t, "unknown_field", result.metadata[0].Key)
	assert.Equal(t, "value", result.metadata[0].Value)
}

func TestProcessMappedAnswers_Collections(t *testing.T) {
	field := "repositories"
	answers := []SurveyAnswerRow{
		{
			QuestionName:  "repos",
			MapsToTmField: &field,
			AnswerValue: json.RawMessage(`[
				{"name": "frontend", "uri": "https://github.com/org/frontend"}
			]`),
		},
	}

	result := processMappedAnswers(answers)
	assert.Len(t, result.repositories, 1)
}
