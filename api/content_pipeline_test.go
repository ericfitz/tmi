package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLPatternMatcher_Identify(t *testing.T) {
	m := NewURLPatternMatcher()

	tests := []struct {
		uri      string
		expected string
	}{
		{"https://docs.google.com/document/d/abc/edit", "google_drive"},
		{"https://drive.google.com/file/d/abc/view", "google_drive"},
		{"https://docs.google.com/spreadsheets/d/abc/edit", "google_drive"},
		{"https://docs.google.com/presentation/d/abc/edit", "google_drive"},
		{"https://mycompany.atlassian.net/wiki/spaces/ENG/pages/123", "confluence"},
		{"https://mycompany.sharepoint.com/sites/team/doc.docx", "onedrive"},
		{"https://onedrive.live.com/edit.aspx?id=abc", "onedrive"},
		{"https://example.com/readme.html", "http"},
		{"https://example.com/doc.pdf", "http"},
		{"", ""},
		{"ftp://example.com/file", ""},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.expected, m.Identify(tt.uri))
		})
	}
}

func TestURLPatternMatcher_IsKnownProvider(t *testing.T) {
	m := NewURLPatternMatcher()
	assert.True(t, m.IsKnownProvider("google_drive"))
	assert.True(t, m.IsKnownProvider("confluence"))
	assert.True(t, m.IsKnownProvider("onedrive"))
	assert.True(t, m.IsKnownProvider("http"))
	assert.False(t, m.IsKnownProvider("dropbox"))
}

func TestContentPipeline_Extract(t *testing.T) {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("<h1>Hello</h1>"),
		ct:        "text/html",
	})

	extractors := NewContentExtractorRegistry()
	extractors.Register(&mockExtractor{
		name:      "test-ext",
		canHandle: true,
		result:    ExtractedContent{Text: "Hello", ContentType: "text/html"},
	})

	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())
	result, err := pipeline.Extract(context.Background(), "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Text)
}

func TestContentPipeline_Extract_NoSource(t *testing.T) {
	sources := NewContentSourceRegistry()
	extractors := NewContentExtractorRegistry()
	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())

	_, err := pipeline.Extract(context.Background(), "https://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no content source")
}

func TestContentPipeline_Extract_FallbackPlainText(t *testing.T) {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("raw data"),
		ct:        "application/octet-stream",
	})

	extractors := NewContentExtractorRegistry()
	// No extractor registered for application/octet-stream

	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())
	result, err := pipeline.Extract(context.Background(), "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "raw data", result.Text)
}
