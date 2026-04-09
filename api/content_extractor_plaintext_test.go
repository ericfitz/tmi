package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlainTextExtractor_Name(t *testing.T) {
	e := NewPlainTextExtractor()
	assert.Equal(t, "plaintext", e.Name())
}

func TestPlainTextExtractor_CanHandle(t *testing.T) {
	e := NewPlainTextExtractor()
	assert.True(t, e.CanHandle("text/plain"))
	assert.True(t, e.CanHandle("text/plain; charset=utf-8"))
	assert.True(t, e.CanHandle("text/csv"))
	assert.False(t, e.CanHandle("text/html"))
	assert.False(t, e.CanHandle("application/json"))
}

func TestPlainTextExtractor_Extract(t *testing.T) {
	e := NewPlainTextExtractor()
	result, err := e.Extract([]byte("hello world"), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Text)
	assert.Equal(t, "text/plain", result.ContentType)
}

func TestPlainTextExtractor_Extract_Empty(t *testing.T) {
	e := NewPlainTextExtractor()
	result, err := e.Extract([]byte(""), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "", result.Text)
}

func TestPlainTextExtractor_Extract_CSV(t *testing.T) {
	e := NewPlainTextExtractor()
	csv := "name,age\nalice,30\nbob,25"
	result, err := e.Extract([]byte(csv), "text/csv")
	require.NoError(t, err)
	assert.Equal(t, csv, result.Text)
	assert.Equal(t, "text/csv", result.ContentType)
}
