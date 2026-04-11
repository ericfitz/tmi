# Unified Seeder & Config Migration Design

**Issue:** [#212](https://github.com/ericfitz/tmi/issues/212)
**Date:** 2026-04-10
**Status:** Draft

## Summary

Replace the hardcoded `cmd/cats-seed/` tool with a unified seeder (`cmd/seed/`) that supports three modes: system seeding (built-in groups/deny list), data seeding (API objects from a file), and config seeding (migrate config file settings to the database). Remove the `POST /admin/settings/migrate` API endpoint in favor of the offline seeder tool.

## Motivation

The current seeding infrastructure has two problems:

1. **CATS test data is hardcoded in Go** — 13 API objects, user setup, and quota grants are all baked into `cmd/cats-seed/api_objects.go`. Changing test data requires recompilation. There is no shared seed format.

2. **No config-to-database migration path** — Settings can be written to the database individually via the admin API, but there is no tool to bulk-migrate a config file into the database and produce a stripped-down config file for the operator to swap in. The existing `POST /admin/settings/migrate` endpoint partially addresses this but requires a running server with admin auth, and conflates runtime API behavior with a one-time operational task.

## Design

### Seeder Modes

The unified seeder (`cmd/seed/`) supports three modes:

| Mode | Input | Output |
|------|-------|--------|
| **system** | None (built-in) | Groups and webhook deny list in database |
| **data** | JSON/YAML seed file | API objects created via DB or HTTP |
| **config** | TMI config YAML | Non-infrastructure settings in DB + stripped config YAML |

### CLI Interface

```
tmi-seed --mode=system --config=config-development.yml

tmi-seed --mode=data --input=test/seeds/cats-seed-data.json \
         --config=config-development.yml \
         --server=http://localhost:8080 \
         --user=charlie --provider=tmi

tmi-seed --mode=config --input=config-production.yml \
         --output=config-production-migrated.yml

tmi-seed --mode=config --input=config-production.yml --dry-run
```

**Common flags:**
- `--config` — TMI config file (provides DB connection info)
- `--dry-run` — show what would happen without writing
- `--verbose` — enable debug logging

**Data mode flags:**
- `--input` — path to seed data file (required)
- `--server` — TMI server URL for API calls (default: `http://localhost:8080`)
- `--user` — OAuth user ID for API authentication (default: `charlie`)
- `--provider` — OAuth provider name (default: `tmi`)

**Config mode flags:**
- `--input` — path to TMI config YAML to migrate (required)
- `--config` — TMI config file for DB connection (defaults to `--input` value; specify separately when seeding a config into a different database than the one described in the config file)
- `--output` — path for migrated YAML (default: derived from input filename, e.g., `config-production-migrated.yml`)
- `--overwrite` — overwrite existing DB settings (default: skip and log)

### Seed Data File Format

```json
{
  "format_version": "1.0",
  "description": "CATS fuzzing test data",
  "created_at": "2026-04-10T00:00:00Z",
  "output": {
    "reference_file": "test/outputs/cats/cats-test-data.json",
    "reference_yaml": "test/outputs/cats/cats-test-data.yml"
  },
  "seeds": [
    {
      "kind": "user",
      "ref": "admin-user",
      "data": {
        "user_id": "charlie",
        "provider": "tmi",
        "admin": true,
        "api_quota": {
          "rpm": 100000,
          "rph": 1000000
        }
      }
    },
    {
      "kind": "threat_model",
      "ref": "cats-tm",
      "data": {
        "name": "CATS Test Threat Model",
        "framework": "STRIDE",
        "metadata": [
          { "key": "version", "value": "1.0" },
          { "key": "purpose", "value": "CATS API fuzzing" }
        ]
      }
    },
    {
      "kind": "diagram",
      "ref": "cats-diagram",
      "data": {
        "name": "CATS Test Diagram",
        "type": "DFD-1.0.0",
        "threat_model_ref": "cats-tm"
      }
    }
  ]
}
```

**Envelope fields:**
- `format_version` — schema version for forward compatibility (currently `"1.0"`)
- `description` — human-readable description of the seed set
- `created_at` — ISO 8601 timestamp
- `output` — optional; paths for reference file generation (CATS parameter substitution)

**Seed entry fields:**
- `kind` — entity type (see table below)
- `ref` — optional local reference name for cross-referencing
- `data` — entity payload

**Reference resolution:** Seeds are processed top-to-bottom. A `ref` assigns a local name to the created object's ID. Later seeds reference it via `{kind}_ref` fields (e.g., `threat_model_ref`, `survey_ref`, `webhook_ref`). The seeder resolves these to actual UUIDs at creation time.

### Entity Kinds and Creation Strategy

| Kind | Strategy | API Endpoint / DB Table |
|------|----------|------------------------|
| `user` | Direct DB | `users` table + `group_members` for admin + `user_api_quotas` |
| `threat_model` | API | `POST /threat_models` |
| `diagram` | API | `POST /threat_models/{id}/diagrams` |
| `threat` | API | `POST /threat_models/{id}/threats` |
| `asset` | API | `POST /threat_models/{id}/assets` |
| `document` | API | `POST /threat_models/{id}/documents` |
| `note` | API | `POST /threat_models/{id}/notes` |
| `repository` | API | `POST /threat_models/{id}/repositories` |
| `webhook` | API | `POST /webhooks` |
| `addon` | API | `POST /addons` |
| `client_credential` | API | `POST /me/client_credentials` |
| `survey` | API | `POST /admin/surveys` |
| `survey_response` | API | `POST /intake/surveys/{id}/responses` |
| `metadata` | API | `PUT /.../metadata/{key}` |
| `setting` | Direct DB | `system_settings` table |

**Direct DB** is used for entities that must exist before authentication works (users, admin grants, quotas) or that bypass the API by design (settings).

**API** is used for domain objects so that validation, audit trail, webhook events, and ownership assignment all function correctly.

### Authentication for API Calls

Data mode authenticates via the OAuth stub, same as today's `cats-seed`:

1. A `user` seed with `admin: true` must appear before any API-strategy seeds
2. The seeder starts an OAuth flow for that user via the OAuth stub at `http://localhost:8079`
3. All subsequent API calls use the resulting JWT

### Reference File Generation

When the seed file includes an `output` section, the seeder writes reference files after all seeds complete. These map `ref` names to created IDs:

**JSON output** (`cats-test-data.json`):
```json
{
  "cats-tm": { "id": "550e8400-...", "kind": "threat_model" },
  "cats-diagram": { "id": "6ba7b810-...", "kind": "diagram" }
}
```

**YAML output** (`cats-test-data.yml`):
CATS parameter substitution format. An `all:` block mapping parameter names to created IDs, used by CATS for path-based parameter replacement:

```yaml
# CATS Reference Data - Path-based format for parameter replacement
all:
  id: <threat_model_id>
  threat_model_id: <threat_model_id>
  threat_id: <threat_id>
  diagram_id: <diagram_id>
  # ... one entry per ref, plus derived identifiers (group_id, internal_uuid, etc.)
```

This format is defined by [CATS parameter replacement](https://endava.github.io/cats/docs/getting-started/running-cats/). The seeder generates it from the ref-to-ID map plus additional lookups (admin group ID, user internal UUID, provider info) needed by CATS path substitution.

### Config Seed Mode

**Input:** Standard TMI config YAML (e.g., `config-production.yml`)

**Process:**
1. Load config via `config.Load()`
2. Call `GetMigratableSettings()` to get the flat key/value list
3. Classify each setting as infrastructure or DB-eligible using the hardcoded prefix list
4. Write DB-eligible settings to `system_settings` table (respecting `--overwrite` flag)
5. Build migrated YAML containing only infrastructure keys
6. Write migrated YAML to `--output` path

**Infrastructure keys** are settings consumed before the settings service is initialized during server startup, or settings that represent circular dependencies (can't read DB config from the DB). They always stay in the config file.

**Infrastructure key prefixes:**

| Category | Prefixes | Reason |
|----------|----------|--------|
| Logging | `logging.*` | Consumed before `runServer()` starts |
| Observability | `observability.*` | Initialized before DB connection |
| Database | `database.*` | Circular: needed to connect to the DB |
| Server bootstrap | `server.port`, `server.interface`, `server.tls_*`, `server.cors.*`, `server.trusted_proxies`, `server.http_to_https_redirect`, `server.read_timeout`, `server.write_timeout`, `server.idle_timeout` | Router setup and listener, before settings service |
| Secrets | `secrets.*` | Needed to create the encryptor that decrypts DB settings |
| Auth bootstrap | `auth.build_mode`, `auth.jwt.secret`, `auth.jwt.signing_method` | Phase 3 init, before settings service |
| Lockout prevention | `administrators` | Always honored to prevent admin lockout after DB corruption/reset |

**Migrated YAML output:** A valid TMI config file containing only infrastructure keys. The operator can swap this in as the production config, knowing all other settings are in the database.

**Encryption:** If the config includes `secrets.*` settings, the seeder initializes the settings encryptor (same code path as the server) and encrypts secret-marked settings before writing to the DB.

**Operational workflow:**
```
1. tmi-seed --mode=config --input=config-production.yml --dry-run
   # Review what moves to DB vs. stays in file

2. tmi-seed --mode=config --input=config-production.yml \
            --output=config-production-migrated.yml
   # Settings written to DB, migrated YAML produced

3. cp config-production.yml config-production.yml.backup
   mv config-production-migrated.yml config-production.yml
   # Swap config files

4. # Restart server
   # Server reads infrastructure from file, everything else from DB
```

### API Changes

**Removed:**
- `POST /admin/settings/migrate` — endpoint, handler, OpenAPI spec entry
- `"migrate"` reserved key check in settings validation

**Kept:**
- `GET /admin/settings` — list all settings (unchanged)
- `GET /admin/settings/{key}` — get individual setting (unchanged)
- `PUT /admin/settings/{key}` — create/update individual setting (unchanged)
- `DELETE /admin/settings/{key}` — delete individual setting (unchanged)
- `POST /admin/settings/reencrypt` — re-encrypt all settings (unchanged)
- Three-tier priority system in `SettingsService` (env > config file > DB) (unchanged)
- `ConfigProvider` interface and adapter (unchanged)

### Code Changes

**New:**
- `cmd/seed/` — unified seeder CLI
- `internal/config/infrastructure_keys.go` — hardcoded infrastructure key prefix list with `IsInfrastructureKey(key string) bool`
- `test/seeds/cats-seed-data.json` — CATS test objects in seed file format

**Modified:**
- `api/config_handlers.go` — remove `MigrateSystemSettings` handler, remove `"migrate"` from reserved keys
- `api-schema/tmi-openapi.json` — remove `/admin/settings/migrate` path
- `api/api.go` — regenerated after OpenAPI change
- `Makefile` — update `cats-seed`, `cats-fuzz`, `cats-seed-oci` targets

**Removed:**
- `cmd/cats-seed/` — entire directory (replaced by `cmd/seed/` + seed data file)

**Unchanged:**
- `api/seed/seed.go` — `SeedDatabase()` remains the core system seeder
- `api/settings_service.go` — three-tier priority system untouched
- `api/config_provider_adapter.go` — `ConfigProvider` kept for runtime priority
- `internal/config/migratable_settings.go` — kept; used by seeder config mode and `ConfigProvider`
- Integration test fixtures (`test/testdb/`, `test/integration/framework/`)
- Unit test fixtures (`api/test_fixtures.go`)

### Wiki Documentation

| Page | Action | Content |
|------|--------|---------|
| Configuration Management | New | Three-tier priority system, infrastructure keys list and rationale, runtime settings admin API |
| Config Migration Guide | New | Operational workflow for migrating config to DB using the seeder, step-by-step with examples, rollback procedure |
| Seed Tool Reference | New | All three modes, CLI flags, seed data file format spec, entity kinds, reference resolution, CATS reference generation |
| CATS Testing | Update | Reference new seed tool and `test/seeds/cats-seed-data.json` instead of `cmd/cats-seed/` |
