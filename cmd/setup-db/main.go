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
	db.Close()

	// Now connect to the target database
	connStr = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	db, err = sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Printf("Connected to database %s successfully", dbName)

	// Execute SQL statements in order
	statements := []struct {
		name string
		sql  string
	}{
		{
			name: "Create users table",
			sql: `CREATE TABLE IF NOT EXISTS users (
				id VARCHAR(36) PRIMARY KEY,
				email VARCHAR(255) UNIQUE NOT NULL,
				name VARCHAR(255) NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_login TIMESTAMP
			)`,
		},
		{
			name: "Create users email index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		},
		{
			name: "Create users last_login index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_users_last_login ON users(last_login)`,
		},
		{
			name: "Create user_providers table",
			sql: `CREATE TABLE IF NOT EXISTS user_providers (
				id VARCHAR(36) PRIMARY KEY,
				user_id VARCHAR(36) NOT NULL,
				provider VARCHAR(50) NOT NULL,
				provider_user_id VARCHAR(255) NOT NULL,
				email VARCHAR(255) NOT NULL,
				is_primary BOOLEAN DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_login TIMESTAMP,
				FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
				UNIQUE(user_id, provider)
			)`,
		},
		{
			name: "Create user_providers indexes",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_providers_user_id ON user_providers(user_id)`,
		},
		{
			name: "Create user_providers provider lookup index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_providers_provider_lookup ON user_providers(provider, provider_user_id)`,
		},
		{
			name: "Create user_providers email index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_providers_email ON user_providers(email)`,
		},
		{
			name: "Create threat_models table",
			sql: `CREATE TABLE IF NOT EXISTS threat_models (
				id VARCHAR(36) PRIMARY KEY,
				owner_email VARCHAR(255) NOT NULL,
				name VARCHAR(255) NOT NULL,
				description TEXT,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "Create threat_models owner_email index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threat_models_owner_email ON threat_models(owner_email)`,
		},
		{
			name: "Create threat_model_access table",
			sql: `CREATE TABLE IF NOT EXISTS threat_model_access (
				id VARCHAR(36) PRIMARY KEY,
				threat_model_id VARCHAR(36) NOT NULL,
				user_email VARCHAR(255) NOT NULL,
				role VARCHAR(50) NOT NULL CHECK (role IN ('owner', 'writer', 'reader')),
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (threat_model_id) REFERENCES threat_models(id) ON DELETE CASCADE,
				UNIQUE(threat_model_id, user_email)
			)`,
		},
		{
			name: "Create threat_model_access indexes",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threat_model_access_threat_model_id ON threat_model_access(threat_model_id)`,
		},
		{
			name: "Create threat_model_access user_email index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threat_model_access_user_email ON threat_model_access(user_email)`,
		},
		{
			name: "Create threat_model_access role index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threat_model_access_role ON threat_model_access(role)`,
		},
		{
			name: "Create threats table",
			sql: `CREATE TABLE IF NOT EXISTS threats (
				id VARCHAR(36) PRIMARY KEY,
				threat_model_id VARCHAR(36) NOT NULL,
				name VARCHAR(255) NOT NULL,
				description TEXT,
				severity VARCHAR(50),
				likelihood VARCHAR(50),
				risk_level VARCHAR(50),
				mitigation TEXT,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (threat_model_id) REFERENCES threat_models(id) ON DELETE CASCADE
			)`,
		},
		{
			name: "Create threats indexes",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threats_threat_model_id ON threats(threat_model_id)`,
		},
		{
			name: "Create threats severity index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threats_severity ON threats(severity)`,
		},
		{
			name: "Create threats risk_level index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_threats_risk_level ON threats(risk_level)`,
		},
		{
			name: "Create diagrams table",
			sql: `CREATE TABLE IF NOT EXISTS diagrams (
				id VARCHAR(36) PRIMARY KEY,
				threat_model_id VARCHAR(36) NOT NULL,
				name VARCHAR(255) NOT NULL,
				type VARCHAR(50),
				content TEXT,
				metadata JSONB,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (threat_model_id) REFERENCES threat_models(id) ON DELETE CASCADE
			)`,
		},
		{
			name: "Create diagrams indexes",
			sql:  `CREATE INDEX IF NOT EXISTS idx_diagrams_threat_model_id ON diagrams(threat_model_id)`,
		},
		{
			name: "Create diagrams type index",
			sql:  `CREATE INDEX IF NOT EXISTS idx_diagrams_type ON diagrams(type)`,
		},
		{
			name: "Create schema_migrations table",
			sql: `CREATE TABLE IF NOT EXISTS schema_migrations (
				version BIGINT NOT NULL PRIMARY KEY,
				dirty BOOLEAN NOT NULL DEFAULT FALSE
			)`,
		},
		{
			name: "Insert migration version",
			sql:  `INSERT INTO schema_migrations (version, dirty) VALUES (6, false) ON CONFLICT (version) DO NOTHING`,
		},
	}

	// Execute each statement
	successCount := 0
	failureCount := 0

	for _, stmt := range statements {
		log.Printf("Executing: %s", stmt.name)
		_, err := db.Exec(stmt.sql)
		if err != nil {
			log.Printf("  ❌ Failed: %v", err)
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
	defer rows.Close()

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
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
