package dbschema

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/logging"
)

// ValidationResult represents the result of a schema validation
type ValidationResult struct {
	TableName string
	Valid     bool
	Errors    []string
	Warnings  []string
}

// ValidateSchema validates the actual database schema against the expected schema
func ValidateSchema(db *sql.DB) ([]ValidationResult, error) {
	logger := logging.Get()
	logger.Debug("Starting database schema validation")

	expectedTables := GetExpectedSchema()
	results := make([]ValidationResult, 0, len(expectedTables))

	// Get list of actual tables
	actualTables, err := getActualTables(db)
	if err != nil {
		return nil, fmt.Errorf("failed to get actual tables: %w", err)
	}

	// Create a map for quick lookup
	actualTableMap := make(map[string]bool)
	for _, table := range actualTables {
		actualTableMap[table] = true
	}

	// Check each expected table
	for _, expectedTable := range expectedTables {
		logger.Debug("Validating table: %s", expectedTable.Name)

		result := ValidationResult{
			TableName: expectedTable.Name,
			Valid:     true,
			Errors:    []string{},
			Warnings:  []string{},
		}

		// Check if table exists
		if !actualTableMap[expectedTable.Name] {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Table '%s' does not exist", expectedTable.Name))
			results = append(results, result)
			continue
		}

		// Validate columns
		if err := validateTableColumns(db, expectedTable, &result); err != nil {
			logger.Error("Failed to validate columns for table %s: %v", expectedTable.Name, err)
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to validate columns: %v", err))
		}

		// Validate indexes
		if err := validateTableIndexes(db, expectedTable, &result); err != nil {
			logger.Error("Failed to validate indexes for table %s: %v", expectedTable.Name, err)
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to validate indexes: %v", err))
		}

		// Validate constraints
		if err := validateTableConstraints(db, expectedTable, &result); err != nil {
			logger.Error("Failed to validate constraints for table %s: %v", expectedTable.Name, err)
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to validate constraints: %v", err))
		}

		results = append(results, result)
	}

	// Check for unexpected tables
	for actualTable := range actualTableMap {
		found := false
		for _, expectedTable := range expectedTables {
			if expectedTable.Name == actualTable {
				found = true
				break
			}
		}
		if !found && !strings.HasPrefix(actualTable, "pg_") && actualTable != "information_schema" {
			logger.Warn("Found unexpected table: %s", actualTable)
		}
	}

	return results, nil
}

// getActualTables returns a list of all tables in the public schema
func getActualTables(db *sql.DB) ([]string, error) {
	query := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but continue
			logging.Get().Error("Error closing rows: %v", err)
		}
	}()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	return tables, rows.Err()
}

// validateTableColumns validates the columns of a table
func validateTableColumns(db *sql.DB, expectedTable TableSchema, result *ValidationResult) error {
	logger := logging.Get()

	query := `
		SELECT 
			column_name,
			data_type,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' 
		AND table_name = $1
		ORDER BY ordinal_position
	`

	rows, err := db.Query(query, expectedTable.Name)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but continue
			logging.Get().Error("Error closing rows: %v", err)
		}
	}()

	actualColumns := make(map[string]ColumnSchema)
	for rows.Next() {
		var col ColumnSchema
		var isNullable string
		var defaultValue sql.NullString

		if err := rows.Scan(&col.Name, &col.DataType, &isNullable, &defaultValue); err != nil {
			return err
		}

		col.IsNullable = (isNullable == "YES")
		if defaultValue.Valid {
			col.DefaultValue = &defaultValue.String
		}

		actualColumns[col.Name] = col
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Check expected columns
	for _, expectedCol := range expectedTable.Columns {
		actualCol, exists := actualColumns[expectedCol.Name]
		if !exists {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Missing column: %s", expectedCol.Name))
			continue
		}

		// Check data type
		if !compareDataTypes(expectedCol.DataType, actualCol.DataType) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Column '%s' has incorrect data type: expected '%s', got '%s'",
				expectedCol.Name, expectedCol.DataType, actualCol.DataType))
		}

		// Check nullability
		if expectedCol.IsNullable != actualCol.IsNullable {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Column '%s' has incorrect nullability: expected %v, got %v",
				expectedCol.Name, expectedCol.IsNullable, actualCol.IsNullable))
		}

		logger.Debug("Validated column %s.%s: type=%s, nullable=%v",
			expectedTable.Name, expectedCol.Name, actualCol.DataType, actualCol.IsNullable)
	}

	// Check for unexpected columns
	for colName := range actualColumns {
		found := false
		for _, expectedCol := range expectedTable.Columns {
			if expectedCol.Name == colName {
				found = true
				break
			}
		}
		if !found {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Unexpected column: %s", colName))
		}
	}

	return nil
}

// validateTableIndexes validates the indexes of a table
func validateTableIndexes(db *sql.DB, expectedTable TableSchema, result *ValidationResult) error {
	logger := logging.Get()

	query := `
		SELECT 
			i.relname as index_name,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) as column_names,
			ix.indisunique as is_unique
		FROM pg_class t
		JOIN pg_index ix ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		WHERE t.relname = $1
		AND t.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
		GROUP BY i.relname, ix.indisunique
		ORDER BY i.relname
	`

	rows, err := db.Query(query, expectedTable.Name)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but continue
			logging.Get().Error("Error closing rows: %v", err)
		}
	}()

	actualIndexes := make(map[string]IndexSchema)
	for rows.Next() {
		var idx IndexSchema
		var columnNames sql.NullString

		if err := rows.Scan(&idx.Name, &columnNames, &idx.IsUnique); err != nil {
			return err
		}

		if columnNames.Valid {
			// Parse the PostgreSQL array format
			cols := strings.Trim(columnNames.String, "{}")
			idx.Columns = strings.Split(cols, ",")
		}

		actualIndexes[idx.Name] = idx
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Check expected indexes
	for _, expectedIdx := range expectedTable.Indexes {
		actualIdx, exists := actualIndexes[expectedIdx.Name]
		if !exists {
			// Some indexes might have different names but same columns
			// Check if there's an index with the same columns
			found := false
			for _, actual := range actualIndexes {
				if indexColumnsMatch(expectedIdx.Columns, actual.Columns) && expectedIdx.IsUnique == actual.IsUnique {
					found = true
					logger.Debug("Found matching index with different name: expected %s, actual %s",
						expectedIdx.Name, actual.Name)
					break
				}
			}

			if !found {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Missing index: %s on columns %v",
					expectedIdx.Name, expectedIdx.Columns))
			}
			continue
		}

		// Check columns
		if !indexColumnsMatch(expectedIdx.Columns, actualIdx.Columns) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Index '%s' has incorrect columns: expected %v, got %v",
				expectedIdx.Name, expectedIdx.Columns, actualIdx.Columns))
		}

		// Check uniqueness
		if expectedIdx.IsUnique != actualIdx.IsUnique {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Index '%s' has incorrect uniqueness: expected %v, got %v",
				expectedIdx.Name, expectedIdx.IsUnique, actualIdx.IsUnique))
		}

		logger.Debug("Validated index %s.%s: columns=%v, unique=%v",
			expectedTable.Name, expectedIdx.Name, actualIdx.Columns, actualIdx.IsUnique)
	}

	return nil
}

// validateTableConstraints validates the constraints of a table
func validateTableConstraints(db *sql.DB, expectedTable TableSchema, result *ValidationResult) error {
	logger := logging.Get()

	// Query for foreign key constraints
	fkQuery := `
		SELECT 
			tc.constraint_name,
			kcu.column_name,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name
		FROM information_schema.table_constraints AS tc 
		JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' 
		AND tc.table_name = $1
		AND tc.table_schema = 'public'
		ORDER BY tc.constraint_name, kcu.ordinal_position
	`

	rows, err := db.Query(fkQuery, expectedTable.Name)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but continue
			logging.Get().Error("Error closing rows: %v", err)
		}
	}()

	actualFKs := make(map[string]ConstraintSchema)
	for rows.Next() {
		var constraintName, columnName, foreignTable, foreignColumn string

		if err := rows.Scan(&constraintName, &columnName, &foreignTable, &foreignColumn); err != nil {
			return err
		}

		if fk, exists := actualFKs[constraintName]; exists {
			// Add to existing foreign key columns
			fk.ForeignColumns = append(fk.ForeignColumns, foreignColumn)
			actualFKs[constraintName] = fk
		} else {
			actualFKs[constraintName] = ConstraintSchema{
				Name:           constraintName,
				Type:           "FOREIGN KEY",
				ForeignTable:   foreignTable,
				ForeignColumns: []string{foreignColumn},
			}
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Query for check constraints
	checkQuery := `
		SELECT 
			con.conname AS constraint_name,
			pg_get_constraintdef(con.oid) AS definition
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
		WHERE con.contype = 'c'
		AND rel.relname = $1
		AND nsp.nspname = 'public'
	`

	checkRows, err := db.Query(checkQuery, expectedTable.Name)
	if err != nil {
		return err
	}
	defer func() {
		if err := checkRows.Close(); err != nil {
			// Log error but continue
			logging.Get().Error("Error closing checkRows: %v", err)
		}
	}()

	actualChecks := make(map[string]ConstraintSchema)
	for checkRows.Next() {
		var constraintName, definition string

		if err := checkRows.Scan(&constraintName, &definition); err != nil {
			return err
		}

		actualChecks[constraintName] = ConstraintSchema{
			Name:       constraintName,
			Type:       "CHECK",
			Definition: definition,
		}
	}

	if err := checkRows.Err(); err != nil {
		return err
	}

	// Validate expected constraints
	for _, expectedConstraint := range expectedTable.Constraints {
		switch expectedConstraint.Type {
		case "FOREIGN KEY":
			found := false
			for _, actualFK := range actualFKs {
				if actualFK.ForeignTable == expectedConstraint.ForeignTable &&
					columnsMatch(actualFK.ForeignColumns, expectedConstraint.ForeignColumns) {
					found = true
					logger.Debug("Found matching foreign key constraint for %s -> %s",
						expectedTable.Name, expectedConstraint.ForeignTable)
					break
				}
			}
			if !found {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Missing foreign key constraint to %s(%v)",
					expectedConstraint.ForeignTable, expectedConstraint.ForeignColumns))
			}

		case "CHECK":
			found := false
			for _, actualCheck := range actualChecks {
				if checkConstraintMatches(expectedConstraint.Definition, actualCheck.Definition) {
					found = true
					logger.Debug("Found matching check constraint: %s", expectedConstraint.Name)
					break
				}
			}
			if !found {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Missing check constraint: %s",
					expectedConstraint.Definition))
			}
		}
	}

	return nil
}

// indexColumnsMatch checks if two sets of index columns match
func indexColumnsMatch(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}

	for i, col := range expected {
		if col != actual[i] {
			return false
		}
	}

	return true
}

// columnsMatch checks if two sets of columns match (order doesn't matter)
func columnsMatch(cols1, cols2 []string) bool {
	if len(cols1) != len(cols2) {
		return false
	}

	// Create maps for comparison
	map1 := make(map[string]bool)
	map2 := make(map[string]bool)

	for _, col := range cols1 {
		map1[col] = true
	}

	for _, col := range cols2 {
		map2[col] = true
	}

	// Check if all columns in map1 are in map2
	for col := range map1 {
		if !map2[col] {
			return false
		}
	}

	return true
}

// normalizeConstraint normalizes constraint strings for comparison
func normalizeConstraint(constraint string) string {
	return strings.ToLower(strings.TrimSpace(constraint))
}

// matchDirectConstraint checks for direct string matching
func matchDirectConstraint(expected, actual string) bool {
	return strings.Contains(actual, expected)
}

// matchInClauseConstraint handles IN clause variations
// Expected: "provider IN ('google', 'github', 'microsoft', 'apple', 'facebook', 'twitter')"
// Actual: "CHECK (((provider)::text = ANY ((ARRAY['google'::character varying, ...]::text[])))"
func matchInClauseConstraint(expected, actual string) bool {
	if !strings.Contains(expected, " in (") {
		return false
	}

	// Extract the column name and values from expected
	parts := strings.SplitN(expected, " in ", 2)
	if len(parts) != 2 {
		return false
	}

	column := strings.TrimSpace(parts[0])
	valuesStr := strings.Trim(parts[1], "()")

	// Check if actual contains the column and all the values
	if !strings.Contains(actual, column) {
		return false
	}

	// Remove quotes and split values
	valuesStr = strings.ReplaceAll(valuesStr, "'", "")
	values := strings.Split(valuesStr, ",")

	for _, val := range values {
		val = strings.TrimSpace(val)
		if !strings.Contains(actual, val) {
			return false
		}
	}

	return true
}

// matchComparisonConstraint handles comparison operators (!=, >, <=, etc.)
func matchComparisonConstraint(expected, actual string) bool {
	// Handle != comparisons
	// Expected: "provider_user_id != ''"
	// Actual: "CHECK (((provider_user_id)::text <> ''::text))"
	if strings.Contains(expected, " != ") {
		// PostgreSQL converts != to <>
		expectedWithAngleBrackets := strings.ReplaceAll(expected, " != ", " <> ")
		if strings.Contains(actual, expectedWithAngleBrackets) {
			return true
		}

		// Also check if the column and the comparison value are present
		parts := strings.Split(expected, " != ")
		if len(parts) == 2 {
			column := strings.TrimSpace(parts[0])
			value := strings.Trim(parts[1], "'")
			if strings.Contains(actual, column) && strings.Contains(actual, "<>") && strings.Contains(actual, value) {
				return true
			}
		}
	}

	// Handle > comparisons
	// Expected: "expires_at > created_at"
	// Actual might have type casts but should contain both column names and >
	if strings.Contains(expected, " > ") {
		parts := strings.Split(expected, " > ")
		if len(parts) == 2 {
			col1 := strings.TrimSpace(parts[0])
			col2 := strings.TrimSpace(parts[1])
			if strings.Contains(actual, col1) && strings.Contains(actual, col2) && strings.Contains(actual, ">") {
				return true
			}
		}
	}

	return false
}

// matchNullOrConstraint handles OR conditions with IS NULL
// Expected: "severity IS NULL OR severity IN ('low', 'medium', 'high', 'critical')"
// Expected: "expires_at IS NULL OR expires_at > created_at"
func matchNullOrConstraint(expected, actual string) bool {
	if !strings.Contains(expected, " is null or ") {
		return false
	}

	// Check if the pattern matches
	parts := strings.Split(expected, " or ")
	if len(parts) != 2 {
		return false
	}

	nullPart := strings.TrimSpace(parts[0])
	inPart := strings.TrimSpace(parts[1])

	// Extract column from null check
	nullParts := strings.Split(nullPart, " is null")
	if len(nullParts) == 0 {
		return false
	}

	column := strings.TrimSpace(nullParts[0])

	// Check if actual contains the column, IS NULL, and the condition
	if !strings.Contains(actual, column) || !strings.Contains(actual, "is null") {
		return false
	}

	// Handle IN clause: "severity IN ('low', 'medium', 'high', 'critical')"
	if strings.Contains(inPart, " in (") {
		return matchInClauseConstraint(inPart, actual)
	}

	// Handle comparison: "expires_at > created_at"
	if strings.Contains(inPart, " > ") {
		return matchComparisonConstraint(inPart, actual)
	}

	return false
}

// matchLengthTrimConstraint handles LENGTH/TRIM patterns
// Expected: "LENGTH(TRIM(name)) > 0"
// Actual: "length(TRIM(BOTH FROM name)) > 0" or "length(trim(both from name)) > 0"
func matchLengthTrimConstraint(expected, actual string) bool {
	if !strings.Contains(expected, "length(trim(") || !strings.Contains(expected, ")) >") {
		return false
	}

	// Extract column name from expected pattern
	// Pattern: "length(trim(column_name)) > value"
	start := strings.Index(expected, "length(trim(") + len("length(trim(")
	end := strings.Index(expected[start:], "))")
	if end <= 0 {
		return false
	}

	column := expected[start : start+end]

	// Extract comparison value
	parts := strings.Split(expected, ")) >")
	if len(parts) != 2 {
		return false
	}

	value := strings.TrimSpace(parts[1])

	// Check if actual contains the column name, trim function, and comparison
	return strings.Contains(actual, column) &&
		(strings.Contains(actual, "trim(both from") || strings.Contains(actual, "trim(")) &&
		strings.Contains(actual, "length(") &&
		strings.Contains(actual, "> "+value)
}

// matchRegexConstraint handles regex patterns with potential type casting
// Expected: "key ~ '^[a-zA-Z0-9_-]+$'"
// Actual: "((key)::text ~ '^[a-zA-Z0-9_-]+$'::text)" or similar
func matchRegexConstraint(expected, actual string) bool {
	if !strings.Contains(expected, " ~ ") {
		return false
	}

	parts := strings.Split(expected, " ~ ")
	if len(parts) != 2 {
		return false
	}

	column := strings.TrimSpace(parts[0])
	pattern := strings.Trim(parts[1], "'")

	// Check if actual contains the column, regex operator, and pattern
	return strings.Contains(actual, column) &&
		strings.Contains(actual, "~") &&
		strings.Contains(actual, pattern)
}

// matchComplexAndConstraint handles complex AND constraints with LENGTH/TRIM
// Expected: "LENGTH(TRIM(key)) > 0 AND LENGTH(key) <= 128"
// Actual: "((length(TRIM(BOTH FROM key)) > 0) AND (length(key) <= 128))" or similar variations
func matchComplexAndConstraint(expected, actual string) bool {
	if !strings.Contains(expected, " and ") {
		return false
	}

	parts := strings.Split(expected, " and ")
	if len(parts) != 2 {
		return false
	}

	part1 := strings.TrimSpace(parts[0])
	part2 := strings.TrimSpace(parts[1])

	// Check if both parts match using existing matchers
	match1 := matchLengthTrimConstraint(part1, actual)
	match2 := false

	// Check second part (likely LENGTH(column) <= value)
	if strings.Contains(part2, "length(") && strings.Contains(part2, ") <=") {
		start := strings.Index(part2, "length(") + len("length(")
		end := strings.Index(part2[start:], ")")
		if end > 0 {
			column := part2[start : start+end]
			compParts := strings.Split(part2, ") <=")
			if len(compParts) == 2 {
				value := strings.TrimSpace(compParts[1])
				if strings.Contains(actual, column) &&
					strings.Contains(actual, "length(") &&
					strings.Contains(actual, "<= "+value) {
					match2 = true
				}
			}
		}
	}

	return match1 && match2
}

// checkConstraintMatches checks if an expected constraint definition matches the actual PostgreSQL definition
// PostgreSQL reformats CHECK constraints with type casts and array syntax, so we need to handle these variations
func checkConstraintMatches(expected, actual string) bool {
	// Normalize both strings for comparison
	expected = normalizeConstraint(expected)
	actual = normalizeConstraint(actual)

	// Direct match
	if matchDirectConstraint(expected, actual) {
		return true
	}

	// Handle IN clause variations
	if matchInClauseConstraint(expected, actual) {
		return true
	}

	// Handle comparison operators
	if matchComparisonConstraint(expected, actual) {
		return true
	}

	// Handle OR conditions with IS NULL
	if matchNullOrConstraint(expected, actual) {
		return true
	}

	// Handle LENGTH/TRIM patterns
	if matchLengthTrimConstraint(expected, actual) {
		return true
	}

	// Handle complex AND constraints
	if matchComplexAndConstraint(expected, actual) {
		return true
	}

	// Handle regex patterns
	if matchRegexConstraint(expected, actual) {
		return true
	}

	return false
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
