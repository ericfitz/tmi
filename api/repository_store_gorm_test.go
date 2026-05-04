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

// setupRepositoryTestDB creates an in-memory SQLite DB for RepositoryRepository tests.
func setupRepositoryTestDB(t *testing.T) (*gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Repository{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("repo-test-user"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM for Repositories",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, user, tm
}

func TestGormRepositoryRepository_CreateAssignsAlias(t *testing.T) {
	db, _, tm := setupRepositoryTestDB(t)
	repo := NewGormRepositoryRepository(db, nil, nil)
	ctx := context.Background()

	repoID := uuid.New()
	repoName := "Test Repository"
	repository := &Repository{
		Id:   &repoID,
		Name: &repoName,
		Uri:  "https://github.com/example/repo",
	}

	require.NoError(t, repo.Create(ctx, repository, tm.ID))

	stored, err := repo.Get(ctx, repoID.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
