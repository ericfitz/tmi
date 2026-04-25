package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestGormDocumentStore creates an in-memory SQLite-backed GormDocumentStore for unit tests.
func newTestGormDocumentStore(t *testing.T) *GormDocumentStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ThreatModel{}, &models.Document{}))
	return &GormDocumentStore{db: db}
}

func TestGormDocumentStore_UpdateAccessStatusWithDiagnostics(t *testing.T) {
	store := newTestGormDocumentStore(t)

	// Arrange: insert a minimal ThreatModel and Document row directly via GORM.
	tmID := uuid.New().String()
	userID := uuid.New().String()
	tm := models.ThreatModel{
		ID:                    tmID,
		OwnerInternalUUID:     userID,
		CreatedByInternalUUID: userID,
		Name:                  "test-tm",
	}
	require.NoError(t, store.db.Create(&tm).Error)

	docID := uuid.New().String()
	doc := models.Document{
		ID:            docID,
		ThreatModelID: tmID,
		Name:          "test-doc",
		URI:           "https://docs.google.com/document/d/abc123/edit",
	}
	require.NoError(t, store.db.Create(&doc).Error)

	ctx := context.Background()

	// Act: populate diagnostic fields.
	err := store.UpdateAccessStatusWithDiagnostics(
		ctx,
		docID,
		AccessStatusPendingAccess,
		"google_workspace",
		"no_accessible_source",
		"",
	)
	require.NoError(t, err)

	// Assert first call: diagnostic fields populated.
	var raw models.Document
	require.NoError(t, store.db.First(&raw, "id = ?", docID).Error)
	require.NotNil(t, raw.AccessStatus)
	assert.Equal(t, AccessStatusPendingAccess, *raw.AccessStatus)
	require.NotNil(t, raw.ContentSource)
	assert.Equal(t, "google_workspace", *raw.ContentSource)
	require.NotNil(t, raw.AccessReasonCode)
	assert.Equal(t, "no_accessible_source", *raw.AccessReasonCode)
	assert.Nil(t, raw.AccessReasonDetail, "detail should be nil when empty string provided")
	require.NotNil(t, raw.AccessStatusUpdatedAt)

	// Act: second call with empty reason_code must clear diagnostic fields.
	err = store.UpdateAccessStatusWithDiagnostics(ctx, docID, AccessStatusAccessible, "", "", "")
	require.NoError(t, err)

	require.NoError(t, store.db.First(&raw, "id = ?", docID).Error)
	require.NotNil(t, raw.AccessStatus)
	assert.Equal(t, AccessStatusAccessible, *raw.AccessStatus)
	assert.Nil(t, raw.AccessReasonCode, "reason_code should be cleared when empty reasonCode provided")
	assert.Nil(t, raw.AccessReasonDetail, "reason_detail should be cleared when empty reasonCode provided")
	require.NotNil(t, raw.AccessStatusUpdatedAt, "access_status_updated_at should still be set after second call")
}

func TestGormDocumentStore_ClearPickerMetadataForOwner(t *testing.T) {
	store := newTestGormDocumentStore(t)

	// Arrange: two users, two threat models, four documents.
	userA := uuid.New().String()
	userB := uuid.New().String()

	tmA := models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     userA,
		CreatedByInternalUUID: userA,
		Name:                  "tm-user-a",
	}
	tmB := models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     userB,
		CreatedByInternalUUID: userB,
		Name:                  "tm-user-b",
	}
	require.NoError(t, store.db.Create(&tmA).Error)
	require.NoError(t, store.db.Create(&tmB).Error)

	providerGW := "google_workspace"
	providerConfluence := "confluence"

	// doc1: user A, google_workspace — should be cleared
	doc1ID := uuid.New().String()
	doc1 := models.Document{
		ID:               doc1ID,
		ThreatModelID:    tmA.ID,
		Name:             "doc1",
		URI:              "https://docs.google.com/d/1",
		PickerProviderID: &providerGW,
		PickerFileID:     strPtr("file-1"),
		PickerMimeType:   strPtr("application/vnd.google-apps.document"),
	}
	require.NoError(t, store.db.Create(&doc1).Error)

	// doc2: user A, confluence — should NOT be cleared (different provider)
	doc2ID := uuid.New().String()
	doc2 := models.Document{
		ID:               doc2ID,
		ThreatModelID:    tmA.ID,
		Name:             "doc2",
		URI:              "https://confluence.example.com/d/2",
		PickerProviderID: &providerConfluence,
		PickerFileID:     strPtr("file-2"),
	}
	require.NoError(t, store.db.Create(&doc2).Error)

	// doc3: user A, no picker — should NOT be touched
	doc3ID := uuid.New().String()
	doc3 := models.Document{
		ID:            doc3ID,
		ThreatModelID: tmA.ID,
		Name:          "doc3",
		URI:           "https://example.com/d/3",
	}
	require.NoError(t, store.db.Create(&doc3).Error)

	// doc4: user B, google_workspace — should NOT be cleared (different owner)
	doc4ID := uuid.New().String()
	doc4 := models.Document{
		ID:               doc4ID,
		ThreatModelID:    tmB.ID,
		Name:             "doc4",
		URI:              "https://docs.google.com/d/4",
		PickerProviderID: &providerGW,
		PickerFileID:     strPtr("file-4"),
	}
	require.NoError(t, store.db.Create(&doc4).Error)

	ctx := context.Background()

	// Act
	n, err := store.ClearPickerMetadataForOwner(ctx, userA, providerGW)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "exactly one document should have been cleared")

	// Assert doc1: picker columns cleared, access_status reset to unknown.
	var raw1 models.Document
	require.NoError(t, store.db.First(&raw1, "id = ?", doc1ID).Error)
	assert.Nil(t, raw1.PickerProviderID, "doc1 picker_provider_id should be NULL")
	assert.Nil(t, raw1.PickerFileID, "doc1 picker_file_id should be NULL")
	assert.Nil(t, raw1.PickerMimeType, "doc1 picker_mime_type should be NULL")
	require.NotNil(t, raw1.AccessStatus)
	assert.Equal(t, AccessStatusUnknown, *raw1.AccessStatus, "doc1 access_status should be 'unknown'")
	assert.NotNil(t, raw1.AccessStatusUpdatedAt, "doc1 access_status_updated_at should be set")

	// Assert doc2: confluence doc untouched.
	var raw2 models.Document
	require.NoError(t, store.db.First(&raw2, "id = ?", doc2ID).Error)
	require.NotNil(t, raw2.PickerProviderID)
	assert.Equal(t, providerConfluence, *raw2.PickerProviderID, "doc2 picker_provider_id should still be 'confluence'")

	// Assert doc3: no-picker doc untouched.
	var raw3 models.Document
	require.NoError(t, store.db.First(&raw3, "id = ?", doc3ID).Error)
	assert.Nil(t, raw3.PickerProviderID, "doc3 had no picker — should still be NULL")

	// Assert doc4: user B's doc untouched.
	var raw4 models.Document
	require.NoError(t, store.db.First(&raw4, "id = ?", doc4ID).Error)
	require.NotNil(t, raw4.PickerProviderID)
	assert.Equal(t, providerGW, *raw4.PickerProviderID, "doc4 (user B) picker_provider_id should be untouched")
}

func TestGormDocumentStore_GetAccessReason(t *testing.T) {
	store := newTestGormDocumentStore(t)

	// Arrange: insert a minimal ThreatModel and Document row directly via GORM.
	tmID := uuid.New().String()
	userID := uuid.New().String()
	tm := models.ThreatModel{
		ID:                    tmID,
		OwnerInternalUUID:     userID,
		CreatedByInternalUUID: userID,
		Name:                  "test-tm",
	}
	require.NoError(t, store.db.Create(&tm).Error)

	docID := uuid.New().String()
	doc := models.Document{
		ID:            docID,
		ThreatModelID: tmID,
		Name:          "test-doc",
		URI:           "https://docs.google.com/document/d/abc123/edit",
	}
	require.NoError(t, store.db.Create(&doc).Error)

	ctx := context.Background()

	// Case (a): no reason has been set yet — expect empty strings and nil updatedAt.
	reasonCode, reasonDetail, updatedAt, err := store.GetAccessReason(ctx, docID)
	require.NoError(t, err)
	assert.Equal(t, "", reasonCode)
	assert.Equal(t, "", reasonDetail)
	assert.Nil(t, updatedAt, "updatedAt should be nil before any diagnostic is set")

	// Case (b): set a reason code with no detail.
	err = store.UpdateAccessStatusWithDiagnostics(
		ctx, docID, AccessStatusPendingAccess, "google_workspace", "foo", "",
	)
	require.NoError(t, err)

	reasonCode, reasonDetail, updatedAt, err = store.GetAccessReason(ctx, docID)
	require.NoError(t, err)
	assert.Equal(t, "foo", reasonCode)
	assert.Equal(t, "", reasonDetail)
	require.NotNil(t, updatedAt, "updatedAt should be set after first UpdateAccessStatusWithDiagnostics")

	// Case (c): update with reason code AND detail.
	firstUpdatedAt := *updatedAt
	// Sleep briefly to ensure the timestamp advances.
	time.Sleep(10 * time.Millisecond)
	err = store.UpdateAccessStatusWithDiagnostics(
		ctx, docID, AccessStatusPendingAccess, "google_workspace", "other", "raw err",
	)
	require.NoError(t, err)

	reasonCode, reasonDetail, updatedAt, err = store.GetAccessReason(ctx, docID)
	require.NoError(t, err)
	assert.Equal(t, "other", reasonCode)
	assert.Equal(t, "raw err", reasonDetail)
	require.NotNil(t, updatedAt, "updatedAt should be set after second UpdateAccessStatusWithDiagnostics")
	assert.True(t, updatedAt.Equal(firstUpdatedAt) || updatedAt.After(firstUpdatedAt),
		"updatedAt should be >= previous updatedAt")

	// Case (d): clear by passing empty reasonCode.
	err = store.UpdateAccessStatusWithDiagnostics(ctx, docID, AccessStatusAccessible, "", "", "")
	require.NoError(t, err)

	reasonCode, reasonDetail, updatedAt, err = store.GetAccessReason(ctx, docID)
	require.NoError(t, err)
	assert.Equal(t, "", reasonCode, "reasonCode should be empty after clear")
	assert.Equal(t, "", reasonDetail, "reasonDetail should be empty after clear")
	require.NotNil(t, updatedAt, "updatedAt should still be set after clear (method always updates it)")

	// Error case: non-existent document ID.
	_, _, _, err = store.GetAccessReason(ctx, uuid.New().String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
