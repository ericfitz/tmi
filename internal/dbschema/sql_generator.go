package dbschema

import (
	"fmt"
	"strings"
)

// GenerateCreateTableSQL generates CREATE TABLE SQL statements from the schema
func GenerateCreateTableSQL() []string {
	var statements []string

	// Add UUID extension first
	statements = append(statements, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`)

	schemas := GetExpectedSchema()

	for _, table := range schemas {
		// Skip schema_migrations as it's handled by the migration tool
		if table.Name == "schema_migrations" {
			continue
		}

		// Generate CREATE TABLE statement
		sql := generateTableSQL(table)
		statements = append(statements, sql)

		// Generate CREATE INDEX statements
		for _, index := range table.Indexes {
			// Skip primary key indexes as they're created with the table
			if strings.HasSuffix(index.Name, "_pkey") {
				continue
			}

			indexSQL := generateIndexSQL(table.Name, index)
			statements = append(statements, indexSQL)
		}

		// Add partial unique index for user_providers.is_primary
		if table.Name == "user_providers" {
			statements = append(statements,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_providers_one_primary ON user_providers(user_id) WHERE is_primary = true`)
		}
	}

	// Add schema_migrations table separately
	statements = append(statements, `CREATE TABLE IF NOT EXISTS schema_migrations (
	version BIGINT NOT NULL PRIMARY KEY,
	dirty BOOLEAN NOT NULL DEFAULT FALSE
)`)

	// Insert initial migration version
	statements = append(statements, `INSERT INTO schema_migrations (version, dirty) VALUES (8, false) ON CONFLICT (version) DO NOTHING`)

	return statements
}

// generateTableSQL generates a CREATE TABLE statement for a table
func generateTableSQL(table TableSchema) string {
	var columns []string
	var constraints []string

	// Add columns
	for _, col := range table.Columns {
		columnDef := fmt.Sprintf("\t%s %s", col.Name, mapDataTypeToSQLWithLength(col.DataType, col.Name, table.Name))

		if col.IsPrimaryKey {
			columnDef += " PRIMARY KEY DEFAULT uuid_generate_v4()"
		}

		if !col.IsNullable {
			columnDef += " NOT NULL"
		}

		// Add default values for timestamps
		if col.Name == "created_at" || col.Name == "updated_at" {
			columnDef += " DEFAULT CURRENT_TIMESTAMP"
		}

		// Add UNIQUE constraint for email in users table
		if table.Name == "users" && col.Name == "email" {
			columnDef += " UNIQUE"
		}

		// Add UNIQUE constraint for token in refresh_tokens table
		if table.Name == "refresh_tokens" && col.Name == "token" {
			columnDef += " UNIQUE"
		}

		columns = append(columns, columnDef)
	}

	// Add constraints
	for _, constraint := range table.Constraints {
		switch constraint.Type {
		case "FOREIGN KEY":
			fkDef := fmt.Sprintf("\tFOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE",
				getForeignKeyColumn(table, constraint),
				constraint.ForeignTable,
				strings.Join(constraint.ForeignColumns, ", "))
			constraints = append(constraints, fkDef)

		case "CHECK":
			checkDef := fmt.Sprintf("\tCONSTRAINT %s CHECK (%s)", constraint.Name, constraint.Definition)
			constraints = append(constraints, checkDef)
		}
	}

	// Add unique constraints
	for _, index := range table.Indexes {
		if index.IsUnique && !strings.HasSuffix(index.Name, "_pkey") && !strings.HasSuffix(index.Name, "_key") {
			if len(index.Columns) > 1 {
				uniqueDef := fmt.Sprintf("\tUNIQUE(%s)", strings.Join(index.Columns, ", "))
				constraints = append(constraints, uniqueDef)
			}
		}
	}

	// Special case for threat_models foreign key with ON DELETE RESTRICT
	if table.Name == "threat_models" {
		for i, c := range constraints {
			if strings.Contains(c, "owner_email") {
				constraints[i] = strings.Replace(c, "ON DELETE CASCADE", "ON DELETE RESTRICT", 1)
			}
		}
	}

	// Combine columns and constraints
	allDefs := append(columns, constraints...)

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n)", table.Name, strings.Join(allDefs, ",\n"))
}

// generateIndexSQL generates a CREATE INDEX statement
func generateIndexSQL(tableName string, index IndexSchema) string {
	unique := ""
	if index.IsUnique {
		unique = "UNIQUE "
	}

	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s(%s)",
		unique,
		index.Name,
		tableName,
		strings.Join(index.Columns, ", "))
}

// mapDataTypeToSQL maps internal data types to PostgreSQL SQL types
func mapDataTypeToSQL(dataType string) string {
	switch dataType {
	case "uuid":
		return "UUID"
	case "character varying":
		return "VARCHAR(255)"
	case "text":
		return "TEXT"
	case "timestamp without time zone":
		return "TIMESTAMP"
	case "boolean":
		return "BOOLEAN"
	case "bigint":
		return "BIGINT"
	case "jsonb":
		return "JSONB"
	default:
		return dataType
	}
}

// mapDataTypeToSQLWithLength maps internal data types to PostgreSQL SQL types with specific lengths
func mapDataTypeToSQLWithLength(dataType string, columnName string, tableName string) string {
	// Special cases for specific columns that need different lengths
	if dataType == "character varying" {
		switch {
		case columnName == "email":
			return "VARCHAR(320)" // RFC 5321 maximum email length
		case columnName == "token" && tableName == "refresh_tokens":
			return "VARCHAR(512)" // Reasonable length for JWT tokens
		case columnName == "provider" && tableName == "user_providers":
			return "VARCHAR(50)" // Provider names are short
		case columnName == "role":
			return "VARCHAR(50)" // Role names are short
		case columnName == "severity" || columnName == "likelihood" || columnName == "risk_level":
			return "VARCHAR(20)" // Enum-like values are short
		case columnName == "type" && tableName == "diagrams":
			return "VARCHAR(50)" // Diagram types are short
		default:
			return "VARCHAR(255)" // Default length
		}
	}
	return mapDataTypeToSQL(dataType)
}

// getForeignKeyColumn determines the foreign key column name from the constraint
func getForeignKeyColumn(_ TableSchema, constraint ConstraintSchema) string {
	// Map constraint names to column names
	switch constraint.Name {
	case "user_providers_user_id_fkey":
		return "user_id"
	case "threat_models_owner_email_fkey":
		return "owner_email"
	case "threat_model_access_threat_model_id_fkey":
		return "threat_model_id"
	case "threat_model_access_user_email_fkey":
		return "user_email"
	case "threats_threat_model_id_fkey":
		return "threat_model_id"
	case "diagrams_threat_model_id_fkey":
		return "threat_model_id"
	case "refresh_tokens_user_id_fkey":
		return "user_id"
	default:
		// Try to infer from constraint name
		parts := strings.Split(constraint.Name, "_")
		if len(parts) >= 3 {
			return strings.Join(parts[1:len(parts)-1], "_")
		}
		return ""
	}
}

// GetTableNames returns a list of all table names in the schema
func GetTableNames() []string {
	schemas := GetExpectedSchema()
	names := make([]string, len(schemas))
	for i, schema := range schemas {
		names[i] = schema.Name
	}
	return names
}
