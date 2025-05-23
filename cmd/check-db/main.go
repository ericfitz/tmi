package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

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

	// Connect to the database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	fmt.Printf("✓ Successfully connected to database '%s'\n\n", dbName)

	// Check if tables exist
	tables := []string{
		"users",
		"user_providers",
		"threat_models",
		"threat_model_access",
		"threats",
		"diagrams",
		"schema_migrations",
	}

	fmt.Println("Checking tables:")
	allTablesExist := true
	for _, table := range tables {
		var exists bool
		query := `SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)`
		err := db.QueryRow(query, table).Scan(&exists)
		if err != nil {
			fmt.Printf("  ✗ Error checking table '%s': %v\n", table, err)
			allTablesExist = false
		} else if exists {
			// Get row count
			var count int
			countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
			db.QueryRow(countQuery).Scan(&count)
			fmt.Printf("  ✓ Table '%s' exists (rows: %d)\n", table, count)
		} else {
			fmt.Printf("  ✗ Table '%s' does not exist\n", table)
			allTablesExist = false
		}
	}

	fmt.Println("\nChecking indexes:")
	// Check some key indexes
	indexes := []string{
		"idx_users_email",
		"idx_user_providers_user_id",
		"idx_threat_models_owner_email",
		"idx_threat_model_access_threat_model_id",
	}

	for _, index := range indexes {
		var exists bool
		query := `SELECT EXISTS (
			SELECT FROM pg_indexes 
			WHERE schemaname = 'public' 
			AND indexname = $1
		)`
		err := db.QueryRow(query, index).Scan(&exists)
		if err != nil {
			fmt.Printf("  ✗ Error checking index '%s': %v\n", index, err)
		} else if exists {
			fmt.Printf("  ✓ Index '%s' exists\n", index)
		} else {
			fmt.Printf("  ✗ Index '%s' does not exist\n", index)
		}
	}

	if allTablesExist {
		fmt.Println("\n✅ Database schema is properly set up!")
	} else {
		fmt.Println("\n❌ Database schema is incomplete. Please run the setup script.")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
