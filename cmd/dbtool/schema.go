// cmd/dbtool/schema.go
package main

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSchema runs schema creation/migration, post-migration fixups, and system data seeding.
func runSchema(db *testdb.TestDB, dryRun, verbose bool) error {
	log := slogging.Get()

	if dryRun {
		return runSchemaDryRun(db, verbose)
	}

	// Step 1: AutoMigrate
	log.Info("Running GORM AutoMigrate...")
	allModels := api.GetAllModels()
	if err := db.DB().AutoMigrate(allModels...); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
		} else {
			return fmt.Errorf("AutoMigrate failed: %w", err)
		}
	}
	log.Info("AutoMigrate completed for %d models", len(allModels))

	// Step 2: Post-migration fixups
	log.Info("Running post-migration fixups...")
	runPostMigrationFixups(db, verbose)

	// Step 3: Seed system data
	log.Info("Seeding system data (groups, webhook deny list)...")
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return fmt.Errorf("failed to seed system data: %w", err)
	}

	log.Info("Schema migration and system seed complete")
	return nil
}

// runSchemaDryRun reports what schema changes would be made without writing.
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

// runPostMigrationFixups runs data fixups that should happen after schema migration.
func runPostMigrationFixups(db *testdb.TestDB, verbose bool) {
	log := slogging.Get()

	if result := db.DB().Exec(
		"UPDATE threats SET severity = LOWER(severity) WHERE severity IS NOT NULL AND severity != LOWER(severity)",
	); result.Error != nil {
		log.Warn("Failed to normalize severity values (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Info("Normalized %d severity values to lowercase", result.RowsAffected)
	} else if verbose {
		log.Debug("Severity values already normalized")
	}

	if result := db.DB().Exec(
		"UPDATE threats SET severity = 'informational' WHERE severity = 'none'",
	); result.Error != nil {
		log.Warn("Failed to migrate 'none' severity to 'informational' (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Info("Migrated %d severity values from 'none' to 'informational'", result.RowsAffected)
	} else if verbose {
		log.Debug("No 'none' severity values to migrate")
	}
}
