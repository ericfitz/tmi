package api

import (
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupAccessTrackerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ThreatModel{}))
	return db
}

func createAccessTrackerTestThreatModel(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	tm := models.ThreatModel{
		ID:                    id,
		OwnerInternalUUID:     "owner-uuid",
		Name:                  "Test TM " + id,
		CreatedByInternalUUID: "creator-uuid",
	}
	require.NoError(t, db.Create(&tm).Error)
}

func TestAccessTracker_FirstAccessUpdatesDB(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	tracker := NewAccessTracker(db)
	defer tracker.Reset()

	tmID := "tm-first-access-001"
	createAccessTrackerTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	// Give the async goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	var tm models.ThreatModel
	require.NoError(t, db.First(&tm, "id = ?", tmID).Error)
	assert.NotNil(t, tm.LastAccessedAt, "LastAccessedAt should be set after first access")
}

func TestAccessTracker_RapidAccessDebounces(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	tracker := NewAccessTracker(db)
	defer tracker.Reset()

	tmID := "tm-debounce-001"
	createAccessTrackerTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	// Record the first write time
	var tm1 models.ThreatModel
	require.NoError(t, db.First(&tm1, "id = ?", tmID).Error)
	require.NotNil(t, tm1.LastAccessedAt)
	firstWrite := *tm1.LastAccessedAt

	// Second access within debounce window should not update
	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm2 models.ThreatModel
	require.NoError(t, db.First(&tm2, "id = ?", tmID).Error)
	require.NotNil(t, tm2.LastAccessedAt)
	assert.Equal(t, firstWrite, *tm2.LastAccessedAt, "LastAccessedAt should not change within debounce window")
}

func TestAccessTracker_AccessAfterDebounceWindowWritesAgain(t *testing.T) {
	db := setupAccessTrackerTestDB(t)
	// Use a short debounce for testing
	tracker := NewAccessTrackerWithDebounce(db, 50*time.Millisecond)
	defer tracker.Reset()

	tmID := "tm-after-debounce-001"
	createAccessTrackerTestThreatModel(t, db, tmID)

	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm1 models.ThreatModel
	require.NoError(t, db.First(&tm1, "id = ?", tmID).Error)
	require.NotNil(t, tm1.LastAccessedAt)
	firstWrite := *tm1.LastAccessedAt

	// Wait for debounce window to expire
	time.Sleep(100 * time.Millisecond)

	// Second access after debounce window should update
	tracker.RecordAccess(tmID)
	time.Sleep(100 * time.Millisecond)

	var tm2 models.ThreatModel
	require.NoError(t, db.First(&tm2, "id = ?", tmID).Error)
	require.NotNil(t, tm2.LastAccessedAt)
	assert.True(t, tm2.LastAccessedAt.After(firstWrite), "LastAccessedAt should be updated after debounce window expires")
}
