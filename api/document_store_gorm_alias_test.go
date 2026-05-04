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

// setupDocumentAliasTestDB creates an in-memory SQLite DB for DocumentRepository alias tests.
func setupDocumentAliasTestDB(t *testing.T) (*gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Document{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("doc-alias-test-user"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM for Documents",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, user, tm
}

func TestGormDocumentRepository_CreateAssignsAlias(t *testing.T) {
	db, _, tm := setupDocumentAliasTestDB(t)
	repo := NewGormDocumentRepository(db, nil, nil)
	ctx := context.Background()

	docID := uuid.New()
	doc := &Document{
		Id:   &docID,
		Name: "Test Document",
		Uri:  "https://example.com/doc.pdf",
	}

	require.NoError(t, repo.Create(ctx, doc, tm.ID))

	stored, err := repo.Get(ctx, docID.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
