package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestUserAPIQuotaStore opens an in-memory SQLite database and
// auto-migrates UserAPIQuota (without its User foreign-key referent, since
// SQLite does not enforce foreign keys by default).
func newTestUserAPIQuotaStore(t *testing.T) *GormUserAPIQuotaStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.UserAPIQuota{}))
	return NewGormUserAPIQuotaStore(db)
}

// TestGormUserAPIQuotaStore_Upsert_ReturnsStoredCreatedAtOnUpdate guards
// against #706: Oracle's MERGE has no RETURNING, so on the UPDATE branch the
// struct passed into Upsert still holds the client-side autoCreateTime
// stamped for an insert that never happened. Upsert must read the row back
// and echo the stored created_at rather than that fresh client-side value.
func TestGormUserAPIQuotaStore_Upsert_ReturnsStoredCreatedAtOnUpdate(t *testing.T) {
	store := newTestUserAPIQuotaStore(t)
	ctx := context.Background()
	userID := uuid.New()

	first, err := store.Upsert(ctx, UserAPIQuota{
		UserId:               userID,
		MaxRequestsPerMinute: 10,
		MaxRequestsPerHour:   intPtr(100),
	})
	require.NoError(t, err)
	require.False(t, first.CreatedAt.IsZero())

	// Force the stored row's created_at to a value clearly distinct from
	// "now", independent of anything a later client-side struct might carry,
	// so the second call's echo can only match by reading the row back.
	stored := first.CreatedAt.Add(-72 * time.Hour).Truncate(time.Second)
	require.NoError(t, store.db.Model(&models.UserAPIQuota{}).
		Where("user_internal_uuid = ?", userID.String()).
		UpdateColumn("created_at", stored).Error)

	second, err := store.Upsert(ctx, UserAPIQuota{
		UserId:               userID,
		MaxRequestsPerMinute: 20,
		MaxRequestsPerHour:   intPtr(200),
	})
	require.NoError(t, err)

	assert.WithinDuration(t, stored, second.CreatedAt, 0,
		"upsert on the UPDATE branch must echo the stored row's created_at, not a fresh client-side value")
	assert.Equal(t, 20, second.MaxRequestsPerMinute)

	// Confirm the update branch was actually taken (still a single row).
	var count int64
	require.NoError(t, store.db.Model(&models.UserAPIQuota{}).
		Where("user_internal_uuid = ?", userID.String()).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
