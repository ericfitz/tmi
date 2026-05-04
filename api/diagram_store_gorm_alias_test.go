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

// setupDiagramAliasTestDB creates an in-memory SQLite DB for GormDiagramStore alias tests.
func setupDiagramAliasTestDB(t *testing.T) (*gorm.DB, *models.ThreatModel) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Diagram{},
		&models.Metadata{},
		&models.AliasCounter{},
	))

	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: strPtr("diagram-alias-user"),
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM for Diagrams",
	}
	require.NoError(t, db.Create(tm).Error)

	return db, tm
}

func TestGormDiagramStore_CreateAssignsAlias(t *testing.T) {
	db, tm := setupDiagramAliasTestDB(t)
	store := NewGormDiagramStore(db)

	diagram := DfdDiagram{
		Name:  "Test Diagram",
		Type:  DfdDiagramTypeDFD100,
		Cells: []DfdDiagram_Cells_Item{},
	}

	idSetter := func(d DfdDiagram, id string) DfdDiagram {
		uid, _ := uuid.Parse(id)
		d.Id = &uid
		return d
	}

	created, err := store.CreateWithThreatModel(diagram, tm.ID, idSetter)
	require.NoError(t, err)
	require.NotNil(t, created.Id)

	stored, err := store.Get(created.Id.String())
	require.NoError(t, err)
	require.NotNil(t, stored.Alias)
	assert.GreaterOrEqual(t, *stored.Alias, int32(1))
}
