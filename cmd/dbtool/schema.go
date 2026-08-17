// cmd/dbtool/schema.go
package main

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSchema runs schema creation/migration and system data seeding.
// Uses a fresh-schema baseline: no ALTER path, no benign-error swallowing.
// If run against a non-empty schema that triggers an "object already exists"
// error, the error surfaces loudly — the operator should drop and recreate
// the schema first.
// SEM@550719956eba05c4b206ae4056df29fa2c66586a: migrate DB schema via GORM AutoMigrate and seed system data (mutates DB)
func runSchema(db *testdb.TestDB, dryRun, verbose bool) error {
	if dryRun {
		return runSchemaDryRun(db, verbose)
	}

	// #737: this issues the same DDL as a server boot's runMigrationsLocked
	// against the same schema, so it must contend for the same cross-replica
	// advisory lock. Without it, `tmi-dbtool --schema` run while a server is
	// booting reintroduces exactly the concurrent-DDL exposure (ORA-00054,
	// duplicate CREATE races) that #723/#726 added the lock to eliminate.
	//
	// Consequence worth knowing before running this against a live
	// deployment: the lock is held across AutoMigrate AND the system seed, so
	// a server booting meanwhile blocks on it. On Oracle that server gives up
	// after DBMS_LOCK.REQUEST's 300s timeout and exits (restart brings it
	// back once this finishes); on PostgreSQL pg_advisory_lock waits
	// indefinitely. Previously the two simply raced, which was worse
	// (oracle-db-admin review, #737).
	return dbschema.WithMigrationLock(context.Background(), db.DB(), dbschema.MigrationLockName, func() error {
		return runSchemaLocked(db)
	})
}

// runSchemaLocked performs the schema migration and system seed. Always called
// with the cross-replica migration advisory lock held (see runSchema).
func runSchemaLocked(db *testdb.TestDB) error {
	log := slogging.Get()

	// Step 1: AutoMigrate (Oracle-aware path via GormDB.AutoMigrate)
	log.Info("Running GORM AutoMigrate...")

	// #724: fail fast with an actionable error if users(provider,
	// provider_user_id) already carries duplicates -- idx_users_provider_lookup
	// is unique (#701), and AutoMigrate's CREATE UNIQUE INDEX would otherwise
	// abort with an opaque ORA-01452 / 23505. Users get no automatic dedupe
	// (merging identities is an operator decision).
	if err := dbschema.CheckDuplicateUserProviderIdentities(db.DB()); err != nil {
		return fmt.Errorf("pre-migration user identity check failed: %w", err)
	}

	// #704: dedupe groups(provider, group_name) before AutoMigrate creates
	// uniq_groups_provider_group_name -- mirrors the same placement in
	// cmd/server/main.go's runMigrationsLocked. A database carrying
	// pre-#704 duplicate rows (from the previously-unbacked ON CONFLICT/
	// MERGE target) would otherwise abort CREATE UNIQUE INDEX (ORA-01452 /
	// PostgreSQL 23505). No-op (a single query) when the groups table
	// doesn't exist yet or carries no duplicates.
	if removed, err := dbschema.DeduplicateGroups(db.DB()); err != nil {
		return fmt.Errorf("failed to dedupe groups before schema migration: %w", err)
	} else if removed > 0 {
		log.Info("Deduped %d duplicate groups rows before schema migration", removed)
	}

	if err := db.AutoMigrate(); err != nil {
		return fmt.Errorf("AutoMigrate failed: %w", err)
	}
	allModels := api.GetAllModels()
	log.Info("AutoMigrate completed for %d models", len(allModels))

	// Stamp the schema fingerprint so a (possibly limited-privilege) server
	// booting against this schema can skip its own introspection-heavy
	// AutoMigrate pass (#480). Non-fatal: a failure just means the server
	// re-runs AutoMigrate on first boot.
	if err := dbschema.RecordSchemaFingerprint(db.DB(), dbschema.ComputeModelsFingerprint(allModels...)); err != nil {
		log.Warn("failed to record schema fingerprint (non-fatal): %v", err)
	}

	// #732: upgrade a pre-#701 non-unique idx_users_provider_lookup to a
	// genuine UNIQUE index. This is the entry point most likely to succeed at
	// it -- dbtool runs with an admin-privileged database user, where a
	// DDL-less server runtime user can only log that the upgrade is needed.
	// Same placement and reasoning as cmd/server/main.go's runMigrationsLocked
	// and auth/config_adapter.go.
	if err := dbschema.EnsureUserProviderLookupUnique(db.DB()); err != nil {
		return fmt.Errorf("failed to check the users provider-lookup index: %w", err)
	}

	// That function warns-and-continues on a DDL failure so it can never
	// crash-loop a server boot. dbtool is the admin-privileged remediation
	// path, so here the outcome has to be checked: exiting 0 having silently
	// not upgraded the index is exactly the failure an operator ran this to
	// fix (oracle-db-admin review, #732).
	unique, err := dbschema.UserProviderLookupIndexIsUnique(db.DB())
	if err != nil {
		return fmt.Errorf("failed to verify the users provider-lookup index: %w", err)
	}
	if !unique {
		return fmt.Errorf(
			"idx_users_provider_lookup is not a valid UNIQUE index after migration; see the logged reason above. " +
				"The most common cause is pre-existing duplicate (provider, provider_user_id) rows, which must be merged or deleted before the index can be made unique (#732)")
	}

	// #720: unique sparse-email index (partial/function-based) is raw DDL
	// AutoMigrate cannot express; idempotent, same placement and reasoning as
	// cmd/server/main.go's runMigrationsLocked and auth/config_adapter.go.
	if err := dbschema.EnsureSparseUserEmailIndex(db.DB()); err != nil {
		return fmt.Errorf("failed to ensure sparse-user email index: %w", err)
	}

	// Step 2: Seed system data
	log.Info("Seeding system data (groups, webhook deny list)...")
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return fmt.Errorf("failed to seed system data: %w", err)
	}

	log.Info("Schema migration and system seed complete")
	return nil
}

// runSchemaDryRun reports what schema changes would be made without writing.
// SEM@7a8bf8de72c6d39387df00ec0eb9901653555db8: report schema health and missing tables without applying any changes (reads DB)
func runSchemaDryRun(db *testdb.TestDB, _ bool) error {
	log := slogging.Get()
	log.Info("[DRY RUN] Checking schema status...")

	sqlDB, err := db.DB().DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	health, err := dbcheck.CheckSchemaHealth(sqlDB, db.DialectName())
	if err != nil {
		return fmt.Errorf("failed to check schema health: %w", err)
	}

	log.Info("[DRY RUN] Database: %s %s", health.DatabaseType, health.DatabaseVersion)
	log.Info("[DRY RUN] Tables: %d/%d present", health.PresentTables, health.ExpectedTables)

	if health.IsCurrent() {
		log.Info("[DRY RUN] Schema is up to date. No migrations needed.")
	} else {
		log.Info("[DRY RUN] Missing tables (%d):", len(health.MissingTables))
		for _, t := range health.MissingTables {
			log.Info("[DRY RUN]   - %s", t)
		}
		log.Info("[DRY RUN] Running --schema would create these tables and seed system data.")
	}

	return nil
}
