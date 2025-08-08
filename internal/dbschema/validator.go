package dbschema

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/ericfitz/tmi/internal/logging"
)

// ValidateSchema validates the actual database schema using migration-based validation
func ValidateSchema(db *sql.DB) (*SchemaValidationResult, error) {
	// Try to find the migration path relative to current directory or project root
	migrationPaths := []string{
		"auth/migrations",
		"../auth/migrations",
		"../../auth/migrations",
		"../../../auth/migrations",
	}

	for _, path := range migrationPaths {
		if _, err := os.Stat(path); err == nil {
			return ValidateSchemaWithMigrations(db, path)
		}
	}

	// Fallback to default path
	return ValidateSchemaWithMigrations(db, "auth/migrations")
}

// ValidateSchemaWithMigrations validates schema using migration-based approach
func ValidateSchemaWithMigrations(db *sql.DB, migrationPath string) (*SchemaValidationResult, error) {
	logger := logging.Get()
	logger.Debug("Starting migration-based schema validation")

	// Use new migration-based validator
	validator := NewMigrationBasedValidator(db, migrationPath)
	result, err := validator.ValidateSchema()
	if err != nil {
		return nil, fmt.Errorf("migration-based validation failed: %w", err)
	}

	// Log results
	validator.LogValidationResults(result)

	return result, nil
}
