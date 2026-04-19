package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubSource implements ContentSource for dispatch tests.
type stubSource struct {
	name      string
	canHandle bool
}

func (s *stubSource) Name() string                                              { return s.name }
func (s *stubSource) CanHandle(_ context.Context, _ string) bool                { return s.canHandle }
func (s *stubSource) Fetch(_ context.Context, _ string) ([]byte, string, error) { return nil, "", nil }

// stubLinkedChecker is a minimal LinkedProviderChecker.
type stubLinkedChecker struct {
	linked map[string]bool
}

func (c *stubLinkedChecker) HasActiveToken(_ context.Context, _, providerID string) bool {
	return c.linked[providerID]
}

func TestFindSourceForDocument_PickerRegistered_WithLinkedToken(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: ProviderGoogleWorkspace, canHandle: true})
	reg.Register(&stubSource{name: ProviderGoogleDrive, canHandle: true})

	provID := ProviderGoogleWorkspace
	pm := &PickerMetadata{ProviderID: &provID}
	checker := &stubLinkedChecker{linked: map[string]bool{ProviderGoogleWorkspace: true}}

	src, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://docs.google.com/document/d/abc/edit",
		pm,
		"alice",
		checker,
	)
	assert.True(t, ok)
	assert.Equal(t, ProviderGoogleWorkspace, src.Name())
}

func TestFindSourceForDocument_PickerRegistered_NoLinkedToken_FallsThrough(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: ProviderGoogleWorkspace, canHandle: true})
	reg.Register(&stubSource{name: ProviderGoogleDrive, canHandle: true})

	provID := ProviderGoogleWorkspace
	pm := &PickerMetadata{ProviderID: &provID}
	checker := &stubLinkedChecker{linked: map[string]bool{}} // no linked tokens

	src, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://docs.google.com/document/d/abc/edit",
		pm,
		"alice",
		checker,
	)
	assert.True(t, ok)
	assert.Equal(t, ProviderGoogleWorkspace, src.Name()) // falls through to first CanHandle match (google_workspace registered first)
}

func TestFindSourceForDocument_NonPicker_URLDispatch(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: ProviderGoogleDrive, canHandle: true})

	src, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://docs.google.com/document/d/abc/edit",
		nil, // no picker metadata
		"alice",
		nil,
	)
	assert.True(t, ok)
	assert.Equal(t, ProviderGoogleDrive, src.Name())
}

func TestFindSourceForDocument_PickerRegistered_ProviderNotRegistered_FallsThrough(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: ProviderGoogleDrive, canHandle: true})
	// DelegatedGoogleWorkspaceSource NOT registered.

	provID := ProviderGoogleWorkspace
	pm := &PickerMetadata{ProviderID: &provID}
	checker := &stubLinkedChecker{linked: map[string]bool{ProviderGoogleWorkspace: true}}

	src, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://docs.google.com/document/d/abc/edit",
		pm,
		"alice",
		checker,
	)
	assert.True(t, ok)
	assert.Equal(t, ProviderGoogleDrive, src.Name())
}

func TestFindSourceForDocument_NilChecker_FallsThrough(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: ProviderGoogleWorkspace, canHandle: true})
	reg.Register(&stubSource{name: ProviderGoogleDrive, canHandle: true})

	provID := ProviderGoogleWorkspace
	pm := &PickerMetadata{ProviderID: &provID}

	src, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://docs.google.com/document/d/abc/edit",
		pm,
		"alice",
		nil, // nil checker
	)
	assert.True(t, ok)
	// With nil checker, picker path is bypassed; falls through to URL-based dispatch which
	// returns the first CanHandle match (google_workspace registered first).
	assert.Equal(t, ProviderGoogleWorkspace, src.Name())
}

func TestFindSourceForDocument_NoMatch(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&stubSource{name: "unrelated", canHandle: false})

	_, ok := reg.FindSourceForDocument(
		context.Background(),
		"https://example.com/doc",
		nil,
		"alice",
		nil,
	)
	assert.False(t, ok)
}
