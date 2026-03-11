// Package main implements a one-shot tool to delete all rows from the USERS table.
// Used to reset Oracle ADB before first-login so admin@tmi.local becomes the first user
// and automatically receives the administrator role.
//
// Required environment variables (set automatically by Dockerfile.server-oracle entrypoint):
//
//	TMI_DATABASE_URL          - Oracle connection URL (oracle://ADMIN:pass@tmidb_high)
//	TMI_ORACLE_WALLET_LOCATION - Path to extracted wallet directory (/tmp/wallet)
package main

import (
	"fmt"
	"os"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := slogging.Initialize(slogging.Config{
		Level:            slogging.LogLevelInfo,
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	log := slogging.Get()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		log.Error("TMI_DATABASE_URL environment variable is not set")
		return 1
	}

	gormConfig, err := db.ParseDatabaseURL(dbURL)
	if err != nil {
		log.Error("Failed to parse TMI_DATABASE_URL: %v", err)
		return 1
	}

	if walletLoc := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); walletLoc != "" {
		gormConfig.OracleWalletLocation = walletLoc
	}

	log.Info("Connecting to %s database...", string(gormConfig.Type))
	dbManager := db.NewManager()
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

	log.Info("Connected. Deleting all rows from USERS table...")
	result := gormDB.DB().Exec("DELETE FROM USERS")
	if result.Error != nil {
		log.Error("Failed to delete users: %v", result.Error)
		return 1
	}

	log.Info("Deleted %d rows from USERS table", result.RowsAffected)
	fmt.Printf("\nDone! Deleted %d user(s). Next login will be treated as first-user (admin).\n", result.RowsAffected)
	return 0
}
