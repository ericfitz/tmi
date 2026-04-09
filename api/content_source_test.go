package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSource is a test double for ContentSource
type mockSource struct {
	name      string
	canHandle bool
	data      []byte
	ct        string
	err       error
}

func (m *mockSource) Name() string                               { return m.name }
func (m *mockSource) CanHandle(_ context.Context, _ string) bool { return m.canHandle }
func (m *mockSource) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return m.data, m.ct, m.err
}

func TestContentSourceRegistry_FindSource(t *testing.T) {
	r := NewContentSourceRegistry()
	s1 := &mockSource{name: "nope", canHandle: false}
	s2 := &mockSource{name: "yep", canHandle: true}
	r.Register(s1)
	r.Register(s2)

	src, ok := r.FindSource(context.Background(), "https://example.com")
	require.True(t, ok)
	assert.Equal(t, "yep", src.Name())
}

func TestContentSourceRegistry_FindSource_NoMatch(t *testing.T) {
	r := NewContentSourceRegistry()
	r.Register(&mockSource{name: "nope", canHandle: false})

	_, ok := r.FindSource(context.Background(), "https://example.com")
	assert.False(t, ok)
}
