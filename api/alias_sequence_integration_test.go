//go:build dev || test || integration

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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

// TestThreatModelAliasSequence_SelfHealsOnMissingSequence pins the Zero-500 fix:
// when the global alias sequence is dropped out from under a running server
// (schema drift / a DB reset that recreated tables but not sequences) while the
// per-process gate is still on, AllocateNextAlias must classify the failure as
// ErrAliasSequenceMissing (not a generic 500), and GormThreatModelStore.Create
// must reinstall the sequence and retry instead of returning 500.
func TestThreatModelAliasSequence_SelfHealsOnMissingSequenceIntegration(t *testing.T) {
	ctx := context.Background()
	db := openAliasSeqIntegrationDB(t)

	// Create needs the user table (FK target) and the access table.
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ThreatModelAccess{}))

	require.NoError(t, db.Exec("DROP SEQUENCE IF EXISTS "+dbschema.ThreatModelAliasSequenceName).Error)
	t.Cleanup(func() {
		_ = db.Exec("DROP SEQUENCE IF EXISTS " + dbschema.ThreatModelAliasSequenceName).Error
	})

	// Unique owner so the shared integration DB is left clean.
	providerID := "alias-selfheal-" + uuid.New().String()[:8]
	owner := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(&providerID),
		Email:          models.DBVarchar(providerID + "@example.com"),
		Name:           models.DBVarchar("Self Heal Owner"),
	}
	require.NoError(t, db.Create(owner).Error)

	createdIDs := []string{}
	t.Cleanup(func() {
		for _, id := range createdIDs {
			_ = db.Exec("DELETE FROM threat_models WHERE id = ?", id).Error
		}
		_ = db.Exec("DELETE FROM users WHERE internal_uuid = ?", string(owner.InternalUUID)).Error
	})

	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db))
	prev := useAliasSequence.Load()
	EnableThreatModelAliasSequence()
	t.Cleanup(func() { useAliasSequence.Store(prev) })

	store := NewGormThreatModelStore(db)
	emptyAuth := []Authorization{}
	idSetter := func(item ThreatModel, id string) ThreatModel {
		uid, _ := uuid.Parse(id)
		item.Id = &uid
		return item
	}
	newTM := func(name string) ThreatModel {
		return ThreatModel{
			Name:          name,
			Owner:         User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: providerID},
			CreatedBy:     &User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: providerID},
			Authorization: &emptyAuth,
		}
	}

	// Baseline: create succeeds on the sequence path.
	first, err := store.Create(newTM("alias self-heal baseline"), idSetter)
	require.NoError(t, err)
	require.NotNil(t, first.Id)
	require.NotNil(t, first.Alias)
	createdIDs = append(createdIDs, first.Id.String())

	// Simulate schema drift: drop the sequence while the gate stays on.
	require.NoError(t, db.Exec("DROP SEQUENCE "+dbschema.ThreatModelAliasSequenceName).Error)

	// Allocator-level: a direct NEXTVAL against the dropped sequence is
	// classified as the recoverable sentinel, not a generic error.
	directErr := db.Transaction(func(tx *gorm.DB) error {
		_, e := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
		return e
	})
	require.Error(t, directErr)
	assert.True(t, errors.Is(directErr, ErrAliasSequenceMissing), "dropped sequence must surface ErrAliasSequenceMissing")
	assert.True(t, errors.Is(directErr, dberrors.ErrUndefinedObject), "and chain to dberrors.ErrUndefinedObject")

	// End-to-end: Create self-heals (reinstall + retry) instead of 500-ing, and
	// the reinstall seeds above the max alias so the new alias does not collide
	// with the unique threat_models.alias index.
	second, err := store.Create(newTM("alias self-heal recovered"), idSetter)
	require.NoError(t, err, "Create must self-heal a dropped alias sequence, not return a 500")
	require.NotNil(t, second.Id)
	require.NotNil(t, second.Alias)
	createdIDs = append(createdIDs, second.Id.String())
	assert.Greater(t, *second.Alias, *first.Alias, "reinstalled sequence must seed above the existing max alias")
}
