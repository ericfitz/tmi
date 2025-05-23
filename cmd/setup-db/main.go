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

	// Get database configuration
	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "postgres")
	password := getEnv("POSTGRES_PASSWORD", "postgres")
	dbName := getEnv("POSTGRES_DB", "tmi")
	sslMode := getEnv("POSTGRES_SSLMODE", "disable")

	// First, try to create the database if it doesn't exist
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=%s",
		user, password, host, port, sslMode)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to postgres database: %v", err)
	}

	// Check if database exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		log.Printf("Warning: Could not check if database exists: %v", err)
	}

	if !exists {
		log.Printf("Creating database %s...", dbName)
		_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
		if err != nil {
			log.Printf("Warning: Could not create database (it may already exist): %v", err)
		}
	}
	if err := db.Close(); err != nil {
		log.Printf("Warning: Error closing initial database connection: %v", err)
	}

	// Now connect to the target database
	connStr = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	db, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Printf("Connected to database %s successfully", dbName)

	// Get SQL statements from the schema
	statements := dbschema.GenerateCreateTableSQL()

	// Execute each statement
	successCount := 0
	failureCount := 0

	for i, stmt := range statements {
		// Extract a name for logging
		name := extractStatementName(stmt)
		log.Printf("Executing: %s", name)

		_, err := db.Exec(stmt)
		if err != nil {
			log.Printf("  ❌ Failed: %v", err)
			log.Printf("  Statement %d: %s", i, stmt)
			failureCount++
			// Don't stop on errors, continue with other statements
		} else {
			log.Printf("  ✅ Success")
			successCount++
		}
	}

	log.Printf("\nDatabase setup completed!")
	log.Printf("Successful operations: %d", successCount)
	log.Printf("Failed operations: %d", failureCount)

	// List created tables
	rows, err := db.Query(`
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`)
	if err != nil {
		log.Printf("Warning: Could not list tables: %v", err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	fmt.Println("\nExisting tables:")
	tableCount := 0
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		fmt.Printf("  - %s\n", tableName)
		tableCount++
	}

	fmt.Printf("\nTotal tables: %d\n", tableCount)

	if failureCount > 0 {
		fmt.Println("\n⚠️  Some operations failed. Please check the logs above.")
		fmt.Println("This might be due to foreign key constraints.")
		fmt.Println("You may need to manually adjust the schema or run the script again.")
	}

	// Initialize logger for schema validation
	if err := logging.Initialize(logging.Config{
		Level:            logging.ParseLogLevel("info"),
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		log.Printf("Warning: Failed to initialize logger: %v", err)
	} else {
		logger := logging.Get()
		defer func() {
			if err := logger.Close(); err != nil {
				log.Printf("Error closing logger: %v", err)
			}
		}()

		// Validate the schema after setup
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("Validating database schema...")
		fmt.Println(strings.Repeat("=", 60))

		results, err := dbschema.ValidateSchema(db)
		if err != nil {
			logger.Error("Failed to validate schema: %v", err)
		} else {
			dbschema.LogValidationResults(results)

			// Check if all validations passed
			allValid := true
			for _, result := range results {
				if !result.Valid {
					allValid = false
					break
				}
			}

			if allValid {
				fmt.Println("\n✅ Database schema validation PASSED!")
			} else {
				fmt.Println("\n❌ Database schema validation FAILED!")
				fmt.Println("   Please review the errors above.")
			}
		}
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// extractStatementName extracts a descriptive name from a SQL statement
func extractStatementName(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	upper := strings.ToUpper(stmt)

	if strings.HasPrefix(upper, "CREATE EXTENSION") {
		return "Enable UUID extension"
	} else if strings.HasPrefix(upper, "CREATE TABLE") {
		// Extract table name
		parts := strings.Fields(stmt)
		for i, part := range parts {
			if strings.ToUpper(part) == "TABLE" && i+2 < len(parts) {
				tableName := strings.Trim(parts[i+2], "(")
				return fmt.Sprintf("Create %s table", tableName)
			}
		}
	} else if strings.HasPrefix(upper, "CREATE INDEX") || strings.HasPrefix(upper, "CREATE UNIQUE INDEX") {
		// Extract index name
		parts := strings.Fields(stmt)
		for i, part := range parts {
			if strings.ToUpper(part) == "INDEX" && i+2 < len(parts) {
				indexName := parts[i+2]
				return fmt.Sprintf("Create index %s", indexName)
			}
		}
	} else if strings.HasPrefix(upper, "INSERT INTO SCHEMA_MIGRATIONS") {
		return "Insert migration version"
	}

	// Default: return first few words
	if len(stmt) > 50 {
		return stmt[:50] + "..."
	}
	return stmt
}
