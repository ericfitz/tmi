package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExtractor struct {
	name      string
	canHandle bool
	result    ExtractedContent
	err       error
}

func (m *mockExtractor) Name() string                      { return m.name }
func (m *mockExtractor) CanHandle(contentType string) bool { return m.canHandle }
func (m *mockExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	return m.result, m.err
}

func TestContentExtractorRegistry_FindExtractor(t *testing.T) {
	r := NewContentExtractorRegistry()
	e1 := &mockExtractor{name: "nope", canHandle: false}
	e2 := &mockExtractor{name: "yep", canHandle: true}
	r.Register(e1)
	r.Register(e2)

	ext, ok := r.FindExtractor("text/html")
	require.True(t, ok)
	assert.Equal(t, "yep", ext.Name())
}

func TestContentExtractorRegistry_FindExtractor_NoMatch(t *testing.T) {
	r := NewContentExtractorRegistry()
	r.Register(&mockExtractor{name: "nope", canHandle: false})

	_, ok := r.FindExtractor("application/octet-stream")
	assert.False(t, ok)
}
