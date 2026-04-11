# tmi-dbtool: Schema Migration & Database Security Design

**Issue:** [#251](https://github.com/ericfitz/tmi/issues/251)
**Date:** 2026-04-11
**Status:** Draft

## Summary

Rename the unified seed tool (`tmi-seed`) to `tmi-dbtool`, add schema creation/migration to the `--schema` operation, add a no-argument health check mode, and modify the server startup to detect insufficient DDL permissions gracefully. Document two database security strategies for operators.

## Motivation

TMI currently runs GORM AutoMigrate at server startup, which requires DDL permissions (CREATE TABLE, ALTER TABLE). This means the server's database user must have admin-level access — a poor security practice that could be exploited if the server is compromised. The server should be able to run with only DML permissions (SELECT, INSERT, UPDATE, DELETE), with schema operations handled separately by an admin tool.

## Design

### Tool Rename

Rename `tmi-seed` to `tmi-dbtool` across the codebase:

| Before | After |
|--------|-------|
| `cmd/seed/` | `cmd/dbtool/` |
| `bin/tmi-seed` | `bin/tmi-dbtool` |
| `make build-seed` | `make build-dbtool` |
| `scripts/run-seed.py` | `scripts/run-dbtool.py` |
| `build-server.py` component `seed` | component `dbtool` |

The `make cats-seed` and `make cats-fuzz` targets keep their names for backward compatibility but call `tmi-dbtool` internally.

### CLI Interface

```
tmi-dbtool [OPTIONS]

Database Operations:
  -s, --schema              Create/migrate schema and seed system data
  -c, --import-config       Import config file settings into database
  -t, --import-test-data    Import test data from a seed file

Input:
  -f, --input-file FILE     Input file (config YAML for -c, seed JSON for -t)
      --config FILE         TMI config file (provides DB connection via database.url)

Output:
      --output FILE         Path for migrated config YAML (with -c; default: input-migrated.yml)

Behavior:
      --dry-run             Show what would happen without writing
      --overwrite           Overwrite existing settings (with -c)
  -v, --verbose             Print step-by-step operations and DB messages
  -h, --help                Print usage

No arguments (health check):
  tmi-dbtool --config=config-development.yml
  Connect to database, print engine info and schema health report.
```

**Connection string resolution:**
1. If `--config` provided -> load config, use `database.url`
2. If `-c` with `--input-file` and no `--config` -> the input file IS a TMI config, use its `database.url`
3. If `TMI_DATABASE_URL` environment variable is set -> use it (standard env override)
4. Else -> error: "No database connection. Provide --config or set TMI_DATABASE_URL"

### Startup Banner

Always printed at tool start:

```
tmi-dbtool v1.4.0 (commit: abc1234, built: 2026-04-11T10:00:00Z)
Schema version: 44 models
```

Version, commit, and build timestamp are injected via `-ldflags` at build time, same pattern as the server binary.

### Exit Summary

Always printed at exit (JSON to stdout):

```json
{
  "tool": "tmi-dbtool",
  "version": "1.4.0",
  "commit": "abc1234",
  "built_at": "2026-04-11T10:00:00Z",
  "schema_models": 44,
  "arguments": {"schema": true, "dry_run": false, "config": "config-development.yml"},
  "status": "success",
  "error": ""
}
```

On failure:
```json
{
  ...
  "status": "failure",
  "error": "AutoMigrate failed: insufficient_privilege (42501): permission denied to create table"
}
```

### No-Argument Mode (Health Check)

When invoked with only `--config` (no operation flags), the tool connects to the database and reports:

1. **Database engine info** — type (PostgreSQL, Oracle, etc.), version
2. **Schema status** — which of the 44 expected tables exist, which are missing, any columns that need adding
3. **System data status** — which built-in groups exist, webhook deny list entry count
4. **Overall health** — "schema current" or "N migrations pending"

No writes are performed. Exit status is 0 if healthy, 1 if schema needs updates.

### Schema Operation (`--schema` / `-s`)

Replaces the old `--mode=system`. Performs:

1. Run GORM AutoMigrate against all models (creates tables, adds columns, creates indexes/constraints)
2. Run post-migration fixups (severity normalization, legacy data cleanup)
3. Seed system data via `seed.SeedDatabase()` (built-in groups, webhook deny list)
4. Report results

All operations are idempotent. Running `--schema` against a fully current database is a no-op.

**Dry run:** Connect to DB, compare expected schema against actual, report what would change. Do not write.

### Config Import (`--import-config` / `-c`)

Same as the current `--mode=config`. Reads a TMI config YAML, splits settings into infrastructure (stays in file) vs. DB-eligible (writes to `system_settings`), produces a stripped migrated YAML.

Requires `-f`/`--input-file` pointing to the config YAML to import.

### Test Data Import (`--import-test-data` / `-t`)

Same as the current `--mode=data`. Reads a JSON seed file, creates entities via direct DB and API calls.

Requires `-f`/`--input-file` pointing to the seed data JSON.

### Idempotency

All database operations must be idempotent:

| Operation | Strategy |
|-----------|----------|
| Schema (AutoMigrate) | GORM AutoMigrate is already idempotent (IF NOT EXISTS semantics) |
| System data (SeedDatabase) | Already uses `FirstOrCreate` |
| Config import | Already uses find-then-create-or-update |
| Test data (DB entities) | Already uses find-or-create (users, settings) |
| Test data (API entities) | Add pre-check: query for existing object by name/key before creating. Skip if exists with matching state. |

Failure to write an object that already exists in the desired state is not an error.

### Server Startup — DDL Permission Detection

**Modified Phase 2 behavior in `cmd/server/main.go`:**

```
1. Try AutoMigrate
2. If success -> proceed (current behavior)
3. If failure:
   a. Classify the error as permissions-related or other
   b. If NOT a permissions error -> fail with the error (current behavior)
   c. If IS a permissions error:
      i.  Run schema health check (are all expected tables present?)
      ii. If schema is current -> log warning, proceed
          "WARN: DDL permissions unavailable, but schema is up to date. Proceeding."
      iii. If schema needs updates -> stop with clear message:

          "ERROR: Database schema requires updates but this database user lacks DDL permissions.

          To resolve this, choose one of:
            1. Run schema migration with an admin-privileged database user:
               tmi-dbtool --schema --config=<config-file>
            2. Grant DDL permissions to the current database user.

          See: https://github.com/ericfitz/tmi/wiki/Database-Security-Strategies"
```

**Permission error detection** — pattern match on database-specific error codes:

| Database | Permission Error Indicators |
|----------|----------------------------|
| PostgreSQL | SQLSTATE `42501` (`insufficient_privilege`), message `permission denied` |
| Oracle | `ORA-01031` (`insufficient privileges`), `ORA-01950` (`no privileges on tablespace`) |
| MySQL | Error 1142 (`command denied`), Error 1044 (`access denied`) |
| SQL Server | Error 262 (`CREATE TABLE permission denied`) |
| SQLite | N/A (file-level permissions, not DB roles) |

**Schema currency check** — lightweight query to determine if migrations are needed:
- Query `information_schema.tables` (PostgreSQL/MySQL/SQL Server) or `ALL_TABLES` (Oracle) for the list of existing tables
- Compare against the list of table names from `api.GetAllModels()`
- If all expected tables exist, schema is considered current (column-level drift is accepted as non-blocking)

### Code Changes

**Renamed:**

| Before | After |
|--------|-------|
| `cmd/seed/main.go` | `cmd/dbtool/main.go` |
| `cmd/seed/types.go` | `cmd/dbtool/types.go` |
| `cmd/seed/system.go` | Removed (merged into `schema.go`) |
| `cmd/seed/data.go` | `cmd/dbtool/data.go` |
| `cmd/seed/data_db.go` | `cmd/dbtool/data_db.go` |
| `cmd/seed/data_api.go` | `cmd/dbtool/data_api.go` |
| `cmd/seed/config.go` | `cmd/dbtool/config.go` |
| `cmd/seed/reference.go` | `cmd/dbtool/reference.go` |
| `scripts/run-seed.py` | `scripts/run-dbtool.py` |

**New files:**

| File | Responsibility |
|------|---------------|
| `cmd/dbtool/schema.go` | `runSchema()` — AutoMigrate + post-migration fixups + SeedDatabase |
| `cmd/dbtool/health.go` | `runHealthCheck()` — DB engine info, schema status, system data status |
| `internal/dbcheck/dbcheck.go` | `IsPermissionError(err)` and `CheckSchemaHealth(db)` — shared between server and dbtool |

**Modified files:**

| File | Change |
|------|--------|
| `cmd/dbtool/main.go` | New CLI flags, startup banner, exit summary, no-arg dispatch to health check |
| `cmd/dbtool/types.go` | Add `ToolInfo`, `ExitSummary` structs |
| `cmd/dbtool/data_api.go` | Add idempotent pre-checks for API-created objects |
| `cmd/server/main.go` | Phase 2: wrap AutoMigrate with permission detection and schema health check |
| `Makefile` | Rename `build-seed`/`build-seed-oci` to `build-dbtool`/`build-dbtool-oci` |
| `scripts/build-server.py` | Component `seed` -> `dbtool` |
| `scripts/run-dbtool.py` | Renamed from `run-seed.py`, updated binary name |
| `scripts/run-cats-fuzz.py` | Update references to new tool name |

**Build info injection:**

Same pattern as the server binary — `-ldflags` with version, commit, and build timestamp. The `scripts/build-server.py` already handles this for the server; extend to the dbtool component.

### Wiki Documentation

**New page: Database-Security-Strategies.md**

1. **Overview** — why database privilege separation matters

2. **Strategy 1: Cloud-Isolated Database** (recommended for cloud deployments)
   - Deploy database in an isolated subnet, accessible only by the server
   - Enable DDL auditing (full auditing if practical)
   - No other systems or users connect to the database
   - Configure the server with admin-level database credentials
   - Server handles schema maintenance automatically at startup
   - Best for: Heroku, AWS RDS in private subnet, OCI ADB with private endpoint

3. **Strategy 2: Least-Privilege Server** (recommended for on-premises deployments)
   - Enable full auditing on the database server
   - Create a limited-privilege database user for the server (DML only)
   - On first install and on each upgrade, operator runs `tmi-dbtool --schema` with admin credentials
   - The server detects missing DDL permissions and stops with a clear message
   - Best for: shared database servers, on-premises environments, compliance-driven orgs

4. **Upgrade workflow for each strategy**
   - Strategy 1: deploy new server -> auto-migrates on startup
   - Strategy 2: run `tmi-dbtool --schema` with admin creds -> deploy new server -> validates schema -> starts

5. **Creating a limited-privilege database user** — example SQL for PostgreSQL and Oracle

6. **How to verify your setup** — run `tmi-dbtool --config=<config>` for health check

**Updated pages:**
- `_Sidebar.md` — add "Database Security Strategies" to Operation section
- `Seed-Tool-Reference.md` — rename to `Database-Tool-Reference.md`, update all CLI examples and tool name references
