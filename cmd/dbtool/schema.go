// cmd/dbtool/schema.go
package main

import (
	"fmt"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSchema runs schema creation/migration and system data seeding.
// Uses a fresh-schema baseline: no ALTER path, no benign-error swallowing.
// If run against a non-empty schema that triggers an "object already exists"
// error, the error surfaces loudly — the operator should drop and recreate
// the schema first.
// SEM@6415706e07613a139449e1bff6eef269e3783417: run GORM AutoMigrate and seed system data against the target database (mutates DB)
func runSchema(db *testdb.TestDB, dryRun, verbose bool) error {
	log := slogging.Get()

	if dryRun {
		return runSchemaDryRun(db, verbose)
	}

	// Step 1: AutoMigrate (Oracle-aware path via GormDB.AutoMigrate)
	log.Info("Running GORM AutoMigrate...")
	if err := db.AutoMigrate(); err != nil {
		return fmt.Errorf("AutoMigrate failed: %w", err)
	}
	log.Info("AutoMigrate completed for %d models", len(api.GetAllModels()))

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
