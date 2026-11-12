package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupProjectTestDB creates an in-memory SQLite DB with the tables the project store touches.
func setupProjectTestDB(t *testing.T) (*gorm.DB, *models.User, *models.TeamRecord) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.TeamRecord{},
		&models.ProjectRecord{},
		&models.ProjectResponsiblePartyRecord{},
		&models.ProjectRelationshipRecord{},
		&models.Metadata{},
	))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("project-test-user")),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(user).Error)

	team := &models.TeamRecord{
		ID:                    models.DBVarchar(uuid.New().String()),
		Name:                  models.DBVarchar("Test Team"),
		CreatedByInternalUUID: user.InternalUUID,
	}
	require.NoError(t, db.Create(team).Error)

	return db, user, team
}

// metadataKeys flattens a metadata list into a key -> value map.
func metadataKeys(md *[]Metadata) map[string]string {
	out := map[string]string{}
	if md == nil {
		return out
	}
	for _, m := range *md {
		out[m.Key] = m.Value
	}
	return out
}

// PUT is a full replacement: a metadata key omitted from the body is removed,
// matching the team store (#841).
func TestGormProjectStore_UpdateReplacesMetadata(t *testing.T) {
	db, user, team := setupProjectTestDB(t)
	store := NewGormProjectStore(db)
	ctx := context.Background()
	teamID := uuid.MustParse(string(team.ID))

	created, err := store.Create(ctx, &Project{
		Name:   "proj",
		TeamId: teamID,
		Metadata: &[]Metadata{
			{Key: "env", Value: "prod"},
			{Key: "owner", Value: "alice"},
		},
	}, string(user.InternalUUID))
	require.NoError(t, err)
	require.Len(t, metadataKeys(created.Metadata), 2)

	id := created.Id.String()

	// Omit "owner": it must be gone, and "env" must carry the new value.
	updated, err := store.Update(ctx, id, &Project{
		Name:     "proj",
		TeamId:   teamID,
		Metadata: &[]Metadata{{Key: "env", Value: "staging"}},
	}, string(user.InternalUUID))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"env": "staging"}, metadataKeys(updated.Metadata))

	// Empty list clears everything.
	updated, err = store.Update(ctx, id, &Project{
		Name:     "proj",
		TeamId:   teamID,
		Metadata: &[]Metadata{},
	}, string(user.InternalUUID))
	require.NoError(t, err)
	require.Empty(t, metadataKeys(updated.Metadata))

	// nil (field absent) leaves metadata untouched.
	_, err = store.Update(ctx, id, &Project{
		Name:     "proj",
		TeamId:   teamID,
		Metadata: &[]Metadata{{Key: "env", Value: "dev"}},
	}, string(user.InternalUUID))
	require.NoError(t, err)
	updated, err = store.Update(ctx, id, &Project{Name: "proj", TeamId: teamID}, string(user.InternalUUID))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"env": "dev"}, metadataKeys(updated.Metadata))
}
