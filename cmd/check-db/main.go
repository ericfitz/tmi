package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/logging"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(".env.dev"); err != nil {
		log.Printf("Warning: Could not load .env.dev: %v", err)
	}

	// Initialize logger for consistent logging
	if err := logging.Initialize(logging.Config{
		Level:            logging.ParseLogLevel("info"),
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	logger := logging.Get()
	defer func() {
		if err := logger.Close(); err != nil {
			log.Printf("Error closing logger: %v", err)
		}
	}()

	// Get database configuration
	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "postgres")
	password := getEnv("POSTGRES_PASSWORD", "postgres")
	dbName := getEnv("POSTGRES_DB", "tmi")
	sslMode := getEnv("POSTGRES_SSLMODE", "disable")

	// Connect to the database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Error closing database: %v", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		logger.Error("Failed to ping database: %v", err)
		os.Exit(1)
	}

	logger.Info("Successfully connected to database '%s'", dbName)

	// Validate schema using shared validation
	logger.Info("Starting database schema validation...")
	results, err := dbschema.ValidateSchema(db)
	if err != nil {
		logger.Error("Failed to validate schema: %v", err)
		os.Exit(1)
	}

	// Log validation results (this is already done by the validator)
	dbschema.LogValidationResults(results)

	// Check if all validations passed
	allValid := true
	errorCount := 0
	warningCount := 0

	for _, result := range results {
		if !result.Valid {
			allValid = false
			errorCount += len(result.Errors)
		}
		warningCount += len(result.Warnings)
	}

	// Print summary
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("VALIDATION SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	// Get row counts for each table
	fmt.Println("\nTable Row Counts:")
	tables := dbschema.GetExpectedSchema()
	// Create a whitelist of valid table names from our schema
	validTables := make(map[string]bool)
	for _, table := range tables {
		validTables[table.Name] = true
	}

	for _, table := range tables {
		// Validate table name against whitelist to prevent SQL injection
		if !validTables[table.Name] {
			fmt.Printf("  %-25s: Invalid table name\n", table.Name)
			continue
		}

		var count int
		// #nosec G201 - table name is validated against whitelist from our schema definition
		countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", table.Name)
		if err := db.QueryRow(countQuery).Scan(&count); err != nil {
			fmt.Printf("  %-25s: Error counting rows\n", table.Name)
		} else {
			fmt.Printf("  %-25s: %d rows\n", table.Name, count)
		}
	}

	fmt.Printf("\nValidation Results:\n")
	fmt.Printf("  Tables Checked: %d\n", len(results))
	fmt.Printf("  Errors Found:   %d\n", errorCount)
	fmt.Printf("  Warnings:       %d\n", warningCount)

	if allValid {
		fmt.Println("\n✅ Database schema validation PASSED!")
		fmt.Println("   All tables match the expected schema.")
	} else {
		fmt.Println("\n❌ Database schema validation FAILED!")
		fmt.Println("   Please review the errors above and run migrations or setup scripts.")
		os.Exit(1)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
