//go:build ignore

// Drop all tables in Oracle ADB

package main

import (
	"fmt"
	"os"

	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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

	// Drop each table
	for _, table := range tables {
		fmt.Printf("Dropping table: %s\n", table)
		if err := db.Exec(fmt.Sprintf("DROP TABLE \"%s\" CASCADE CONSTRAINTS PURGE", table)).Error; err != nil {
			fmt.Printf("  Warning: Failed to drop %s: %v\n", table, err)
		}
	}

	fmt.Println("Done!")
}
