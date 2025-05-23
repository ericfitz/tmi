# Database Schema Validation

This document describes the database schema validation system implemented for the TMI application.

## Overview

The schema validation system ensures that the database is in the expected state when the server starts and after running migrations or setup scripts. It validates:

- Table existence
- Column definitions (names, data types, nullability)
- Indexes
- Constraints (foreign keys, check constraints)

## Components

### 1. Schema Definition (`internal/dbschema/schema.go`)

Defines the expected database schema with:

- `TableSchema`: Represents a table with columns, indexes, and constraints
- `ColumnSchema`: Defines column properties
- `IndexSchema`: Defines index properties
- `ConstraintSchema`: Defines constraint properties
- `GetExpectedSchema()`: Returns the complete expected schema for all tables

### 2. Schema Validator (`internal/dbschema/validator.go`)

Provides validation functionality:

- `ValidateSchema(db *sql.DB)`: Validates the actual database against expected schema
- `LogValidationResults(results []ValidationResult)`: Logs validation results with appropriate log levels
- Validates tables, columns, indexes, and constraints
- Handles PostgreSQL data type variations

### 3. Integration Points

The schema validation is integrated into:

#### Server Startup (`cmd/server/main.go`)

- Validates schema after auth system initialization
- Logs errors if schema doesn't match expectations
- Continues running (in development) even if validation fails

#### Setup Database Tool (`cmd/setup-db/main.go`)

- Validates schema after creating all tables
- Provides immediate feedback on setup success

#### Migration Tool (`cmd/migrate/main.go`)

- Validates schema after running migrations
- Ensures migrations resulted in expected schema

#### Check Database Tool (`cmd/check-db/main.go`)

- Uses shared validation system
- Provides detailed validation results and row counts

## Usage

### Running Schema Validation

1. **During Server Startup**:

   ```bash
   go run cmd/server/main.go --env=.env.dev
   ```

   The server will automatically validate the schema and log results.

2. **Using Check-DB Tool**:

   ```bash
   go run cmd/check-db/main.go
   ```

   Provides detailed validation results and table statistics.

3. **After Setup**:

   ```bash
   go run cmd/setup-db/main.go
   ```

   Validates schema after creating tables.

4. **After Migrations**:
   ```bash
   go run cmd/migrate/main.go --env=.env.dev
   ```
   Validates schema after applying migrations.

## Validation Output

The validation system provides different levels of output:

- **Debug**: Detailed information about each validation check
- **Info**: Summary of validation results
- **Warn**: Warnings about unexpected elements (e.g., extra columns)
- **Error**: Critical issues (missing tables, wrong data types)

Example output:

```
Starting database schema validation
Validating table: users
Validated column users.id: type=character varying, nullable=false
Validated column users.email: type=character varying, nullable=false
...
Database schema validation completed successfully - all tables match expected schema
```

## Error Handling

- Missing tables are reported as errors
- Incorrect column data types are errors
- Missing indexes are errors
- Extra columns/indexes are warnings
- Validation continues even if errors are found to provide complete results

## Testing

Unit tests are provided in `internal/dbschema/schema_test.go` to verify:

- Schema definition correctness
- Data type normalization
- Data type comparison logic

Run tests with:

```bash
go test ./internal/dbschema/...
```

## Future Enhancements

Potential improvements:

1. Add support for validating default values
2. Validate sequence/auto-increment settings
3. Add schema migration suggestions when mismatches are found
4. Support for custom validation rules
5. Schema versioning support
