package dbschema

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/ericfitz/tmi/internal/logging"
)

// ValidationResult represents the result of a schema validation
type ValidationResult struct {
	TableName string
	Valid     bool
	Errors    []string
	Warnings  []string
}

// ValidateSchema validates the actual database schema using migration-based validation
func ValidateSchema(db *sql.DB) ([]ValidationResult, error) {
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
func ValidateSchemaWithMigrations(db *sql.DB, migrationPath string) ([]ValidationResult, error) {
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

	// Convert to legacy format for compatibility
	return convertToLegacyFormat(result), nil
}

// convertToLegacyFormat converts migration-based validation results to legacy format
func convertToLegacyFormat(result *SchemaValidationResult) []ValidationResult {
	var legacyResults []ValidationResult

	// Create a summary validation result
	summaryResult := ValidationResult{
		TableName: "migration_validation_summary",
		Valid:     result.Valid,
		Errors:    make([]string, len(result.Errors)),
		Warnings:  make([]string, len(result.Warnings)),
	}

	copy(summaryResult.Errors, result.Errors)
	copy(summaryResult.Warnings, result.Warnings)

	// Add migration-specific errors and warnings
	for _, migration := range result.MissingMigrations {
		summaryResult.Errors = append(summaryResult.Errors,
			fmt.Sprintf("Missing migration %d: %s", migration.Version, migration.Name))
	}

	for _, migration := range result.DirtyMigrations {
		summaryResult.Errors = append(summaryResult.Errors,
			fmt.Sprintf("Dirty migration %d: %s", migration.Version, migration.Name))
	}

	for _, migration := range result.UnappliedMigrations {
		summaryResult.Warnings = append(summaryResult.Warnings,
			fmt.Sprintf("Unknown applied migration %d found in database", migration.Version))
	}

	// Add summary information
	summaryResult.Warnings = append(summaryResult.Warnings,
		fmt.Sprintf("Applied migrations: %d/%d", result.AppliedMigrations, result.TotalMigrations))

	legacyResults = append(legacyResults, summaryResult)
	return legacyResults
}

// LogValidationResults logs the validation results
func LogValidationResults(results []ValidationResult) {
	logger := logging.Get()

	allValid := true
	for _, result := range results {
		if !result.Valid {
			allValid = false
			logger.Error("Schema validation failed for table '%s':", result.TableName)
			for _, err := range result.Errors {
				logger.Error("  - %s", err)
			}
		} else {
			logger.Debug("Schema validation passed for table '%s'", result.TableName)
		}

		for _, warning := range result.Warnings {
			logger.Warn("  Warning for table '%s': %s", result.TableName, warning)
		}
	}

	if allValid {
		logger.Info("Database schema validation completed successfully - all tables match expected schema")
	} else {
		logger.Error("Database schema validation failed - some tables do not match expected schema")
	}
}
