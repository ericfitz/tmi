package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockDocumentStoreForCascade is a testify mock that records calls to
// ClearPickerMetadataForOwner. It stubs all other interface methods via
// embedding MockDocumentStore.
type mockDocumentStoreForCascade struct {
	mock.Mock
}

// Delegate every method except ClearPickerMetadataForOwner to no-op stubs so
// the mock satisfies the full DocumentStore interface without effort.

func (m *mockDocumentStoreForCascade) Create(ctx context.Context, doc *Document, tmID string) error {
	return nil
}
func (m *mockDocumentStoreForCascade) Get(ctx context.Context, id string) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForCascade) Update(ctx context.Context, doc *Document, tmID string) error {
	return nil
}
func (m *mockDocumentStoreForCascade) Delete(ctx context.Context, id string) error     { return nil }
func (m *mockDocumentStoreForCascade) SoftDelete(ctx context.Context, id string) error { return nil }
func (m *mockDocumentStoreForCascade) Restore(ctx context.Context, id string) error    { return nil }
func (m *mockDocumentStoreForCascade) HardDelete(ctx context.Context, id string) error { return nil }
func (m *mockDocumentStoreForCascade) GetIncludingDeleted(ctx context.Context, id string) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForCascade) Patch(ctx context.Context, id string, ops []PatchOperation) (*Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForCascade) List(ctx context.Context, tmID string, offset, limit int) ([]Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForCascade) ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error) {
	return nil, nil
}
func (m *mockDocumentStoreForCascade) Count(ctx context.Context, tmID string) (int, error) {
	return 0, nil
}
func (m *mockDocumentStoreForCascade) BulkCreate(ctx context.Context, docs []Document, tmID string) error {
	return nil
}
func (m *mockDocumentStoreForCascade) UpdateAccessStatus(ctx context.Context, id, status, src string) error {
	return nil
}
func (m *mockDocumentStoreForCascade) UpdateAccessStatusWithDiagnostics(
	ctx context.Context, id, status, src, code, detail string,
) error {
	return nil
}
func (m *mockDocumentStoreForCascade) GetAccessReason(ctx context.Context, id string) (string, string, *time.Time, error) {
	return "", "", nil, nil
}
func (m *mockDocumentStoreForCascade) InvalidateCache(ctx context.Context, id string) error {
	return nil
}
func (m *mockDocumentStoreForCascade) WarmCache(ctx context.Context, tmID string) error { return nil }

func (m *mockDocumentStoreForCascade) SetPickerMetadata(
	ctx context.Context, id string, providerID, fileID, mimeType string,
) error {
	return nil
}

func (m *mockDocumentStoreForCascade) ClearPickerMetadataForOwner(
	ctx context.Context, ownerInternalUUID, providerID string,
) (int64, error) {
	args := m.Called(ctx, ownerInternalUUID, providerID)
	return int64(args.Int(0)), args.Error(1)
}

func (m *mockDocumentStoreForCascade) GetPickerDispatch(
	_ context.Context, _ string,
) (*PickerMetadata, string, error) {
	return nil, "", nil
}

// =============================================================================
// Tests
// =============================================================================

// TestContentOAuthHandlers_Delete_CallsPickerUnlinkCascade asserts that the
// Delete handler calls DocumentStore.ClearPickerMetadataForOwner with the
// correct (userID, providerID) before deleting the token.
func TestContentOAuthHandlers_Delete_CallsPickerUnlinkCascade(t *testing.T) {
	const userID = "cascade-user-uuid"
	const providerID = "google_workspace"

	docStore := &mockDocumentStoreForCascade{}
	docStore.On("ClearPickerMetadataForOwner", mock.Anything, userID, providerID).
		Return(3, nil).Once()

	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, uid, pid string) (*ContentToken, error) {
			return &ContentToken{
				UserID:      uid,
				ProviderID:  pid,
				AccessToken: "at-to-revoke",
				Status:      ContentTokenStatusActive,
			}, nil
		},
	}

	h, _ := newTestHandlers(t, repo, nil)
	h.Documents = docStore // inject cascade store

	r := ginTestRouter()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", userID); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/"+providerID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
	docStore.AssertExpectations(t)
}

// TestContentOAuthHandlers_Delete_CascadeSkippedWhenDocumentsNil confirms that a
// nil Documents field does not cause a panic and the handler still returns 204.
func TestContentOAuthHandlers_Delete_CascadeSkippedWhenDocumentsNil(t *testing.T) {
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, uid, pid string) (*ContentToken, error) {
			return &ContentToken{UserID: uid, ProviderID: pid, AccessToken: "at", Status: ContentTokenStatusActive}, nil
		},
	}

	h, _ := newTestHandlers(t, repo, nil)
	// h.Documents is nil by default — no cascade store set

	r := ginTestRouter()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", "some-user"); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/google_workspace", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestContentOAuthHandlers_Delete_CascadeFailureDoesNotBlock asserts that a
// cascade error does not prevent the token from being deleted (best-effort).
func TestContentOAuthHandlers_Delete_CascadeFailureDoesNotBlock(t *testing.T) {
	const userID = "cascade-user-uuid"
	const providerID = "google_workspace"

	docStore := &mockDocumentStoreForCascade{}
	docStore.On("ClearPickerMetadataForOwner", mock.Anything, userID, providerID).
		Return(0, assert.AnError).Once()

	var tokenDeleted bool
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, uid, pid string) (*ContentToken, error) {
			tokenDeleted = true
			return &ContentToken{UserID: uid, ProviderID: pid, AccessToken: "at", Status: ContentTokenStatusActive}, nil
		},
	}

	h, _ := newTestHandlers(t, repo, nil)
	h.Documents = docStore

	r := ginTestRouter()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", userID); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/"+providerID, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, tokenDeleted, "token should still be deleted even when cascade fails")
	docStore.AssertExpectations(t)
}
