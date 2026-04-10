package api

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLLMForDecomposer satisfies DecomposerLLM for unit tests.
type mockLLMForDecomposer struct {
	response string
	err      error
}

func (m *mockLLMForDecomposer) GenerateResponse(_ context.Context, _ string, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// TestLLMQueryDecomposer_ValidDecomposition verifies that a well-formed JSON response
// is correctly parsed into a DecomposedQuery.
func TestLLMQueryDecomposer_ValidDecomposition(t *testing.T) {
	mock := &mockLLMForDecomposer{
		response: `{"text_query": "find threats", "code_query": "scan code for vulns", "strategy": "parallel"}`,
	}
	d := NewLLMQueryDecomposer(mock)

	result, err := d.Decompose(context.Background(), "what are the threats?", true)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "find threats", result.TextQuery)
	assert.Equal(t, "scan code for vulns", result.CodeQuery)
	assert.Equal(t, "parallel", result.Strategy)
}

// TestLLMQueryDecomposer_NoCodeIndex verifies that CodeQuery is cleared when
// hasCodeIndex is false, even if the LLM would otherwise return one.
func TestLLMQueryDecomposer_NoCodeIndex(t *testing.T) {
	mock := &mockLLMForDecomposer{
		response: `{"text_query": "find threats", "code_query": "scan code", "strategy": "parallel"}`,
	}
	d := NewLLMQueryDecomposer(mock)

	result, err := d.Decompose(context.Background(), "what are the threats?", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "find threats", result.TextQuery)
	assert.Empty(t, result.CodeQuery, "CodeQuery must be empty when hasCodeIndex is false")
}

// TestLLMQueryDecomposer_LLMError_FallsBack verifies that an LLM error results in
// the original query being used as fallback, and no error is returned to the caller.
func TestLLMQueryDecomposer_LLMError_FallsBack(t *testing.T) {
	mock := &mockLLMForDecomposer{
		err: errors.New("LLM unavailable"),
	}
	d := NewLLMQueryDecomposer(mock)

	const original = "list all assets"
	result, err := d.Decompose(context.Background(), original, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, original, result.TextQuery)
	assert.Equal(t, original, result.CodeQuery)
}

// TestLLMQueryDecomposer_UnparseableJSON_FallsBack verifies that garbage LLM output
// triggers the fallback without returning an error.
func TestLLMQueryDecomposer_UnparseableJSON_FallsBack(t *testing.T) {
	mock := &mockLLMForDecomposer{
		response: "I don't know how to answer that in JSON format!",
	}
	d := NewLLMQueryDecomposer(mock)

	const original = "find SQL injection risks"
	result, err := d.Decompose(context.Background(), original, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, original, result.TextQuery)
	assert.Equal(t, original, result.CodeQuery)
}

// TestLLMQueryDecomposer_EmptyTextQuery_FallsBackToOriginal verifies that when the
// LLM returns an empty text_query the original query is substituted.
func TestLLMQueryDecomposer_EmptyTextQuery_FallsBackToOriginal(t *testing.T) {
	mock := &mockLLMForDecomposer{
		response: `{"text_query": "", "code_query": "scan for injection", "strategy": "parallel"}`,
	}
	d := NewLLMQueryDecomposer(mock)

	const original = "check for injection attacks"
	result, err := d.Decompose(context.Background(), original, true)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, original, result.TextQuery, "empty text_query should be replaced with original query")
	assert.Equal(t, "scan for injection", result.CodeQuery)
}
