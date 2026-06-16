//go:build oracle

package api

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestSerializableRetryOnConflictOracleIntegration verifies the #450 / #451
// acceptance criterion on real Oracle ADB: that WithRetryableGormTransaction
// actually runs SERIALIZABLE and that an ORA-08177 ("can't serialize access")
// is observed under contention and transparently retried to success.
//
// It deterministically induces the conflict: inside the retryable closure the
// transaction first SELECTs a counter row (establishing its serializable
// snapshot), then — on the first attempt only — a SEPARATE auto-committing
// connection UPDATEs+commits that same row, and finally the transaction UPDATEs
// the row it read. Under SERIALIZABLE, Oracle raises ORA-08177 on that write
// because the row was committed-changed since the snapshot; under READ
// COMMITTED it would simply succeed and the closure would run exactly once.
// So observing a retry (attempts >= 2) is proof the transaction was
// SERIALIZABLE, and the nil error is proof the wrapper retried past it.
//
// Run via `make test-integration-oci`.
func TestSerializableRetryOnConflictOracleIntegration(t *testing.T) {
	ctx := context.Background()
	db := openAuditAppendOnlyOracleDB(t)
	if !db.Migrator().HasTable(&models.AliasCounter{}) {
		require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	}

	const parent = "__ser_test__"
	const objType = "ser"
	clean := func() {
		_ = db.Exec("DELETE FROM ALIAS_COUNTERS WHERE PARENT_ID = ? AND OBJECT_TYPE = ?", parent, objType).Error
	}
	clean()
	t.Cleanup(clean)
	require.NoError(t, db.Create(&models.AliasCounter{
		ParentID:   models.DBVarchar(parent),
		ObjectType: models.DBVarchar(objType),
		NextAlias:  1,
	}).Error)

	attempts := 0
	serializable := &sql.TxOptions{Isolation: sql.LevelSerializable}
	err := authdb.WithRetryableGormTransaction(ctx, db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		attempts++

		// Establish the serializable snapshot with a read.
		var c models.AliasCounter
		if err := tx.Where("parent_id = ? AND object_type = ?", parent, objType).First(&c).Error; err != nil {
			return err
		}

		// On the first attempt only, commit a conflicting change from a
		// separate (auto-committing) connection AFTER the snapshot is set.
		if attempts == 1 {
			if err := db.Exec(
				"UPDATE ALIAS_COUNTERS SET NEXT_ALIAS = NEXT_ALIAS + 1 WHERE PARENT_ID = ? AND OBJECT_TYPE = ?",
				parent, objType,
			).Error; err != nil {
				return err
			}
		}

		// Write the row we read. On attempt 1 this raises ORA-08177 under
		// SERIALIZABLE (row committed-changed since the snapshot); the wrapper
		// classifies it transient and retries the whole closure.
		return tx.Model(&models.AliasCounter{}).
			Where("parent_id = ? AND object_type = ?", parent, objType).
			Update("next_alias", c.NextAlias+100).Error
	}, serializable)

	require.NoError(t, err, "wrapper must retry past ORA-08177 and commit")
	require.GreaterOrEqual(t, attempts, 2,
		"a serialization conflict must have forced at least one retry — proof the transaction ran SERIALIZABLE and the wrapper retried (got %d attempts)", attempts)
}
