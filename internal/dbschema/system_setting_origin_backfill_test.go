package dbschema

import (
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newSystemSettingOriginTestDB builds an in-memory SQLite database migrated
// with the real models.SystemSetting model. Unlike most other tests in this
// package, this one uses the real model directly rather than a test-local
// mirror: BackfillSystemSettingOrigin's logic is defined in terms of
// models.DefaultSystemSettings(), models.SystemSettingOriginExplicit, and
// SystemSetting.IsExplicit() themselves, so mirroring the struct would just
// duplicate DBText/NullableDBVarchar's Scan/Value behavior under test.
// SEM@0000000000000000000000000000000000000000: build an in-memory SQLite DB migrated with the real SystemSetting model (pure)
func newSystemSettingOriginTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemSetting{}))
	return db
}

// getSystemSetting reloads a single row by key, failing the test if it is
// not found.
// SEM@0000000000000000000000000000000000000000: fetch one system_settings row by key for test assertions (reads DB)
func getSystemSetting(t *testing.T, db *gorm.DB, key string) models.SystemSetting {
	t.Helper()
	var row models.SystemSetting
	require.NoError(t, db.Where("setting_key = ?", key).First(&row).Error)
	return row
}

// TestBackfillSystemSettingOrigin_ModifiedByStampsExplicit covers the first
// operator-intent signal: a NULL-origin row with modified_by set (an admin
// API write or dbtool import from before Origin existed) must become
// explicit, even though its value still happens to equal the registry
// default.
// SEM@0000000000000000000000000000000000000000: validate a NULL-origin row with modified_by set is stamped explicit
func TestBackfillSystemSettingOrigin_ModifiedByStampsExplicit(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)
	modifier := uuid.NewString()

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "websocket.max_participants",
		Value:       "10", // matches the registry default (models.DefaultSystemSettings)
		SettingType: models.SystemSettingTypeInt,
		ModifiedBy:  models.NullableDBVarchar{String: modifier, Valid: true},
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "websocket.max_participants")
	assert.True(t, row.IsExplicit(), "a modified_by row must become explicit regardless of its value")
}

// TestBackfillSystemSettingOrigin_PreservesModifiedAt pins oracle-db-admin's
// #794 finding: stamping origin must not disturb modified_at.
// SystemSetting.ModifiedAt carries autoUpdateTime, and a plain GORM Update
// call injects every autoUpdateTime field into the SET clause even when it
// isn't named -- silently destroying the row's real last-modified timestamp,
// which is operator-visible via the admin config API. BackfillSystemSettingOrigin
// must use UpdateColumn to avoid this.
// SEM@0000000000000000000000000000000000000000: validate the backfill does not disturb a stamped row's modified_at timestamp
func TestBackfillSystemSettingOrigin_PreservesModifiedAt(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)
	modifier := uuid.NewString()
	original := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "websocket.max_participants",
		Value:       "10",
		SettingType: models.SystemSettingTypeInt,
		ModifiedBy:  models.NullableDBVarchar{String: modifier, Valid: true},
		ModifiedAt:  original,
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "websocket.max_participants")
	require.True(t, row.IsExplicit())
	assert.True(t, original.Equal(row.ModifiedAt),
		"backfill must not rewrite modified_at: want %s, got %s", original, row.ModifiedAt)
}

// TestBackfillSystemSettingOrigin_EncryptedValueRelinquishesToSeeded covers
// the at-rest-encryption gap oracle-db-admin flagged for #794: an
// ENC:-prefixed value can never be compared against a plaintext registry
// default, so a NULL-origin encrypted row with no modified_by must be left
// alone (reads as seeded) rather than stamped explicit on a meaningless
// ciphertext mismatch.
// SEM@0000000000000000000000000000000000000000: validate an encrypted NULL-origin row with no modified_by stays seeded rather than being stamped explicit on a meaningless comparison
func TestBackfillSystemSettingOrigin_EncryptedValueRelinquishesToSeeded(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "session.timeout_minutes", // registry default is "60"
		Value:       "ENC:v1:1:1:notarealciphertextblob",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated, "an encrypted value must not be compared against the plaintext default")

	row := getSystemSetting(t, db, "session.timeout_minutes")
	assert.False(t, row.Origin.Valid)
	assert.False(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_ValueDiffersStampsExplicit covers the
// second operator-intent signal: a NULL-origin row whose value no longer
// matches what SeedDefaults would seed for that key today must become
// explicit.
// SEM@0000000000000000000000000000000000000000: validate a NULL-origin row whose value differs from the registry default is stamped explicit
func TestBackfillSystemSettingOrigin_ValueDiffersStampsExplicit(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "9999", // registry default is "60"
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "session.timeout_minutes")
	assert.True(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_UnknownKeyStampsExplicit covers a
// NULL-origin row whose key SeedDefaults would never have inserted at all
// (e.g. an operator- or extension-created setting): with no registry default
// to compare against, it must be treated as showing operator intent.
// SEM@0000000000000000000000000000000000000000: validate a NULL-origin row for a key absent from the registry is stamped explicit
func TestBackfillSystemSettingOrigin_UnknownKeyStampsExplicit(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "custom.not_in_registry",
		Value:       "anything",
		SettingType: models.SystemSettingTypeString,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "custom.not_in_registry")
	assert.True(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_UntouchedDefaultStaysNil covers the
// negative case: a NULL-origin row that still holds the registry default
// with no modified_by must be left alone, so it continues to read as seeded.
// SEM@0000000000000000000000000000000000000000: validate a NULL-origin row matching the registry default with no modified_by is left untouched
func TestBackfillSystemSettingOrigin_UntouchedDefaultStaysNil(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100", // exact registry default
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated)

	row := getSystemSetting(t, db, "rate_limit.requests_per_minute")
	assert.False(t, row.Origin.Valid, "an untouched seeded default must stay NULL-origin")
	assert.False(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_AlreadyStampedRowsUntouched covers rows
// already carrying a non-NULL origin: the backfill must never revisit them,
// even when their value would otherwise look like it "differs" from the
// registry default.
// SEM@0000000000000000000000000000000000000000: validate rows already stamped seeded or explicit are never revisited
func TestBackfillSystemSettingOrigin_AlreadyStampedRowsUntouched(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "ui.default_theme",
		Value:       "definitely-not-the-default",
		SettingType: models.SystemSettingTypeString,
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginSeeded, Valid: true},
		ModifiedAt:  time.Now(),
	}).Error)
	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "upload.max_file_size_mb",
		Value:       "10", // matches the registry default, but already explicit
		SettingType: models.SystemSettingTypeInt,
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated, "already-stamped rows must not be touched")

	seededRow := getSystemSetting(t, db, "ui.default_theme")
	assert.Equal(t, models.SystemSettingOriginSeeded, seededRow.Origin.String)

	explicitRow := getSystemSetting(t, db, "upload.max_file_size_mb")
	assert.True(t, explicitRow.IsExplicit())
}

// TestBackfillSystemSettingOrigin_Idempotent covers running the backfill
// twice: the second pass must find nothing left to stamp and must not
// change any row the first pass already resolved.
// SEM@0000000000000000000000000000000000000000: validate running the backfill twice produces the same result as running it once
func TestBackfillSystemSettingOrigin_Idempotent(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)
	modifier := uuid.NewString()

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "websocket.max_participants",
		Value:       "10",
		SettingType: models.SystemSettingTypeInt,
		ModifiedBy:  models.NullableDBVarchar{String: modifier, Valid: true},
		ModifiedAt:  time.Now(),
	}).Error)
	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "rate_limit.requests_per_minute",
		Value:       "100",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	firstUpdated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), firstUpdated)

	explicitAfterFirst := getSystemSetting(t, db, "websocket.max_participants")
	seededAfterFirst := getSystemSetting(t, db, "rate_limit.requests_per_minute")

	secondUpdated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), secondUpdated, "a second pass must find nothing left to stamp")

	explicitAfterSecond := getSystemSetting(t, db, "websocket.max_participants")
	seededAfterSecond := getSystemSetting(t, db, "rate_limit.requests_per_minute")
	assert.Equal(t, explicitAfterFirst.Origin, explicitAfterSecond.Origin)
	assert.Equal(t, seededAfterFirst.Origin, seededAfterSecond.Origin)
}

// TestBackfillSystemSettingOrigin_NoRows covers the empty-table fast path:
// no NULL-origin rows at all must be a cheap no-op, not an error.
// SEM@0000000000000000000000000000000000000000: validate the backfill no-ops cleanly against an empty system_settings table
func TestBackfillSystemSettingOrigin_NoRows(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	updated, err := BackfillSystemSettingOrigin(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated)
}

// TestEnsureSystemSettingOriginCheckConstraint_NoOpOnSQLite documents and
// pins the SQLite short-circuit: SQLite has no ALTER TABLE ADD CONSTRAINT
// form, so the function must return cleanly without attempting any DDL
// against the test-fixture dialect used throughout this package's tests.
// SEM@0000000000000000000000000000000000000000: validate the origin CHECK constraint installer no-ops cleanly on SQLite
func TestEnsureSystemSettingOriginCheckConstraint_NoOpOnSQLite(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)
	require.NoError(t, EnsureSystemSettingOriginCheckConstraint(db))
	// Calling it again must remain a harmless no-op.
	require.NoError(t, EnsureSystemSettingOriginCheckConstraint(db))
}
