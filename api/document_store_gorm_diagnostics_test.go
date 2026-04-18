package api

import (
	"context"
	"testing"

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
