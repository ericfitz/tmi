# Unified Seeder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `cmd/cats-seed/` with a unified `cmd/seed/` tool supporting system, data, and config seed modes; remove the `POST /admin/settings/migrate` endpoint.

**Architecture:** A single CLI tool (`tmi-seed`) with three modes sharing a common seed file format. Data seeds use direct DB writes for bootstrap entities (users, quotas) and HTTP API calls for domain objects (threat models, diagrams). Config seeds read a TMI YAML config, split settings into infrastructure vs. DB-eligible, write DB-eligible settings to `system_settings`, and output a stripped config YAML.

**Tech Stack:** Go, GORM, `flag` package, `encoding/json`, `gopkg.in/yaml.v3`, existing `test/testdb` package for DB access, HTTP client for API calls.

**Spec:** `docs/superpowers/specs/2026-04-10-unified-seeder-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/config/infrastructure_keys.go` | Hardcoded list of infrastructure key prefixes + `IsInfrastructureKey()` function |
| `internal/config/infrastructure_keys_test.go` | Tests for infrastructure key classification |
| `cmd/seed/main.go` | CLI entry point: flag parsing, mode dispatch |
| `cmd/seed/types.go` | Seed file format structs (envelope, seed entry, output config) |
| `cmd/seed/system.go` | System seed mode (delegates to `api/seed.SeedDatabase()`) |
| `cmd/seed/data.go` | Data seed mode: file loading, ref resolution, entity creation dispatch |
| `cmd/seed/data_db.go` | Direct-DB seed handlers (user, admin grant, quotas, settings) |
| `cmd/seed/data_api.go` | API-based seed handlers (threat models, diagrams, etc.) + OAuth auth |
| `cmd/seed/config.go` | Config seed mode: split settings, write to DB, generate migrated YAML |
| `cmd/seed/reference.go` | Reference file generation (JSON + YAML for CATS) |
| `test/seeds/cats-seed-data.json` | CATS test objects in seed file format |
| `scripts/run-seed.py` | Python wrapper for building and running the seed tool |

### Modified Files

| File | Change |
|------|--------|
| `api-schema/tmi-openapi.json` | Remove `/admin/settings/migrate` path |
| `api/api.go` | Regenerated (removes `MigrateSystemSettings` interface method and params) |
| `api/config_handlers.go` | Remove `MigrateSystemSettings` handler, remove `"migrate"` from reserved keys |
| `api/config_handlers_test.go` | Remove 7 `TestMigrateSystemSettings_*` test functions |
| `test/integration/workflows/settings_crud_test.go` | Remove migrate subtests |
| `Makefile` | Update build/seed targets |
| `scripts/build-server.py` | Rename `cats-seed` component to `seed` |
| `scripts/run-cats-fuzz.py` | Update to call new seed tool |

### Removed Files

| File | Reason |
|------|--------|
| `cmd/cats-seed/main.go` | Replaced by `cmd/seed/` |
| `cmd/cats-seed/api_objects.go` | Logic moved to `cmd/seed/data_api.go` |
| `cmd/cats-seed/reference_data.go` | Logic moved to `cmd/seed/reference.go` |
| `scripts/run-cats-seed.py` | Replaced by `scripts/run-seed.py` |

---

## Task 1: Infrastructure Key Classification

**Files:**
- Create: `internal/config/infrastructure_keys.go`
- Create: `internal/config/infrastructure_keys_test.go`

- [ ] **Step 1: Write the test file**

```go
// internal/config/infrastructure_keys_test.go
package config

import "testing"

func TestIsInfrastructureKey(t *testing.T) {
	tests := []struct {
		key            string
		infrastructure bool
	}{
		// Infrastructure keys - must always come from file/env
		{"logging.level", true},
		{"logging.is_dev", true},
		{"logging.log_dir", true},
		{"observability.enabled", true},
		{"observability.sampling_rate", true},
		{"database.url", true},
		{"database.connection_pool.max_open_conns", true},
		{"database.redis.host", true},
		{"database.redis.password", true},
		{"server.port", true},
		{"server.interface", true},
		{"server.tls_enabled", true},
		{"server.tls_cert_file", true},
		{"server.tls_key_file", true},
		{"server.tls_subject_name", true},
		{"server.cors.allowed_origins", true},
		{"server.trusted_proxies", true},
		{"server.http_to_https_redirect", true},
		{"server.read_timeout", true},
		{"server.write_timeout", true},
		{"server.idle_timeout", true},
		{"secrets.provider", true},
		{"secrets.vault_address", true},
		{"secrets.oci_vault_id", true},
		{"auth.build_mode", true},
		{"auth.jwt.secret", true},
		{"auth.jwt.signing_method", true},
		{"administrators", true},
		// DB-eligible keys - can be stored in database
		{"auth.jwt.expiration_seconds", false},
		{"auth.jwt.refresh_token_days", false},
		{"auth.jwt.session_lifetime_days", false},
		{"auth.auto_promote_first_user", false},
		{"auth.everyone_is_a_reviewer", false},
		{"auth.cookie.enabled", false},
		{"auth.cookie.domain", false},
		{"auth.cookie.secure", false},
		{"auth.oauth.providers.google.enabled", false},
		{"auth.oauth.providers.google.client_id", false},
		{"auth.oauth_callback_url", false},
		{"auth.saml.providers.okta.enabled", false},
		{"features.saml_enabled", false},
		{"features.webhooks_enabled", false},
		{"websocket.inactivity_timeout_seconds", false},
		{"operator.name", false},
		{"operator.contact", false},
		{"session.timeout_minutes", false},
		{"rate_limit.requests_per_minute", false},
		{"ui.default_theme", false},
		{"upload.max_file_size_mb", false},
		// Edge cases
		{"server.base_url", false},
		{"server.rate_limit_public_rpm", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := IsInfrastructureKey(tt.key)
			if got != tt.infrastructure {
				t.Errorf("IsInfrastructureKey(%q) = %v, want %v", tt.key, got, tt.infrastructure)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestIsInfrastructureKey`
Expected: FAIL — `IsInfrastructureKey` not defined

- [ ] **Step 3: Write the implementation**

```go
// internal/config/infrastructure_keys.go
package config

import "strings"

// infrastructureKeyPrefixes lists setting key prefixes that must always be read
// from config file or environment variables, never from the database.
//
// These settings are consumed during server startup before the settings service
// is initialized, or represent circular dependencies (e.g., database connection
// settings cannot be read from the database).
//
// See docs/superpowers/specs/2026-04-10-unified-seeder-design.md for the full
// startup phase analysis that derived this list.
var infrastructureKeyPrefixes = []string{
	"logging.",
	"observability.",
	"database.",
	"server.port",
	"server.interface",
	"server.tls_",
	"server.cors.",
	"server.trusted_proxies",
	"server.http_to_https_redirect",
	"server.read_timeout",
	"server.write_timeout",
	"server.idle_timeout",
	"secrets.",
	"auth.build_mode",
	"auth.jwt.secret",
	"auth.jwt.signing_method",
}

// infrastructureKeyExact lists setting keys that are exact matches
// (not prefix-based) for infrastructure keys.
var infrastructureKeyExact = []string{
	"administrators",
}

// IsInfrastructureKey returns true if the given setting key is an infrastructure
// key that must always be read from config file or environment variables.
func IsInfrastructureKey(key string) bool {
	for _, exact := range infrastructureKeyExact {
		if key == exact {
			return true
		}
	}
	for _, prefix := range infrastructureKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestIsInfrastructureKey`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: PASS (no new warnings)

- [ ] **Step 6: Commit**

```bash
git add internal/config/infrastructure_keys.go internal/config/infrastructure_keys_test.go
git commit -m "feat(config): add infrastructure key classification

Hardcoded list of setting key prefixes that must always be read from
config file or environment variables. Derived from server startup
phase analysis. Used by the unified seeder to split config settings
into infrastructure (stay in file) vs. DB-eligible.

Refs #212"
```

---

## Task 2: Seed File Format Types

**Files:**
- Create: `cmd/seed/types.go`

- [ ] **Step 1: Create the types file**

```go
// cmd/seed/types.go
package main

import "time"

// SeedFile is the top-level envelope for a seed data file.
type SeedFile struct {
	FormatVersion string      `json:"format_version" yaml:"format_version"`
	Description   string      `json:"description" yaml:"description"`
	CreatedAt     time.Time   `json:"created_at" yaml:"created_at"`
	Output        *SeedOutput `json:"output,omitempty" yaml:"output,omitempty"`
	Seeds         []SeedEntry `json:"seeds" yaml:"seeds"`
}

// SeedOutput configures reference file generation after seeding.
type SeedOutput struct {
	ReferenceFile string `json:"reference_file,omitempty" yaml:"reference_file,omitempty"`
	ReferenceYAML string `json:"reference_yaml,omitempty" yaml:"reference_yaml,omitempty"`
}

// SeedEntry is a single entity to seed.
type SeedEntry struct {
	Kind string         `json:"kind" yaml:"kind"`
	Ref  string         `json:"ref,omitempty" yaml:"ref,omitempty"`
	Data map[string]any `json:"data" yaml:"data"`
}

// SeedResult tracks the result of seeding a single entry.
type SeedResult struct {
	Ref  string
	Kind string
	ID   string
	// Extra holds additional fields needed for reference file generation
	// (e.g., threat_model_id for child resources, provider info for users).
	Extra map[string]string
}

// RefMap tracks ref names to their created IDs for cross-referencing.
type RefMap map[string]*SeedResult
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/efitz/Projects/tmi && go build ./cmd/seed/...` (will fail because no `main.go` yet — that's fine, just verify no syntax errors in types.go)

Actually, since there's no main yet, just run:
```bash
go vet ./cmd/seed/...
```
Expected: package not found error (acceptable — we'll add main.go next)

- [ ] **Step 3: Commit**

```bash
git add cmd/seed/types.go
git commit -m "feat(seed): add seed file format types

Envelope, entry, output, and ref-tracking types for the unified
seed data file format.

Refs #212"
```

---

## Task 3: CLI Entry Point and System Mode

**Files:**
- Create: `cmd/seed/main.go`
- Create: `cmd/seed/system.go`

- [ ] **Step 1: Create the system mode handler**

```go
// cmd/seed/system.go
package main

import (
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSystemSeed runs the system seed mode, which seeds built-in groups and
// webhook deny list entries via api/seed.SeedDatabase().
func runSystemSeed(db *testdb.TestDB, dryRun bool) error {
	log := slogging.Get()

	if dryRun {
		log.Info("[DRY RUN] Would seed built-in groups and webhook deny list")
		return nil
	}

	log.Info("Seeding built-in groups and webhook deny list...")
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return err
	}

	log.Info("System seed complete")
	return nil
}
```

- [ ] **Step 2: Create the CLI entry point**

```go
// cmd/seed/main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		mode       = flag.String("mode", "", "Seed mode: system, data, config (required)")
		configFile = flag.String("config", "", "Path to TMI configuration file (provides DB connection)")
		inputFile  = flag.String("input", "", "Path to seed data file (data mode) or config YAML (config mode)")
		outputFile = flag.String("output", "", "Path for migrated config YAML (config mode only)")
		serverURL  = flag.String("server", "http://localhost:8080", "TMI server URL for API calls (data mode)")
		user       = flag.String("user", "charlie", "OAuth user ID for API authentication (data mode)")
		provider   = flag.String("provider", "tmi", "OAuth provider name (data mode)")
		overwrite  = flag.Bool("overwrite", false, "Overwrite existing DB settings (config mode)")
		dryRun     = flag.Bool("dry-run", false, "Show what would happen without writing")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
	)
	flag.Parse()

	if *mode == "" {
		fmt.Fprintln(os.Stderr, "Error: --mode flag is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: tmi-seed --mode=<mode> [OPTIONS]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Modes:")
		fmt.Fprintln(os.Stderr, "  system  Seed built-in groups and webhook deny list")
		fmt.Fprintln(os.Stderr, "  data    Seed entities from a JSON/YAML seed file")
		fmt.Fprintln(os.Stderr, "  config  Migrate config file settings to database")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		return 1
	}

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
	log := slogging.Get()

	// Determine config file for DB connection
	dbConfigFile := *configFile
	if dbConfigFile == "" && *mode == "config" {
		dbConfigFile = *inputFile
	}
	if dbConfigFile == "" {
		log.Error("--config flag is required (or --input for config mode)")
		return 1
	}

	// Connect to database
	log.Info("Connecting to database...")
	db, err := testdb.New(dbConfigFile)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		return 1
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()
	log.Info("Connected to %s database", db.DialectName())

	// Ensure schema is up to date
	log.Info("Ensuring database schema is up to date...")
	if err := db.AutoMigrate(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
		} else {
			log.Error("Failed to auto-migrate schema: %v", err)
			return 1
		}
	}

	switch *mode {
	case "system":
		if err := runSystemSeed(db, *dryRun); err != nil {
			log.Error("System seed failed: %v", err)
			return 1
		}

	case "data":
		if *inputFile == "" {
			log.Error("--input flag is required for data mode")
			return 1
		}
		if err := runDataSeed(db, *inputFile, *serverURL, *user, *provider, *dryRun); err != nil {
			log.Error("Data seed failed: %v", err)
			return 1
		}

	case "config":
		if *inputFile == "" {
			log.Error("--input flag is required for config mode")
			return 1
		}
		if err := runConfigSeed(db, *inputFile, *outputFile, *overwrite, *dryRun); err != nil {
			log.Error("Config seed failed: %v", err)
			return 1
		}

	default:
		log.Error("Unknown mode: %s (expected: system, data, config)", *mode)
		return 1
	}

	return 0
}
```

Note: `runDataSeed` and `runConfigSeed` don't exist yet — they'll be created in subsequent tasks. For now, add stub functions so the code compiles:

- [ ] **Step 3: Add temporary stubs for data and config modes**

Create temporary stubs in `cmd/seed/data.go` and `cmd/seed/config.go`:

```go
// cmd/seed/data.go
package main

import (
	"fmt"
	"github.com/ericfitz/tmi/test/testdb"
)

func runDataSeed(db *testdb.TestDB, inputFile, serverURL, user, provider string, dryRun bool) error {
	return fmt.Errorf("data seed mode not yet implemented")
}
```

```go
// cmd/seed/config.go
package main

import (
	"fmt"
	"github.com/ericfitz/tmi/test/testdb"
)

func runConfigSeed(db *testdb.TestDB, inputFile, outputFile string, overwrite, dryRun bool) error {
	return fmt.Errorf("config seed mode not yet implemented")
}
```

- [ ] **Step 4: Build and verify system mode works**

Run: `go build -o bin/tmi-seed ./cmd/seed && ./bin/tmi-seed --mode=system --config=config-development.yml --dry-run`
Expected: Output showing `[DRY RUN] Would seed built-in groups and webhook deny list`

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/seed/main.go cmd/seed/system.go cmd/seed/data.go cmd/seed/config.go
git commit -m "feat(seed): add CLI entry point and system seed mode

Unified seeder CLI with --mode flag dispatching to system, data, and
config modes. System mode delegates to api/seed.SeedDatabase(). Data
and config modes are stubs pending implementation.

Refs #212"
```

---

## Task 4: Data Seed — File Loading and Reference Resolution

**Files:**
- Modify: `cmd/seed/data.go` (replace stub)

- [ ] **Step 1: Implement seed file loading and dispatch loop**

Replace the stub in `cmd/seed/data.go`:

```go
// cmd/seed/data.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runDataSeed runs the data seed mode: loads a seed file, creates entities
// in order, resolves cross-references, and optionally generates reference files.
func runDataSeed(db *testdb.TestDB, inputFile, serverURL, user, provider string, dryRun bool) error {
	log := slogging.Get()

	// Load seed file
	seedFile, err := loadSeedFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to load seed file: %w", err)
	}

	log.Info("Loaded seed file: %s", seedFile.Description)
	log.Info("  Format version: %s", seedFile.FormatVersion)
	log.Info("  Seeds: %d entries", len(seedFile.Seeds))

	// Seed system data first (groups, deny list)
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return fmt.Errorf("failed to seed system data: %w", err)
	}

	// Process seeds in order
	refs := make(RefMap)
	var token string // JWT token for API calls, obtained lazily

	for i, entry := range seedFile.Seeds {
		log.Info("Processing seed %d/%d: kind=%s ref=%s", i+1, len(seedFile.Seeds), entry.Kind, entry.Ref)

		if dryRun {
			log.Info("  [DRY RUN] Would create %s", entry.Kind)
			if entry.Ref != "" {
				refs[entry.Ref] = &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: "dry-run-id"}
			}
			continue
		}

		var result *SeedResult

		switch classifyStrategy(entry.Kind) {
		case "db":
			result, err = seedViaDB(db, entry, refs)
		case "api":
			// Authenticate lazily on first API-strategy seed
			if token == "" {
				log.Info("Authenticating via OAuth stub for API calls...")
				token, err = authenticateViaOAuthStub(serverURL, user, provider)
				if err != nil {
					return fmt.Errorf("failed to authenticate for API calls: %w", err)
				}
			}
			result, err = seedViaAPI(serverURL, token, entry, refs)
		default:
			err = fmt.Errorf("unknown seed kind: %s", entry.Kind)
		}

		if err != nil {
			return fmt.Errorf("failed to seed entry %d (kind=%s, ref=%s): %w", i+1, entry.Kind, entry.Ref, err)
		}

		if entry.Ref != "" && result != nil {
			refs[entry.Ref] = result
			log.Info("  Registered ref %q -> %s", entry.Ref, result.ID)
		}
	}

	// Generate reference files if configured
	if seedFile.Output != nil && !dryRun {
		if err := writeReferenceFiles(seedFile.Output, refs, serverURL, user, provider); err != nil {
			return fmt.Errorf("failed to write reference files: %w", err)
		}
	}

	log.Info("Data seed complete: %d entries processed", len(seedFile.Seeds))
	return nil
}

// loadSeedFile reads and parses a seed file (JSON or YAML).
func loadSeedFile(path string) (*SeedFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var seedFile SeedFile

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &seedFile); err != nil {
			return nil, fmt.Errorf("failed to parse JSON seed file: %w", err)
		}
	case ".yml", ".yaml":
		if err := yamlUnmarshal(data, &seedFile); err != nil {
			return nil, fmt.Errorf("failed to parse YAML seed file: %w", err)
		}
	default:
		// Try JSON first, fall back to YAML
		if err := json.Unmarshal(data, &seedFile); err != nil {
			if err2 := yamlUnmarshal(data, &seedFile); err2 != nil {
				return nil, fmt.Errorf("failed to parse seed file as JSON (%v) or YAML (%v)", err, err2)
			}
		}
	}

	if seedFile.FormatVersion == "" {
		return nil, fmt.Errorf("seed file missing format_version")
	}
	if len(seedFile.Seeds) == 0 {
		return nil, fmt.Errorf("seed file has no seeds")
	}

	return &seedFile, nil
}

// yamlUnmarshal is a helper that handles YAML parsing.
// Uses encoding/json round-trip via gopkg.in/yaml.v3 for map[string]any compatibility.
func yamlUnmarshal(data []byte, v any) error {
	// For now, only JSON is supported. YAML support can be added when needed
	// by importing gopkg.in/yaml.v3.
	return fmt.Errorf("YAML seed files not yet supported; use JSON format")
}

// classifyStrategy returns "db" or "api" for the given seed kind.
func classifyStrategy(kind string) string {
	switch kind {
	case "user", "setting":
		return "db"
	default:
		return "api"
	}
}

// resolveRef looks up a ref in the ref map and returns its ID.
// Returns an error if the ref is not found.
func resolveRef(refs RefMap, refName string) (string, error) {
	result, ok := refs[refName]
	if !ok {
		return "", fmt.Errorf("unresolved ref: %q (referenced before creation or missing)", refName)
	}
	return result.ID, nil
}

// resolveRefField checks if a map has a "{kind}_ref" field, resolves it, and
// returns the resolved ID. Returns empty string if no ref field is present.
func resolveRefField(data map[string]any, refFieldName string, refs RefMap) (string, error) {
	refName, ok := data[refFieldName].(string)
	if !ok || refName == "" {
		return "", nil
	}
	return resolveRef(refs, refName)
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles successfully (the `seedViaDB`, `seedViaAPI`, `authenticateViaOAuthStub`, and `writeReferenceFiles` functions don't exist yet — they'll be added in subsequent tasks. Add forward-declaration stubs.)

Actually, add stubs at the bottom of the file for now:

```go
// Stubs for functions implemented in subsequent tasks.
// Remove these as the real implementations are added.

func seedViaDB(db *testdb.TestDB, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	return nil, fmt.Errorf("DB seed not yet implemented for kind=%s", entry.Kind)
}

func seedViaAPI(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	return nil, fmt.Errorf("API seed not yet implemented for kind=%s", entry.Kind)
}

func authenticateViaOAuthStub(serverURL, user, provider string) (string, error) {
	return "", fmt.Errorf("OAuth stub authentication not yet implemented")
}

func writeReferenceFiles(output *SeedOutput, refs RefMap, serverURL, user, provider string) error {
	return fmt.Errorf("reference file generation not yet implemented")
}
```

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/seed/data.go
git commit -m "feat(seed): add data seed file loading and dispatch loop

Loads JSON seed files, iterates entries in order, classifies each as
DB or API strategy, resolves cross-references. Functions for actual
entity creation are stubs pending implementation.

Refs #212"
```

---

## Task 5: Data Seed — Direct DB Handlers (Users, Settings)

**Files:**
- Create: `cmd/seed/data_db.go`
- Modify: `cmd/seed/data.go` (remove `seedViaDB` stub)

- [ ] **Step 1: Implement DB seed handlers**

```go
// cmd/seed/data_db.go
package main

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"github.com/google/uuid"
)

const administratorsGroupUUID = "00000000-0000-0000-0000-000000000002"

// seedViaDB creates an entity directly in the database.
func seedViaDB(db *testdb.TestDB, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	switch entry.Kind {
	case "user":
		return seedUser(db, entry)
	case "setting":
		return seedSetting(db, entry)
	default:
		return nil, fmt.Errorf("unsupported DB seed kind: %s", entry.Kind)
	}
}

// seedUser creates a user, optionally grants admin, and sets API quotas.
func seedUser(db *testdb.TestDB, entry SeedEntry) (*SeedResult, error) {
	log := slogging.Get()

	userID, _ := entry.Data["user_id"].(string)
	providerName, _ := entry.Data["provider"].(string)
	if userID == "" || providerName == "" {
		return nil, fmt.Errorf("user seed requires user_id and provider")
	}

	// Find or create user
	var user models.User
	result := db.DB().Where(
		"provider_user_id = ? AND provider = ?",
		userID,
		providerName,
	).First(&user)

	if result.Error != nil {
		// Create new user
		user = models.User{
			InternalUUID:   uuid.New().String(),
			Provider:       providerName,
			ProviderUserID: &userID,
			Email:          fmt.Sprintf("%s@tmi.local", userID),
			Name:           fmt.Sprintf("%s (Seed User)", capitalize(userID)),
			EmailVerified:  models.DBBool(true),
		}
		if err := db.DB().Create(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		log.Info("  Created user: %s (UUID: %s)", userID, user.InternalUUID)
	} else {
		log.Info("  User already exists: %s (UUID: %s)", userID, user.InternalUUID)
	}

	// Grant admin if requested
	if admin, ok := entry.Data["admin"].(bool); ok && admin {
		if err := grantAdmin(db, &user); err != nil {
			return nil, err
		}
	}

	// Set API quotas if requested
	if quota, ok := entry.Data["api_quota"].(map[string]any); ok {
		if err := setQuotas(db, user.InternalUUID, quota); err != nil {
			return nil, err
		}
	}

	return &SeedResult{
		Ref:  entry.Ref,
		Kind: "user",
		ID:   user.InternalUUID,
		Extra: map[string]string{
			"provider":         providerName,
			"provider_user_id": userID,
			"email":            user.Email,
		},
	}, nil
}

// grantAdmin adds a user to the Administrators group.
func grantAdmin(db *testdb.TestDB, user *models.User) error {
	log := slogging.Get()

	var count int64
	db.DB().Model(&models.GroupMember{}).
		Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?",
			administratorsGroupUUID, user.InternalUUID, "user").
		Count(&count)

	if count > 0 {
		log.Info("  User is already an administrator")
		return nil
	}

	notes := "Granted by tmi-seed"
	member := models.GroupMember{
		ID:                uuid.New().String(),
		GroupInternalUUID: administratorsGroupUUID,
		UserInternalUUID:  &user.InternalUUID,
		SubjectType:       "user",
		Notes:             &notes,
	}

	if err := db.DB().Create(&member).Error; err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "unique constraint") ||
			strings.Contains(errStr, "ORA-00001") ||
			strings.Contains(errStr, "duplicate key") {
			log.Info("  User is already an administrator")
			return nil
		}
		return fmt.Errorf("failed to grant admin: %w", err)
	}

	log.Info("  Granted admin privileges")
	return nil
}

// setQuotas sets API quotas for a user.
func setQuotas(db *testdb.TestDB, userInternalUUID string, quota map[string]any) error {
	log := slogging.Get()

	rpm := intFromAny(quota["rpm"], 0)
	rph := intFromAny(quota["rph"], 0)

	if rpm == 0 && rph == 0 {
		return nil
	}

	var existing models.UserAPIQuota
	result := db.DB().Where("user_internal_uuid = ?", userInternalUUID).First(&existing)

	if result.Error == nil {
		updates := map[string]any{}
		if rpm > 0 {
			updates["max_requests_per_minute"] = rpm
		}
		if rph > 0 {
			updates["max_requests_per_hour"] = rph
		}
		if err := db.DB().Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update quotas: %w", err)
		}
	} else {
		q := models.UserAPIQuota{
			UserInternalUUID:     userInternalUUID,
			MaxRequestsPerMinute: rpm,
		}
		if rph > 0 {
			q.MaxRequestsPerHour = &rph
		}
		if err := db.DB().Create(&q).Error; err != nil {
			return fmt.Errorf("failed to create quotas: %w", err)
		}
	}

	log.Info("  Set quotas: %d/min, %d/hour", rpm, rph)
	return nil
}

// seedSetting writes a single setting to system_settings.
func seedSetting(db *testdb.TestDB, entry SeedEntry) (*SeedResult, error) {
	log := slogging.Get()

	key, _ := entry.Data["key"].(string)
	value, _ := entry.Data["value"].(string)
	settingType, _ := entry.Data["type"].(string)
	if key == "" || settingType == "" {
		return nil, fmt.Errorf("setting seed requires key and type")
	}

	description, _ := entry.Data["description"].(string)

	setting := models.SystemSetting{
		SettingKey:   key,
		Value:        value,
		SettingType:  settingType,
		Description:  &description,
	}

	// Upsert: create or update
	var existing models.SystemSetting
	if err := db.DB().Where("setting_key = ?", key).First(&existing).Error; err == nil {
		if err := db.DB().Model(&existing).Updates(map[string]any{
			"value":        value,
			"setting_type": settingType,
			"description":  description,
		}).Error; err != nil {
			return nil, fmt.Errorf("failed to update setting %s: %w", key, err)
		}
		log.Info("  Updated setting: %s", key)
	} else {
		if err := db.DB().Create(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to create setting %s: %w", key, err)
		}
		log.Info("  Created setting: %s", key)
	}

	return &SeedResult{Ref: entry.Ref, Kind: "setting", ID: key}, nil
}

// intFromAny converts an any value to int (handles float64 from JSON).
func intFromAny(v any, defaultVal int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return defaultVal
	}
}

// capitalize capitalizes the first letter of a string.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
```

- [ ] **Step 2: Remove the `seedViaDB` stub from `data.go`**

Remove the `seedViaDB` stub function from the bottom of `cmd/seed/data.go`.

- [ ] **Step 3: Build to verify compilation**

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles

- [ ] **Step 4: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/seed/data_db.go cmd/seed/data.go
git commit -m "feat(seed): add direct-DB seed handlers for users and settings

Handles user creation, admin grants, API quota configuration, and
system_settings writes via direct GORM operations. These entities
must exist before API authentication works.

Refs #212"
```

---

## Task 6: Data Seed — API Handlers and OAuth Authentication

**Files:**
- Create: `cmd/seed/data_api.go`
- Modify: `cmd/seed/data.go` (remove `seedViaAPI` and `authenticateViaOAuthStub` stubs)

- [ ] **Step 1: Implement API seed handlers and OAuth auth**

This is the largest single file. It ports the API call logic from `cmd/cats-seed/api_objects.go` into the ref-based seed system.

```go
// cmd/seed/data_api.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const oauthStubPort = 8079

// authenticateViaOAuthStub performs OAuth authentication through the oauth-client-callback-stub.
func authenticateViaOAuthStub(serverURL, user, provider string) (string, error) {
	log := slogging.Get()
	oauthStubURL := fmt.Sprintf("http://localhost:%d", oauthStubPort)

	log.Info("Authenticating as %s@%s via OAuth stub...", user, provider)

	flowPayload := fmt.Sprintf(`{"userid": "%s", "idp": "%s", "tmi_server": "%s"}`, user, provider, serverURL)
	resp, err := http.Post(
		oauthStubURL+"/flows/start",
		"application/json",
		strings.NewReader(flowPayload),
	)
	if err != nil {
		return "", fmt.Errorf("failed to start OAuth flow: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var flowResult map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&flowResult); err != nil {
		return "", fmt.Errorf("failed to decode OAuth flow response: %w", err)
	}

	flowID, ok := flowResult["flow_id"].(string)
	if !ok || flowID == "" {
		return "", fmt.Errorf("no flow_id in OAuth stub response: %v", flowResult)
	}

	log.Info("  Waiting for OAuth flow to complete (flow_id: %s)...", flowID)
	for range 30 {
		pollResp, err := http.Get(fmt.Sprintf("%s/flows/%s", oauthStubURL, flowID))
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		var pollResult map[string]any
		if err := json.NewDecoder(pollResp.Body).Decode(&pollResult); err != nil {
			_ = pollResp.Body.Close()
			time.Sleep(time.Second)
			continue
		}
		_ = pollResp.Body.Close()

		tokensReady, _ := pollResult["tokens_ready"].(bool)
		if tokensReady {
			if tokens, ok := pollResult["tokens"].(map[string]any); ok {
				if token, ok := tokens["access_token"].(string); ok && token != "" {
					log.Info("  Authentication successful")
					return token, nil
				}
			}
			return "", fmt.Errorf("completed flow has no access_token: %v", pollResult)
		}

		if status, ok := pollResult["status"].(string); ok && (status == "failed" || status == "error") {
			errMsg, _ := pollResult["error"].(string)
			return "", fmt.Errorf("OAuth flow failed: %s", errMsg)
		}

		time.Sleep(time.Second)
	}

	return "", fmt.Errorf("OAuth flow timed out after 30 seconds")
}

// seedViaAPI creates an entity via HTTP API call.
func seedViaAPI(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	switch entry.Kind {
	case "threat_model":
		return seedThreatModel(serverURL, token, entry)
	case "diagram":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "diagrams")
	case "threat":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "threats")
	case "asset":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "assets")
	case "document":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "documents")
	case "note":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "notes")
	case "repository":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "repositories")
	case "webhook":
		return seedTopLevel(serverURL, token, entry, "/admin/webhooks/subscriptions")
	case "webhook_test_delivery":
		return seedWebhookTestDelivery(serverURL, token, entry, refs)
	case "addon":
		return seedAddon(serverURL, token, entry, refs)
	case "client_credential":
		return seedTopLevel(serverURL, token, entry, "/me/client_credentials")
	case "survey":
		return seedTopLevel(serverURL, token, entry, "/admin/surveys")
	case "survey_response":
		return seedSurveyResponse(serverURL, token, entry, refs)
	case "metadata":
		return seedMetadata(serverURL, token, entry, refs)
	default:
		return nil, fmt.Errorf("unsupported API seed kind: %s", entry.Kind)
	}
}

// seedThreatModel creates a threat model via API.
func seedThreatModel(serverURL, token string, entry SeedEntry) (*SeedResult, error) {
	id, err := createAPIObject(entry.Kind, serverURL+"/threat_models", token, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

// seedChildResource creates a resource nested under a threat model.
func seedChildResource(serverURL, token string, entry SeedEntry, refs RefMap, refField, resourcePath string) (*SeedResult, error) {
	tmID, err := resolveRefField(entry.Data, refField, refs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", refField, err)
	}
	if tmID == "" {
		return nil, fmt.Errorf("%s is required for %s seed", refField, entry.Kind)
	}

	// Remove the ref field from data before sending to API
	payload := copyMap(entry.Data)
	delete(payload, refField)

	url := fmt.Sprintf("%s/threat_models/%s/%s", serverURL, tmID, resourcePath)
	id, err := createAPIObject(entry.Kind, url, token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{
		Ref:  entry.Ref,
		Kind: entry.Kind,
		ID:   id,
		Extra: map[string]string{
			"threat_model_id": tmID,
		},
	}, nil
}

// seedTopLevel creates a top-level resource via API.
func seedTopLevel(serverURL, token string, entry SeedEntry, path string) (*SeedResult, error) {
	id, err := createAPIObject(entry.Kind, serverURL+path, token, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

// seedAddon creates an addon, resolving webhook_ref and threat_model_ref.
func seedAddon(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if webhookRef, err := resolveRefField(payload, "webhook_ref", refs); err != nil {
		return nil, err
	} else if webhookRef != "" {
		payload["webhook_id"] = webhookRef
		delete(payload, "webhook_ref")
	}

	if tmRef, err := resolveRefField(payload, "threat_model_ref", refs); err != nil {
		return nil, err
	} else if tmRef != "" {
		payload["threat_model_id"] = tmRef
		delete(payload, "threat_model_ref")
	}

	id, err := createAPIObject("addon", serverURL+"/addons", token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: "addon", ID: id}, nil
}

// seedSurveyResponse creates a survey response, resolving survey_ref.
func seedSurveyResponse(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if surveyRef, err := resolveRefField(payload, "survey_ref", refs); err != nil {
		return nil, err
	} else if surveyRef != "" {
		payload["survey_id"] = surveyRef
		delete(payload, "survey_ref")
	}

	id, err := createAPIObject("survey response", serverURL+"/intake/survey_responses", token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: "survey_response", ID: id, Extra: map[string]string{
		"survey_id": fmt.Sprint(payload["survey_id"]),
	}}, nil
}

// seedWebhookTestDelivery triggers a test delivery for a webhook.
func seedWebhookTestDelivery(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()
	payload := copyMap(entry.Data)

	webhookID, err := resolveRefField(payload, "webhook_ref", refs)
	if err != nil {
		return nil, err
	}
	if webhookID == "" {
		return nil, fmt.Errorf("webhook_ref is required for webhook_test_delivery")
	}

	log.Info("  Triggering webhook test delivery...")
	url := fmt.Sprintf("%s/admin/webhooks/subscriptions/%s/test", serverURL, webhookID)
	result, status, err := apiRequest("POST", url, token, map[string]any{
		"event_type": "webhook.test",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook test delivery: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("webhook test delivery failed: HTTP %d - %v", status, result)
	}

	deliveryID, ok := result["delivery_id"].(string)
	if !ok || deliveryID == "" {
		return nil, fmt.Errorf("no delivery_id in response: %v", result)
	}

	log.Info("  Created webhook test delivery: %s", deliveryID)
	return &SeedResult{Ref: entry.Ref, Kind: "webhook_test_delivery", ID: deliveryID}, nil
}

// seedMetadata creates a metadata entry on a resource.
func seedMetadata(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	// Metadata requires: target_ref (resolved to URL path), key, value
	targetRef, _ := entry.Data["target_ref"].(string)
	targetKind, _ := entry.Data["target_kind"].(string)
	key, _ := entry.Data["key"].(string)
	value, _ := entry.Data["value"].(string)

	if targetRef == "" || key == "" {
		return nil, fmt.Errorf("metadata seed requires target_ref and key")
	}

	target, ok := refs[targetRef]
	if !ok {
		return nil, fmt.Errorf("unresolved target_ref: %q", targetRef)
	}

	tmID := target.ID
	resourceID := ""
	resourcePath := ""

	// Determine URL path based on target kind
	switch targetKind {
	case "threat_model":
		// PUT /threat_models/{id}/metadata/{key}
		resourcePath = fmt.Sprintf("/threat_models/%s/metadata/%s", tmID, key)
	case "survey":
		// POST /admin/surveys/{id}/metadata
		resourcePath = fmt.Sprintf("/admin/surveys/%s/metadata", tmID)
	case "survey_response":
		// POST /intake/survey_responses/{id}/metadata
		resourcePath = fmt.Sprintf("/intake/survey_responses/%s/metadata", tmID)
	default:
		// Child resources: need threat_model_id from Extra
		if target.Extra != nil {
			tmID = target.Extra["threat_model_id"]
		}
		resourceID = target.ID
		childPath := pluralizeKind(target.Kind)
		resourcePath = fmt.Sprintf("/threat_models/%s/%s/%s/metadata/%s", tmID, childPath, resourceID, key)
	}

	payload := map[string]any{"key": key, "value": value}
	method := "PUT"
	if targetKind == "survey" || targetKind == "survey_response" {
		method = "POST"
	}

	_, status, err := apiRequest(method, serverURL+resourcePath, token, payload)
	if err != nil || status >= 300 {
		log.Debug("  Warning: failed to create metadata %s on %s (status %d): %v", key, targetRef, status, err)
	} else {
		log.Info("  Created metadata %s on %s", key, targetRef)
	}

	return &SeedResult{Ref: entry.Ref, Kind: "metadata", ID: key}, nil
}

// apiRequest makes an authenticated HTTP request and returns the response.
func apiRequest(method, url, token string, payload any) (map[string]any, int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req) //nolint:gosec // URL from CLI flags
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("failed to parse response (status %d): %s", resp.StatusCode, string(respBody))
		}
	}

	return result, resp.StatusCode, nil
}

// createAPIObject creates an API object via POST and returns its ID.
func createAPIObject(name, url, token string, payload any) (string, error) {
	log := slogging.Get()
	log.Info("  Creating %s...", name)

	result, status, err := apiRequest("POST", url, token, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", name, err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("failed to create %s: HTTP %d - %v", name, status, result)
	}

	id, ok := result["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("no 'id' field in response for %s: %v", name, result)
	}

	log.Info("    Created %s: %s", name, id)
	return id, nil
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// pluralizeKind returns the URL path segment for a resource kind.
func pluralizeKind(kind string) string {
	switch kind {
	case "threat":
		return "threats"
	case "diagram":
		return "diagrams"
	case "asset":
		return "assets"
	case "document":
		return "documents"
	case "note":
		return "notes"
	case "repository":
		return "repositories"
	default:
		return kind + "s"
	}
}
```

- [ ] **Step 2: Remove stubs from `data.go`**

Remove the `seedViaAPI`, `authenticateViaOAuthStub` stubs from the bottom of `cmd/seed/data.go`.

- [ ] **Step 3: Build to verify compilation**

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles (only `writeReferenceFiles` stub remains)

- [ ] **Step 4: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/seed/data_api.go cmd/seed/data.go
git commit -m "feat(seed): add API-based seed handlers and OAuth authentication

Handles creation of threat models, diagrams, threats, assets, documents,
notes, repositories, webhooks, addons, credentials, surveys, responses,
and metadata via HTTP API calls. Authenticates via OAuth stub.

Refs #212"
```

---

## Task 7: Reference File Generation

**Files:**
- Create: `cmd/seed/reference.go`
- Modify: `cmd/seed/data.go` (remove `writeReferenceFiles` stub)

- [ ] **Step 1: Implement reference file generation**

```go
// cmd/seed/reference.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// writeReferenceFiles generates JSON and YAML reference files from seed results.
func writeReferenceFiles(output *SeedOutput, refs RefMap, serverURL, user, provider string) error {
	log := slogging.Get()

	if output.ReferenceFile != "" {
		if err := writeJSONReference(output.ReferenceFile, refs, serverURL, user, provider); err != nil {
			return err
		}
		log.Info("Wrote JSON reference: %s", output.ReferenceFile)
	}

	if output.ReferenceYAML != "" {
		if err := writeYAMLReference(output.ReferenceYAML, refs, user, provider); err != nil {
			return err
		}
		log.Info("Wrote YAML reference: %s", output.ReferenceYAML)
	}

	return nil
}

// referenceJSON is the JSON structure for the reference file.
type referenceJSON struct {
	Version   string                     `json:"version"`
	CreatedAt string                     `json:"created_at"`
	Server    string                     `json:"server"`
	User      referenceUser              `json:"user"`
	Objects   map[string]referenceObject `json:"objects"`
}

type referenceUser struct {
	ProviderUserID string `json:"provider_user_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email"`
}

type referenceObject struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name,omitempty"`
	ThreatModelID string `json:"threat_model_id,omitempty"`
	URL           string `json:"url,omitempty"`
	SurveyID      string `json:"survey_id,omitempty"`
}

func writeJSONReference(path string, refs RefMap, serverURL, user, provider string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	objects := make(map[string]referenceObject, len(refs))
	for refName, result := range refs {
		obj := referenceObject{
			ID:   result.ID,
			Kind: result.Kind,
		}
		if result.Extra != nil {
			obj.ThreatModelID = result.Extra["threat_model_id"]
			obj.SurveyID = result.Extra["survey_id"]
			obj.URL = result.Extra["url"]
			if name, ok := result.Extra["name"]; ok {
				obj.Name = name
			}
		}
		objects[refName] = obj
	}

	ref := referenceJSON{
		Version:   "1.0.0",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Server:    serverURL,
		User: referenceUser{
			ProviderUserID: user,
			Provider:       provider,
			Email:          user + "@tmi.local",
		},
		Objects: objects,
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON reference: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

func writeYAMLReference(path string, refs RefMap, user, provider string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build the CATS parameter substitution YAML.
	// The "all:" block maps parameter names to IDs for CATS path replacement.
	// We need to find specific refs by kind for the well-known parameter names.
	tmID := findRefByKind(refs, "threat_model")
	threatID := findRefByKind(refs, "threat")
	diagramID := findRefByKind(refs, "diagram")
	documentID := findRefByKind(refs, "document")
	assetID := findRefByKind(refs, "asset")
	noteID := findRefByKind(refs, "note")
	repoID := findRefByKind(refs, "repository")
	webhookID := findRefByKind(refs, "webhook")
	deliveryID := findRefByKind(refs, "webhook_test_delivery")
	addonID := findRefByKind(refs, "addon")
	credID := findRefByKind(refs, "client_credential")
	surveyID := findRefByKind(refs, "survey")
	responseID := findRefByKind(refs, "survey_response")
	metadataKey := findRefByKind(refs, "metadata")

	// Find user info from user seed result
	adminUUID := findExtraByKind(refs, "user", "internal_uuid")
	if adminUUID == "" {
		// Use the ID field which is the internal UUID for users
		for _, r := range refs {
			if r.Kind == "user" {
				adminUUID = r.ID
				break
			}
		}
	}
	adminGroupID := "00000000-0000-0000-0000-000000000000"

	yaml := fmt.Sprintf(`# CATS Reference Data - Path-based format for parameter replacement
# Generated: %s
# See: https://endava.github.io/cats/docs/getting-started/running-cats/

# All paths - global parameter substitution
all:
  id: %s
  threat_model_id: %s
  threat_id: %s
  diagram_id: %s
  document_id: %s
  asset_id: %s
  note_id: %s
  repository_id: %s
  webhook_id: %s
  delivery_id: %s
  addon_id: %s
  client_credential_id: %s
  survey_id: %s
  survey_response_id: %s
  key: %s
  # Admin resource identifiers
  group_id: %s
  # internal_uuid for /admin/users/{internal_uuid} and /admin/groups/{internal_uuid} endpoints
  internal_uuid: %s
  # User identity uses provider:provider_id format
  user_provider: %s
  user_provider_id: %s
  admin_user_provider: %s
  admin_user_provider_id: %s
  # SAML/OAuth provider endpoints - uses the IDP name directly
  provider: %s
  idp: %s
  # Admin quota endpoints - user_id is internal UUID (OpenAPI spec defines it as UUID format)
  user_id: %s
  # Group member endpoints - user_uuid is the internal UUID of the test user
  user_uuid: %s
`,
		time.Now().UTC().Format(time.RFC3339),
		tmID, tmID,
		threatID, diagramID, documentID, assetID, noteID, repoID,
		webhookID, deliveryID, addonID, credID,
		surveyID, responseID,
		metadataKey,
		adminGroupID, adminUUID,
		provider, user,
		provider, user,
		provider, provider,
		adminUUID, adminUUID,
	)

	return os.WriteFile(path, []byte(yaml), 0o600)
}

// findRefByKind returns the ID of the first ref matching the given kind.
func findRefByKind(refs RefMap, kind string) string {
	for _, r := range refs {
		if r.Kind == kind {
			return r.ID
		}
	}
	return "00000000-0000-0000-0000-000000000000"
}

// findExtraByKind returns an Extra field value for the first ref matching the given kind.
func findExtraByKind(refs RefMap, kind, field string) string {
	for _, r := range refs {
		if r.Kind == kind && r.Extra != nil {
			if v, ok := r.Extra[field]; ok {
				return v
			}
		}
	}
	return ""
}
```

- [ ] **Step 2: Remove `writeReferenceFiles` stub from `data.go`**

- [ ] **Step 3: Build to verify compilation**

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles with no stubs remaining

- [ ] **Step 4: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/seed/reference.go cmd/seed/data.go
git commit -m "feat(seed): add reference file generation for CATS

Generates JSON and YAML reference files from seed results. YAML output
matches CATS parameter substitution format for path-based replacement.

Refs #212"
```

---

## Task 8: Config Seed Mode

**Files:**
- Modify: `cmd/seed/config.go` (replace stub)

- [ ] **Step 1: Implement config seed mode**

Replace the stub in `cmd/seed/config.go`:

```go
// cmd/seed/config.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"gopkg.in/yaml.v3"
)

// runConfigSeed reads a TMI config file, writes DB-eligible settings to
// system_settings, and generates a migrated YAML with only infrastructure keys.
func runConfigSeed(db *testdb.TestDB, inputFile, outputFile string, overwrite, dryRun bool) error {
	log := slogging.Get()

	// Load config
	cfg, err := config.Load(inputFile)
	if err != nil {
		return fmt.Errorf("failed to load config %s: %w", inputFile, err)
	}

	// Get all migratable settings
	allSettings := cfg.GetMigratableSettings()
	log.Info("Found %d settings in config", len(allSettings))

	// Split into infrastructure vs. DB-eligible
	var infraSettings []config.MigratableSetting
	var dbSettings []config.MigratableSetting

	for _, s := range allSettings {
		if config.IsInfrastructureKey(s.Key) {
			infraSettings = append(infraSettings, s)
		} else {
			dbSettings = append(dbSettings, s)
		}
	}

	log.Info("  Infrastructure (stay in file): %d settings", len(infraSettings))
	log.Info("  DB-eligible (move to database): %d settings", len(dbSettings))

	if dryRun {
		log.Info("")
		log.Info("[DRY RUN] Infrastructure settings (would stay in config file):")
		for _, s := range infraSettings {
			displayValue := s.Value
			if s.Secret {
				displayValue = "<secret>"
			}
			log.Info("  %s = %s [%s] (source: %s)", s.Key, displayValue, s.Type, s.Source)
		}
		log.Info("")
		log.Info("[DRY RUN] DB-eligible settings (would write to database):")
		for _, s := range dbSettings {
			displayValue := s.Value
			if s.Secret {
				displayValue = "<secret>"
			}
			log.Info("  %s = %s [%s] (source: %s)", s.Key, displayValue, s.Type, s.Source)
		}

		if outputFile == "" {
			outputFile = deriveOutputPath(inputFile)
		}
		log.Info("")
		log.Info("[DRY RUN] Would write migrated config to: %s", outputFile)
		return nil
	}

	// Initialize encryptor if secrets config is available
	var encryptor *crypto.SettingsEncryptor
	secretsProvider, err := secrets.NewProvider(context.Background(), &cfg.Secrets)
	if err != nil {
		log.Debug("No secrets provider available for encryption: %v", err)
	} else {
		enc, err := crypto.NewSettingsEncryptor(context.Background(), secretsProvider)
		if err != nil {
			log.Debug("No encryptor available: %v", err)
		} else {
			encryptor = enc
			log.Info("Settings encryption enabled")
		}
		if closeErr := secretsProvider.Close(); closeErr != nil {
			log.Debug("Failed to close secrets provider: %v", closeErr)
		}
	}

	// Write DB-eligible settings
	var written, skipped int
	for _, s := range dbSettings {
		var existing models.SystemSetting
		exists := db.DB().Where("setting_key = ?", s.Key).First(&existing).Error == nil

		if exists && !overwrite {
			skipped++
			log.Debug("  Skipping existing: %s", s.Key)
			continue
		}

		value := s.Value
		// Encrypt secret values if encryptor is available
		if s.Secret && encryptor != nil && value != "" {
			encrypted, err := encryptor.Encrypt(value)
			if err != nil {
				return fmt.Errorf("failed to encrypt setting %s: %w", s.Key, err)
			}
			value = encrypted
		}

		description := s.Description
		setting := models.SystemSetting{
			SettingKey:   s.Key,
			Value:        value,
			SettingType:  s.Type,
			Description:  &description,
			ModifiedAt:   time.Now(),
		}

		if exists {
			if err := db.DB().Model(&existing).Updates(map[string]any{
				"value":        value,
				"setting_type": s.Type,
				"description":  description,
				"modified_at":  time.Now(),
			}).Error; err != nil {
				return fmt.Errorf("failed to update setting %s: %w", s.Key, err)
			}
		} else {
			if err := db.DB().Create(&setting).Error; err != nil {
				return fmt.Errorf("failed to create setting %s: %w", s.Key, err)
			}
		}
		written++
		log.Debug("  Wrote: %s", s.Key)
	}

	log.Info("Settings written to database: %d written, %d skipped", written, skipped)

	// Generate migrated YAML
	if outputFile == "" {
		outputFile = deriveOutputPath(inputFile)
	}

	if err := writeMigratedConfig(cfg, infraSettings, outputFile); err != nil {
		return fmt.Errorf("failed to write migrated config: %w", err)
	}

	log.Info("Migrated config written to: %s", outputFile)
	log.Info("")
	log.Info("Next steps:")
	log.Info("  1. Review the migrated config: %s", outputFile)
	log.Info("  2. Backup your current config")
	log.Info("  3. Replace your config with the migrated version")
	log.Info("  4. Restart the server")

	return nil
}

// writeMigratedConfig writes a config YAML containing only infrastructure settings.
func writeMigratedConfig(cfg *config.Config, infraSettings []config.MigratableSetting, outputPath string) error {
	// Build a set of infrastructure keys for quick lookup
	infraKeys := make(map[string]bool, len(infraSettings))
	for _, s := range infraSettings {
		infraKeys[s.Key] = true
	}

	// Build a nested map structure from the flat infrastructure keys.
	// This reconstructs the YAML hierarchy from dot-notation keys.
	root := make(map[string]any)

	for _, s := range infraSettings {
		parts := strings.Split(s.Key, ".")
		current := root
		for i, part := range parts {
			if i == len(parts)-1 {
				// Leaf node — set the value
				current[part] = s.Value
			} else {
				// Intermediate node — create nested map if needed
				if _, ok := current[part]; !ok {
					current[part] = make(map[string]any)
				}
				if next, ok := current[part].(map[string]any); ok {
					current = next
				}
			}
		}
	}

	// Add a header comment
	header := fmt.Sprintf("# TMI Configuration (infrastructure keys only)\n"+
		"# Generated by tmi-seed --mode=config on %s\n"+
		"# Non-infrastructure settings have been migrated to the database.\n"+
		"# See: https://github.com/ericfitz/tmi/wiki/Configuration-Management\n\n",
		time.Now().UTC().Format(time.RFC3339))

	yamlData, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	output := []byte(header)
	output = append(output, yamlData...)

	return os.WriteFile(outputPath, output, 0o600)
}

// deriveOutputPath derives the migrated config output path from the input path.
// e.g., "config-production.yml" -> "config-production-migrated.yml"
func deriveOutputPath(inputPath string) string {
	ext := ""
	base := inputPath
	for _, e := range []string{".yml", ".yaml", ".json"} {
		if strings.HasSuffix(inputPath, e) {
			ext = e
			base = strings.TrimSuffix(inputPath, e)
			break
		}
	}
	if ext == "" {
		ext = ".yml"
	}
	return base + "-migrated" + ext
}
```

- [ ] **Step 2: Build to verify compilation**

Run: `go build -o bin/tmi-seed ./cmd/seed`
Expected: Compiles

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/seed/config.go
git commit -m "feat(seed): add config seed mode

Reads a TMI config YAML, splits settings into infrastructure (stays in
file) vs. DB-eligible (writes to system_settings), and generates a
stripped migrated YAML for the operator to swap in.

Refs #212"
```

---

## Task 9: Create CATS Seed Data File

**Files:**
- Create: `test/seeds/cats-seed-data.json`

- [ ] **Step 1: Create the CATS seed data file**

Extract all hardcoded objects from `cmd/cats-seed/api_objects.go` into the seed file format:

```json
{
  "format_version": "1.0",
  "description": "CATS fuzzing test data — all objects needed for comprehensive API security testing",
  "output": {
    "reference_file": "test/outputs/cats/cats-test-data.json",
    "reference_yaml": "test/outputs/cats/cats-test-data.yml"
  },
  "seeds": [
    {
      "kind": "user",
      "ref": "cats-user",
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
        "description": "Created by tmi-seed for comprehensive API fuzzing. DO NOT DELETE.",
        "threat_model_framework": "STRIDE",
        "metadata": [
          {"key": "version", "value": "1.0"},
          {"key": "purpose", "value": "cats-fuzzing-test-data"}
        ]
      }
    },
    {
      "kind": "threat",
      "ref": "cats-threat",
      "data": {
        "threat_model_ref": "cats-tm",
        "name": "CATS Test Threat",
        "description": "Test threat for CATS fuzzing",
        "threat_type": ["Tampering", "Information Disclosure"],
        "severity": "high",
        "priority": "high",
        "status": "identified"
      }
    },
    {
      "kind": "diagram",
      "ref": "cats-diagram",
      "data": {
        "threat_model_ref": "cats-tm",
        "name": "CATS Test Diagram",
        "type": "DFD-1.0.0"
      }
    },
    {
      "kind": "document",
      "ref": "cats-document",
      "data": {
        "threat_model_ref": "cats-tm",
        "name": "CATS Test Document",
        "uri": "https://docs.example.com/cats-test-document.pdf",
        "description": "Test document for CATS fuzzing"
      }
    },
    {
      "kind": "asset",
      "ref": "cats-asset",
      "data": {
        "threat_model_ref": "cats-tm",
        "name": "CATS Test Asset",
        "description": "Test asset for CATS fuzzing",
        "type": "software"
      }
    },
    {
      "kind": "note",
      "ref": "cats-note",
      "data": {
        "threat_model_ref": "cats-tm",
        "name": "CATS Test Note",
        "content": "CATS test note for comprehensive API fuzzing"
      }
    },
    {
      "kind": "repository",
      "ref": "cats-repo",
      "data": {
        "threat_model_ref": "cats-tm",
        "uri": "https://github.com/example/cats-test-repo"
      }
    },
    {
      "kind": "webhook",
      "ref": "cats-webhook",
      "data": {
        "name": "CATS Test Webhook",
        "url": "https://webhook.site/cats-test-webhook",
        "events": ["threat_model.created", "threat.created"]
      }
    },
    {
      "kind": "webhook_test_delivery",
      "ref": "cats-delivery",
      "data": {
        "webhook_ref": "cats-webhook"
      }
    },
    {
      "kind": "addon",
      "ref": "cats-addon",
      "data": {
        "name": "CATS Test Addon",
        "webhook_ref": "cats-webhook",
        "threat_model_ref": "cats-tm"
      }
    },
    {
      "kind": "client_credential",
      "ref": "cats-credential",
      "data": {
        "name": "CATS Test Credential",
        "description": "Test credential for CATS fuzzing"
      }
    },
    {
      "kind": "survey",
      "ref": "cats-survey",
      "data": {
        "name": "CATS Test Survey",
        "description": "Created by tmi-seed for comprehensive API fuzzing. DO NOT DELETE.",
        "version": "v1-cats-seed",
        "status": "active",
        "survey_json": {
          "pages": [
            {
              "name": "page1",
              "elements": [
                {
                  "type": "text",
                  "name": "project_name",
                  "title": "Project Name"
                }
              ]
            }
          ]
        },
        "settings": {
          "allow_threat_model_linking": true
        }
      }
    },
    {
      "kind": "survey_response",
      "ref": "cats-response",
      "data": {
        "survey_ref": "cats-survey",
        "answers": {
          "project_name": "CATS Test Project"
        },
        "authorization": [
          {
            "principal_type": "user",
            "provider": "tmi",
            "provider_id": "charlie",
            "role": "owner"
          }
        ]
      }
    },
    {
      "kind": "metadata",
      "ref": "cats-metadata-tm",
      "data": {
        "target_ref": "cats-tm",
        "target_kind": "threat_model",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-threat",
        "target_kind": "threat",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-diagram",
        "target_kind": "diagram",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-document",
        "target_kind": "document",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-asset",
        "target_kind": "asset",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-note",
        "target_kind": "note",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-repo",
        "target_kind": "repository",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-survey",
        "target_kind": "survey",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    },
    {
      "kind": "metadata",
      "data": {
        "target_ref": "cats-response",
        "target_kind": "survey_response",
        "key": "cats-test-key",
        "value": "cats-test-value"
      }
    }
  ]
}
```

- [ ] **Step 2: Validate JSON syntax**

Run: `jq empty test/seeds/cats-seed-data.json && echo "Valid JSON"`
Expected: `Valid JSON`

- [ ] **Step 3: Commit**

```bash
git add test/seeds/cats-seed-data.json
git commit -m "feat(seed): add CATS test data seed file

Extracts all 13+ hardcoded API objects from cmd/cats-seed/ into a
declarative JSON seed file. Includes users, threat models, diagrams,
threats, assets, documents, notes, repositories, webhooks, addons,
credentials, surveys, responses, and metadata entries.

Refs #212"
```

---

## Task 10: Remove Migration Endpoint

**Files:**
- Modify: `api-schema/tmi-openapi.json`
- Modify: `api/config_handlers.go`
- Modify: `api/config_handlers_test.go`
- Modify: `test/integration/workflows/settings_crud_test.go`
- Regenerate: `api/api.go`

- [ ] **Step 1: Remove `/admin/settings/migrate` from OpenAPI spec**

Run:
```bash
jq 'del(.paths["/admin/settings/migrate"])' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Validate OpenAPI spec**

Run: `make validate-openapi`
Expected: PASS

- [ ] **Step 3: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated without `MigrateSystemSettings` and `MigrateSystemSettingsParams`

- [ ] **Step 4: Remove the handler from `config_handlers.go`**

Remove:
1. The `MigrateSystemSettings` function (lines ~625-724)
2. The `"migrate"` entry from the `isReservedSettingKey` function

To find the reserved key check:
```bash
grep -n "migrate" api/config_handlers.go
```

Remove the `"migrate"` case from the reserved keys map/switch, keeping `"reencrypt"`.

- [ ] **Step 5: Remove migrate tests from `config_handlers_test.go`**

Remove these test functions:
- `TestMigrateSystemSettings_AdminRequired`
- `TestMigrateSystemSettings_ServiceUnavailable`
- `TestMigrateSystemSettings_ConfigProviderUnavailable`
- `TestMigrateSystemSettings_Success_NoExisting`
- `TestMigrateSystemSettings_SkipExisting_OverwriteFalse`
- `TestMigrateSystemSettings_OverwriteExisting_OverwriteTrue`
- `TestMigrateSystemSettings_EmptyConfigProvider`

- [ ] **Step 6: Remove migrate subtests from integration tests**

In `test/integration/workflows/settings_crud_test.go`, remove the subtests that reference `/admin/settings/migrate`:
- The "Migrate settings from config" subtest
- The "Migrate with overwrite" subtest
- The migrate reference in the non-admin access test (keep the rest of the non-admin test)

Also update the file header comment to remove the migrate endpoint reference.

- [ ] **Step 7: Build and test**

Run: `make build-server && make test-unit`
Expected: Both pass. The build should succeed because `api/api.go` no longer declares `MigrateSystemSettings` in the `ServerInterface`, and the handler + tests are removed.

- [ ] **Step 8: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go api/config_handlers.go api/config_handlers_test.go test/integration/workflows/settings_crud_test.go
git commit -m "refactor(api): remove POST /admin/settings/migrate endpoint

Config migration is now handled by the tmi-seed CLI tool (--mode=config)
instead of a runtime API endpoint. The three-tier priority system
(env > config > DB) is unchanged.

Refs #212"
```

---

## Task 11: Update Build Scripts and Makefile

**Files:**
- Create: `scripts/run-seed.py`
- Modify: `scripts/build-server.py`
- Modify: `scripts/run-cats-fuzz.py`
- Modify: `Makefile`

- [ ] **Step 1: Update `scripts/build-server.py` component map**

Replace the `"cats-seed"` entry in the `COMPONENTS` dict with `"seed"`:

```python
"seed": {
    "output": "bin/tmi-seed",
    "package": "github.com/ericfitz/tmi/cmd/seed",
    "tags": [],
    "ldflags": False,
},
```

Also update the `--oci` flag check from `cats-seed` to `seed`, and update the module docstring.

- [ ] **Step 2: Create `scripts/run-seed.py`**

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Build and run the TMI unified seeding tool.

Builds the tmi-seed binary (with or without Oracle support) and runs it
in data mode to seed test data for CATS API fuzzing.
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build and run the TMI unified seeding tool."
    )
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument(
        "--oci",
        action="store_true",
        default=False,
        help="Build with Oracle support (requires scripts/oci-env.sh)",
    )
    parser.add_argument(
        "--user",
        metavar="USER",
        default="charlie",
        help="CATS user (default: charlie)",
    )
    parser.add_argument(
        "--provider",
        metavar="PROVIDER",
        default="tmi",
        help="Auth provider (default: tmi)",
    )
    parser.add_argument(
        "--server",
        metavar="URL",
        default="http://localhost:8080",
        help="Server URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "--input",
        metavar="FILE",
        default="test/seeds/cats-seed-data.json",
        help="Seed data file (default: test/seeds/cats-seed-data.json)",
    )
    return parser.parse_args()


def build_seed(oci: bool, project_root: Path) -> None:
    """Build the tmi-seed binary."""
    if oci:
        oci_env_path = project_root / "scripts" / "oci-env.sh"
        if not oci_env_path.exists():
            log_error(f"OCI env script not found: {oci_env_path}")
            sys.exit(1)
        log_info("Building seed tool with Oracle support...")
        run_cmd(
            [
                "/bin/bash",
                "-c",
                f". {oci_env_path} && go build -tags oracle -o bin/tmi-seed github.com/ericfitz/tmi/cmd/seed",
            ],
            cwd=str(project_root),
        )
        log_success("Seed tool built with Oracle support: bin/tmi-seed")
    else:
        log_info("Building seed tool...")
        run_cmd(
            ["go", "build", "-o", "bin/tmi-seed", "github.com/ericfitz/tmi/cmd/seed"],
            cwd=str(project_root),
        )
        log_success("Seed tool built: bin/tmi-seed")


def run_seed(config: str, user: str, provider: str, server: str, input_file: str, project_root: Path) -> None:
    """Run the tmi-seed binary in data mode."""
    log_info(f"Seeding test data (user={user}, provider={provider}, server={server})...")
    run_cmd(
        [
            "./bin/tmi-seed",
            "--mode=data",
            f"--config={config}",
            f"--input={input_file}",
            f"--user={user}",
            f"--provider={provider}",
            f"--server={server}",
        ],
        cwd=str(project_root),
    )
    log_success("Seeding completed")


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()

    config = args.config
    if args.oci:
        default_config = str(project_root / "config-development.yml")
        if config == default_config:
            config = str(project_root / "config-development-oci.yml")

    build_seed(args.oci, project_root)
    run_seed(config, args.user, args.provider, args.server, args.input, project_root)


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Update `scripts/run-cats-fuzz.py`**

Update references from `cats-seed` to `tmi-seed` / `run-seed.py`. Search for the seed invocation and update it to call `run-seed.py` instead of `run-cats-seed.py`.

- [ ] **Step 4: Update Makefile targets**

Replace the build and seed targets. Key changes:

```makefile
# In the build section:
build-seed:  ## Build unified seeding tool (database-agnostic)
	@uv run scripts/build-server.py --component seed

build-seed-oci:  ## Build unified seeding tool with Oracle support
	@uv run scripts/build-server.py --component seed --oci

# In the CATS section:
cats-seed:  ## Seed database for CATS fuzzing
	@uv run scripts/run-seed.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)

cats-seed-oci:  ## Seed database for CATS fuzzing (Oracle ADB)
	@uv run scripts/run-seed.py --oci --user=$(CATS_USER) --provider=$(CATS_PROVIDER)
```

Update the `.PHONY` declarations to replace `build-cats-seed build-cats-seed-oci` with `build-seed build-seed-oci`.

- [ ] **Step 5: Lint and verify**

Run: `make list-targets | grep seed` to confirm targets are renamed correctly.
Run: `make build-seed` to verify the build works via the new target.

- [ ] **Step 6: Commit**

```bash
git add scripts/run-seed.py scripts/build-server.py scripts/run-cats-fuzz.py Makefile
git commit -m "build: update Makefile and scripts for unified seed tool

Renames cats-seed targets to seed, adds run-seed.py wrapper, updates
build-server.py component map from cats-seed to seed.

Refs #212"
```

---

## Task 12: Remove Old `cmd/cats-seed/` and `scripts/run-cats-seed.py`

**Files:**
- Remove: `cmd/cats-seed/main.go`
- Remove: `cmd/cats-seed/api_objects.go`
- Remove: `cmd/cats-seed/reference_data.go`
- Remove: `scripts/run-cats-seed.py`

- [ ] **Step 1: Remove the old files**

```bash
git rm cmd/cats-seed/main.go cmd/cats-seed/api_objects.go cmd/cats-seed/reference_data.go scripts/run-cats-seed.py
```

- [ ] **Step 2: Build to verify nothing depends on the removed files**

Run: `make build-server && make build-seed`
Expected: Both compile

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor: remove cmd/cats-seed/ (replaced by cmd/seed/)

All CATS seeding logic is now in cmd/seed/ with test data in
test/seeds/cats-seed-data.json instead of hardcoded Go.

Refs #212
Closes #212"
```

---

## Task 13: End-to-End Verification

This task verifies the full system works before pushing.

- [ ] **Step 1: Build all binaries**

Run: `make build-server && make build-seed`
Expected: Both compile

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 4: Start dev environment and test data seed**

Run:
```bash
make start-dev
make cats-seed
```

Expected: The new seed tool creates all CATS test objects, writes reference files to `test/outputs/cats/`.

- [ ] **Step 5: Verify reference files**

Run:
```bash
jq '.objects | keys' test/outputs/cats/cats-test-data.json
cat test/outputs/cats/cats-test-data.yml | head -20
```

Expected: Reference files contain all expected object refs with valid UUIDs.

- [ ] **Step 6: Test config seed mode (dry run)**

Run:
```bash
./bin/tmi-seed --mode=config --input=config-development.yml --dry-run
```

Expected: Output showing infrastructure vs. DB-eligible settings split.

- [ ] **Step 7: Run integration tests**

Run: `make test-integration`
Expected: PASS (migrate subtests removed, remaining settings tests pass)

- [ ] **Step 8: Stop dev environment**

Run: `make stop-server` or `make clean-everything`

---

## Task 14: Wiki Documentation

**Files:**
- GitHub Wiki pages (external to repo)

- [ ] **Step 1: Create "Configuration Management" wiki page**

Content must cover:
- The three-tier priority system (env > config file > DB) with concrete examples
- The full infrastructure keys list with the table from the spec (category, prefixes, reason)
- How to manage runtime settings via the admin API (`GET/PUT/DELETE /admin/settings/*`)
- The `administrators` lockout-prevention behavior

- [ ] **Step 2: Create "Config Migration Guide" wiki page**

Content must cover:
- The operational workflow (run seeder → review → swap config → restart)
- Complete step-by-step walkthrough with `tmi-seed --mode=config` commands
- What to verify after migration (server starts, settings visible in admin API)
- Rollback procedure (swap backup config file back, restart)
- Note about encryption when secrets provider is configured

- [ ] **Step 3: Create "Seed Tool Reference" wiki page**

Content must cover:
- All three modes (system, data, config) with CLI flags and examples
- Seed data file format specification (envelope, kinds, refs, output)
- Complete entity kinds table with strategy and endpoint
- Reference resolution rules (top-to-bottom, `{kind}_ref` convention)
- Reference file generation for CATS (JSON and YAML formats)
- Common scenarios: CATS setup, fresh deployment bootstrapping, config migration

- [ ] **Step 4: Update "CATS Testing" wiki page**

Update existing page to:
- Reference `tmi-seed --mode=data` instead of `cats-seed`
- Document `test/seeds/cats-seed-data.json` as the source of CATS test data
- Update the `make cats-seed` / `make cats-fuzz` workflow description
