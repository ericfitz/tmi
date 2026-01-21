//go:build ignore

// Drop all tables in Oracle ADB

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// validOracleTableNames contains uppercase versions of all valid TMI table names.
// Oracle stores table names in uppercase by default.
// This whitelist prevents SQL injection via table name parameters.
var validOracleTableNames = map[string]bool{
	"USERS":                   true,
	"THREAT_MODELS":           true,
	"DIAGRAMS":                true,
	"THREATS":                 true,
	"DOCUMENTS":               true,
	"METADATA":                true,
	"CLIENT_CREDENTIALS":      true,
	"WEBHOOK_SUBSCRIPTIONS":   true,
	"WEBHOOK_DELIVERIES":      true,
	"WEBHOOK_QUOTAS":          true,
	"WEBHOOK_URL_DENY_LISTS":  true,
	"ADDON_INVOCATION_QUOTAS": true,
	"ADDONS":                  true,
	"USER_API_QUOTAS":         true,
	"ADMINISTRATORS":          true,
	"GROUP_MEMBERS":           true,
	"THREAT_MODEL_ACCESS":     true,
	"REPOSITORIES":            true,
	"NOTES":                   true,
	"ASSETS":                  true,
	"COLLABORATION_SESSIONS":  true,
	"SESSION_PARTICIPANTS":    true,
	"REFRESH_TOKEN_RECORDS":   true,
	"GROUPS":                  true,
	"SCHEMA_MIGRATIONS":       true,
}

// isValidOracleTableName checks if an Oracle table name is in the allowed whitelist
func isValidOracleTableName(tableName string) bool {
	return validOracleTableNames[strings.ToUpper(tableName)]
}

func main() {
	password := os.Getenv("ORACLE_PASSWORD")
	if password == "" {
		fmt.Println("ERROR: ORACLE_PASSWORD environment variable not set")
		os.Exit(1)
	}

	// Connect to Oracle
	dsn := fmt.Sprintf(`user=ADMIN password="%s" connectString=tmiadb_medium`, password)
	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Printf("Failed to connect to Oracle: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Connected to Oracle ADB")

	// Get all table names
	var tables []string
	result := db.Raw("SELECT table_name FROM user_tables").Scan(&tables)
	if result.Error != nil {
		fmt.Printf("Failed to list tables: %v\n", result.Error)
		os.Exit(1)
	}

	fmt.Printf("Found %d tables\n", len(tables))

	// Drop each table (only if it's in our whitelist to prevent SQL injection)
	for _, table := range tables {
		if !isValidOracleTableName(table) {
			fmt.Printf("Skipping unknown table: %s (not in TMI schema whitelist)\n", table)
			continue
		}
		fmt.Printf("Dropping table: %s\n", table)
		// Table name is validated against whitelist, safe to use in query
		if err := db.Exec(fmt.Sprintf("DROP TABLE \"%s\" CASCADE CONSTRAINTS PURGE", table)).Error; err != nil {
			fmt.Printf("  Warning: Failed to drop %s: %v\n", table, err)
		}
	}

	fmt.Println("Done!")
}
