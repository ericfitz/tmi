package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/logging"
	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	// Command line flags
	var (
		envFile = flag.String("env", ".env.dev", "Path to environment file")
		down    = flag.Bool("down", false, "Run down migrations")
		steps   = flag.Int("steps", 0, "Number of migration steps (0 = all)")
	)
	flag.Parse()

	// Load environment variables
	if err := godotenv.Load(*envFile); err != nil {
		logging.Get().Info("Warning: Could not load env file %s: %v", *envFile, err)
	}

	// Create database configuration
	pgConfig := db.PostgresConfig{
		Host:     getEnv("POSTGRES_HOST", "localhost"),
		Port:     getEnv("POSTGRES_PORT", "5432"),
		User:     getEnv("POSTGRES_USER", "postgres"),
		Password: getEnv("POSTGRES_PASSWORD", "postgres"),
		Database: getEnv("POSTGRES_DB", "tmi"),
		SSLMode:  getEnv("POSTGRES_SSLMODE", "disable"),
	}

	// Create database manager
	dbManager := db.NewManager()

	// Initialize PostgreSQL connection
	logging.Get().Info("Connecting to PostgreSQL at %s:%s/%s", pgConfig.Host, pgConfig.Port, pgConfig.Database)
	if err := dbManager.InitPostgres(pgConfig); err != nil {
		logging.Get().Error("Failed to connect to PostgreSQL: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			logging.Get().Info("Error closing database manager: %v", err)
		}
	}()

	// Get migrations path - try multiple possible locations
	var migrationsPath string
	var absPath string

	possiblePaths := []string{
		filepath.Join("auth", "migrations"),             // From project root
		filepath.Join("..", "..", "auth", "migrations"), // From cmd/migrate
	}

	for _, path := range possiblePaths {
		if tempAbsPath, tempErr := filepath.Abs(path); tempErr == nil {
			if _, statErr := os.Stat(tempAbsPath); statErr == nil {
				migrationsPath = path
				absPath = tempAbsPath
				break
			}
		}
	}

	if migrationsPath == "" {
		logging.Get().Error("Could not find migrations directory. Tried paths: %v", possiblePaths)
		os.Exit(1)
	}

	logging.Get().Info("Using migrations from: %s", absPath)

	// Create migration config
	migrationConfig := db.MigrationConfig{
		MigrationsPath: migrationsPath,
		DatabaseName:   pgConfig.Database,
	}

	// Run migrations based on flags
	if *down {
		logging.Get().Info("Running down migrations...")
		if err := dbManager.MigrateDown(migrationConfig); err != nil {
			logging.Get().Error("Failed to run down migrations: %v", err)
			os.Exit(1)
		}
		logging.Get().Info("Down migrations completed successfully")
	} else if *steps != 0 {
		logging.Get().Info("Running %d migration steps...", *steps)
		if err := dbManager.MigrateStep(migrationConfig, *steps); err != nil {
			logging.Get().Error("Failed to run migration steps: %v", err)
			os.Exit(1)
		}
		logging.Get().Info("%d migration steps completed successfully", *steps)
	} else {
		logging.Get().Info("Running all pending migrations...")
		if err := dbManager.RunMigrations(migrationConfig); err != nil {
			logging.Get().Error("Failed to run migrations: %v", err)
			os.Exit(1)
		}
		logging.Get().Info("All migrations completed successfully")
	}

	fmt.Println("\nDatabase migration complete!")

	// Only validate schema if we're not rolling back
	if !*down {
		validateSchema(pgConfig)
	}
}

// validateSchema validates the database schema after migrations
func validateSchema(pgConfig db.PostgresConfig) {
	// Initialize logger for schema validation
	if err := logging.Initialize(logging.Config{
		Level:            logging.ParseLogLevel("info"),
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		logging.Get().Info("Warning: Failed to initialize logger: %v", err)
		return
	}
	logger := logging.Get()
	defer func() {
		if err := logger.Close(); err != nil {
			logging.Get().Info("Error closing logger: %v", err)
		}
	}()

	// Create database connection for validation
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		pgConfig.User, pgConfig.Password, pgConfig.Host, pgConfig.Port, pgConfig.Database, pgConfig.SSLMode)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Error("Failed to open database connection for validation: %v", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Error closing database: %v", err)
		}
	}()

	// Validate the schema
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Validating database schema...")
	fmt.Println(strings.Repeat("=", 60))

	result, err := dbschema.ValidateSchema(db)
	if err != nil {
		logger.Error("Failed to validate schema: %v", err)
		return
	}

	// Validation results are already logged by the validator

	// Check if all validations passed
	allValid := result.Valid
	errorCount := len(result.Errors)

	if allValid {
		fmt.Println("\n✅ Database schema validation PASSED!")
		fmt.Println("   All migrations have been applied successfully.")
	} else {
		fmt.Println("\n❌ Database schema validation FAILED!")
		fmt.Printf("   Found %d schema errors.\n", errorCount)
		fmt.Println("   Please review the errors above.")
		fmt.Println("\n   This might indicate:")
		fmt.Println("   - Missing migrations")
		fmt.Println("   - Manual database changes that need to be captured in migrations")
		fmt.Println("   - Outdated schema expectations in internal/dbschema/schema.go")
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
