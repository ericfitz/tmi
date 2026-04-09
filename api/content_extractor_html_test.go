package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLExtractor_Name(t *testing.T) {
	e := NewHTMLExtractor()
	assert.Equal(t, "html", e.Name())
}

func TestHTMLExtractor_CanHandle(t *testing.T) {
	e := NewHTMLExtractor()
	assert.True(t, e.CanHandle("text/html"))
	assert.True(t, e.CanHandle("text/html; charset=utf-8"))
	assert.False(t, e.CanHandle("text/plain"))
	assert.False(t, e.CanHandle("application/json"))
}

func TestHTMLExtractor_Extract(t *testing.T) {
	e := NewHTMLExtractor()
	html := []byte(`<html><body><h1>Hello</h1><script>evil()</script><p>World</p></body></html>`)
	result, err := e.Extract(html, "text/html")
	require.NoError(t, err)
	assert.Contains(t, result.Text, "Hello")
	assert.Contains(t, result.Text, "World")
	assert.NotContains(t, result.Text, "evil")
	assert.Equal(t, "text/html", result.ContentType)
}

func TestHTMLExtractor_Extract_Empty(t *testing.T) {
	e := NewHTMLExtractor()
	result, err := e.Extract([]byte(""), "text/html")
	require.NoError(t, err)
	assert.Equal(t, "", result.Text)
}

func TestHTMLExtractor_Extract_StyleStripped(t *testing.T) {
	e := NewHTMLExtractor()
	htmlContent := []byte(`<html><head><style>body { color: red; }</style></head><body><p>Visible text</p></body></html>`)
	result, err := e.Extract(htmlContent, "text/html")
	require.NoError(t, err)
	assert.Contains(t, result.Text, "Visible text")
	assert.NotContains(t, result.Text, "color")
}
