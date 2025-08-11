package dbschema

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ericfitz/tmi/internal/logging"
)

// MigrationBasedValidator validates database schema against applied migrations
type MigrationBasedValidator struct {
	db            *sql.DB
	migrationPath string
}

// NewMigrationBasedValidator creates a new migration-based validator
func NewMigrationBasedValidator(db *sql.DB, migrationPath string) *MigrationBasedValidator {
	return &MigrationBasedValidator{
		db:            db,
		migrationPath: migrationPath,
	}
}

// MigrationInfo represents information about a migration
type MigrationInfo struct {
	Version int64
	Name    string
	Applied bool
	Dirty   bool
}

// SchemaValidationResult represents the result of migration-based validation
type SchemaValidationResult struct {
	Valid               bool
	Errors              []string
	Warnings            []string
	MissingMigrations   []MigrationInfo
	UnappliedMigrations []MigrationInfo
	DirtyMigrations     []MigrationInfo
	TotalMigrations     int
	AppliedMigrations   int
	DatabaseSchemaValid bool
	MigrationConsistent bool
}

// ValidateSchema performs comprehensive migration-based schema validation
func (v *MigrationBasedValidator) ValidateSchema() (*SchemaValidationResult, error) {
	logger := logging.Get()
	logger.Debug("Starting migration-based schema validation")

	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Step 1: Get all available migration files
	availableMigrations, err := v.getAvailableMigrations()
	if err != nil {
		return nil, fmt.Errorf("failed to read migration files: %w", err)
	}

	result.TotalMigrations = len(availableMigrations)
	logger.Debug("Found %d migration files", len(availableMigrations))

	// Step 2: Get applied migrations from database
	appliedMigrations, err := v.getAppliedMigrations()
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	result.AppliedMigrations = len(appliedMigrations)
	logger.Debug("Found %d applied migrations in database", len(appliedMigrations))

	// Step 3: Check migration completeness
	v.checkMigrationCompleteness(availableMigrations, appliedMigrations, result)

	// Step 4: Check for dirty migrations
	v.checkDirtyMigrations(appliedMigrations, result)

	// Step 5: Validate database schema consistency
	if err := v.validateDatabaseConsistency(result); err != nil {
		logger.Error("Database consistency validation failed: %v", err)
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Database consistency check failed: %v", err))
	}

	// Determine overall validation result
	if len(result.Errors) > 0 || len(result.MissingMigrations) > 0 || len(result.DirtyMigrations) > 0 {
		result.Valid = false
	}

	logger.Debug("Migration-based schema validation completed: valid=%v, errors=%d, warnings=%d",
		result.Valid, len(result.Errors), len(result.Warnings))

	return result, nil
}

// getAvailableMigrations reads all migration files from the filesystem
func (v *MigrationBasedValidator) getAvailableMigrations() ([]MigrationInfo, error) {
	var migrations []MigrationInfo

	err := filepath.WalkDir(v.migrationPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip subdirectories (like 'old') to avoid scanning old migration files
		if d.IsDir() {
			if path != v.migrationPath {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".up.sql") {
			return nil
		}

		// Extract version and name from filename
		filename := d.Name()
		parts := strings.SplitN(filename, "_", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename format: %s", filename)
		}

		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid migration version in filename %s: %w", filename, err)
		}

		name := strings.TrimSuffix(parts[1], ".up.sql")

		migrations = append(migrations, MigrationInfo{
			Version: version,
			Name:    name,
			Applied: false, // Will be updated later
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getAppliedMigrations gets the list of applied migrations from the database
// Returns a map where key=version, value=isDirty (true if migration failed)
func (v *MigrationBasedValidator) getAppliedMigrations() (map[int64]bool, error) {
	query := `SELECT version, dirty FROM schema_migrations ORDER BY version`

	rows, err := v.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	// Map tracks dirty state: key=version, value=isDirty
	dirtyState := make(map[int64]bool)
	var latestVersion int64
	var latestDirty bool

	for rows.Next() {
		var version int64
		var dirty bool

		if err := rows.Scan(&version, &dirty); err != nil {
			return nil, err
		}

		// Track the latest version (golang-migrate behavior)
		if version > latestVersion {
			latestVersion = version
			latestDirty = dirty
		}
		dirtyState[version] = dirty
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If we have a latest version and it's not dirty, mark all versions up to it as clean
	// This handles golang-migrate's behavior of only recording the latest version
	if latestVersion > 0 && !latestDirty {
		for i := int64(1); i <= latestVersion; i++ {
			dirtyState[i] = false // Mark all as clean (not dirty)
		}
	}

	return dirtyState, nil
}

// checkMigrationCompleteness validates that all expected migrations have been applied
func (v *MigrationBasedValidator) checkMigrationCompleteness(available []MigrationInfo, dirtyState map[int64]bool, result *SchemaValidationResult) {
	logger := logging.Get()

	for i, migration := range available {
		dirty, exists := dirtyState[migration.Version]

		if exists {
			// Migration has been applied
			migration.Applied = true
			migration.Dirty = dirty

			if dirty {
				result.DirtyMigrations = append(result.DirtyMigrations, migration)
				logger.Warn("Migration %d is dirty (failed during application)", migration.Version)
			}
		} else {
			// Migration has not been applied
			result.MissingMigrations = append(result.MissingMigrations, migration)
			logger.Error("Migration %d (%s) has not been applied", migration.Version, migration.Name)
		}

		// Update the migration info in the slice
		available[i] = migration
	}

	// Check for unapplied migrations (gaps in sequence)
	for version := range dirtyState {
		found := false
		for _, migration := range available {
			if migration.Version == version {
				found = true
				break
			}
		}
		if !found {
			result.UnappliedMigrations = append(result.UnappliedMigrations, MigrationInfo{
				Version: version,
				Name:    "unknown",
				Applied: true,
			})
			logger.Warn("Database contains migration %d that doesn't exist in filesystem", version)
		}
	}

	result.MigrationConsistent = len(result.MissingMigrations) == 0 && len(result.UnappliedMigrations) == 0
}

// checkDirtyMigrations identifies any dirty (failed) migrations
func (v *MigrationBasedValidator) checkDirtyMigrations(applied map[int64]bool, result *SchemaValidationResult) {
	// Dirty migrations are already identified in checkMigrationCompleteness
	// This function can be extended for additional dirty migration checks
}

// validateDatabaseConsistency performs basic database schema consistency checks
func (v *MigrationBasedValidator) validateDatabaseConsistency(result *SchemaValidationResult) error {
	logger := logging.Get()
	logger.Debug("Validating database schema consistency")

	// Check that essential tables exist
	essentialTables := []string{
		"users", "user_providers", "threat_models", "threat_model_access",
		"threats", "diagrams", "schema_migrations",
	}

	for _, table := range essentialTables {
		exists, err := v.tableExists(table)
		if err != nil {
			return fmt.Errorf("error checking table %s: %w", table, err)
		}

		if !exists {
			result.Errors = append(result.Errors, fmt.Sprintf("Essential table '%s' does not exist", table))
			logger.Error("Essential table %s is missing", table)
		}
	}

	// Additional consistency checks can be added here
	// For example: check foreign key constraints, indexes, etc.

	result.DatabaseSchemaValid = len(result.Errors) == 0

	return nil
}

// tableExists checks if a table exists in the database
func (v *MigrationBasedValidator) tableExists(tableName string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)
	`

	var exists bool
	err := v.db.QueryRow(query, tableName).Scan(&exists)
	return exists, err
}

// LogValidationResults logs the validation results in a user-friendly format
func (v *MigrationBasedValidator) LogValidationResults(result *SchemaValidationResult) {
	logger := logging.Get()

	if result.Valid {
		logger.Info("✅ Migration-based schema validation PASSED!")
		logger.Info("   Applied migrations: %d/%d", result.AppliedMigrations, result.TotalMigrations)
		logger.Info("   Database schema: consistent")
		logger.Info("   Migration state: clean")
	} else {
		logger.Error("❌ Migration-based schema validation FAILED!")
		logger.Error("   Applied migrations: %d/%d", result.AppliedMigrations, result.TotalMigrations)

		if len(result.MissingMigrations) > 0 {
			logger.Error("   Missing migrations: %d", len(result.MissingMigrations))
			for _, migration := range result.MissingMigrations {
				logger.Error("     - Migration %d: %s", migration.Version, migration.Name)
			}
		}

		if len(result.DirtyMigrations) > 0 {
			logger.Error("   Dirty migrations: %d", len(result.DirtyMigrations))
			for _, migration := range result.DirtyMigrations {
				logger.Error("     - Migration %d: %s (failed during application)", migration.Version, migration.Name)
			}
		}

		if len(result.UnappliedMigrations) > 0 {
			logger.Error("   Unknown applied migrations: %d", len(result.UnappliedMigrations))
			for _, migration := range result.UnappliedMigrations {
				logger.Error("     - Migration %d: not found in filesystem", migration.Version)
			}
		}

		for _, err := range result.Errors {
			logger.Error("   Error: %s", err)
		}
	}

	for _, warning := range result.Warnings {
		logger.Warn("   Warning: %s", warning)
	}
}
