//go:build oracle

package api

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAliasFirstAllocationRaceOracleIntegration covers the window the #906
// opt-down exposed: concurrent first-ever allocations for one scope at READ
// COMMITTED. gorm-oracle's MERGE lets every session plan the counter-row
// INSERT, so all but one get ORA-00001; the allocator must treat that as
// "row exists" and still hand out distinct, gapless aliases.
//
// Run via `make test-integration-oci`.
// SEM@dcd8d846ec500f67627f500efa9b1d25b7bc6c99: verify concurrent first-ever alias allocations yield distinct gapless aliases on Oracle ADB (writes DB)
func TestAliasFirstAllocationRaceOracleIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := openAuditAppendOnlyOracleDB(t).WithContext(ctx)
	if !db.Migrator().HasTable(&models.AliasCounter{}) {
		require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	}

	parent := uuid.New().String()
	const objType = "threat"
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM ALIAS_COUNTERS WHERE PARENT_ID = ? AND OBJECT_TYPE = ?", parent, objType).Error
	})

	rc := &sql.TxOptions{Isolation: sql.LevelReadCommitted}

	// Session A makes the first-ever allocation and holds it uncommitted.
	txA := db.Begin(rc)
	require.NoError(t, txA.Error)
	first, err := AllocateNextAlias(ctx, txA, parent, objType)
	require.NoError(t, err)
	require.EqualValues(t, 1, first)

	// Session B starts its own first-ever allocation: its MERGE also plans the
	// INSERT and blocks on A's uncommitted primary-key entry.
	var second int32
	done := make(chan error, 1)
	go func() {
		done <- authdb.WithRetryableGormTransaction(ctx, db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
			v, err := AllocateNextAlias(ctx, tx, parent, objType)
			second = v
			return err
		}, rc)
	}()

	select {
	case err := <-done:
		t.Fatalf("session B finished before A committed (err=%v); it should block on A's counter row", err)
	case <-time.After(3 * time.Second):
	}

	// A commits; B's blocked INSERT now fails with ORA-00001, which the
	// allocator must absorb and then read A's committed row under lock.
	require.NoError(t, txA.Commit().Error)
	require.NoError(t, <-done)
	require.EqualValues(t, 2, second)
}
