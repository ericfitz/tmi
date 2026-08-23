package dbschema

import (
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// countSystemSettings returns how many rows carry the given key.
// SEM@24731679561a852b21b37271caffd9a597080f0b: count system_settings rows for one key in tests (reads DB)
func countSystemSettings(t *testing.T, db *gorm.DB, key string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.SystemSetting{}).Where("setting_key = ?", key).Count(&n).Error)
	return n
}

// TestPruneRetiredSystemSettings_DeletesRetiredRows is the core case: an
// upgraded deployment carrying the retired rate_limit.* rows must lose them.
//
// This is the half that removing the declarations alone does not achieve.
// A row whose key has no registry entry resolves to VisibilityInternal, so
// LIST would keep showing it while GET and DELETE 404 -- exactly the
// inconsistency #809 reported. Leaving the row behind fixes the bug for new
// databases and re-creates it for every existing one.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate the prune removes retired setting rows
func TestPruneRetiredSystemSettings_DeletesRetiredRows(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	for _, key := range []string{"rate_limit.requests_per_minute", "rate_limit.requests_per_hour"} {
		require.NoError(t, db.Create(&models.SystemSetting{
			SettingKey:  models.DBVarchar(key),
			Value:       "100",
			SettingType: models.SystemSettingTypeInt,
			ModifiedAt:  time.Now(),
		}).Error)
	}

	removed, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(2), removed)

	for _, key := range retiredSettingKeys {
		assert.Zero(t, countSystemSettings(t, db, key), "%s must not survive the prune", key)
	}
}

// TestPruneRetiredSystemSettings_LeavesLiveSettingsAlone pins the blast
// radius. server.disable_rate_limiting is the live setting that actually
// drives rate limiting and shares a prefix-adjacent name with the retired
// keys, so it is the one most at risk from a careless LIKE 'rate_limit%'
// implementation.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate the prune leaves live settings untouched
func TestPruneRetiredSystemSettings_LeavesLiveSettingsAlone(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	live := []string{
		"server.disable_rate_limiting",
		"server.ratelimit_public_rpm",
		"websocket.max_participants",
		"upload.max_file_size_mb",
	}
	for _, key := range live {
		require.NoError(t, db.Create(&models.SystemSetting{
			SettingKey:  models.DBVarchar(key),
			Value:       "10",
			SettingType: models.SystemSettingTypeInt,
			ModifiedAt:  time.Now(),
		}).Error)
	}
	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	removed, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed, "only the retired key may be deleted")

	for _, key := range live {
		assert.Equal(t, int64(1), countSystemSettings(t, db, key),
			"%s is a live setting and must survive the prune", key)
	}
}

// TestPruneRetiredSystemSettings_DeletesExplicitRowsToo pins the deliberate
// choice to delete unconditionally rather than only seeded-origin rows.
//
// The keys are leaving the product: an operator-set value for a setting
// nothing reads has no effect to preserve, and an explicit-origin row left
// behind reproduces the orphan case above for precisely the installs most
// likely to notice it.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate the prune removes operator-set rows as well
func TestPruneRetiredSystemSettings_DeletesExplicitRowsToo(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_hour",
		Value:       "9999", // operator-customised, not the registry default
		SettingType: models.SystemSettingTypeInt,
		ModifiedBy:  models.NullableDBVarchar{String: "11111111-1111-1111-1111-111111111111", Valid: true},
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
		ModifiedAt:  time.Now(),
	}).Error)

	removed, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	assert.Zero(t, countSystemSettings(t, db, "rate_limit.requests_per_hour"))
}

// TestPruneRetiredSystemSettings_Idempotent covers the steady state: this
// runs on every boot, so a second pass must find nothing and report zero
// rather than erroring or re-reporting.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate a repeated prune finds nothing left to delete
func TestPruneRetiredSystemSettings_Idempotent(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	first, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), first)

	second, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), second, "a second pass must find nothing left to delete")
}

// TestPruneRetiredSystemSettings_NoRows covers the empty-table fast path: a
// fresh database has nothing to prune and must not error.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate the prune no-ops against an empty settings table
func TestPruneRetiredSystemSettings_NoRows(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	removed, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed)
}

// TestPruneRetiredSystemSettings_RunsBeforeOriginBackfill pins the ordering
// constraint that the call sites depend on.
//
// expectedSeedValues derives from models.DefaultSystemSettings(), which
// projects the registry. Now that the retired defs are gone, a surviving
// NULL-origin rate_limit row matches no known default, so
// BackfillSystemSettingOrigin would stamp it explicit. Running the prune
// first means the backfill never sees it.
//
// Watched to fail: swapping the two calls below makes the backfill report 1
// stamped row instead of 0.
// SEM@24731679561a852b21b37271caffd9a597080f0b: validate pruning precedes the origin backfill
func TestPruneRetiredSystemSettings_RunsBeforeOriginBackfill(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	// Call-site order: prune, then backfill.
	removed, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	stamped, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stamped,
		"the retired row must be gone before the backfill runs, so it never gets stamped explicit")
}

// TestPruneRetiredSystemSettings_HardDeletes pins that the prune actually
// removes the row rather than soft-deleting it.
//
// models.SystemSetting carries no soft-delete field today, so Delete emits a
// real DELETE. If one were added later, Delete silently becomes an UPDATE and
// the row survives -- still resolving to VisibilityInternal, still producing
// the LIST-shows-it/GET-404s shape #809 reported. Every other test here would
// keep passing, because GORM's Count filters soft-deleted rows exactly like
// Find does. Unscoped() is what makes the difference visible.
//
// The Oracle trigger is looser than GORM core's, which is why the column
// assertion below is by NAME rather than by type (oracle-db-admin review,
// #813). GORM core only diverts Delete to an UPDATE for a field typed
// gorm.DeletedAt. gorm-oracle's replaced delete callback diverts on
// stmt.Schema.LookUpField("deleted_at") != nil -- any field mapping to that
// column, whatever its type. TMI's tombstoned models deliberately use a plain
// *time.Time (api/models/models.go:159,212,252,323), so a future field of that
// shape on SystemSetting is realistic; it would leave SQLite and Postgres
// hard-deleting, this test green, and Oracle alone silently not deleting --
// reproducing #809 on the one platform this change exists to fix.
func TestPruneRetiredSystemSettings_HardDeletes(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	// The by-name check: any deleted_at column at all, regardless of the Go
	// field's type, is enough to divert Delete into an UPDATE on Oracle.
	assert.False(t, db.Migrator().HasColumn(&models.SystemSetting{}, "deleted_at"),
		"models.SystemSetting must have no deleted_at column: gorm-oracle diverts Delete "+
			"to an UPDATE for any field mapping to that column name, regardless of type, "+
			"which would silently stop this prune working on Oracle only")

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	_, err := PruneRetiredSystemSettings(db)
	require.NoError(t, err)

	var n int64
	require.NoError(t, db.Unscoped().Model(&models.SystemSetting{}).
		Where("setting_key = ?", "rate_limit.requests_per_minute").Count(&n).Error)
	assert.Zero(t, n, "the row must be gone, not soft-deleted — Unscoped() still sees soft-deleted rows")
}
