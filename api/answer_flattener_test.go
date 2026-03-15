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
