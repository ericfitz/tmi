//go:build ignore

// Check users table in Oracle ADB

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
	fmt.Println()

	// Check users table structure
	fmt.Println("=== USERS TABLE COLUMNS ===")
	var columns []struct {
		ColumnName string `gorm:"column:COLUMN_NAME"`
		DataType   string `gorm:"column:DATA_TYPE"`
		Nullable   string `gorm:"column:NULLABLE"`
	}
	result := db.Raw("SELECT column_name, data_type, nullable FROM user_tab_columns WHERE table_name = 'USERS' ORDER BY column_id").Scan(&columns)
	if result.Error != nil {
		fmt.Printf("Failed to get columns: %v\n", result.Error)
	} else {
		for _, col := range columns {
			fmt.Printf("  %s: %s (nullable: %s)\n", col.ColumnName, col.DataType, col.Nullable)
		}
	}
	fmt.Println()

	// Check if there are any users
	fmt.Println("=== USERS TABLE DATA ===")
	var count int64
	db.Raw("SELECT COUNT(*) FROM USERS").Count(&count)
	fmt.Printf("User count: %d\n", count)

	if count > 0 {
		var users []map[string]interface{}
		db.Raw("SELECT * FROM USERS").Scan(&users)
		for i, user := range users {
			fmt.Printf("\nUser %d:\n", i+1)
			for k, v := range user {
				fmt.Printf("  %s: %v\n", k, v)
			}
		}
	}
}
