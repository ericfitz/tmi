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
	gormlogger "gorm.io/gorm/logger"
)

// TestChunkIDs verifies chunkIDs splits slices correctly at various sizes.
func TestChunkIDs(t *testing.T) {
	cases := []struct {
		name     string
		ids      []string
		size     int
		wantLens []int // expected length of each chunk
	}{
		{
			name:     "empty slice",
			ids:      []string{},
			size:     1000,
			wantLens: nil,
		},
		{
			name:     "nil slice",
			ids:      nil,
			size:     1000,
			wantLens: nil,
		},
		{
			name:     "exact multiple",
			ids:      makeIDs(2000),
			size:     1000,
			wantLens: []int{1000, 1000},
		},
		{
			name:     "remainder",
			ids:      makeIDs(2500),
			size:     1000,
			wantLens: []int{1000, 1000, 500},
		},
		{
			name:     "size 1",
			ids:      makeIDs(3),
			size:     1,
			wantLens: []int{1, 1, 1},
		},
		{
			name:     "fewer than size",
			ids:      makeIDs(5),
			size:     1000,
			wantLens: []int{5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks := chunkIDs(tc.ids, tc.size)
			require.Len(t, chunks, len(tc.wantLens), "number of chunks")
			for i, wantLen := range tc.wantLens {
				assert.Len(t, chunks[i], wantLen, "chunk %d length", i)
			}
			// Verify no elements are lost
			var total int
			for _, c := range chunks {
				total += len(c)
			}
			assert.Equal(t, len(tc.ids), total, "total elements must equal input length")
		})
	}
}

// makeIDs generates n unique string IDs.
func makeIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.New().String()
	}
	return ids
}

// newPruneChunkingTestDB creates an in-memory SQLite DB with the audit tables.
func newPruneChunkingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}, &models.VersionSnapshot{}))
	return db
}

// TestPruneAuditEntries_ManyRows seeds 2500 backdated audit entries via raw UPDATE
// (mimicking the integration-test seeding pattern) and verifies PruneAuditEntries
// correctly deletes all of them across multiple IN-list chunks.
func TestPruneAuditEntries_ManyRows(t *testing.T) {
	const total = 2500
	const retentionDays = 1 // prune anything older than 1 day

	db := newPruneChunkingTestDB(t)
	ctx := context.Background()

	// Seed 2500 audit entries, then backdate created_at so they all fall
	// outside the retention window. We use raw UPDATE (same approach as
	// seedBackdatedEntry in the append-only integration tests) because the
	// entries must be backdated after creation.
	backdated := time.Now().UTC().AddDate(0, 0, -10) // 10 days ago, well outside 1-day retention
	for i := 0; i < total; i++ {
		v := i + 1
		entry := models.AuditEntry{
			ThreatModelID:    models.DBVarchar(uuid.New().String()),
			ObjectType:       models.DBVarchar(models.ObjectTypeThreatModel),
			ObjectID:         models.DBVarchar(uuid.New().String()),
			Version:          &v,
			ChangeType:       models.DBVarchar(models.ChangeTypeCreated),
			ActorEmail:       models.DBVarchar("alice@tmi.local"),
			ActorProvider:    models.DBVarchar("tmi"),
			ActorProviderID:  models.DBVarchar("alice"),
			ActorDisplayName: models.DBVarchar("Alice (TMI User)"),
		}
		require.NoError(t, db.Create(&entry).Error)
		require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", backdated, entry.ID).Error)
	}

	// Confirm all rows were inserted.
	var count int64
	require.NoError(t, db.Model(&models.AuditEntry{}).Count(&count).Error)
	require.Equal(t, int64(total), count, "expected %d rows before prune", total)

	// Configure retention to 1 day so all 2500 entries qualify for pruning.
	t.Setenv("AUDIT_RETENTION_DAYS", "1")
	svc := &GormAuditService{
		db:                     db,
		auditRetentionDays:     retentionDays,
		versionRetentionCount:  defaultVersionRetentionCount,
		versionRetentionDays:   defaultVersionRetentionDays,
		tombstoneRetentionDays: defaultTombstoneRetentionDays,
	}

	pruned, err := svc.PruneAuditEntries(ctx)
	require.NoError(t, err, "PruneAuditEntries must not error on large row set")
	assert.Equal(t, total, pruned, "all %d rows should be pruned", total)

	// Verify all entries were removed.
	require.NoError(t, db.Model(&models.AuditEntry{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "no audit entries should remain after prune")
}
