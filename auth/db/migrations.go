package db

import (
	"fmt"
	"path/filepath"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrationConfig holds the configuration for database migrations
type MigrationConfig struct {
	MigrationsPath string
	DatabaseName   string
}

// RunMigrations runs database migrations
func (m *Manager) RunMigrations(cfg MigrationConfig) error {
	if m.postgres == nil {
		return fmt.Errorf("postgres connection not initialized")
	}

	// Get the database connection
	db := m.postgres.GetDB()

	// Create a new postgres driver for migrations
	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: cfg.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create a new migrate instance
	absPath, err := filepath.Abs(cfg.MigrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	sourceURL := fmt.Sprintf("file://%s", absPath)
	migrator, err := migrate.NewWithDatabaseInstance(sourceURL, cfg.DatabaseName, driver)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	// Run migrations
	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slogging.Get().Error("Database migrations completed successfully")
	return nil
}

// MigrateDown rolls back all migrations
func (m *Manager) MigrateDown(cfg MigrationConfig) error {
	if m.postgres == nil {
		return fmt.Errorf("postgres connection not initialized")
	}

	// Get the database connection
	db := m.postgres.GetDB()

	// Create a new postgres driver for migrations
	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: cfg.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create a new migrate instance
	absPath, err := filepath.Abs(cfg.MigrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	sourceURL := fmt.Sprintf("file://%s", absPath)
	migrator, err := migrate.NewWithDatabaseInstance(sourceURL, cfg.DatabaseName, driver)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	// Roll back all migrations
	if err := migrator.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to roll back migrations: %w", err)
	}

	slogging.Get().Error("Database migrations rolled back successfully")
	return nil
}

// MigrateStep runs a specific number of migrations
func (m *Manager) MigrateStep(cfg MigrationConfig, steps int) error {
	if m.postgres == nil {
		return fmt.Errorf("postgres connection not initialized")
	}

	// Get the database connection
	db := m.postgres.GetDB()

	// Create a new postgres driver for migrations
	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: cfg.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create a new migrate instance
	absPath, err := filepath.Abs(cfg.MigrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	sourceURL := fmt.Sprintf("file://%s", absPath)
	migrator, err := migrate.NewWithDatabaseInstance(sourceURL, cfg.DatabaseName, driver)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	// Run migrations
	if err := migrator.Steps(steps); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slogging.Get().Error("Database migrations completed successfully")
	return nil
}
