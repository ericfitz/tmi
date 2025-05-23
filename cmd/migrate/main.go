package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ericfitz/tmi/auth/db"
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
		log.Printf("Warning: Could not load env file %s: %v", *envFile, err)
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
	log.Printf("Connecting to PostgreSQL at %s:%s/%s", pgConfig.Host, pgConfig.Port, pgConfig.Database)
	if err := dbManager.InitPostgres(pgConfig); err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer dbManager.Close()

	// Get migrations path
	migrationsPath := filepath.Join("auth", "migrations")
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}
	log.Printf("Using migrations from: %s", absPath)

	// Create migration config
	migrationConfig := db.MigrationConfig{
		MigrationsPath: migrationsPath,
		DatabaseName:   pgConfig.Database,
	}

	// Run migrations based on flags
	if *down {
		log.Println("Running down migrations...")
		if err := dbManager.MigrateDown(migrationConfig); err != nil {
			log.Fatalf("Failed to run down migrations: %v", err)
		}
		log.Println("Down migrations completed successfully")
	} else if *steps != 0 {
		log.Printf("Running %d migration steps...", *steps)
		if err := dbManager.MigrateStep(migrationConfig, *steps); err != nil {
			log.Fatalf("Failed to run migration steps: %v", err)
		}
		log.Printf("%d migration steps completed successfully", *steps)
	} else {
		log.Println("Running all pending migrations...")
		if err := dbManager.RunMigrations(migrationConfig); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		log.Println("All migrations completed successfully")
	}

	fmt.Println("\nDatabase schema setup complete!")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
