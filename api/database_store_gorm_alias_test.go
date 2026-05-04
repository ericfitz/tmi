package api

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupThreatModelAliasTestDB creates an in-memory SQLite DB for ThreatModelStore alias tests.
func setupThreatModelAliasTestDB(t *testing.T) (*gorm.DB, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.Group{},
		&models.ThreatModel{},
		&models.ThreatModelAccess{},
		&models.Diagram{},
		&models.Threat{},
		&models.Asset{},
		&models.Document{},
		&models.Note{},
		&models.Repository{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	providerID := "tm-alias-test-user"
	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: &providerID,
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	return db, user
}

func TestGormThreatModelStore_CreateAssignsAlias(t *testing.T) {
	db, user := setupThreatModelAliasTestDB(t)
	store := NewGormThreatModelStore(db)

	providerID := *user.ProviderUserID

	emptyAuth := []Authorization{}
	tm := ThreatModel{
		Name: "Test ThreatModel for Alias",
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      user.Provider,
			ProviderId:    providerID,
		},
		CreatedBy: &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      user.Provider,
			ProviderId:    providerID,
		},
		Authorization: &emptyAuth,
	}

	idSetter := func(item ThreatModel, id string) ThreatModel {
		uid, _ := uuid.Parse(id)
		item.Id = &uid
		return item
	}

	created, err := store.Create(tm, idSetter)
	require.NoError(t, err)
	require.NotNil(t, created.Id)

	stored, err := store.Get(created.Id.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
