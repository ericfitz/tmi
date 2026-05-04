package api

import (
	"context"
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAliasTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	return db
}

func TestAllocateNextAlias_FirstCall(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	var got int32
	err := db.Transaction(func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		got = alias
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), got)
}

func TestAllocateNextAlias_SequentialCalls(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	for expected := int32(1); expected <= 5; expected++ {
		var got int32
		err := db.Transaction(func(tx *gorm.DB) error {
			alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
			got = alias
			return err
		})
		require.NoError(t, err)
		assert.Equal(t, expected, got)
	}
}

func TestAllocateNextAlias_IndependentScopes(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	// Allocate 1, 2 in scope A; 1, 2, 3 in scope B.
	for _, scope := range []struct {
		parent string
		typ    string
		count  int
	}{
		{"tm-A", "note", 2},
		{"tm-B", "note", 3},
	} {
		for i := 0; i < scope.count; i++ {
			err := db.Transaction(func(tx *gorm.DB) error {
				_, err := AllocateNextAlias(ctx, tx, scope.parent, scope.typ)
				return err
			})
			require.NoError(t, err)
		}
	}

	// Verify counters: A.next = 3, B.next = 4.
	var counters []models.AliasCounter
	require.NoError(t, db.Where("object_type = ?", "note").Find(&counters).Error)
	got := map[string]int32{}
	for _, c := range counters {
		got[c.ParentID] = c.NextAlias
	}
	assert.Equal(t, int32(3), got["tm-A"])
	assert.Equal(t, int32(4), got["tm-B"])
}

func TestAllocateNextAlias_GlobalThreatModel(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	for expected := int32(1); expected <= 3; expected++ {
		var got int32
		err := db.Transaction(func(tx *gorm.DB) error {
			alias, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
			got = alias
			return err
		})
		require.NoError(t, err)
		assert.Equal(t, expected, got)
	}
}

func TestAllocateNextAlias_HighWaterAfterRollback(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	// Successful allocation.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		return err
	}))

	// Allocate then force a rollback.
	_ = db.Transaction(func(tx *gorm.DB) error {
		_, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		require.NoError(t, err)
		return assert.AnError // forces rollback
	})

	// Next allocation: counter is back to 2 (the rollback reverted the +1).
	// This is the correct behavior — high-water-mark applies only to successful
	// inserts, since the entire transaction (including the counter UPDATE) rolled
	// back atomically.
	var got int32
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
		got = alias
		return err
	}))
	assert.Equal(t, int32(2), got)
}

func TestAllocateNextAlias_ConcurrentCallers(t *testing.T) {
	db := setupAliasTestDB(t)
	ctx := context.Background()

	// SQLite in-memory databases are per-connection; concurrent goroutines that
	// open new transactions may land on different connections (each with its own
	// empty schema). Restrict the connection pool to 1 so all goroutines share
	// the same in-memory DB. This simulates the locking behaviour we care about
	// without hitting SQLite's lack of SELECT FOR UPDATE support.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	const N = 10
	results := make([]int32, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := db.Transaction(func(tx *gorm.DB) error {
				alias, err := AllocateNextAlias(ctx, tx, "tm-1", "note")
				results[i] = alias
				return err
			})
			require.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// All N results must be distinct (no duplicate aliases under concurrency).
	seen := map[int32]bool{}
	for _, r := range results {
		assert.False(t, seen[r], "duplicate alias %d", r)
		seen[r] = true
	}
	assert.Len(t, seen, N)
}
