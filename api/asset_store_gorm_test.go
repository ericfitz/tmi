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

// setupAssetTestDB creates an in-memory SQLite DB with all tables needed for AssetRepository tests.
func setupAssetTestDB(t *testing.T) (*gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Asset{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("asset-test-user"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM for Assets",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, user, tm
}

func TestGormAssetRepository_CreateAssignsAlias(t *testing.T) {
	db, _, tm := setupAssetTestDB(t)
	repo := NewGormAssetRepository(db, nil, nil)
	ctx := context.Background()

	assetID := uuid.New()
	asset := &Asset{
		Id:   &assetID,
		Name: "Test Asset",
		Type: AssetTypeSoftware,
	}

	require.NoError(t, repo.Create(ctx, asset, tm.ID))

	stored, err := repo.Get(ctx, assetID.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
