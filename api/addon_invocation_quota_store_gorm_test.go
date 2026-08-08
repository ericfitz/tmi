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

// newTestAddonInvocationQuotaStore opens an in-memory SQLite database and
// auto-migrates AddonInvocationQuota (without its User foreign-key
// referent, since SQLite does not enforce foreign keys by default).
func newTestAddonInvocationQuotaStore(t *testing.T) *GormAddonInvocationQuotaStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AddonInvocationQuota{}))
	return NewGormAddonInvocationQuotaStore(db)
}

// TestGormAddonInvocationQuotaStore_Set_ReturnsStoredCreatedAtOnUpdate
// guards against #706: Oracle's MERGE has no RETURNING, so on the UPDATE
// branch the struct passed into Set still holds the client-side
// autoCreateTime stamped for an insert that never happened. Set must read
// the row back and echo the stored created_at rather than that fresh
// client-side value.
func TestGormAddonInvocationQuotaStore_Set_ReturnsStoredCreatedAtOnUpdate(t *testing.T) {
	store := newTestAddonInvocationQuotaStore(t)
	ctx := context.Background()
	ownerID := uuid.New()

	first := &AddonInvocationQuota{
		OwnerId:               ownerID,
		MaxActiveInvocations:  3,
		MaxInvocationsPerHour: 10,
	}
	require.NoError(t, store.Set(ctx, first))
	require.False(t, first.CreatedAt.IsZero())

	// Force the stored row's created_at to a value clearly distinct from
	// "now", independent of anything a later client-side struct might carry,
	// so the second call's echo can only match by reading the row back.
	stored := first.CreatedAt.Add(-72 * time.Hour).Truncate(time.Second)
	require.NoError(t, store.db.Model(&models.AddonInvocationQuota{}).
		Where("owner_internal_uuid = ?", ownerID.String()).
		UpdateColumn("created_at", stored).Error)

	second := &AddonInvocationQuota{
		OwnerId:               ownerID,
		MaxActiveInvocations:  5,
		MaxInvocationsPerHour: 50,
	}
	require.NoError(t, store.Set(ctx, second))

	assert.WithinDuration(t, stored, second.CreatedAt, 0,
		"set on the UPDATE branch must echo the stored row's created_at, not a fresh client-side value")
	assert.Equal(t, 5, second.MaxActiveInvocations)

	// Confirm the update branch was actually taken (still a single row).
	var count int64
	require.NoError(t, store.db.Model(&models.AddonInvocationQuota{}).
		Where("owner_internal_uuid = ?", ownerID.String()).
		Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
