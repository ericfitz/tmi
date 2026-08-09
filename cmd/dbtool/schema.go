// cmd/dbtool/schema.go
package main

import (
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
	log := slogging.Get()

	if dryRun {
		return runSchemaDryRun(db, verbose)
	}

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
