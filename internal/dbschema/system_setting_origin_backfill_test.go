package dbschema

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
)

// newSystemSettingOriginTestDB builds an in-memory SQLite database migrated
// with the real models.SystemSetting model. Unlike most other tests in this
// package, this one uses the real model directly rather than a test-local
// mirror: BackfillSystemSettingOrigin's logic is defined in terms of
// models.DefaultSystemSettings(), models.SystemSettingOriginExplicit, and
// SystemSetting.IsExplicit() themselves, so mirroring the struct would just
// duplicate DBText/NullableDBVarchar's Scan/Value behavior under test.
func newSystemSettingOriginTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemSetting{}))
	return db
}

// getSystemSetting reloads a single row by key, failing the test if it is
// not found.
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

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
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

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
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
func TestBackfillSystemSettingOrigin_EncryptedValueRelinquishesToSeeded(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "session.timeout_minutes", // registry default is "60"
		Value:       "ENC:v1:1:1:notarealciphertextblob",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
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
func TestBackfillSystemSettingOrigin_ValueDiffersStampsExplicit(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "9999", // registry default is "60"
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "session.timeout_minutes")
	assert.True(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_UnknownKeyStampsExplicit covers a
// NULL-origin row whose key SeedDefaults would never have inserted at all
// (e.g. an operator- or extension-created setting): with no registry default
// to compare against, it must be treated as showing operator intent.
func TestBackfillSystemSettingOrigin_UnknownKeyStampsExplicit(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "custom.not_in_registry",
		Value:       "anything",
		SettingType: models.SystemSettingTypeString,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)

	row := getSystemSetting(t, db, "custom.not_in_registry")
	assert.True(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_UntouchedDefaultStaysNil covers the
// negative case: a NULL-origin row that still holds the registry default
// with no modified_by must be left alone, so it continues to read as seeded.
func TestBackfillSystemSettingOrigin_UntouchedDefaultStaysNil(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "upload.max_file_size_mb",
		Value:       "10", // exact registry default
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated)

	row := getSystemSetting(t, db, "upload.max_file_size_mb")
	assert.False(t, row.Origin.Valid, "an untouched seeded default must stay NULL-origin")
	assert.False(t, row.IsExplicit())
}

// TestBackfillSystemSettingOrigin_AlreadyStampedRowsUntouched covers rows
// already carrying a non-NULL origin: the backfill must never revisit them,
// even when their value would otherwise look like it "differs" from the
// registry default.
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

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
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
		SettingKey:  "upload.max_file_size_mb",
		Value:       "10",
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  time.Now(),
	}).Error)

	firstUpdated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), firstUpdated)

	explicitAfterFirst := getSystemSetting(t, db, "websocket.max_participants")
	seededAfterFirst := getSystemSetting(t, db, "upload.max_file_size_mb")

	secondUpdated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), secondUpdated, "a second pass must find nothing left to stamp")

	explicitAfterSecond := getSystemSetting(t, db, "websocket.max_participants")
	seededAfterSecond := getSystemSetting(t, db, "upload.max_file_size_mb")
	assert.Equal(t, explicitAfterFirst.Origin, explicitAfterSecond.Origin)
	assert.Equal(t, seededAfterFirst.Origin, seededAfterSecond.Origin)
}

// TestBackfillSystemSettingOrigin_NoRows covers the empty-table fast path:
// no NULL-origin rows at all must be a cheap no-op, not an error.
func TestBackfillSystemSettingOrigin_NoRows(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)

	updated, err := BackfillSystemSettingOrigin(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), updated)
}

// TestEnsureSystemSettingOriginCheckConstraint_NoOpOnSQLite documents and
// pins the SQLite short-circuit: SQLite has no ALTER TABLE ADD CONSTRAINT
// form, so the function must return cleanly without attempting any DDL
// against the test-fixture dialect used throughout this package's tests.
func TestEnsureSystemSettingOriginCheckConstraint_NoOpOnSQLite(t *testing.T) {
	db := newSystemSettingOriginTestDB(t)
	require.NoError(t, EnsureSystemSettingOriginCheckConstraint(context.Background(), db))
	// Calling it again must remain a harmless no-op.
	require.NoError(t, EnsureSystemSettingOriginCheckConstraint(context.Background(), db))
}
