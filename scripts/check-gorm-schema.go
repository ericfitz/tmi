//go:build ignore

// Quick test to check GORM schema for models.Threat

package main

import (
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	// Use SQLite in memory - we just need to parse the schema
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		fmt.Printf("Failed to open SQLite: %v\n", err)
		return
	}

	// Parse the Threat schema
	fmt.Println("=== PARSING models.Threat SCHEMA ===")
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&models.Threat{}); err != nil {
		fmt.Printf("ERROR parsing schema: %v\n", err)
		return
	}

	schema := stmt.Schema
	fmt.Printf("Table Name: %s\n", schema.Table)
	fmt.Printf("Primary Fields: ")
	for _, f := range schema.PrimaryFields {
		fmt.Printf("%s ", f.DBName)
	}
	fmt.Println()

	fmt.Printf("\nFieldsWithDefaultDBValue (%d fields):\n", len(schema.FieldsWithDefaultDBValue))
	for _, f := range schema.FieldsWithDefaultDBValue {
		fmt.Printf("  - %s (DBName: %s, HasDefaultValue: %v, AutoCreateTime: %d, AutoUpdateTime: %d)\n",
			f.Name, f.DBName, f.HasDefaultValue, f.AutoCreateTime, f.AutoUpdateTime)
	}

	if len(schema.FieldsWithDefaultDBValue) > 0 {
		fmt.Println()
		fmt.Println("WARNING: FieldsWithDefaultDBValue > 0")
		fmt.Println("Some Oracle drivers may use RETURNING INTO for these fields.")
	} else {
		fmt.Println()
		fmt.Println("GOOD: No FieldsWithDefaultDBValue.")
	}

	// Also check ThreatModel for comparison
	fmt.Println("\n=== PARSING models.ThreatModel SCHEMA (for comparison) ===")
	stmt2 := &gorm.Statement{DB: db}
	if err := stmt2.Parse(&models.ThreatModel{}); err != nil {
		fmt.Printf("ERROR parsing schema: %v\n", err)
		return
	}

	schema2 := stmt2.Schema
	fmt.Printf("Table Name: %s\n", schema2.Table)
	fmt.Printf("FieldsWithDefaultDBValue (%d fields):\n", len(schema2.FieldsWithDefaultDBValue))
	for _, f := range schema2.FieldsWithDefaultDBValue {
		fmt.Printf("  - %s (DBName: %s, HasDefaultValue: %v, AutoCreateTime: %d, AutoUpdateTime: %d)\n",
			f.Name, f.DBName, f.HasDefaultValue, f.AutoCreateTime, f.AutoUpdateTime)
	}
}
