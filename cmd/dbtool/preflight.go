package main

import (
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// preflightSchemaVersion refuses to run a data operation against a database
// whose schema fingerprint (#480) does not match the model set this binary
// was compiled with (#807). The server owns migration and stamps the
// fingerprint at boot; dbtool only migrates on --schema, so running
// --import-config against a database the new server has not booted against
// used to fail deep inside a write with ORA-00904 / SQLSTATE 42703 instead of
// saying "deploy the server first".
//
// A missing stamp is ambiguous (pre-#480 database, or migrated by an older
// binary) and is a warning, not a failure. skip (--skip-schema-check) turns a
// mismatch into a warning for an operator who knows better: the fingerprint
// is an equality check over every model, so it also changes for columns the
// requested operation never touches, and it cannot tell "database older than
// tool" (breaks) from "tool older than database" (usually harmless).
//
// A matching stamp is not a full schema guarantee either: on Oracle the
// additive migrator never alters an existing column, so a size or nullability
// change can be stamped current while the column keeps its old shape
// (oracle-db-admin review). The preflight catches the common "column missing"
// deploy-ordering mistake, which is the one that recurs.
//
// Callers exempt --schema (the remedy) and the no-flag health check.
// SEM@0000000000000000000000000000000000000000: verify the database schema fingerprint matches this binary's models before a data operation, else fail with a remedy (reads DB)
func preflightSchemaVersion(db *gorm.DB, toolVersion string, skip bool) error {
	log := slogging.Get()
	expected := dbschema.ComputeModelsFingerprint(api.GetAllModels()...)

	stored, appliedAt, found, err := dbschema.ReadSchemaStamp(db)
	if err != nil {
		log.Warn("schema version preflight unavailable (%v); continuing without it", err)
		return nil
	}
	if !found {
		log.Warn("database has no schema fingerprint stamp (never migrated by a stamping server or tmi-dbtool --schema); cannot verify it matches tmi-dbtool %s (expected %s). Continuing.", toolVersion, short(expected))
		return nil
	}
	if stored == expected {
		log.Debug("schema fingerprint %s matches this binary (stamped %s)", short(stored), appliedAt.UTC().Format(time.RFC3339))
		return nil
	}

	msg := fmt.Sprintf(`database schema is not at the version this tool expects.

  database fingerprint: %s (stamped %s)
  expected fingerprint: %s (tmi-dbtool %s)

The TMI server owns schema migration and applies it at startup. Deploy and
start the server against this database first, then re-run tmi-dbtool. To
migrate with this tool instead, run: tmi-dbtool --schema --config <config>
To proceed anyway, re-run with --skip-schema-check.`,
		short(stored), appliedAt.UTC().Format(time.RFC3339), short(expected), toolVersion)

	if skip {
		log.Warn("--skip-schema-check: %s", msg)
		return nil
	}
	return fmt.Errorf("%s", msg)
}

// short abbreviates a fingerprint for display.
// SEM@0000000000000000000000000000000000000000: abbreviate a schema fingerprint for display (pure)
func short(fp string) string {
	if len(fp) > 12 {
		return fp[:12] + "…"
	}
	return fp
}
