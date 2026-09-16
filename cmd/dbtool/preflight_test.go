package main

import (
	"testing"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// SEM@0000000000000000000000000000000000000000: verify the schema-version preflight passes, warns, fails, or is skipped per stamp state
func TestPreflightSchemaVersion(t *testing.T) {
	open := func(t *testing.T) *gorm.DB {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
		require.NoError(t, err)
		return db
	}
	expected := dbschema.ComputeModelsFingerprint(api.GetAllModels()...)

	t.Run("no stamp is a warning, not a failure", func(t *testing.T) {
		assert.NoError(t, preflightSchemaVersion(open(t), "test", false))
	})
	t.Run("matching stamp passes", func(t *testing.T) {
		db := open(t)
		require.NoError(t, dbschema.RecordSchemaFingerprint(db, expected))
		assert.NoError(t, preflightSchemaVersion(db, "test", false))
	})
	t.Run("mismatch fails with an actionable message", func(t *testing.T) {
		db := open(t)
		require.NoError(t, dbschema.RecordSchemaFingerprint(db, "deadbeefdeadbeefdeadbeef"))
		err := preflightSchemaVersion(db, "1.2.3", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deadbeefdead")
		assert.Contains(t, err.Error(), expected[:12])
		assert.Contains(t, err.Error(), "tmi-dbtool --schema")
		assert.Contains(t, err.Error(), "--skip-schema-check")
	})
	t.Run("mismatch is downgraded to a warning with skip", func(t *testing.T) {
		db := open(t)
		require.NoError(t, dbschema.RecordSchemaFingerprint(db, "deadbeefdeadbeefdeadbeef"))
		assert.NoError(t, preflightSchemaVersion(db, "test", true))
	})
}
