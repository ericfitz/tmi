package dbschema

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// deref returns the underlying struct reflect.Type for a (possibly pointer) model.
func deref(m any) reflect.Type {
	t := reflect.TypeOf(m)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// joinSigs joins field signatures for substring assertions.
func joinSigs(sigs []string) string { return strings.Join(sigs, "\n") }

// Models used only by these tests to exercise the fingerprint logic.
type fpUser struct {
	ID   string `gorm:"primaryKey;size:36"`
	Name string `gorm:"size:100;index:idx_fp_name"`
}

type fpUserReordered struct {
	Name string `gorm:"size:100;index:idx_fp_name"`
	ID   string `gorm:"primaryKey;size:36"`
}

type fpUserChangedTag struct {
	ID   string `gorm:"primaryKey;size:36"`
	Name string `gorm:"size:200;index:idx_fp_name"` // size 100 -> 200
}

type fpBase struct {
	CreatedAt time.Time `gorm:"not null"`
}

type fpEmbedder struct {
	fpBase
	ID string `gorm:"primaryKey;size:36"`
}

// TestComputeModelsFingerprint_Deterministic verifies the fingerprint is stable
// across calls and independent of struct field order and model slice order.
func TestComputeModelsFingerprint_Deterministic(t *testing.T) {
	a := ComputeModelsFingerprint(&fpUser{})
	b := ComputeModelsFingerprint(&fpUser{})
	require.Equal(t, a, b, "fingerprint must be stable across calls")
	require.Len(t, a, 64, "sha256 hex is 64 chars")
}

// TestComputeModelsFingerprint_FieldOrderInsensitive verifies reordering the
// fields of a struct does not change the fingerprint (column order is not a
// migration trigger we care about).
func TestComputeModelsFingerprint_FieldOrderInsensitive(t *testing.T) {
	// Same type name + same fields, different declaration order. Use a single
	// model each so the only difference is field order; type names differ, so
	// to isolate field-order we compare the field-signature set directly.
	require.Equal(t,
		fieldSignaturesSorted(&fpUser{}),
		fieldSignaturesSorted(&fpUserReordered{}),
		"reordering fields must not change the field signature set",
	)
}

// TestComputeModelsFingerprint_ModelOrderInsensitive verifies the order of the
// model slice does not change the fingerprint.
func TestComputeModelsFingerprint_ModelOrderInsensitive(t *testing.T) {
	a := ComputeModelsFingerprint(&fpUser{}, &fpEmbedder{})
	b := ComputeModelsFingerprint(&fpEmbedder{}, &fpUser{})
	require.Equal(t, a, b)
}

// TestComputeModelsFingerprint_SensitiveToTagChange verifies a migratable
// change (a column size change via the gorm tag) changes the fingerprint.
func TestComputeModelsFingerprint_SensitiveToTagChange(t *testing.T) {
	before := fieldSignaturesSorted(&fpUser{})
	after := fieldSignaturesSorted(&fpUserChangedTag{})
	require.NotEqual(t, before, after, "a size: tag change must change the signatures")
}

// TestComputeModelsFingerprint_FlattensEmbeds verifies anonymous embedded
// structs contribute their columns to the fingerprint.
func TestComputeModelsFingerprint_FlattensEmbeds(t *testing.T) {
	sigs := fieldSignaturesSorted(&fpEmbedder{})
	require.Contains(t, joinSigs(sigs), "CreatedAt|time.Time|not null",
		"embedded struct's column must appear in the signature")
}

// fieldSignaturesSorted is a test helper that returns the flattened, sorted
// field signatures for a single model.
func fieldSignaturesSorted(m any) []string {
	return fieldSignatures(deref(m))
}

// TestSchemaFingerprintRoundTrip verifies record -> current against a real
// (sqlite) database, including the missing-table and missing-row fast-path
// fallbacks.
func TestSchemaFingerprintRoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	fp := ComputeModelsFingerprint(&fpUser{})

	// No stamp table / no row yet: must report "not current" (fall back to migrate).
	require.False(t, SchemaFingerprintCurrent(db, fp), "no row yet -> not current")

	// Record it, then the same fingerprint must read back as current.
	require.NoError(t, RecordSchemaFingerprint(db, fp))
	require.True(t, SchemaFingerprintCurrent(db, fp), "recorded fingerprint must read back as current")

	// A different fingerprint must report not current.
	require.False(t, SchemaFingerprintCurrent(db, "deadbeef"), "different fingerprint -> not current")

	// Re-recording (upsert) must not error and must update the value.
	require.NoError(t, RecordSchemaFingerprint(db, "deadbeef"))
	require.True(t, SchemaFingerprintCurrent(db, "deadbeef"))
	require.False(t, SchemaFingerprintCurrent(db, fp))

	// Exactly one row exists (upsert, not insert-twice).
	var count int64
	require.NoError(t, db.Model(&schemaVersion{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
