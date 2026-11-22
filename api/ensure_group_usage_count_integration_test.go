//go:build dev || test || integration

package api

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureGroupExists_UsageCountUpsert_Integration guards #1199: on
// PostgreSQL the upsert's `usage_count = usage_count + 1` was ambiguous
// (SQLSTATE 42702) between the target row and EXCLUDED, so naming any group in
// a threat model authorization returned 500. SQLite resolves the bare column
// silently, which is why the SQLite unit tests never caught it.
func TestEnsureGroupExists_UsageCountUpsert_Integration(t *testing.T) {
	db := openSortPaginationIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.Group{}))

	store := NewGormThreatModelStore(db)
	provider := "okta"
	groupName := "usage-count-" + uuid.NewString()
	t.Cleanup(func() {
		db.Where("provider = ? AND group_name = ?", provider, groupName).Delete(&models.Group{})
	})

	// First call inserts; the bug surfaced here already because PostgreSQL
	// rejects the statement at parse time.
	id1, err := store.ensureGroupExists(db, groupName, &provider)
	require.NoError(t, err, "creating a new group via the upsert must not fail")
	require.NotEmpty(t, id1)

	// Second call takes the conflict branch and must increment usage_count.
	id2, err := store.ensureGroupExists(db, groupName, &provider)
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	var g models.Group
	require.NoError(t, db.Where("provider = ? AND group_name = ?", provider, groupName).First(&g).Error)
	assert.Equal(t, 2, g.UsageCount, "usage_count after two upserts")

}
