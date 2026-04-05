package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectTextProvider_CanHandle(t *testing.T) {
	p := NewDirectTextProvider()

	// DB-resident entities without URIs
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "asset", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "threat", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "repository", EntityID: "123"}))

	// Entities with URIs should not be handled by DirectTextProvider
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", EntityID: "123", URI: "https://example.com/doc.pdf"}))

	// Diagrams are handled by the JSON provider, not direct text
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "diagram", EntityID: "123"}))
}

// --- JSONContentProvider tests ---

func TestJSONContentProvider_CanHandle(t *testing.T) {
	p := NewJSONContentProvider()

	// Diagrams without URI are handled
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "diagram", EntityID: "123"}))

	// Non-diagram entities are not handled
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))

	// Diagrams with a URI are not handled (would be fetched by HTTP provider)
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "diagram", EntityID: "123", URI: "https://example.com"}))
}

func TestJSONContentProvider_Name(t *testing.T) {
	p := NewJSONContentProvider()
	assert.Equal(t, "json-dfd", p.Name())
}

func TestJSONContentProvider_Extract_NilStore(t *testing.T) {
	// Save and restore the global store
	original := DiagramStore
	DiagramStore = nil
	defer func() { DiagramStore = original }()

	p := NewJSONContentProvider()
	_, err := p.Extract(context.Background(), EntityReference{EntityType: "diagram", EntityID: "some-id"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store is not initialized")
}

func TestJSONContentProvider_Extract(t *testing.T) {
	// Set up an in-memory diagram with labelled nodes and an edge
	InitTestFixtures()

	p := NewJSONContentProvider()
	result, err := p.Extract(context.Background(), EntityReference{
		EntityType: "diagram",
		EntityID:   TestFixtures.DiagramID,
	})
	require.NoError(t, err)

	// The test fixtures contain two nodes and one edge
	assert.NotEmpty(t, result.Text)
	assert.Equal(t, "Test Diagram", result.Title)
	assert.Equal(t, "application/json", result.ContentType)

	// Should contain a Flow line for the edge
	assert.Contains(t, result.Text, "Flow:")
}

// --- HTTPContentProvider tests ---

func TestHTTPContentProvider_CanHandle(t *testing.T) {
	p := NewHTTPContentProvider(NewSSRFValidator(nil))

	// HTTP and HTTPS URIs are handled
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "https://example.com/doc"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "http://example.com/doc"}))

	// No URI: not handled
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))

	// Non-HTTP scheme: not handled
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "ftp://example.com/doc"}))
}

func TestHTTPContentProvider_Name(t *testing.T) {
	p := NewHTTPContentProvider(NewSSRFValidator(nil))
	assert.Equal(t, "http-html", p.Name())
}

func TestHTTPContentProvider_Extract_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	// Allow the test server's loopback address via the allowlist
	p := NewHTTPContentProvider(NewSSRFValidator([]string{"127.0.0.1"}))
	result, err := p.Extract(context.Background(), EntityReference{
		EntityType: "document",
		URI:        srv.URL,
		Name:       "test doc",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Text)
	assert.Equal(t, "test doc", result.Title)
}

func TestHTTPContentProvider_Extract_HTML(t *testing.T) {
	htmlBody := `<html><head><title>Test</title><style>body{}</style></head><body><h1>Hello</h1><p>World</p><script>alert('x')</script></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	p := NewHTTPContentProvider(NewSSRFValidator([]string{"127.0.0.1"}))
	result, err := p.Extract(context.Background(), EntityReference{
		EntityType: "document",
		URI:        srv.URL,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Text, "Hello")
	assert.Contains(t, result.Text, "World")
	assert.NotContains(t, result.Text, "alert")
	assert.NotContains(t, result.Text, "body{}")
}

func TestHTTPContentProvider_Extract_SSRFBlocked(t *testing.T) {
	p := NewHTTPContentProvider(NewSSRFValidator(nil))
	_, err := p.Extract(context.Background(), EntityReference{
		EntityType: "document",
		URI:        "http://localhost/secret",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF check failed")
}

// --- extractTextFromHTML unit tests ---

func TestExtractTextFromHTML(t *testing.T) {
	htmlContent := `<html><head><title>Test</title><style>body{}</style></head><body><h1>Hello</h1><p>World</p><script>alert('x')</script></body></html>`
	text := extractTextFromHTML(htmlContent)
	assert.Contains(t, text, "Hello")
	assert.Contains(t, text, "World")
	assert.NotContains(t, text, "alert")
	assert.NotContains(t, text, "body{}")
}

func TestExtractTextFromHTML_Empty(t *testing.T) {
	text := extractTextFromHTML("")
	assert.Equal(t, "", text)
}

func TestExtractTextFromHTML_PlainText(t *testing.T) {
	text := extractTextFromHTML("just some text")
	assert.Contains(t, text, "just some text")
}

// --- PDFContentProvider tests ---

func TestPDFContentProvider_CanHandle(t *testing.T) {
	p := NewPDFContentProvider(NewSSRFValidator(nil))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "https://example.com/doc.pdf"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "https://example.com/DOC.PDF"}))
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "https://example.com/doc.html"}))
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))
}

func TestPDFContentProvider_Name(t *testing.T) {
	p := NewPDFContentProvider(NewSSRFValidator(nil))
	assert.Equal(t, "pdf", p.Name())
}

func TestPDFContentProvider_Extract_SSRFBlocked(t *testing.T) {
	p := NewPDFContentProvider(NewSSRFValidator(nil))
	_, err := p.Extract(context.Background(), EntityReference{
		EntityType: "document",
		URI:        "http://localhost/secret.pdf",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF check failed")
}
