# tmi-dbtool Schema Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `tmi-seed` to `tmi-dbtool`, add schema creation/migration and health check modes, modify server startup to detect DDL permission errors gracefully, and document database security strategies.

**Architecture:** A shared `internal/dbcheck` package provides permission error classification and schema health checking, used by both the dbtool CLI and the server startup. The dbtool CLI is restructured with new flag-based arguments replacing `--mode`, a startup banner with build info, and a JSON exit summary. The server's Phase 2 wraps AutoMigrate with error classification to provide clear guidance when DDL permissions are insufficient.

**Tech Stack:** Go, GORM, `flag` package, `database/sql` for schema introspection, `-ldflags` for build info injection.

**Spec:** `docs/superpowers/specs/2026-04-11-dbtool-schema-migration-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/dbcheck/dbcheck.go` | `IsPermissionError(err, dbType)` — classifies DB errors as permission-related |
| `internal/dbcheck/dbcheck_test.go` | Tests for permission error classification |
| `internal/dbcheck/health.go` | `CheckSchemaHealth(db, dbType)` — compares expected tables against actual |
| `internal/dbcheck/health_test.go` | Tests for schema health check |
| `cmd/dbtool/schema.go` | `runSchema()` — AutoMigrate + post-migration fixups + SeedDatabase |
| `cmd/dbtool/health.go` | `runHealthCheck()` — DB info, schema status, system data status |

### Renamed Files (git mv)

| Before | After |
|--------|-------|
| `cmd/seed/main.go` | `cmd/dbtool/main.go` |
| `cmd/seed/types.go` | `cmd/dbtool/types.go` |
| `cmd/seed/data.go` | `cmd/dbtool/data.go` |
| `cmd/seed/data_db.go` | `cmd/dbtool/data_db.go` |
| `cmd/seed/data_api.go` | `cmd/dbtool/data_api.go` |
| `cmd/seed/config.go` | `cmd/dbtool/config.go` |
| `cmd/seed/reference.go` | `cmd/dbtool/reference.go` |
| `scripts/run-seed.py` | `scripts/run-dbtool.py` |

### Removed Files

| File | Reason |
|------|--------|
| `cmd/seed/system.go` | Merged into `cmd/dbtool/schema.go` (was only 26 lines) |
| `cmd/dbtool/system.go` | Removed after rename, replaced by `schema.go` |

### Modified Files

| File | Change |
|------|--------|
| `cmd/dbtool/main.go` | Complete rewrite: new flag-based CLI, startup banner, exit summary JSON, health check dispatch |
| `cmd/dbtool/types.go` | Add `ToolInfo`, `ExitSummary` structs |
| `cmd/dbtool/data_api.go` | Add idempotent pre-checks for API-created objects |
| `cmd/server/main.go` | Phase 2: wrap AutoMigrate with permission detection and schema health fallback |
| `Makefile` | Rename `build-seed`/`build-seed-oci` to `build-dbtool`/`build-dbtool-oci` |
| `scripts/build-server.py` | Component `seed` -> `dbtool`, enable ldflags for dbtool |
| `scripts/run-dbtool.py` | Updated binary name and references |
| `scripts/run-cats-fuzz.py` | Update references to new tool name |

---

## Task 1: Permission Error Classification (`internal/dbcheck`)

**Files:**
- Create: `internal/dbcheck/dbcheck.go`
- Create: `internal/dbcheck/dbcheck_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/dbcheck/dbcheck_test.go
package dbcheck

import (
	"errors"
	"testing"
)

func TestIsPermissionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		dbType   string
		expected bool
	}{
		// PostgreSQL
		{"pg permission denied", errors.New("ERROR: permission denied for table users (SQLSTATE 42501)"), "postgres", true},
		{"pg insufficient privilege", errors.New("insufficient_privilege"), "postgres", true},
		{"pg permission denied simple", errors.New("permission denied"), "postgres", true},
		{"pg connection error", errors.New("connection refused"), "postgres", false},
		{"pg syntax error", errors.New("syntax error at or near"), "postgres", false},

		// Oracle
		{"ora insufficient privileges", errors.New("ORA-01031: insufficient privileges"), "oracle", true},
		{"ora no privileges on tablespace", errors.New("ORA-01950: no privileges on tablespace"), "oracle", true},
		{"ora table already exists", errors.New("ORA-00955: name is already used by an existing object"), "oracle", false},
		{"ora connection error", errors.New("ORA-12541: TNS:no listener"), "oracle", false},

		// MySQL
		{"mysql command denied", errors.New("Error 1142: INSERT command denied to user"), "mysql", true},
		{"mysql access denied", errors.New("Error 1044: Access denied for user"), "mysql", true},
		{"mysql syntax error", errors.New("Error 1064: You have an error in your SQL syntax"), "mysql", false},

		// SQL Server
		{"mssql create table denied", errors.New("Error 262: CREATE TABLE permission denied in database"), "sqlserver", true},
		{"mssql connection error", errors.New("login failed for user"), "sqlserver", false},

		// SQLite (never permission errors from DB roles)
		{"sqlite readonly", errors.New("attempt to write a readonly database"), "sqlite", false},

		// Unknown database type
		{"unknown type", errors.New("permission denied"), "unknown", false},

		// Nil-safe
		{"nil error", nil, "postgres", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPermissionError(tt.err, tt.dbType)
			if got != tt.expected {
				errStr := "<nil>"
				if tt.err != nil {
					errStr = tt.err.Error()
				}
				t.Errorf("IsPermissionError(%q, %q) = %v, want %v", errStr, tt.dbType, got, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dbcheck/ -run TestIsPermissionError -v`
Expected: FAIL — package not found

- [ ] **Step 3: Write the implementation**

```go
// internal/dbcheck/dbcheck.go
package dbcheck

import "strings"

// IsPermissionError returns true if the given error indicates insufficient
// database privileges for DDL operations (CREATE TABLE, ALTER TABLE, etc.).
//
// This is used by the server startup to distinguish "schema needs migration
// but user lacks DDL permissions" from other migration errors.
func IsPermissionError(err error, dbType string) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	switch dbType {
	case "postgres", "postgresql":
		return strings.Contains(errStr, "42501") ||
			strings.Contains(errStr, "insufficient_privilege") ||
			strings.Contains(errStr, "permission denied")

	case "oracle":
		return strings.Contains(errStr, "ora-01031") ||
			strings.Contains(errStr, "ora-01950")

	case "mysql":
		return strings.Contains(errStr, "error 1142") ||
			strings.Contains(errStr, "error 1044")

	case "sqlserver":
		return strings.Contains(errStr, "error 262") ||
			strings.Contains(errStr, "permission denied")

	default:
		return false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dbcheck/ -run TestIsPermissionError -v`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dbcheck/dbcheck.go internal/dbcheck/dbcheck_test.go
git commit -m "feat(dbcheck): add permission error classification

Classifies database errors as permission-related (insufficient DDL
privileges) across PostgreSQL, Oracle, MySQL, and SQL Server. Used
by server startup and tmi-dbtool for graceful handling.

Refs #251"
```

---

## Task 2: Schema Health Check (`internal/dbcheck`)

**Files:**
- Create: `internal/dbcheck/health.go`
- Create: `internal/dbcheck/health_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/dbcheck/health_test.go
package dbcheck

import (
	"testing"
)

func TestSchemaHealthResult_IsCurrent(t *testing.T) {
	tests := []struct {
		name     string
		result   SchemaHealthResult
		expected bool
	}{
		{
			"all tables present",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  44,
				MissingTables:  nil,
			},
			true,
		},
		{
			"some tables missing",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  42,
				MissingTables:  []string{"teams", "projects"},
			},
			false,
		},
		{
			"empty database",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  0,
				MissingTables:  []string{"users", "groups"},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.IsCurrent()
			if got != tt.expected {
				t.Errorf("IsCurrent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExpectedTableNames(t *testing.T) {
	names := ExpectedTableNames()
	if len(names) == 0 {
		t.Fatal("ExpectedTableNames() returned empty list")
	}
	// Check a few known tables exist
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	required := []string{"users", "groups", "threat_models", "diagrams", "threats"}
	for _, r := range required {
		if !nameSet[r] {
			t.Errorf("Expected table %q not in ExpectedTableNames()", r)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dbcheck/ -run 'TestSchemaHealth|TestExpectedTable' -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/dbcheck/health.go
package dbcheck

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// SchemaHealthResult reports the state of the database schema.
type SchemaHealthResult struct {
	DatabaseType    string   `json:"database_type"`
	DatabaseVersion string   `json:"database_version"`
	ExpectedTables  int      `json:"expected_tables"`
	PresentTables   int      `json:"present_tables"`
	MissingTables   []string `json:"missing_tables,omitempty"`
}

// IsCurrent returns true if all expected tables are present.
func (r *SchemaHealthResult) IsCurrent() bool {
	return len(r.MissingTables) == 0
}

// ExpectedTableNames returns the list of table names that TMI expects to exist.
// Derived from the GORM model definitions in api/models.
func ExpectedTableNames() []string {
	allModels := models.AllModels()
	names := make([]string, 0, len(allModels))

	// Use a temporary GORM statement to extract table names from models
	// without needing a real database connection.
	for _, model := range allModels {
		stmt := &gorm.Statement{}
		if err := stmt.Parse(model); err == nil && stmt.Table != "" {
			names = append(names, stmt.Table)
		}
	}

	// Fallback: if GORM parsing didn't work (no DB context), use the hardcoded list
	// from the existing schema validator.
	if len(names) == 0 {
		names = hardcodedTableNames()
	}

	return names
}

// hardcodedTableNames returns the known TMI table names as a fallback.
func hardcodedTableNames() []string {
	return []string{
		"users", "refresh_tokens", "client_credentials",
		"groups", "group_members",
		"threat_models", "threat_model_access",
		"threats", "diagrams", "assets", "documents", "notes", "repositories", "metadata",
		"collaboration_sessions", "session_participants",
		"webhook_subscriptions", "webhook_quotas", "webhook_url_deny_list",
		"addons", "addon_invocation_quotas",
		"user_api_quotas", "user_preferences",
		"system_settings",
		"survey_templates", "survey_responses", "survey_answers", "triage_notes",
		"teams", "team_members", "projects", "project_notes", "team_notes",
		"timmy_sessions", "timmy_messages", "timmy_embeddings", "timmy_usage",
		"audit_entries", "version_snapshots",
	}
}

// CheckSchemaHealth queries the database to determine which expected tables exist.
// Works across PostgreSQL, Oracle, MySQL, SQL Server, and SQLite.
func CheckSchemaHealth(sqlDB *sql.DB, dbType string) (*SchemaHealthResult, error) {
	log := slogging.Get()

	result := &SchemaHealthResult{
		DatabaseType: dbType,
	}

	// Get database version
	version, err := getDatabaseVersion(sqlDB, dbType)
	if err != nil {
		log.Debug("Could not determine database version: %v", err)
		result.DatabaseVersion = "unknown"
	} else {
		result.DatabaseVersion = version
	}

	// Get expected table names
	expected := ExpectedTableNames()
	result.ExpectedTables = len(expected)

	// Check which tables exist
	for _, table := range expected {
		exists, err := tableExistsForType(sqlDB, dbType, table)
		if err != nil {
			return nil, fmt.Errorf("error checking table %s: %w", table, err)
		}
		if exists {
			result.PresentTables++
		} else {
			result.MissingTables = append(result.MissingTables, table)
		}
	}

	return result, nil
}

// getDatabaseVersion returns a human-readable database version string.
func getDatabaseVersion(db *sql.DB, dbType string) (string, error) {
	var query string
	switch dbType {
	case "postgres", "postgresql":
		query = "SELECT version()"
	case "oracle":
		query = "SELECT banner FROM v$version WHERE ROWNUM = 1"
	case "mysql":
		query = "SELECT version()"
	case "sqlserver":
		query = "SELECT @@VERSION"
	case "sqlite":
		query = "SELECT sqlite_version()"
	default:
		return "unknown", nil
	}

	var version string
	if err := db.QueryRow(query).Scan(&version); err != nil {
		return "", err
	}
	return version, nil
}

// tableExistsForType checks if a table exists, handling different DB dialects.
func tableExistsForType(db *sql.DB, dbType, tableName string) (bool, error) {
	var query string
	var args []any

	switch dbType {
	case "postgres", "postgresql":
		query = "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)"
		args = []any{tableName}
	case "oracle":
		query = "SELECT COUNT(*) FROM all_tables WHERE UPPER(table_name) = UPPER(:1)"
		args = []any{tableName}
	case "mysql":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
		args = []any{tableName}
	case "sqlserver":
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = @p1"
		args = []any{tableName}
	case "sqlite":
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		args = []any{tableName}
	default:
		return false, fmt.Errorf("unsupported database type: %s", dbType)
	}

	var result any
	if err := db.QueryRow(query, args...).Scan(&result); err != nil {
		return false, err
	}

	// PostgreSQL returns bool, others return count
	switch v := result.(type) {
	case bool:
		return v, nil
	case int64:
		return v > 0, nil
	default:
		// Try string conversion for edge cases
		return fmt.Sprint(v) != "0" && strings.ToLower(fmt.Sprint(v)) != "false", nil
	}
}
```

Note: The `ExpectedTableNames()` function tries to derive table names from GORM models. If that doesn't work without a DB connection (GORM may need a dialector), it falls back to a hardcoded list. The implementer should test both paths and ensure one works.

- [ ] **Step 4: Run tests**

Run: `cd /Users/efitz/Projects/tmi && go test ./internal/dbcheck/ -v`
Expected: PASS

- [ ] **Step 5: Lint and build**

Run: `make lint && make build-server`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/dbcheck/health.go internal/dbcheck/health_test.go
git commit -m "feat(dbcheck): add schema health check

Cross-database schema health checker that compares expected TMI tables
against actual tables. Supports PostgreSQL, Oracle, MySQL, SQL Server,
and SQLite. Reports database version, present/missing tables.

Refs #251"
```

---

## Task 3: Rename `cmd/seed/` to `cmd/dbtool/`

**Files:**
- Rename: all files in `cmd/seed/` -> `cmd/dbtool/`
- Remove: `cmd/dbtool/system.go` (will be replaced by `schema.go` in next task)

- [ ] **Step 1: Git mv the directory**

```bash
cd /Users/efitz/Projects/tmi
git mv cmd/seed cmd/dbtool
```

- [ ] **Step 2: Remove system.go (will be replaced by schema.go)**

```bash
git rm cmd/dbtool/system.go
```

- [ ] **Step 3: Verify build**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool`
Expected: Compiles (will have a missing `runSystemSeed` reference — that's OK, we fix it next task)

Actually, this will fail because `main.go` calls `runSystemSeed`. We need to add a temporary stub. Create a minimal stub:

```go
// cmd/dbtool/schema.go (temporary — replaced fully in Task 4)
package main

import (
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

func runSystemSeed(db *testdb.TestDB, dryRun bool) error {
	log := slogging.Get()
	if dryRun {
		log.Info("[DRY RUN] Would run schema migration and seed system data")
		return nil
	}
	log.Info("Running schema migration and seeding system data...")
	if err := db.AutoMigrate(); err != nil {
		return err
	}
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return err
	}
	log.Info("Schema migration and system seed complete")
	return nil
}
```

- [ ] **Step 4: Verify build**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool`
Expected: Compiles

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/dbtool/ cmd/seed/
git commit -m "refactor: rename cmd/seed to cmd/dbtool

Renames the unified seed tool directory to reflect its expanded scope
as a general-purpose database administration tool. The system.go file
is replaced by schema.go with AutoMigrate + seed combined.

Refs #251"
```

---

## Task 4: Schema Mode with Post-Migration Fixups

**Files:**
- Modify: `cmd/dbtool/schema.go` (replace temporary stub with full implementation)

- [ ] **Step 1: Replace schema.go with full implementation**

```go
// cmd/dbtool/schema.go
package main

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSchema runs schema creation/migration, post-migration fixups, and system data seeding.
// This is the --schema / -s operation.
func runSchema(db *testdb.TestDB, dryRun, verbose bool) error {
	log := slogging.Get()

	if dryRun {
		return runSchemaDryRun(db, verbose)
	}

	// Step 1: AutoMigrate
	log.Info("Running GORM AutoMigrate...")
	allModels := api.GetAllModels()
	if err := db.DB().AutoMigrate(allModels...); err != nil {
		errStr := err.Error()
		// Oracle benign errors
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
		} else {
			return fmt.Errorf("AutoMigrate failed: %w", err)
		}
	}
	log.Info("AutoMigrate completed for %d models", len(allModels))

	// Step 2: Post-migration fixups
	log.Info("Running post-migration fixups...")
	runPostMigrationFixups(db, verbose)

	// Step 3: Seed system data
	log.Info("Seeding system data (groups, webhook deny list)...")
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return fmt.Errorf("failed to seed system data: %w", err)
	}

	log.Info("Schema migration and system seed complete")
	return nil
}

// runSchemaDryRun reports what schema changes would be made without writing.
func runSchemaDryRun(db *testdb.TestDB, verbose bool) error {
	log := slogging.Get()
	log.Info("[DRY RUN] Checking schema status...")

	sqlDB, err := db.DB().DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	health, err := dbcheck.CheckSchemaHealth(sqlDB, db.DialectName())
	if err != nil {
		return fmt.Errorf("failed to check schema health: %w", err)
	}

	log.Info("[DRY RUN] Database: %s %s", health.DatabaseType, health.DatabaseVersion)
	log.Info("[DRY RUN] Tables: %d/%d present", health.PresentTables, health.ExpectedTables)

	if health.IsCurrent() {
		log.Info("[DRY RUN] Schema is up to date. No migrations needed.")
	} else {
		log.Info("[DRY RUN] Missing tables (%d):", len(health.MissingTables))
		for _, t := range health.MissingTables {
			log.Info("[DRY RUN]   - %s", t)
		}
		log.Info("[DRY RUN] Running --schema would create these tables and seed system data.")
	}

	return nil
}

// runPostMigrationFixups runs data fixups that should happen after schema migration.
// These are idempotent operations that normalize legacy data.
func runPostMigrationFixups(db *testdb.TestDB, verbose bool) {
	log := slogging.Get()

	// Normalize legacy severity enum values to snake_case
	if result := db.DB().Exec(
		"UPDATE threats SET severity = LOWER(severity) WHERE severity IS NOT NULL AND severity != LOWER(severity)",
	); result.Error != nil {
		log.Warn("Failed to normalize severity values (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Info("Normalized %d severity values to lowercase", result.RowsAffected)
	} else if verbose {
		log.Debug("Severity values already normalized")
	}

	// Migrate 'none' severity to 'informational'
	if result := db.DB().Exec(
		"UPDATE threats SET severity = 'informational' WHERE severity = 'none'",
	); result.Error != nil {
		log.Warn("Failed to migrate 'none' severity to 'informational' (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Info("Migrated %d severity values from 'none' to 'informational'", result.RowsAffected)
	} else if verbose {
		log.Debug("No 'none' severity values to migrate")
	}
}
```

- [ ] **Step 2: Verify build**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool`
Expected: Compiles

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/dbtool/schema.go
git commit -m "feat(dbtool): add schema mode with AutoMigrate and post-migration fixups

Schema mode runs GORM AutoMigrate, post-migration data fixups
(severity normalization), and system data seeding. Supports dry-run
with schema health reporting.

Refs #251"
```

---

## Task 5: Health Check Mode

**Files:**
- Create: `cmd/dbtool/health.go`

- [ ] **Step 1: Create health check implementation**

```go
// cmd/dbtool/health.go
package main

import (
	"fmt"

	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runHealthCheck connects to the database and reports schema health.
// This is the no-operation-flag mode.
func runHealthCheck(db *testdb.TestDB, verbose bool) error {
	log := slogging.Get()

	sqlDB, err := db.DB().DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	health, err := dbcheck.CheckSchemaHealth(sqlDB, db.DialectName())
	if err != nil {
		return fmt.Errorf("failed to check schema health: %w", err)
	}

	// Database info
	log.Info("Database: %s", health.DatabaseType)
	log.Info("Version:  %s", health.DatabaseVersion)
	log.Info("")

	// Schema status
	log.Info("Schema Status:")
	log.Info("  Expected tables: %d", health.ExpectedTables)
	log.Info("  Present tables:  %d", health.PresentTables)

	if health.IsCurrent() {
		log.Info("  Status: CURRENT")
	} else {
		log.Info("  Status: NEEDS MIGRATION (%d tables missing)", len(health.MissingTables))
		for _, t := range health.MissingTables {
			log.Info("    - %s", t)
		}
	}

	// System data status
	log.Info("")
	log.Info("System Data:")
	systemDataStatus := checkSystemDataHealth(db, verbose)
	for _, s := range systemDataStatus {
		log.Info("  %s", s)
	}

	if !health.IsCurrent() {
		log.Info("")
		log.Info("To migrate: tmi-dbtool --schema --config=<config-file>")
		return fmt.Errorf("schema needs migration: %d tables missing", len(health.MissingTables))
	}

	return nil
}

// checkSystemDataHealth checks whether required system data exists.
func checkSystemDataHealth(db *testdb.TestDB, verbose bool) []string {
	var status []string

	// Check built-in groups
	var groupCount int64
	db.DB().Table("groups").Where("provider = ?", "*").Count(&groupCount)
	expectedGroups := len(seed.BuiltInGroups())
	if int(groupCount) >= expectedGroups {
		status = append(status, fmt.Sprintf("Built-in groups: %d/%d present", groupCount, expectedGroups))
	} else {
		status = append(status, fmt.Sprintf("Built-in groups: %d/%d present (INCOMPLETE)", groupCount, expectedGroups))
	}

	// Check webhook deny list
	var denyCount int64
	db.DB().Table("webhook_url_deny_list").Count(&denyCount)
	if denyCount > 0 {
		status = append(status, fmt.Sprintf("Webhook deny list: %d entries", denyCount))
	} else {
		status = append(status, "Webhook deny list: EMPTY (needs seeding)")
	}

	return status
}
```

Note: The `seed.BuiltInGroups()` function may not exist — the implementer should check the `api/seed` package and either use it if available or count the expected groups differently (e.g., hardcode 7 based on the known built-in groups, or query by a known group UUID). Adjust as needed.

- [ ] **Step 2: Verify build**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool`
Expected: Compiles (or needs minor adjustments to seed package references)

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/dbtool/health.go
git commit -m "feat(dbtool): add health check mode

No-operation-flag mode that reports database engine info, schema
status (present vs. missing tables), and system data health
(built-in groups, webhook deny list).

Refs #251"
```

---

## Task 6: New CLI Interface with Banner and Exit Summary

**Files:**
- Modify: `cmd/dbtool/main.go` (complete rewrite)
- Modify: `cmd/dbtool/types.go` (add ToolInfo, ExitSummary)

- [ ] **Step 1: Add types for banner and exit summary**

Add to `cmd/dbtool/types.go`:

```go
// ToolInfo holds build-time metadata for the startup banner.
type ToolInfo struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuiltAt      string `json:"built_at"`
	SchemaModels int    `json:"schema_models"`
}

// ExitSummary is the JSON structure printed at exit.
type ExitSummary struct {
	Tool         string         `json:"tool"`
	Version      string         `json:"version"`
	Commit       string         `json:"commit"`
	BuiltAt      string         `json:"built_at"`
	SchemaModels int            `json:"schema_models"`
	Arguments    map[string]any `json:"arguments"`
	Status       string         `json:"status"` // "success" or "failure"
	Error        string         `json:"error"`
}
```

- [ ] **Step 2: Rewrite main.go**

Replace `cmd/dbtool/main.go` entirely:

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// Build-time variables injected via -ldflags
var (
	toolVersion = "development"
	toolCommit  = "unknown"
	toolBuiltAt = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Define flags
	schema := flag.Bool("schema", false, "Create/migrate database schema and seed system data")
	flag.BoolVar(schema, "s", false, "Create/migrate database schema and seed system data (short)")

	importConfig := flag.Bool("import-config", false, "Import config file settings into database")
	flag.BoolVar(importConfig, "c", false, "Import config file settings into database (short)")

	importTestData := flag.Bool("import-test-data", false, "Import test data from a seed file")
	flag.BoolVar(importTestData, "t", false, "Import test data from a seed file (short)")

	inputFile := flag.String("input-file", "", "Input file (config YAML for -c, seed JSON for -t)")
	flag.StringVar(inputFile, "f", "", "Input file (short)")

	configFile := flag.String("config", "", "TMI config file (provides DB connection via database.url)")
	outputFile := flag.String("output", "", "Path for migrated config YAML (with -c)")
	overwrite := flag.Bool("overwrite", false, "Overwrite existing settings (with -c)")

	serverURL := flag.String("server", "http://localhost:8080", "TMI server URL for API calls (with -t)")
	user := flag.String("user", "charlie", "OAuth user ID for API authentication (with -t)")
	provider := flag.String("provider", "tmi", "OAuth provider name (with -t)")

	dryRun := flag.Bool("dry-run", false, "Show what would happen without writing")
	verbose := flag.Bool("verbose", false, "Print step-by-step operations and DB messages")
	flag.BoolVar(verbose, "v", false, "Print step-by-step operations (short)")

	flag.Usage = printUsage
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

	// Print startup banner
	schemaModels := len(api.GetAllModels())
	info := ToolInfo{
		Version:      toolVersion,
		Commit:       toolCommit,
		BuiltAt:      toolBuiltAt,
		SchemaModels: schemaModels,
	}
	fmt.Fprintf(os.Stderr, "tmi-dbtool %s (commit: %s, built: %s)\n", info.Version, info.Commit, info.BuiltAt)
	fmt.Fprintf(os.Stderr, "Schema version: %d models\n\n", info.SchemaModels)

	// Build arguments map for exit summary
	args := map[string]any{
		"schema":           *schema,
		"import_config":    *importConfig,
		"import_test_data": *importTestData,
		"dry_run":          *dryRun,
	}
	if *configFile != "" {
		args["config"] = *configFile
	}
	if *inputFile != "" {
		args["input_file"] = *inputFile
	}

	// Determine which operation to run
	opCount := boolCount(*schema, *importConfig, *importTestData)

	// Resolve config file
	dbConfigFile := *configFile
	if dbConfigFile == "" && *importConfig && *inputFile != "" {
		dbConfigFile = *inputFile
	}
	if dbConfigFile == "" {
		printExitSummary(info, args, "failure", "No database connection. Provide --config or set TMI_DATABASE_URL")
		return 1
	}

	// Connect to database
	log := slogging.Get()
	log.Info("Connecting to database...")
	db, err := testdb.New(dbConfigFile)
	if err != nil {
		printExitSummary(info, args, "failure", fmt.Sprintf("Failed to connect to database: %v", err))
		return 1
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()
	log.Info("Connected to %s database", db.DialectName())

	// Dispatch
	var runErr error

	if opCount == 0 {
		// No operation flags — health check mode
		runErr = runHealthCheck(db, *verbose)
	} else if opCount > 1 {
		runErr = fmt.Errorf("only one operation flag can be specified at a time (-s, -c, -t)")
	} else if *schema {
		// Ensure schema is up to date first (AutoMigrate runs inside runSchema)
		runErr = runSchema(db, *dryRun, *verbose)
	} else if *importConfig {
		if *inputFile == "" {
			runErr = fmt.Errorf("--input-file / -f is required for --import-config")
		} else {
			// Run schema first to ensure tables exist
			if !*dryRun {
				if migrateErr := ensureSchema(db); migrateErr != nil {
					log.Warn("Schema migration skipped or failed: %v", migrateErr)
				}
			}
			runErr = runConfigSeed(db, *inputFile, *outputFile, *overwrite, *dryRun)
		}
	} else if *importTestData {
		if *inputFile == "" {
			runErr = fmt.Errorf("--input-file / -f is required for --import-test-data")
		} else {
			// Run schema first to ensure tables exist
			if !*dryRun {
				if migrateErr := ensureSchema(db); migrateErr != nil {
					log.Warn("Schema migration skipped or failed: %v", migrateErr)
				}
			}
			runErr = runDataSeed(db, *inputFile, *serverURL, *user, *provider, *dryRun)
		}
	}

	if runErr != nil {
		log.Error("%v", runErr)
		printExitSummary(info, args, "failure", runErr.Error())
		return 1
	}

	printExitSummary(info, args, "success", "")
	return 0
}

// ensureSchema runs AutoMigrate + system seed if needed.
// Errors are logged but non-fatal for import operations.
func ensureSchema(db *testdb.TestDB) error {
	log := slogging.Get()
	log.Info("Ensuring schema is up to date...")
	if err := db.AutoMigrate(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
			return nil
		}
		return err
	}
	return nil
}

// printExitSummary prints the JSON exit summary to stdout.
func printExitSummary(info ToolInfo, args map[string]any, status, errMsg string) {
	summary := ExitSummary{
		Tool:         "tmi-dbtool",
		Version:      info.Version,
		Commit:       info.Commit,
		BuiltAt:      info.BuiltAt,
		SchemaModels: info.SchemaModels,
		Arguments:    args,
		Status:       status,
		Error:        errMsg,
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(data))
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `tmi-dbtool - TMI Database Administration Tool

Usage: tmi-dbtool [OPTIONS]

Database Operations:
  -s, --schema              Create/migrate schema and seed system data
  -c, --import-config       Import config file settings into database
  -t, --import-test-data    Import test data from a seed file

Input:
  -f, --input-file FILE     Input file (config YAML for -c, seed JSON for -t)
      --config FILE         TMI config file (provides DB connection via database.url)

Output:
      --output FILE         Path for migrated config YAML (with -c)

Behavior:
      --dry-run             Show what would happen without writing
      --overwrite           Overwrite existing settings (with -c)
  -v, --verbose             Print step-by-step operations and DB messages
  -h, --help                Print usage

Test Data Options (with -t):
      --server URL          TMI server URL for API calls (default: http://localhost:8080)
      --user USER           OAuth user ID (default: charlie)
      --provider PROVIDER   OAuth provider name (default: tmi)

No-argument mode (health check):
  tmi-dbtool --config=config.yml
  Connects to database, prints engine info and schema health report.

Examples:
  tmi-dbtool --config=config-development.yml                    # Health check
  tmi-dbtool -s --config=config-development.yml                 # Schema + seed
  tmi-dbtool -s --config=config-development.yml --dry-run       # Preview changes
  tmi-dbtool -c -f config-production.yml                        # Import config
  tmi-dbtool -t -f test/seeds/cats-seed-data.json --config=config-development.yml
`)
}

func boolCount(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}
```

- [ ] **Step 3: Verify build**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool`
Expected: Compiles

- [ ] **Step 4: Test help output**

Run: `./bin/tmi-dbtool -h`
Expected: Shows the usage text with all flags

- [ ] **Step 5: Lint and unit tests**

Run: `make lint && make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/dbtool/main.go cmd/dbtool/types.go
git commit -m "feat(dbtool): new flag-based CLI with banner and exit summary

Replaces --mode flag with -s/--schema, -c/--import-config,
-t/--import-test-data. Adds startup banner with version/commit/models,
JSON exit summary with status/error. No-arg mode runs health check.
Build info injected via -ldflags.

Refs #251"
```

---

## Task 7: Server Startup DDL Permission Detection

**Files:**
- Modify: `cmd/server/main.go` (Phase 2, around line 602-644)

- [ ] **Step 1: Modify Phase 2 in server startup**

Replace the Phase 2 block (lines 602-644 approximately) with:

```go
	// ==== PHASE 2: Migrations ====
	// All databases use GORM AutoMigrate for schema management
	// This provides a single source of truth (api/models/models.go) for all supported databases
	logger.Info("==== PHASE 2: Running database migrations ====")
	logger.Info("Running GORM AutoMigrate for %s database", dbType)
	allModels := api.GetAllModels()
	if err := gormDB.AutoMigrate(allModels...); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") {
			// Oracle: table already exists — benign, continue
			logger.Debug("Some tables already exist, continuing: %v", err)
		} else if dbcheck.IsPermissionError(err, dbType) {
			// DDL permission denied — check if schema is already current
			logger.Warn("AutoMigrate failed with permission error: %v", err)
			sqlDB, sqlErr := gormDB.DB().DB()
			if sqlErr != nil {
				logger.Error("Failed to get sql.DB for schema check: %v", sqlErr)
				os.Exit(1)
			}
			health, healthErr := dbcheck.CheckSchemaHealth(sqlDB, dbType)
			if healthErr != nil {
				logger.Error("Failed to check schema health: %v", healthErr)
				os.Exit(1)
			}
			if health.IsCurrent() {
				logger.Warn("DDL permissions unavailable, but schema is up to date. Proceeding.")
			} else {
				logger.Error("Database schema requires updates but this database user lacks DDL permissions.")
				logger.Error("")
				logger.Error("Missing tables: %s", strings.Join(health.MissingTables, ", "))
				logger.Error("")
				logger.Error("To resolve this, choose one of:")
				logger.Error("  1. Run schema migration with an admin-privileged database user:")
				logger.Error("     tmi-dbtool --schema --config=<config-file>")
				logger.Error("  2. Grant DDL permissions to the current database user.")
				logger.Error("")
				logger.Error("See: https://github.com/ericfitz/tmi/wiki/Database-Security-Strategies")
				os.Exit(1)
			}
		} else {
			logger.Error("Failed to auto-migrate schema: %v", err)
			os.Exit(1)
		}
	}
	logger.Info("GORM AutoMigrate completed for %d models", len(allModels))

	// Post-migration data fixups (idempotent)
	if result := gormDB.DB().Exec(
		"UPDATE threats SET severity = LOWER(severity) WHERE severity IS NOT NULL AND severity != LOWER(severity)",
	); result.Error != nil {
		logger.Warn("Failed to normalize severity values (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Info("Normalized %d severity values to lowercase", result.RowsAffected)
	}
	if result := gormDB.DB().Exec(
		"UPDATE threats SET severity = 'informational' WHERE severity = 'none'",
	); result.Error != nil {
		logger.Warn("Failed to migrate 'none' severity to 'informational' (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Info("Migrated %d severity values from 'none' to 'informational'", result.RowsAffected)
	}

	// Seed required data (everyone group, webhook deny list)
	if err := seed.SeedDatabase(gormDB.DB()); err != nil {
		logger.Error("Failed to seed database: %v", err)
		os.Exit(1)
	}
```

Add the import for `dbcheck`:

```go
import (
	// ... existing imports ...
	"github.com/ericfitz/tmi/internal/dbcheck"
)
```

- [ ] **Step 2: Build and test**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): detect DDL permission errors at startup

When AutoMigrate fails with a permission error, the server checks
if the schema is already current. If current, it proceeds with a
warning. If schema needs updates, it stops with a clear message
directing the operator to tmi-dbtool or granting DDL permissions.

Refs #251"
```

---

## Task 8: Build Scripts and Makefile Rename

**Files:**
- Rename: `scripts/run-seed.py` -> `scripts/run-dbtool.py`
- Modify: `scripts/build-server.py`
- Modify: `scripts/run-cats-fuzz.py`
- Modify: `Makefile`

- [ ] **Step 1: Rename the run script**

```bash
git mv scripts/run-seed.py scripts/run-dbtool.py
```

- [ ] **Step 2: Update `scripts/run-dbtool.py`**

Update all references from `tmi-seed` to `tmi-dbtool`, from `seed` to `dbtool`, and update the `run_seed` function to use the new flag syntax (`--mode=data` becomes `-t`):

Key changes:
- Binary name: `./bin/tmi-seed` -> `./bin/tmi-dbtool`
- Go package: `github.com/ericfitz/tmi/cmd/seed` -> `github.com/ericfitz/tmi/cmd/dbtool`
- Run command: `--mode=data` -> `-t`
- `--input` -> `--input-file`
- Function names: `build_seed` -> `build_dbtool`, `run_seed` -> `run_dbtool`

- [ ] **Step 3: Update `scripts/build-server.py`**

Change the component entry from `"seed"` to `"dbtool"`:

```python
"dbtool": {
    "output": "bin/tmi-dbtool",
    "package": "github.com/ericfitz/tmi/cmd/dbtool",
    "tags": [],
    "ldflags": True,  # Enable ldflags for version injection
},
```

Also update the `--oci` check from `component != "seed"` to `component != "dbtool"`.

Also update the ldflags prefix for dbtool — the server uses `github.com/ericfitz/tmi/api` but dbtool uses `github.com/ericfitz/tmi/cmd/dbtool`. The `build_ldflags` function needs to support different prefixes per component.

- [ ] **Step 4: Update `scripts/run-cats-fuzz.py`**

Update references from `run-seed.py` to `run-dbtool.py`.

- [ ] **Step 5: Update Makefile**

Replace `build-seed`/`build-seed-oci` with `build-dbtool`/`build-dbtool-oci`:

```makefile
build-dbtool:  ## Build TMI database administration tool (database-agnostic)
	@uv run scripts/build-server.py --component dbtool

build-dbtool-oci:  ## Build TMI database administration tool with Oracle support
	@uv run scripts/build-server.py --component dbtool --oci
```

Update `.PHONY` declarations. Update `cats-seed` and `cats-seed-oci` targets to call `run-dbtool.py`.

- [ ] **Step 6: Verify**

Run: `make build-dbtool && make list-targets | grep -i dbtool`
Expected: Binary builds, targets show

- [ ] **Step 7: Lint and test**

Run: `make lint && make test-unit`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add scripts/run-dbtool.py scripts/run-seed.py scripts/build-server.py scripts/run-cats-fuzz.py Makefile
git commit -m "build: rename seed to dbtool in Makefile and scripts

Renames build-seed to build-dbtool, updates run-dbtool.py with new
flag syntax, enables ldflags for version injection, updates all
script cross-references.

Refs #251"
```

---

## Task 9: Idempotent API Object Creation

**Files:**
- Modify: `cmd/dbtool/data_api.go`

- [ ] **Step 1: Add idempotent pre-checks**

The current `createAPIObject` always POSTs. For idempotency, add a pre-check that queries for an existing object by name before creating. If found, return the existing ID.

Add a helper function and modify the entity-creation functions to use it where possible:

```go
// findExistingByName queries the API for an existing object by name.
// Returns the ID if found, empty string if not found, error on failure.
func findExistingByName(url, token, name string) (string, error) {
	result, status, err := apiRequest("GET", url, token, nil)
	if err != nil || status >= 300 {
		return "", nil // Can't check, proceed with creation
	}

	// Look for items in array responses
	if items, ok := result["items"].([]any); ok {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if n, ok := m["name"].(string); ok && n == name {
					if id, ok := m["id"].(string); ok {
						return id, nil
					}
				}
			}
		}
	}

	return "", nil
}
```

Then in `seedThreatModel`, `seedTopLevel`, and `seedChildResource`, check for existing before creating. For example:

```go
func seedThreatModel(serverURL, token string, entry SeedEntry) (*SeedResult, error) {
	log := slogging.Get()

	// Idempotency: check if threat model with this name already exists
	if name, ok := entry.Data["name"].(string); ok && name != "" {
		existingID, err := findExistingByName(serverURL+"/threat_models", token, name)
		if err == nil && existingID != "" {
			log.Info("  %s already exists: %s (skipping)", entry.Kind, existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	id, err := createAPIObject(entry.Kind, serverURL+"/threat_models", token, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}
```

Apply similar idempotency checks to all API-created entities where feasible (threat models, surveys, webhooks — things with unique names). For child resources (threats, diagrams, etc.) that don't have unique name constraints, the create-and-accept-duplicates behavior is acceptable.

- [ ] **Step 2: Build and test**

Run: `go build -o bin/tmi-dbtool ./cmd/dbtool && make lint`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/dbtool/data_api.go
git commit -m "feat(dbtool): add idempotent pre-checks for API-created objects

Before creating API objects, check if an object with the same name
already exists. Skip creation if the desired state already exists.
Supports threat models, surveys, webhooks, and other named resources.

Refs #251"
```

---

## Task 10: Wiki Documentation

**Files:**
- Create: `/Users/efitz/Projects/tmi.wiki/Database-Security-Strategies.md`
- Modify: `/Users/efitz/Projects/tmi.wiki/Seed-Tool-Reference.md` (rename + update)
- Modify: `/Users/efitz/Projects/tmi.wiki/_Sidebar.md`

- [ ] **Step 1: Create Database-Security-Strategies.md**

Content per the spec:
1. Overview — why privilege separation matters
2. Strategy 1: Cloud-Isolated Database (cloud deployments)
3. Strategy 2: Least-Privilege Server (on-premises)
4. Upgrade workflows for each strategy
5. Creating a limited-privilege database user (PostgreSQL + Oracle SQL examples)
6. Verifying your setup with `tmi-dbtool --config=<config>`

- [ ] **Step 2: Rename and update Seed-Tool-Reference.md**

```bash
cd /Users/efitz/Projects/tmi.wiki
git mv Seed-Tool-Reference.md Database-Tool-Reference.md
```

Update all content:
- Tool name: `tmi-seed` -> `tmi-dbtool`
- CLI flags: `--mode=system` -> `--schema / -s`, etc.
- Add schema mode and health check documentation
- Add startup banner and exit summary documentation

- [ ] **Step 3: Update _Sidebar.md**

In the Operation section, add:
- [Database Security Strategies](Database-Security-Strategies)

In the Tools section, rename:
- [Seed Tool Reference](Seed-Tool-Reference) -> [Database Tool Reference](Database-Tool-Reference)

- [ ] **Step 4: Commit and push wiki**

```bash
cd /Users/efitz/Projects/tmi.wiki
git add -A
git commit -m "docs: add database security strategies, rename seed tool to dbtool

New page: Database Security Strategies with two strategies for
cloud-isolated and on-premises deployments. Renamed Seed Tool
Reference to Database Tool Reference with updated CLI.

Refs ericfitz/tmi#251"
git push
```

---

## Task 11: End-to-End Verification

- [ ] **Step 1: Build all binaries**

Run: `make build-server && make build-dbtool`
Expected: Both compile

- [ ] **Step 2: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Test health check mode**

Run: `./bin/tmi-dbtool --config=config-development.yml`
Expected: Prints startup banner, database info, schema status, system data status, exit summary JSON

- [ ] **Step 5: Test schema dry-run**

Run: `./bin/tmi-dbtool -s --config=config-development.yml --dry-run`
Expected: Reports schema status without writing

- [ ] **Step 6: Test help**

Run: `./bin/tmi-dbtool -h`
Expected: Usage text with all flags

- [ ] **Step 7: Push**

Run:
```bash
git pull --rebase
git push
```
