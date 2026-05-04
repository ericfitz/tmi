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

// setupThreatAliasTestDB creates an in-memory SQLite DB for ThreatRepository alias tests.
func setupThreatAliasTestDB(t *testing.T) (*gorm.DB, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Threat{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("threat-alias-user"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM for Threats",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, tm
}

func TestGormThreatRepository_CreateAssignsAlias(t *testing.T) {
	db, tm := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)
	ctx := context.Background()

	tmUUID, err := uuid.Parse(tm.ID)
	require.NoError(t, err)

	threat := &Threat{
		Name:          "Test Threat",
		ThreatType:    []string{"spoofing"},
		ThreatModelId: &tmUUID,
	}

	require.NoError(t, repo.Create(ctx, threat))
	require.NotNil(t, threat.Id)

	stored, err := repo.Get(ctx, threat.Id.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
