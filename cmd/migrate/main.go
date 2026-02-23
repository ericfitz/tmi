// Package main implements the migrate CLI tool for TMI database schema management.
// It uses GORM AutoMigrate for all supported databases (PostgreSQL, Oracle, MySQL,
// SQL Server, SQLite), providing a single source of truth via api/models/models.go.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	_ "github.com/jackc/pgx/v4/stdlib"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Command line flags
	var (
		configFile   = flag.String("config", "config-development.yml", "Path to configuration file")
		seedData     = flag.Bool("seed", true, "Seed required data after migration")
		validateOnly = flag.Bool("validate", false, "Only validate schema, don't run migrations")
		verbose      = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Initialize logging
	logLevel := slogging.LogLevelInfo
	if *verbose {
		logLevel = slogging.LogLevelDebug
	}
	if err := slogging.Initialize(slogging.Config{
		Level:            logLevel,
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	log := slogging.Get()

	// Load configuration from YAML file
	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Error("Failed to load config file %s: %v", *configFile, err)
		return 1
	}

	// Create GORM database configuration from DATABASE_URL
	gormConfig, err := db.ParseDatabaseURL(cfg.Database.URL)
	if err != nil {
		log.Error("Failed to parse DATABASE_URL: %v", err)
		return 1
	}

	// Copy Oracle wallet location if configured
	if cfg.Database.OracleWalletLocation != "" {
		gormConfig.OracleWalletLocation = cfg.Database.OracleWalletLocation
	}

	dbType := string(gormConfig.Type)

	// Create database manager
	dbManager := db.NewManager()

	// Initialize GORM connection
	log.Info("Connecting to %s database...", dbType)
	if err := dbManager.InitGorm(*gormConfig); err != nil {
		log.Error("Failed to connect to database: %v", err)
		return 1
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			log.Error("Error closing database manager: %v", err)
		}
	}()

	gormDB := dbManager.Gorm()
	if gormDB == nil {
		log.Error("GORM database not initialized")
		return 1
	}

	log.Info("Connected to %s database", dbType)

	// Validate-only mode
	if *validateOnly {
		if dbType == "postgres" {
			validateSchema(*gormConfig)
		} else {
			log.Info("Schema validation is only supported for PostgreSQL")
		}
		return 0
	}

	// Run GORM AutoMigrate
	log.Info("Running GORM AutoMigrate for %d models...", len(models.AllModels()))
	if err := gormDB.AutoMigrate(models.AllModels()...); err != nil {
		// Oracle ORA-00955: name is already used by an existing object
		// This is acceptable - table already exists from a previous migration
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") {
			log.Debug("Some tables already exist, continuing: %v", err)
		} else {
			log.Error("Failed to auto-migrate schema: %v", err)
			return 1
		}
	}
	log.Info("GORM AutoMigrate completed successfully")

	// Seed required data
	if *seedData {
		log.Info("Seeding required data...")
		if err := seed.SeedDatabase(gormDB.DB()); err != nil {
			log.Error("Failed to seed database: %v", err)
			return 1
		}
		log.Info("Database seeding completed")
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Database migration complete!")
	fmt.Println(strings.Repeat("=", 60))

	// Validate schema for PostgreSQL
	if dbType == "postgres" {
		validateSchema(*gormConfig)
	}
	return 0
}

// validateSchema validates the database schema after migrations (PostgreSQL only)
func validateSchema(gormConfig db.GormConfig) {
	logger := slogging.Get()

	// Create database connection for validation using unified fields
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		gormConfig.User, gormConfig.Password, gormConfig.Host,
		gormConfig.Port, gormConfig.Database, gormConfig.SSLMode)

	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		logger.Error("Failed to open database connection for validation: %v", err)
		return
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			logger.Error("Error closing database: %v", err)
		}
	}()

	// Validate the schema
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Validating database schema...")
	fmt.Println(strings.Repeat("=", 60))

	result, err := dbschema.ValidateSchema(sqlDB)
	if err != nil {
		logger.Error("Failed to validate schema: %v", err)
		return
	}

	// Check if all validations passed
	allValid := result.Valid
	errorCount := len(result.Errors)

	if allValid {
		fmt.Println("\n✅ Database schema validation PASSED!")
		fmt.Println("   All models have been migrated successfully.")
	} else {
		fmt.Println("\n❌ Database schema validation FAILED!")
		fmt.Printf("   Found %d schema errors.\n", errorCount)
		fmt.Println("   Please review the errors above.")
		fmt.Println("\n   This might indicate:")
		fmt.Println("   - Model definitions in api/models/models.go need updating")
		fmt.Println("   - Manual database changes that need to be captured in models")
		fmt.Println("   - Outdated schema expectations in internal/dbschema/schema.go")
	}
}
