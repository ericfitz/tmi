//go:build ignore

// Drop all tables in Oracle ADB

package main

import (
	"fmt"
	"os"
	"regexp"

	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// oracleIdentifier matches a syntactically valid unquoted Oracle identifier:
// a letter followed by up to 127 letters, digits, or the characters _ $ #.
// Table names returned by USER_TABLES (Oracle's own catalog, not user input)
// always satisfy this; the check is a defense-in-depth guard before the name
// is interpolated into a DROP TABLE statement, not a TMI-schema membership test.
var oracleIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]{0,127}$`)

// isValidOracleIdentifier reports whether name is a well-formed Oracle table
// identifier safe to interpolate into DDL. This intentionally does NOT check
// the name against a hand-maintained list of TMI tables: this is a dev/test
// reset tool that drops every table the connected schema owns, and a stale
// whitelist silently leaves tables behind, producing a non-fresh "fresh"
// schema (see #412).
func isValidOracleIdentifier(name string) bool {
	return oracleIdentifier.MatchString(name)
}

func main() {
	password := os.Getenv("ORACLE_PASSWORD")
	if password == "" {
		fmt.Println("ERROR: ORACLE_PASSWORD environment variable not set")
		os.Exit(1)
	}

	connectString := os.Getenv("ORACLE_CONNECT_STRING")
	if connectString == "" {
		fmt.Println("ERROR: ORACLE_CONNECT_STRING environment variable not set")
		os.Exit(1)
	}

	// Connect to Oracle. timezone=UTC keeps godror from emitting a warning when
	// the local host TZ differs from the database's SYSTIMESTAMP offset, and
	// matches the UTC session timezone the TMI server itself configures.
	dsn := fmt.Sprintf(`user=ADMIN password="%s" connectString=%s timezone=UTC`, password, connectString)
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

	// Drop every table the connected schema owns. The name is validated as a
	// well-formed Oracle identifier (defense-in-depth) before interpolation.
	for _, table := range tables {
		if !isValidOracleIdentifier(table) {
			fmt.Printf("Skipping table with non-identifier name: %q\n", table)
			continue
		}
		fmt.Printf("Dropping table: %s\n", table)
		if err := db.Exec(fmt.Sprintf("DROP TABLE \"%s\" CASCADE CONSTRAINTS PURGE", table)).Error; err != nil {
			fmt.Printf("  Warning: Failed to drop %s: %v\n", table, err)
		}
	}

	fmt.Println("Done!")
}
