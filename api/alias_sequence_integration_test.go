//go:build dev || test || integration

package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openAliasSeqIntegrationDB opens the real PostgreSQL integration database.
// Sequences only exist on PG/Oracle, so this SKIPS when TEST_DB_* is unset
// instead of falling back to SQLite (which keeps the row-counter allocator).
func openAliasSeqIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Skip("TEST_DB_* not set; alias sequence test requires PostgreSQL")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL integration DB")
	require.NoError(t, db.AutoMigrate(&models.ThreatModel{}, &models.AliasCounter{}))
	return db
}

// TestThreatModelAliasSequence_Integration exercises the #452 sequence-backed
// global alias path on PostgreSQL: seeding above the in-use max, monotonic
// allocation, and gap-on-rollback (proving NEXTVAL is non-transactional).
func TestThreatModelAliasSequence_Integration(t *testing.T) {
	ctx := context.Background()
	db := openAliasSeqIntegrationDB(t)

	require.NoError(t, db.Exec("DROP SEQUENCE IF EXISTS "+dbschema.ThreatModelAliasSequenceName).Error)
	t.Cleanup(func() {
		_ = db.Exec("DROP SEQUENCE IF EXISTS " + dbschema.ThreatModelAliasSequenceName).Error
	})

	// Seed a known high-water mark via the legacy global counter row (no FK,
	// unlike threat_models). aliasSeedStart reads MAX(next_alias)-1 from it, so
	// the sequence must start strictly above seededHigh. Snapshot and restore
	// any pre-existing row so the shared integration DB is left untouched.
	const seededHigh = int32(4242)
	var saved *models.AliasCounter
	var existing models.AliasCounter
	if err := db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&existing).Error; err == nil {
		c := existing
		saved = &c
	}
	require.NoError(t, db.Exec("DELETE FROM alias_counters WHERE parent_id = ? AND object_type = ?", "__global__", "threat_model").Error)
	require.NoError(t, db.Create(&models.AliasCounter{
		ParentID:   models.DBVarchar("__global__"),
		ObjectType: models.DBVarchar("threat_model"),
		NextAlias:  seededHigh + 1,
	}).Error)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM alias_counters WHERE parent_id = ? AND object_type = ?", "__global__", "threat_model").Error
		if saved != nil {
			_ = db.Create(saved).Error
		}
	})

	// Install the sequence and flip the allocator onto it, restoring prior
	// state afterwards so other tests are unaffected.
	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db))
	prev := useAliasSequence.Load()
	EnableThreatModelAliasSequence()
	t.Cleanup(func() { useAliasSequence.Store(prev) })

	alloc := func() int32 {
		var got int32
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			v, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
			got = v
			return err
		}))
		return got
	}

	// First allocation is strictly above the seeded max, and values increase.
	first := alloc()
	require.Greater(t, first, seededHigh, "sequence must start above the in-use max alias")
	second := alloc()
	require.Equal(t, first+1, second, "sequence allocations must be monotonic")

	// Gap-on-rollback: a value drawn in a rolled-back transaction is consumed,
	// not reused — proving NEXTVAL does not participate in the snapshot.
	var rolledBack int32
	require.Error(t, db.Transaction(func(tx *gorm.DB) error {
		v, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
		require.NoError(t, err)
		rolledBack = v
		return fmt.Errorf("force rollback")
	}))
	afterRollback := alloc()
	require.Greater(t, afterRollback, rolledBack, "rolled-back NEXTVAL must leave a gap, not be reused")
}
