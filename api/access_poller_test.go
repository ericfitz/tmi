package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestAccessPoller_Creation(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Minute, 7*24*time.Hour)
	assert.NotNil(t, poller)
	assert.Equal(t, time.Minute, poller.interval)
	assert.Equal(t, 7*24*time.Hour, poller.maxAge)
}

func TestAccessPoller_StopSignal(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
	poller.Start()
	// Should not panic on stop
	poller.Stop()
}

func TestAccessPoller_PollOnce_NilStore(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Minute, 7*24*time.Hour)
	// pollOnce with nil store should not panic
	poller.pollOnce()
}

// mockDocumentStoreForPoller is a minimal mock for AccessPoller tests.
type mockDocumentStoreForPoller struct {
	documents     []Document
	listErr       error
	updatedID     string
	updatedStatus string
	updateCalled  bool
}

func (m *mockDocumentStoreForPoller) ListByAccessStatus(_ context.Context, _ string, _ int) ([]Document, error) {
	return m.documents, m.listErr
}

func (m *mockDocumentStoreForPoller) UpdateAccessStatus(_ context.Context, id string, status string, _ string) error {
	m.updateCalled = true
	m.updatedID = id
	m.updatedStatus = status
	return nil
}

func (m *mockDocumentStoreForPoller) UpdateAccessStatusWithDiagnostics(
	_ context.Context, _ string, _ string, _ string, _ string, _ string,
) error {
	return nil
}

func (m *mockDocumentStoreForPoller) GetAccessReason(
	_ context.Context, _ string,
) (string, string, *time.Time, error) {
	return "", "", nil, nil
}

// Stub out all other DocumentStore methods (required by interface).
func (m *mockDocumentStoreForPoller) Create(_ context.Context, _ *Document, _ string) error {
	return nil
}
func (m *mockDocumentStoreForPoller) Get(_ context.Context, _ string) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) Update(_ context.Context, _ *Document, _ string) error {
	return nil
}
func (m *mockDocumentStoreForPoller) Delete(_ context.Context, _ string) error     { return nil }
func (m *mockDocumentStoreForPoller) SoftDelete(_ context.Context, _ string) error { return nil }
func (m *mockDocumentStoreForPoller) Restore(_ context.Context, _ string) error    { return nil }
func (m *mockDocumentStoreForPoller) HardDelete(_ context.Context, _ string) error { return nil }
func (m *mockDocumentStoreForPoller) GetIncludingDeleted(_ context.Context, _ string) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) Patch(_ context.Context, _ string, _ []PatchOperation) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) List(_ context.Context, _ string, _, _ int) ([]Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForPoller) Count(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockDocumentStoreForPoller) BulkCreate(_ context.Context, _ []Document, _ string) error {
	return nil
}
func (m *mockDocumentStoreForPoller) InvalidateCache(_ context.Context, _ string) error { return nil }
func (m *mockDocumentStoreForPoller) WarmCache(_ context.Context, _ string) error       { return nil }

// mockAccessSource implements ContentSource and AccessValidator for testing.
type mockAccessSource struct {
	name       string
	canHandle  bool
	accessible bool
	valErr     error
}

func (m *mockAccessSource) Name() string                               { return m.name }
func (m *mockAccessSource) CanHandle(_ context.Context, _ string) bool { return m.canHandle }
func (m *mockAccessSource) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (m *mockAccessSource) ValidateAccess(_ context.Context, _ string) (bool, error) {
	return m.accessible, m.valErr
}

func TestAccessPoller_PollOnce_UpdatesAccessible(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &now,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.True(t, store.updateCalled, "UpdateAccessStatus should have been called")
	assert.Equal(t, docID.String(), store.updatedID)
	assert.Equal(t, AccessStatusAccessible, store.updatedStatus)
}

func TestAccessPoller_PollOnce_StillInaccessible(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &now,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: false}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called when still inaccessible")
}

func TestAccessPoller_PollOnce_SkipsExpired(t *testing.T) {
	docID := uuid.New()
	oldTime := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://docs.google.com/document/d/abc123/edit",
				CreatedAt: &oldTime,
			},
		},
	}

	src := &mockAccessSource{name: "google_drive", canHandle: true, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	// maxAge is 7 days -- document is 30 days old, should be skipped
	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called for expired documents")
}

func TestAccessPoller_PollOnce_NoMatchingSource(t *testing.T) {
	docID := uuid.New()
	now := time.Now()
	store := &mockDocumentStoreForPoller{
		documents: []Document{
			{
				Id:        &docID,
				Uri:       "https://confluence.example.com/wiki/page",
				CreatedAt: &now,
			},
		},
	}

	// Register only a Google Drive source -- it won't handle confluence URLs
	src := &mockAccessSource{name: "google_drive", canHandle: false, accessible: true}
	sources := NewContentSourceRegistry()
	sources.Register(src)

	poller := NewAccessPoller(sources, store, time.Minute, 7*24*time.Hour)
	// Should not panic
	poller.pollOnce()

	assert.False(t, store.updateCalled, "UpdateAccessStatus should NOT be called when no source matches")
}
