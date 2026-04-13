// cmd/dbtool/health.go
package main

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runHealthCheck connects to the database and reports schema health.
func runHealthCheck(db *testdb.TestDB, _ bool) error {
	log := slogging.Get()

	sqlDB, err := db.DB().DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	health, err := dbcheck.CheckSchemaHealth(sqlDB, db.DialectName())
	if err != nil {
		return fmt.Errorf("failed to check schema health: %w", err)
	}

	// Database info
	log.Info("Database: %s", health.DatabaseType)
	log.Info("Version:  %s", health.DatabaseVersion)
	log.Info("")

	// Schema status
	log.Info("Schema Status:")
	log.Info("  Expected tables: %d", health.ExpectedTables)
	log.Info("  Present tables:  %d", health.PresentTables)

	if health.IsCurrent() {
		log.Info("  Status: CURRENT")
	} else {
		log.Info("  Status: NEEDS MIGRATION (%d tables missing)", len(health.MissingTables))
		for _, t := range health.MissingTables {
			log.Info("    - %s", t)
		}
	}

	// System data status
	log.Info("")
	log.Info("System Data:")
	checkSystemDataHealth(db, log)

	if !health.IsCurrent() {
		log.Info("")
		log.Info("To migrate: tmi-dbtool --schema --config=<config-file>")
		return fmt.Errorf("schema needs migration: %d tables missing", len(health.MissingTables))
	}

	return nil
}

// checkSystemDataHealth checks whether required system data exists.
func checkSystemDataHealth(db *testdb.TestDB, log *slogging.Logger) {
	// Check built-in groups (provider = "tmi" are built-in)
	var groupCount int64
	if err := db.DB().Table("groups").Where("provider = ?", "tmi").Count(&groupCount).Error; err != nil {
		log.Info("  Built-in groups: unable to query (%v)", err)
	} else {
		// There are 7 built-in groups defined in api/seed/seed.go
		expected := 7
		if int(groupCount) >= expected {
			log.Info("  Built-in groups: %d/%d present", groupCount, expected)
		} else {
			log.Info("  Built-in groups: %d/%d present (INCOMPLETE)", groupCount, expected)
		}
	}

	// Check webhook deny list
	var denyCount int64
	if err := db.DB().Table("webhook_url_deny_list").Count(&denyCount).Error; err != nil {
		log.Info("  Webhook deny list: unable to query (%v)", err)
	} else if denyCount > 0 {
		log.Info("  Webhook deny list: %d entries", denyCount)
	} else {
		log.Info("  Webhook deny list: EMPTY (needs seeding)")
	}
}
