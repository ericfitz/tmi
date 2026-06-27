// Package dbschema: schema-version fingerprint (#480).
//
// GORM AutoMigrate is introspection-heavy: for every model it issues a series
// of per-object existence checks (USER_TABLES / USER_INDEXES / USER_CONSTRAINTS
// on Oracle, the information_schema equivalents on PostgreSQL) before deciding
// what, if anything, to create. For the full TMI schema that is hundreds of
// sequential statements. Against a local database each is sub-millisecond and
// the cost is invisible; against a remote database (Oracle ADB) every one pays
// a full network round-trip (~300-900 ms observed), so a single AutoMigrate
// pass takes many minutes — long enough that the in-cluster server pod was
// killed by its liveness probe mid-migration and crash-looped (#479), and long
// enough to stretch every production pod start, rollout, and scale-up (#480).
//
// The fix is to skip the introspection pass when the schema is already current.
// On a successful migration we record a fingerprint of the model set in a tiny
// single-row table (tmi_schema_versions). On the next boot we compute the same
// fingerprint and, if it matches what is recorded, we skip AutoMigrate
// entirely — turning hundreds of round-trips into two (ensure the stamp table,
// read one row). The fingerprint changes iff a model's migratable definition
// changes, so a mismatch (or a missing stamp) conservatively falls back to a
// full AutoMigrate, which remains additive and idempotent (#474).
package dbschema

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// schemaVersionRowID is the primary key of the single stamp row. The table
// holds exactly one row; the fixed id makes the read and the upsert trivial.
const schemaVersionRowID = "current"

// schemaVersionTable is the stamp table name. Unquoted, so Oracle folds it to
// upper case (TMI_SCHEMA_VERSIONS); PostgreSQL/SQLite keep it lower case.
const schemaVersionTable = "tmi_schema_versions"

// schemaVersion is the single-row stamp table recording a fingerprint of the
// GORM model set that was last successfully migrated.
//
// Its content is ASCII-only (a fixed row id and a hex SHA-256), so plain string
// columns are safe across every dialect: there is no Oracle BYTE-vs-CHAR
// concern (#379) because each byte is exactly one character. Keeping the model
// local to this package (rather than reusing api/models types) avoids coupling
// the low-level dbschema package to the model layer.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: store the schema fingerprint and timestamp for the migration fast-path (pure)
type schemaVersion struct {
	ID          string    `gorm:"column:id;primaryKey;size:32"`
	Fingerprint string    `gorm:"column:fingerprint;size:64;not null"`
	AppliedAt   time.Time `gorm:"column:applied_at;not null"`
}

// TableName pins the table name so it is stable regardless of GORM's default
// pluralization rules across versions.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: return the fixed table name for the schema version model (pure)
func (schemaVersion) TableName() string { return schemaVersionTable }

var (
	timeType   = reflect.TypeOf(time.Time{})
	valuerType = reflect.TypeOf((*driver.Valuer)(nil)).Elem()
)

// ComputeModelsFingerprint returns a stable SHA-256 over the schema-relevant
// shape of the given GORM models: each model's type identity, its fields, the
// Go type of each field, and the field's `gorm` struct tag (which carries
// column name, type, size, index, unique, and constraint directives). It thus
// changes iff a model's migratable definition changes, so a recorded
// fingerprint equal to the freshly computed one means AutoMigrate would be a
// no-op. Model and field ordering are normalized (sorted) so merely reordering
// struct fields or the model slice does not change the result.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: compute a stable SHA-256 fingerprint over GORM model schemas (pure)
func ComputeModelsFingerprint(models ...any) string {
	parts := make([]string, 0, len(models))
	for _, m := range models {
		if m == nil {
			continue
		}
		t := reflect.TypeOf(m)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			continue
		}
		parts = append(parts, t.PkgPath()+"."+t.Name()+"{"+strings.Join(fieldSignatures(t), ",")+"}")
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// fieldSignatures returns a sorted list of per-field signatures for a struct
// type, flattening anonymous embedded structs (which contribute their own
// columns) but treating named column types (time.Time, anything implementing
// driver.Valuer such as DBVarchar / NullableDBText) as leaf columns.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: build sorted field GORM tag signatures for a struct type (pure)
func fieldSignatures(t reflect.Type) []string {
	var sigs []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Skip unexported, non-embedded fields: GORM never maps them.
		if f.PkgPath != "" && !f.Anonymous {
			continue
		}
		gormTag := f.Tag.Get("gorm")
		if gormTag == "-" {
			continue
		}
		if f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && !isColumnType(ft) {
				sigs = append(sigs, fieldSignatures(ft)...)
				continue
			}
		}
		sigs = append(sigs, f.Name+"|"+f.Type.String()+"|"+gormTag)
	}
	sort.Strings(sigs)
	return sigs
}

// isColumnType reports whether a struct type maps to a single column (and so
// must not be recursed into when encountered as an anonymous embed).
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: report whether a struct type maps to a single DB column (pure)
func isColumnType(t reflect.Type) bool {
	if t == timeType {
		return true
	}
	return t.Implements(valuerType) || reflect.PointerTo(t).Implements(valuerType)
}

// SchemaFingerprintCurrent reports whether the database already records a schema
// fingerprint equal to `desired`. When true, the caller may safely skip the
// introspection-heavy AutoMigrate pass (#480).
//
// Any error — the stamp table does not exist yet, the database user lacks DDL
// permission to create it, or no row has been written — is treated as "not
// current" so the caller falls back to running AutoMigrate. Such conditions are
// logged at debug, never surfaced, because an unavailable fast path must never
// turn into a failed boot.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: report whether the stored schema fingerprint matches the desired one (reads DB)
func SchemaFingerprintCurrent(db *gorm.DB, desired string) bool {
	stored, ok, err := readSchemaFingerprint(db)
	if err != nil {
		slogging.Get().Debug("schema fingerprint check unavailable (will run AutoMigrate): %v", err)
		return false
	}
	return ok && stored == desired
}

// readSchemaFingerprint ensures the stamp table exists and returns the recorded
// fingerprint, if any.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: fetch the stored schema fingerprint from the DB stamp table (reads DB)
func readSchemaFingerprint(db *gorm.DB) (string, bool, error) {
	if err := ensureSchemaVersionTable(db); err != nil {
		return "", false, err
	}
	var row schemaVersion
	err := db.Where("id = ?", schemaVersionRowID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Fingerprint, true, nil
}

// ensureSchemaVersionTable creates the single-row stamp table if it does not
// already exist. This is a one-table AutoMigrate (no indexes, no foreign keys),
// so even on a remote database it costs only a handful of round-trips — the
// cheap price that buys skipping the full-schema pass.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: create the schema version stamp table if absent (writes DB)
func ensureSchemaVersionTable(db *gorm.DB) error {
	// Probe for the table directly before attempting AutoMigrate. gorm-oracle's
	// Migrator().HasTable matches the literal lower-case name and misses the
	// upper-folded TMI_SCHEMA_VERSIONS, so an unconditional AutoMigrate would
	// re-issue CREATE TABLE on every call — a swallowed but ERROR-logged
	// ORA-00955 on every boot. A direct COUNT (unquoted, so it folds to the
	// same case the table was created with) detects the existing table on every
	// dialect; a missing table errors and falls through to AutoMigrate.
	//
	// Run the probe under a silenced logger: a genuinely-absent table is the
	// expected first-boot case and emits a hard error (Oracle ORA-00942, PG
	// 42P01), which would otherwise surface as a spurious ERROR line and trip
	// error-based alerting (the OKE log pipeline) on every first boot. Use
	// gorm's built-in logger at Silent level rather than db.Logger.LogMode():
	// TMI's custom gormLogger ignores LogMode (it returns itself unchanged), so
	// that route would not actually silence the probe.
	probe := db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	var n int64
	if err := probe.Table(schemaVersionTable).Count(&n).Error; err == nil {
		return nil
	}
	if err := db.AutoMigrate(&schemaVersion{}); err != nil {
		// Belt-and-suspenders for a concurrent create (two processes racing the
		// AutoMigrate): the loser sees ORA-00955 ("name already used by an
		// existing object"); the table is in fact present, so treat as success.
		if strings.Contains(err.Error(), "ORA-00955") {
			return nil
		}
		return err
	}
	return nil
}

// RecordSchemaFingerprint upserts the single stamp row to `fp`. Callers invoke
// it only after a successful AutoMigrate, so a subsequent boot computing the
// same fingerprint can take the fast path. The upsert is portable across
// dialects (update-then-insert rather than ON CONFLICT / MERGE); server callers
// run it under the cross-replica migration advisory lock, so the update/insert
// pair cannot race a second writer.
// SEM@70c02e3f4b4dd833280d8f3ca9d152b483013ffe: store the schema fingerprint in the DB stamp table after migration (writes DB)
func RecordSchemaFingerprint(db *gorm.DB, fp string) error {
	if err := ensureSchemaVersionTable(db); err != nil {
		return err
	}
	now := time.Now().UTC()
	res := db.Model(&schemaVersion{}).
		Where("id = ?", schemaVersionRowID).
		Updates(map[string]any{"fingerprint": fp, "applied_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return db.Create(&schemaVersion{ID: schemaVersionRowID, Fingerprint: fp, AppliedAt: now}).Error
	}
	return nil
}
