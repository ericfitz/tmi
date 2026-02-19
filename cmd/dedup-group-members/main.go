// Package main implements a one-off utility to remove duplicate rows from the
// group_members table. Run this before deploying the unique index migration
// (idx_gm_group_user_type) on databases that may contain duplicate memberships
// created by the auto-promotion race condition.
//
// Usage:
//
//	go run ./cmd/dedup-group-members --config=config-development.yml
//	go run ./cmd/dedup-group-members --config=config-development.yml --dry-run
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	_ "github.com/jackc/pgx/v4/stdlib"
	"gorm.io/gorm"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configFile = flag.String("config", "config-development.yml", "Path to configuration file")
		dryRun     = flag.Bool("dry-run", false, "Report duplicates without deleting them")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Initialize logging
	logLevel := slogging.LogLevelInfo
	if *verbose {
		logLevel = slogging.LogLevelDebug
	}
	if err := slogging.Initialize(slogging.Config{
		Level:            logLevel,
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	log := slogging.Get()

	// Load configuration
	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Error("Failed to load config file %s: %v", *configFile, err)
		return 1
	}

	// Connect to database
	gormConfig, err := db.ParseDatabaseURL(cfg.Database.URL)
	if err != nil {
		log.Error("Failed to parse DATABASE_URL: %v", err)
		return 1
	}
	if cfg.Database.OracleWalletLocation != "" {
		gormConfig.OracleWalletLocation = cfg.Database.OracleWalletLocation
	}

	dbManager := db.NewManager()
	log.Info("Connecting to %s database...", string(gormConfig.Type))
	if err := dbManager.InitGorm(*gormConfig); err != nil {
		log.Error("Failed to connect to database: %v", err)
		return 1
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			log.Error("Error closing database manager: %v", err)
		}
	}()

	gormDB := dbManager.Gorm()
	if gormDB == nil {
		log.Error("GORM database not initialized")
		return 1
	}

	log.Info("Connected to %s database", string(gormConfig.Type))

	// Run dedup
	removed, err := deduplicateGroupMembers(gormDB.DB(), *dryRun)
	if err != nil {
		log.Error("Deduplication failed: %v", err)
		return 1
	}

	switch {
	case *dryRun:
		log.Info("Dry run complete — %d duplicate rows would be removed", removed)
	case removed > 0:
		log.Info("Done — removed %d duplicate group membership rows", removed)
	default:
		log.Info("No duplicate group memberships found")
	}
	return 0
}

// deduplicateGroupMembers finds and removes duplicate (group, user, subject_type)
// rows in group_members, keeping the earliest row by added_at.
func deduplicateGroupMembers(gormDB *gorm.DB, dryRun bool) (int64, error) {
	log := slogging.Get()

	// Check if the table exists
	if !gormDB.Migrator().HasTable("group_members") {
		return 0, fmt.Errorf("group_members table does not exist")
	}

	type dupGroup struct {
		GroupInternalUUID string `gorm:"column:group_internal_uuid"`
		UserInternalUUID  string `gorm:"column:user_internal_uuid"`
		SubjectType       string `gorm:"column:subject_type"`
		Count             int64  `gorm:"column:cnt"`
	}

	var dups []dupGroup
	err := gormDB.Raw(`
		SELECT group_internal_uuid, user_internal_uuid, subject_type, COUNT(*) AS cnt
		FROM group_members
		WHERE user_internal_uuid IS NOT NULL
		GROUP BY group_internal_uuid, user_internal_uuid, subject_type
		HAVING COUNT(*) > 1
	`).Scan(&dups).Error
	if err != nil {
		return 0, fmt.Errorf("failed to find duplicate group memberships: %w", err)
	}

	if len(dups) == 0 {
		return 0, nil
	}

	totalRemoved := int64(0)
	for _, dup := range dups {
		log.Info("  group=%s user=%s type=%s: %d rows (keeping 1)",
			dup.GroupInternalUUID, dup.UserInternalUUID, dup.SubjectType, dup.Count)

		if dryRun {
			totalRemoved += dup.Count - 1
			continue
		}

		// Find the ID of the earliest row to keep
		var keepID string
		err := gormDB.Table("group_members").
			Select("id").
			Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?",
				dup.GroupInternalUUID, dup.UserInternalUUID, dup.SubjectType).
			Order("added_at ASC").
			Limit(1).
			Scan(&keepID).Error
		if err != nil {
			return totalRemoved, fmt.Errorf("failed to find earliest membership for group %s, user %s: %w",
				dup.GroupInternalUUID, dup.UserInternalUUID, err)
		}

		result := gormDB.Exec(`
			DELETE FROM group_members
			WHERE group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ? AND id != ?
		`, dup.GroupInternalUUID, dup.UserInternalUUID, dup.SubjectType, keepID)
		if result.Error != nil {
			return totalRemoved, fmt.Errorf("failed to delete duplicate memberships for group %s, user %s: %w",
				dup.GroupInternalUUID, dup.UserInternalUUID, result.Error)
		}
		totalRemoved += result.RowsAffected
	}

	return totalRemoved, nil
}
