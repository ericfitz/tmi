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

func setupBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.ThreatModel{},
		&models.Note{},
		&models.AliasCounter{},
	))
	return db
}

func makeUser(t *testing.T, db *gorm.DB) *models.User {
	t.Helper()
	u := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: ptrStr("u-" + uuid.New().String()[:8]),
		Email:          "u@example.com",
		Name:           "Test",
	}
	require.NoError(t, db.Create(u).Error)
	return u
}

func ptrStr(s string) *string { return &s }

func TestBackfillThreatModels_AssignsByCreatedAt(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	tmA := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000001", OwnerInternalUUID: user.InternalUUID, Name: "A", CreatedAt: now.Add(-3 * time.Hour)}
	tmB := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000002", OwnerInternalUUID: user.InternalUUID, Name: "B", CreatedAt: now.Add(-1 * time.Hour)}
	tmC := &models.ThreatModel{ID: "00000000-aaaa-aaaa-aaaa-000000000003", OwnerInternalUUID: user.InternalUUID, Name: "C", CreatedAt: now.Add(-2 * time.Hour)}
	require.NoError(t, db.Create(tmA).Error)
	require.NoError(t, db.Create(tmB).Error)
	require.NoError(t, db.Create(tmC).Error)

	require.NoError(t, RunAliasBackfill(ctx, db))

	var got []models.ThreatModel
	require.NoError(t, db.Order("alias ASC").Find(&got).Error)
	require.Len(t, got, 3)
	// A (oldest), C, B (newest) — in created_at ASC order.
	assert.Equal(t, tmA.ID, got[0].ID)
	assert.Equal(t, int32(1), got[0].Alias)
	assert.Equal(t, tmC.ID, got[1].ID)
	assert.Equal(t, int32(2), got[1].Alias)
	assert.Equal(t, tmB.ID, got[2].ID)
	assert.Equal(t, int32(3), got[2].Alias)

	// Counter is set to N+1.
	var counter models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&counter).Error)
	assert.Equal(t, int32(4), counter.NextAlias)
}

func TestBackfillSubObjectsScopedPerTM(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	tm1 := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM1"}
	tm2 := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM2"}
	require.NoError(t, db.Create(tm1).Error)
	require.NoError(t, db.Create(tm2).Error)

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		require.NoError(t, db.Create(&models.Note{
			ID: uuid.New().String(), ThreatModelID: tm1.ID, Name: "n", Content: models.DBText("x"), CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}).Error)
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&models.Note{
			ID: uuid.New().String(), ThreatModelID: tm2.ID, Name: "n", Content: models.DBText("x"), CreatedAt: now.Add(time.Duration(i) * time.Minute),
		}).Error)
	}

	require.NoError(t, RunAliasBackfill(ctx, db))

	var notes1, notes2 []models.Note
	require.NoError(t, db.Where("threat_model_id = ?", tm1.ID).Order("alias ASC").Find(&notes1).Error)
	require.NoError(t, db.Where("threat_model_id = ?", tm2.ID).Order("alias ASC").Find(&notes2).Error)

	require.Len(t, notes1, 3)
	assert.Equal(t, int32(1), notes1[0].Alias)
	assert.Equal(t, int32(2), notes1[1].Alias)
	assert.Equal(t, int32(3), notes1[2].Alias)

	require.Len(t, notes2, 2)
	assert.Equal(t, int32(1), notes2[0].Alias)
	assert.Equal(t, int32(2), notes2[1].Alias)

	// Verify per-TM counters.
	var c1, c2 models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", tm1.ID, "note").First(&c1).Error)
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", tm2.ID, "note").First(&c2).Error)
	assert.Equal(t, int32(4), c1.NextAlias)
	assert.Equal(t, int32(3), c2.NextAlias)
}

func TestBackfillIdempotent(t *testing.T) {
	db := setupBackfillTestDB(t)
	user := makeUser(t, db)
	ctx := context.Background()

	tm := &models.ThreatModel{ID: uuid.New().String(), OwnerInternalUUID: user.InternalUUID, Name: "TM"}
	require.NoError(t, db.Create(tm).Error)

	require.NoError(t, RunAliasBackfill(ctx, db))

	var before models.ThreatModel
	require.NoError(t, db.Where("id = ?", tm.ID).First(&before).Error)
	assert.Equal(t, int32(1), before.Alias)

	// Second run should be a no-op (no rows have alias=0).
	require.NoError(t, RunAliasBackfill(ctx, db))

	var after models.ThreatModel
	require.NoError(t, db.Where("id = ?", tm.ID).First(&after).Error)
	assert.Equal(t, int32(1), after.Alias) // unchanged

	var counter models.AliasCounter
	require.NoError(t, db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&counter).Error)
	assert.Equal(t, int32(2), counter.NextAlias)
}
