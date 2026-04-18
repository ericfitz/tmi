//go:build dev || test

package api

import (
	"context"
	"fmt"
	"strings"
)

// MockDelegatedSource is a build-tagged (dev/test) delegated content source
// whose URIs have the form "mock://doc/{id}". It stores canned byte payloads
// in its Contents map and is intended to enable end-to-end content-token
// integration tests without coupling them to a real provider (Confluence, etc.).
//
// Usage:
//
//	mock := NewMockDelegatedSource(tokenRepo, registry)
//	mock.Contents["doc1"] = []byte("hello world")
//	data, ct, err := mock.FetchForUser(ctx, userID, "mock://doc/doc1")
type MockDelegatedSource struct {
	*DelegatedSource
	// Contents maps a document ID to its raw bytes.
	// The document ID is the portion of a mock://doc/{id} URI after the prefix.
	Contents map[string][]byte
}

// NewMockDelegatedSource creates a MockDelegatedSource wired to the given token
// repository and OAuth provider registry. Both are passed through to the
// embedded DelegatedSource so that token lookup, lazy refresh, and revocation
// follow the same code paths as production delegated sources.
func NewMockDelegatedSource(tokens ContentTokenRepository, registry *ContentOAuthProviderRegistry) *MockDelegatedSource {
	m := &MockDelegatedSource{
		Contents: make(map[string][]byte),
	}
	m.DelegatedSource = &DelegatedSource{
		ProviderID: "mock",
		Tokens:     tokens,
		Registry:   registry,
		DoFetch:    m.doFetch,
	}
	return m
}

// Name returns the source name ("mock").
func (m *MockDelegatedSource) Name() string { return "mock" }

// CanHandle returns true when uri has the "mock://doc/" prefix.
func (m *MockDelegatedSource) CanHandle(_ context.Context, uri string) bool {
	return strings.HasPrefix(uri, "mock://doc/")
}

// Fetch requires a user ID in ctx (set via WithUserID or by JWT middleware).
// It delegates to FetchForUser using that ID. Returns an error when no user ID
// is present in the context.
func (m *MockDelegatedSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, "", fmt.Errorf("mock delegated source: no user in context")
	}
	return m.FetchForUser(ctx, userID, uri)
}

// doFetch is the DelegatedSourceDoFetch callback. It strips the "mock://doc/"
// prefix and looks up the remaining ID in Contents.
func (m *MockDelegatedSource) doFetch(_ context.Context, _ string, uri string) ([]byte, string, error) {
	id := strings.TrimPrefix(uri, "mock://doc/")
	data, ok := m.Contents[id]
	if !ok {
		return nil, "", fmt.Errorf("mock doc %q not found", id)
	}
	return data, "text/plain", nil
}
