// Package dbschema provides database schema validation for TMI.
// It validates that the database schema matches the expected structure
// defined by GORM models in api/models/models.go.
package dbschema

import (
	"database/sql"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SchemaValidationResult represents the result of schema validation
type SchemaValidationResult struct {
	Valid               bool
	Errors              []string
	Warnings            []string
	TotalMigrations     int // Repurposed: total expected tables
	AppliedMigrations   int // Repurposed: tables found
	DatabaseSchemaValid bool
	MigrationConsistent bool
}

// ValidateSchema validates the actual database schema against expected tables.
// This is a PostgreSQL-specific validation that checks essential tables exist.
func ValidateSchema(db *sql.DB) (*SchemaValidationResult, error) {
	return ValidateSchemaWithTables(db)
}

// ValidateSchemaWithTables validates schema by checking that essential tables exist.
// This replaces the migration-based validation now that TMI uses GORM AutoMigrate.
func ValidateSchemaWithTables(db *sql.DB) (*SchemaValidationResult, error) {
	logger := slogging.Get()
	logger.Debug("Starting GORM-based schema validation")

	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Essential tables that must exist (from api/models/models.go)
	essentialTables := []string{
		// Core infrastructure
		"users",
		"refresh_tokens",
		"client_credentials",
		"groups",
		"group_members",
		// Business domain
		"threat_models",
		"threat_model_access",
		"threats",
		"diagrams",
		"assets",
		"documents",
		"notes",
		"repositories",
		"metadata",
		// Collaboration
		"collaboration_sessions",
		"session_participants",
		// Webhooks and addons
		"webhook_subscriptions",
		"webhook_deliveries",
		"webhook_quotas",
		"webhook_url_deny_list",
		"addons",
		"addon_invocation_quotas",
		// Administration
		"user_api_quotas",
		// User preferences
		"user_preferences",
	}

	result.TotalMigrations = len(essentialTables) // Repurpose for table count
	tablesFound := 0

	for _, table := range essentialTables {
		exists, err := tableExists(db, table)
		if err != nil {
			return nil, fmt.Errorf("error checking table %s: %w", table, err)
		}

		if exists {
			tablesFound++
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("Required table '%s' does not exist", table))
			logger.Error("Required table %s is missing", table)
		}
	}

	result.AppliedMigrations = tablesFound // Repurpose for tables found
	result.DatabaseSchemaValid = len(result.Errors) == 0
	result.MigrationConsistent = result.DatabaseSchemaValid
	result.Valid = result.DatabaseSchemaValid

	// Log results
	if result.Valid {
		logger.Info("✅ Schema validation PASSED!")
		logger.Info("   Tables found: %d/%d", tablesFound, len(essentialTables))
	} else {
		logger.Error("❌ Schema validation FAILED!")
		logger.Error("   Tables found: %d/%d", tablesFound, len(essentialTables))
		for _, err := range result.Errors {
			logger.Error("   Error: %s", err)
		}
	}

	return result, nil
}

// tableExists checks if a table exists in the PostgreSQL database
func tableExists(db *sql.DB, tableName string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_name = $1
		)
	`

	var exists bool
	err := db.QueryRow(query, tableName).Scan(&exists)
	return exists, err
}

// ValidateSchemaWithMigrations is deprecated - kept for backward compatibility.
// Use ValidateSchema instead which validates against GORM models.
// Deprecated: This function references legacy SQL migrations that are no longer used.
func ValidateSchemaWithMigrations(db *sql.DB, migrationPath string) (*SchemaValidationResult, error) {
	logger := slogging.Get()
	logger.Warn("ValidateSchemaWithMigrations is deprecated - using GORM-based validation instead")
	return ValidateSchemaWithTables(db)
}
