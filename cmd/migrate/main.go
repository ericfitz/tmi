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
		os.Exit(1)
	}

	// Determine database type from config
	dbType := cfg.Database.Type
	if dbType == "" {
		dbType = "postgres" // Default for backward compatibility
	}

	// Create GORM database configuration from unified config
	gormConfig := createGormConfig(cfg, dbType)

	// Create database manager
	dbManager := db.NewManager()

	// Initialize GORM connection
	log.Info("Connecting to %s database...", dbType)
	if err := dbManager.InitGorm(gormConfig); err != nil {
		log.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := dbManager.Close(); err != nil {
			log.Error("Error closing database manager: %v", err)
		}
	}()

	gormDB := dbManager.Gorm()
	if gormDB == nil {
		log.Error("GORM database not initialized")
		os.Exit(1)
	}

	log.Info("Connected to %s database", dbType)

	// Validate-only mode
	if *validateOnly {
		if dbType == "postgres" {
			validateSchema(gormConfig)
		} else {
			log.Info("Schema validation is only supported for PostgreSQL")
		}
		return
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
			os.Exit(1)
		}
	}
	log.Info("GORM AutoMigrate completed successfully")

	// Seed required data
	if *seedData {
		log.Info("Seeding required data...")
		if err := seed.SeedDatabase(gormDB.DB()); err != nil {
			log.Error("Failed to seed database: %v", err)
			os.Exit(1)
		}
		log.Info("Database seeding completed")
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Database migration complete!")
	fmt.Println(strings.Repeat("=", 60))

	// Validate schema for PostgreSQL
	if dbType == "postgres" {
		validateSchema(gormConfig)
	}
}

// createGormConfig creates a GORM configuration from the unified config
func createGormConfig(cfg *config.Config, dbType string) db.GormConfig {
	gormConfig := db.GormConfig{}

	switch dbType {
	case "postgres":
		gormConfig.Type = db.DatabaseTypePostgres
		gormConfig.PostgresHost = cfg.Database.Postgres.Host
		gormConfig.PostgresPort = cfg.Database.Postgres.Port
		gormConfig.PostgresUser = cfg.Database.Postgres.User
		gormConfig.PostgresPassword = cfg.Database.Postgres.Password
		gormConfig.PostgresDatabase = cfg.Database.Postgres.Database
		gormConfig.PostgresSSLMode = cfg.Database.Postgres.SSLMode

	case "oracle":
		gormConfig.Type = db.DatabaseTypeOracle
		gormConfig.OracleConnectString = cfg.Database.Oracle.ConnectString
		gormConfig.OracleUser = cfg.Database.Oracle.User
		gormConfig.OraclePassword = cfg.Database.Oracle.Password
		gormConfig.OracleWalletLocation = cfg.Database.Oracle.WalletLocation

	case "mysql":
		gormConfig.Type = db.DatabaseTypeMySQL
		gormConfig.MySQLHost = cfg.Database.MySQL.Host
		gormConfig.MySQLPort = cfg.Database.MySQL.Port
		gormConfig.MySQLUser = cfg.Database.MySQL.User
		gormConfig.MySQLPassword = cfg.Database.MySQL.Password
		gormConfig.MySQLDatabase = cfg.Database.MySQL.Database

	case "sqlserver":
		gormConfig.Type = db.DatabaseTypeSQLServer
		gormConfig.SQLServerHost = cfg.Database.SQLServer.Host
		gormConfig.SQLServerPort = cfg.Database.SQLServer.Port
		gormConfig.SQLServerUser = cfg.Database.SQLServer.User
		gormConfig.SQLServerPassword = cfg.Database.SQLServer.Password
		gormConfig.SQLServerDatabase = cfg.Database.SQLServer.Database

	case "sqlite":
		gormConfig.Type = db.DatabaseTypeSQLite
		gormConfig.SQLitePath = cfg.Database.SQLite.Path

	default:
		slogging.Get().Error("Unsupported database type: %s", dbType)
		os.Exit(1)
	}

	return gormConfig
}

// validateSchema validates the database schema after migrations (PostgreSQL only)
func validateSchema(gormConfig db.GormConfig) {
	logger := slogging.Get()

	// Create database connection for validation
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		gormConfig.PostgresUser, gormConfig.PostgresPassword, gormConfig.PostgresHost,
		gormConfig.PostgresPort, gormConfig.PostgresDatabase, gormConfig.PostgresSSLMode)

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
