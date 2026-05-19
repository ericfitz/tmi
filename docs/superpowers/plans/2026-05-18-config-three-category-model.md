# Config Classification Model + Platform-Shared Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Classify every TMI configuration item by an explicit orthogonal property set so config behavior (DB seeding, masking, envelope-stamping, API visibility, secret resolution) is derived from the classification and self-enforced by a validation suite; collapse ten `config-*.yml` files to three bootstrap-only files; add a worker bootstrap contract proven by a stub worker.

**Architecture:** Issue #415, dependency of #347. Approach A — the single `config.Config` struct is retained; classification is added as typed data on each `MigratableSetting` literal in `internal/config/`. A new `ConfigClass` struct carries `Category`, `Secret`, `ValueKind`, `Delivery`, `Visibility`, `Mutability`, `Consumers`, `Required`. A validation suite proves the classification is total, disjoint, and internally consistent. Shared config (the embedding profile) is read through a single `StampedConfigProvider` and stamped into job envelopes by the monolith; the worker bootstrap lives in a new `internal/config/bootstrap` package proven by `cmd/worker-probe`.

**Tech Stack:** Go, GORM, the existing `api/settings_service.go` (DB-backed settings with Redis + in-memory cache), `internal/crypto.SettingsEncryptor`, `internal/secrets` (Vault/AWS/Azure/GCP/OCI providers), `internal/worker` (NATS JetStream, `github.com/nats-io/nats.go v1.36.0`).

**Branch / integration:** All work lands on `dev/1.4.0`. No PR, no merge to `main`. Issue #415 is closed manually (comment + `gh issue close`) at the end, because `Closes #415` only auto-closes from `main`.

**Reference spec:** `docs/superpowers/specs/2026-05-18-config-three-category-model-design.md`

---

## File Structure

**New files:**
- `internal/config/classification.go` — the `ConfigClass` struct and all classification enums (`Category`, `ValueKind`, `Visibility`, `Mutability`, `Consumer`), plus the `Delivery` struct.
- `internal/config/classification_registry.go` — the `classificationFor(key string) ConfigClass` lookup: the single registry mapping every setting key to its `ConfigClass`.
- `internal/config/classification_validation.go` — `ValidateClassifications([]MigratableSetting) error`: the cross-property rule checker.
- `internal/config/classification_test.go` — the validation suite (the "never revisit" guarantee).
- `internal/config/stamped_config.go` — the `EmbeddingProfile` and `StampedConfig` types and `StampedConfigProvider` interface.
- `internal/config/stamped_config_test.go` — unit tests for `StampedConfig` assembly.
- `internal/config/bootstrap/bootstrap.go` — the `WorkerBootstrap` struct and `LoadWorker()` (env-only).
- `internal/config/bootstrap/bootstrap_test.go` — unit tests for `LoadWorker()`.
- `api/stamped_config_provider.go` — the concrete `StampedConfigProvider` reading through `SettingsService`.
- `api/stamped_config_provider_test.go` — unit tests for the provider.
- `cmd/worker-probe/main.go` — the stub worker proving the bootstrap + envelope + secret-mount contract.
- `cmd/dbtool/config_legacy.go` — `runLegacyConfigImport()`, the `config import-legacy` subcommand.
- `internal/config/example_gen.go` — `GenerateExampleConfig() ([]byte, error)`: emits `config-example.yml` from the registry.
- `cmd/genconfig/main.go` — tiny binary invoked by `make generate-config-example` to write `config-example.yml`.
- `test/integration/worker_probe_integration_test.go` — the probe ↔ NATS ↔ envelope-builder integration test.

**Modified files:**
- `internal/config/migratable_settings.go` — `MigratableSetting` gains a `Class ConfigClass` field; every `getMigratable*Settings()` helper attaches a class via the registry.
- `internal/config/infrastructure_keys.go` — **deleted**.
- `internal/config/infrastructure_keys_test.go` — **deleted**.
- `cmd/dbtool/config.go` — replace `config.IsInfrastructureKey(s.Key)` with `s.Class.Category == config.CategoryBootstrap`.
- `cmd/dbtool/main.go` — register the `import-legacy` flag and dispatch case.
- `api/settings_service.go` — `Get` refuses to DB-serve a `CategoryBootstrap` key.
- `api/timmy_session_manager.go` / `api/timmy_llm_service.go` — embedding-model reads route through `StampedConfigProvider`.
- `scripts/build-server.py` — add `worker-probe` and `genconfig` components.
- `Makefile` — add `build-worker-probe`, `build-genconfig`, `generate-config-example` targets.
- `config-development.yml`, `config-test.yml`, `config-production.yml` — reduced to bootstrap-only.
- Removed: `config-development-sqlite.yml`, `config-development-mysql.yml`, `config-development-sqlserver.yml`, `config-development-oci.yml`, `config-test-integration-pg.yml`, `config-test-integration-oci.yml`.
- `api/seed/seed.go` or a new seed source — operational-config seed set.

---

## Task ordering rationale

Tasks 1–4 build the classification model and its validation suite — the spine everything else derives from. Task 5 deletes the old prefix denylist. Tasks 6–8 build shared config (`StampedConfig` + provider + Timmy rewiring). Tasks 9–11 build the worker bootstrap and the stub worker. Tasks 12–14 do the `config-*.yml` collapse, the seeder, and legacy migration. Task 15 wires API visibility. Task 16 is the final integration test and cleanup. Each task ends green and committed.

---

### Task 1: Classification types and enums

**Files:**
- Create: `internal/config/classification.go`
- Test: `internal/config/classification_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/classification_test.go`:

```go
package config

import "testing"

func TestConfigClass_String(t *testing.T) {
	if CategoryBootstrap.String() != "bootstrap" {
		t.Errorf("CategoryBootstrap.String() = %q, want %q", CategoryBootstrap.String(), "bootstrap")
	}
	if CategoryOperational.String() != "operational" {
		t.Errorf("CategoryOperational.String() = %q, want %q", CategoryOperational.String(), "operational")
	}
}

func TestConfigClass_ZeroValueIsUnclassified(t *testing.T) {
	var c ConfigClass
	if c.Category != CategoryUnclassified {
		t.Errorf("zero ConfigClass.Category = %v, want CategoryUnclassified", c.Category)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestConfigClass_String`
Expected: FAIL — `undefined: CategoryBootstrap`.

- [ ] **Step 3: Write the implementation**

Create `internal/config/classification.go`:

```go
package config

// Category answers: where does the value come from at rest?
type Category int

const (
	// CategoryUnclassified is the zero value. A setting with this category
	// fails the validation suite — it forces every setting to be classified.
	CategoryUnclassified Category = iota
	// CategoryBootstrap settings are loaded from file/env only, never the DB,
	// and consumed at startup before the settings service exists.
	CategoryBootstrap
	// CategoryOperational settings are DB-backed and runtime-editable.
	CategoryOperational
)

func (c Category) String() string {
	switch c {
	case CategoryBootstrap:
		return "bootstrap"
	case CategoryOperational:
		return "operational"
	default:
		return "unclassified"
	}
}

// ValueKind answers: is the stored value the secret itself, or a pointer to it?
type ValueKind int

const (
	// ValueKindInline means the field holds the actual value.
	ValueKindInline ValueKind = iota
	// ValueKindReference means the field holds a locator (vault://..., a file
	// path, an env-var name) dereferenced at use time. Only valid when Secret.
	ValueKindReference
)

func (v ValueKind) String() string {
	if v == ValueKindReference {
		return "reference"
	}
	return "inline"
}

// Visibility answers: who may read this setting through the API?
type Visibility int

const (
	// VisibilityInternal: server-side only, never in any API response.
	VisibilityInternal Visibility = iota
	// VisibilityAdminOnly: visible to admins via /admin config endpoints.
	VisibilityAdminOnly
	// VisibilityPublic: exposed on the unauthenticated /config endpoint.
	VisibilityPublic
)

func (v Visibility) String() string {
	switch v {
	case VisibilityAdminOnly:
		return "admin-only"
	case VisibilityPublic:
		return "public"
	default:
		return "internal"
	}
}

// Mutability answers: can it change after startup?
type Mutability int

const (
	// MutabilityStatic: read once at boot; a change needs a restart.
	MutabilityStatic Mutability = iota
	// MutabilityHot: re-read at use time; a runtime edit takes effect at once.
	MutabilityHot
)

func (m Mutability) String() string {
	if m == MutabilityHot {
		return "hot"
	}
	return "static"
}

// Consumer is a closed enum of the processes that read configuration.
// Add a value here when a new component type is introduced.
type Consumer int

const (
	ConsumerMonolith Consumer = iota
	ConsumerTMIUX
	ConsumerWorkerExtractor
	ConsumerWorkerChunkEmbed
)

func (c Consumer) String() string {
	switch c {
	case ConsumerTMIUX:
		return "tmi-ux"
	case ConsumerWorkerExtractor:
		return "worker:extractor"
	case ConsumerWorkerChunkEmbed:
		return "worker:chunk-embed"
	default:
		return "monolith"
	}
}

// Delivery describes how an operational setting reaches a process that cannot
// ask the monolith over HTTP. It is nil on bootstrap settings.
type Delivery struct {
	// StampedIntoEnvelope: the monolith copies this into job envelopes.
	StampedIntoEnvelope bool
	// SharedInvariant: the monolith ALSO consumes this; ingest and the
	// monolith must agree. Implies StampedIntoEnvelope.
	SharedInvariant bool
}

// ConfigClass is the complete classification of one configuration item.
type ConfigClass struct {
	Category   Category
	Secret     bool
	ValueKind  ValueKind
	Delivery   *Delivery // nil for CategoryBootstrap
	Visibility Visibility
	Mutability Mutability
	Consumers  []Consumer
	Required   bool
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestConfigClass`
Expected: PASS (both `TestConfigClass_String` and `TestConfigClass_ZeroValueIsUnclassified`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/classification.go internal/config/classification_test.go
git commit -m "feat(config): add ConfigClass classification types"
```

---

### Task 2: The classification validation rules

**Files:**
- Create: `internal/config/classification_validation.go`
- Test: `internal/config/classification_validation_test.go`

This task builds the rule checker. The registry it validates is built in Task 4; here the checker takes an explicit slice so it is testable in isolation.

- [ ] **Step 1: Write the failing test**

Create `internal/config/classification_validation_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func validClass() ConfigClass {
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     false,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{},
		Visibility: VisibilityInternal,
		Mutability: MutabilityHot,
		Consumers:  []Consumer{ConsumerMonolith},
		Required:   false,
	}
}

func TestValidateClassifications_RejectsUnclassified(t *testing.T) {
	s := []MigratableSetting{{Key: "x", Description: "x", Class: ConfigClass{}}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "unclassified") {
		t.Fatalf("want unclassified error, got %v", err)
	}
}

func TestValidateClassifications_BootstrapHasNoDelivery(t *testing.T) {
	c := validClass()
	c.Category = CategoryBootstrap
	c.Delivery = &Delivery{}
	c.Consumers = []Consumer{ConsumerMonolith}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("want bootstrap-delivery error, got %v", err)
	}
}

func TestValidateClassifications_SharedInvariantImpliesStamped(t *testing.T) {
	c := validClass()
	c.Delivery = &Delivery{StampedIntoEnvelope: false, SharedInvariant: true}
	c.Consumers = []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "SharedInvariant") {
		t.Fatalf("want SharedInvariant-implies-Stamped error, got %v", err)
	}
}

func TestValidateClassifications_SharedInvariantNeedsWorkerConsumer(t *testing.T) {
	c := validClass()
	c.Delivery = &Delivery{StampedIntoEnvelope: true, SharedInvariant: true}
	c.Consumers = []Consumer{ConsumerMonolith}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("want SharedInvariant-needs-worker error, got %v", err)
	}
}

func TestValidateClassifications_ReferenceImpliesSecret(t *testing.T) {
	c := validClass()
	c.Secret = false
	c.ValueKind = ValueKindReference
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "Reference") {
		t.Fatalf("want Reference-implies-Secret error, got %v", err)
	}
}

func TestValidateClassifications_PublicCannotBeSecret(t *testing.T) {
	c := validClass()
	c.Secret = true
	c.Visibility = VisibilityPublic
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("want public-cannot-be-secret error, got %v", err)
	}
}

func TestValidateClassifications_NeedsDescriptionAndConsumer(t *testing.T) {
	c := validClass()
	c.Consumers = nil
	s := []MigratableSetting{{Key: "x", Description: "", Class: c}}
	err := ValidateClassifications(s)
	if err == nil {
		t.Fatal("want error for empty description and no consumer")
	}
}

func TestValidateClassifications_AcceptsValidSet(t *testing.T) {
	good := []MigratableSetting{
		{Key: "database.url", Description: "DB URL", Class: ConfigClass{
			Category: CategoryBootstrap, Visibility: VisibilityInternal,
			Mutability: MutabilityStatic, Consumers: []Consumer{ConsumerMonolith},
			Required: true,
		}},
		{Key: "embedding.model", Description: "Embedding model", Class: ConfigClass{
			Category: CategoryOperational, Visibility: VisibilityAdminOnly,
			Mutability: MutabilityHot,
			Delivery:   &Delivery{StampedIntoEnvelope: true, SharedInvariant: true},
			Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
		}},
	}
	if err := ValidateClassifications(good); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}
}
```

Note: `MigratableSetting` does not yet have a `Class` field — Step 3 of this task adds it.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestValidateClassifications`
Expected: FAIL — `MigratableSetting` has no field `Class`, and `ValidateClassifications` is undefined.

- [ ] **Step 3: Add the `Class` field to `MigratableSetting`**

In `internal/config/migratable_settings.go`, change the struct (lines 9-17):

```go
// MigratableSetting represents a setting that can be migrated from config to database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
	Secret      bool   // true = mask value in API responses (kept for back-compat; mirrors Class.Secret)
	Source      string // "config" or "environment"
	Class       ConfigClass
}
```

- [ ] **Step 4: Write the validation implementation**

Create `internal/config/classification_validation.go`:

```go
package config

import (
	"fmt"
	"strings"
)

// ValidateClassifications checks that every setting carries a complete and
// internally consistent ConfigClass. It returns a single error listing every
// violation found. This is the mechanism that makes the classification model
// self-enforcing: a misclassified setting fails the build.
func ValidateClassifications(settings []MigratableSetting) error {
	var problems []string
	add := func(key, msg string) {
		problems = append(problems, fmt.Sprintf("%s: %s", key, msg))
	}

	for _, s := range settings {
		c := s.Class

		if c.Category == CategoryUnclassified {
			add(s.Key, "unclassified — Category is the zero value")
			continue
		}
		if s.Description == "" {
			add(s.Key, "empty Description")
		}
		if len(c.Consumers) == 0 {
			add(s.Key, "no Consumers declared")
		}

		switch c.Category {
		case CategoryBootstrap:
			if c.Delivery != nil {
				add(s.Key, "bootstrap setting must not carry a Delivery")
			}
		case CategoryOperational:
			if c.Delivery == nil {
				add(s.Key, "operational setting must carry a Delivery")
				continue
			}
			if c.Delivery.SharedInvariant && !c.Delivery.StampedIntoEnvelope {
				add(s.Key, "SharedInvariant requires StampedIntoEnvelope")
			}
			if c.Delivery.SharedInvariant && !hasWorkerConsumer(c.Consumers) {
				add(s.Key, "SharedInvariant requires at least one worker Consumer")
			}
			if c.Delivery.SharedInvariant && !hasConsumer(c.Consumers, ConsumerMonolith) {
				add(s.Key, "SharedInvariant requires the monolith as a Consumer")
			}
		}

		if c.ValueKind == ValueKindReference && !c.Secret {
			add(s.Key, "ValueKindReference is only valid on a Secret setting")
		}
		if c.Visibility == VisibilityPublic && c.Secret {
			add(s.Key, "a public setting must not be a secret")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("config classification invalid:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

func hasConsumer(cs []Consumer, want Consumer) bool {
	for _, c := range cs {
		if c == want {
			return true
		}
	}
	return false
}

func hasWorkerConsumer(cs []Consumer) bool {
	return hasConsumer(cs, ConsumerWorkerExtractor) || hasConsumer(cs, ConsumerWorkerChunkEmbed)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestValidateClassifications`
Expected: PASS — all eight `TestValidateClassifications_*` tests.

- [ ] **Step 6: Commit**

```bash
git add internal/config/classification_validation.go internal/config/classification_validation_test.go internal/config/migratable_settings.go
git commit -m "feat(config): add classification validation rules"
```

---

### Task 3: The classification registry

**Files:**
- Create: `internal/config/classification_registry.go`
- Test: `internal/config/classification_registry_test.go`

The registry maps every setting key (or key prefix, for repeating provider keys) to a `ConfigClass`. It is the single source of truth for classification.

- [ ] **Step 1: Write the failing test**

Create `internal/config/classification_registry_test.go`:

```go
package config

import "testing"

func TestClassificationFor_KnownBootstrapKey(t *testing.T) {
	c := classificationFor("database.url")
	if c.Category != CategoryBootstrap {
		t.Errorf("database.url Category = %v, want CategoryBootstrap", c.Category)
	}
}

func TestClassificationFor_KnownOperationalKey(t *testing.T) {
	c := classificationFor("websocket.inactivity_timeout_seconds")
	if c.Category != CategoryOperational {
		t.Errorf("websocket.inactivity_timeout_seconds Category = %v, want CategoryOperational", c.Category)
	}
}

func TestClassificationFor_SharedEmbeddingKey(t *testing.T) {
	c := classificationFor("timmy.text_embedding_model")
	if c.Delivery == nil || !c.Delivery.SharedInvariant {
		t.Errorf("timmy.text_embedding_model should be a SharedInvariant setting, got %+v", c.Delivery)
	}
}

func TestClassificationFor_ProviderPrefixKey(t *testing.T) {
	// A repeating OAuth provider key resolves by prefix.
	c := classificationFor("auth.oauth.providers.google.client_secret")
	if !c.Secret {
		t.Error("oauth provider client_secret should be classified Secret")
	}
}

func TestClassificationFor_UnknownKeyIsUnclassified(t *testing.T) {
	c := classificationFor("totally.unknown.key")
	if c.Category != CategoryUnclassified {
		t.Errorf("unknown key Category = %v, want CategoryUnclassified", c.Category)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestClassificationFor`
Expected: FAIL — `undefined: classificationFor`.

- [ ] **Step 3: Write the registry**

Create `internal/config/classification_registry.go`. The registry has an exact-key map and an ordered prefix list (prefixes handle repeating provider keys). Use the existing key set from `migratable_settings.go` as the authoritative list of keys to classify.

```go
package config

import "strings"

// classificationFor returns the ConfigClass for a setting key. It checks the
// exact-match table first, then the ordered prefix table. An unknown key
// returns the zero ConfigClass (CategoryUnclassified), which the validation
// suite rejects.
func classificationFor(key string) ConfigClass {
	if c, ok := exactClassifications[key]; ok {
		return c
	}
	for _, p := range prefixClassifications {
		if strings.HasPrefix(key, p.prefix) {
			return p.class
		}
	}
	return ConfigClass{}
}

type prefixClass struct {
	prefix string
	class  ConfigClass
}

// bootstrapClass is a helper for the common bootstrap shape.
func bootstrapClass(required bool, vis Visibility, secret bool) ConfigClass {
	return ConfigClass{
		Category:   CategoryBootstrap,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Visibility: vis,
		Mutability: MutabilityStatic,
		Consumers:  []Consumer{ConsumerMonolith},
		Required:   required,
	}
}

// operationalClass is a helper for the common monolith-only operational shape.
func operationalClass(vis Visibility, secret bool, consumers ...Consumer) ConfigClass {
	if len(consumers) == 0 {
		consumers = []Consumer{ConsumerMonolith}
	}
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{},
		Visibility: vis,
		Mutability: MutabilityHot,
		Consumers:  consumers,
	}
}

// sharedEmbeddingClass is the SharedInvariant shape for the embedding profile.
func sharedEmbeddingClass(secret bool) ConfigClass {
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     secret,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{StampedIntoEnvelope: true, SharedInvariant: true},
		Visibility: VisibilityAdminOnly,
		Mutability: MutabilityHot,
		Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
	}
}

// exactClassifications maps exact setting keys to their ConfigClass.
// Every key produced by GetMigratableSettings must appear here or match a
// prefix in prefixClassifications, or the validation suite fails.
var exactClassifications = map[string]ConfigClass{
	// --- Bootstrap: server ---
	"server.port":                   bootstrapClass(true, VisibilityInternal, false),
	"server.interface":              bootstrapClass(true, VisibilityInternal, false),
	"server.tls_enabled":            bootstrapClass(false, VisibilityInternal, false),
	"server.tls_subject_name":       bootstrapClass(false, VisibilityInternal, false),
	"server.tls_cert_file":          bootstrapClass(false, VisibilityInternal, false),
	"server.tls_key_file":           bootstrapClass(false, VisibilityInternal, true),
	"server.http_to_https_redirect": bootstrapClass(false, VisibilityInternal, false),
	"server.read_timeout":           bootstrapClass(false, VisibilityInternal, false),
	"server.write_timeout":          bootstrapClass(false, VisibilityInternal, false),
	"server.idle_timeout":           bootstrapClass(false, VisibilityInternal, false),
	"server.base_url":               bootstrapClass(false, VisibilityPublic, false),
	"server.cors.allowed_origins":   bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: database ---
	"database.url": bootstrapClass(true, VisibilityInternal, true),
	"database.connection_pool.max_open_conns":     bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.max_idle_conns":     bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.conn_max_lifetime":  bootstrapClass(false, VisibilityInternal, false),
	"database.connection_pool.conn_max_idle_time": bootstrapClass(false, VisibilityInternal, false),
	"database.redis.url":      bootstrapClass(false, VisibilityInternal, true),
	"database.redis.host":     bootstrapClass(false, VisibilityInternal, false),
	"database.redis.port":     bootstrapClass(false, VisibilityInternal, false),
	"database.redis.password": bootstrapClass(false, VisibilityInternal, true),
	"database.redis.db":       bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: auth (JWT signing, build mode) ---
	"auth.build_mode":          bootstrapClass(true, VisibilityInternal, false),
	"auth.jwt.secret":          bootstrapClass(true, VisibilityInternal, true),
	"auth.jwt.signing_method":  bootstrapClass(false, VisibilityInternal, false),

	// --- Operational: auth (runtime-tunable auth knobs) ---
	"auth.auto_promote_first_user":   operationalClass(VisibilityAdminOnly, false),
	"auth.everyone_is_a_reviewer":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.expiration_seconds":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.refresh_token_days":    operationalClass(VisibilityAdminOnly, false),
	"auth.jwt.session_lifetime_days": operationalClass(VisibilityAdminOnly, false),
	"auth.step_up_window_seconds":    operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.enabled":            operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.domain":             operationalClass(VisibilityAdminOnly, false),
	"auth.cookie.secure":             operationalClass(VisibilityAdminOnly, false),
	"auth.oauth_callback_url":        operationalClass(VisibilityAdminOnly, false),

	// --- Operational: feature flags, runtime, operator ---
	"features.saml_enabled":                operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"websocket.inactivity_timeout_seconds": operationalClass(VisibilityAdminOnly, false),
	"session.timeout_minutes":              operationalClass(VisibilityAdminOnly, false),
	"operator.name":                        operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"operator.contact":                     operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	"administrators":                       operationalClass(VisibilityAdminOnly, false),

	// --- Bootstrap: logging & observability ---
	"logging.level":                       bootstrapClass(false, VisibilityInternal, false),
	"logging.is_dev":                      bootstrapClass(false, VisibilityInternal, false),
	"logging.is_test":                     bootstrapClass(false, VisibilityInternal, false),
	"logging.log_dir":                     bootstrapClass(false, VisibilityInternal, false),
	"logging.max_age_days":                bootstrapClass(false, VisibilityInternal, false),
	"logging.max_size_mb":                 bootstrapClass(false, VisibilityInternal, false),
	"logging.max_backups":                 bootstrapClass(false, VisibilityInternal, false),
	"logging.also_log_to_console":         bootstrapClass(false, VisibilityInternal, false),
	"logging.cloud_error_threshold":       bootstrapClass(false, VisibilityInternal, false),
	"logging.log_api_requests":            bootstrapClass(false, VisibilityInternal, false),
	"logging.log_api_responses":           bootstrapClass(false, VisibilityInternal, false),
	"logging.log_websocket_messages":      bootstrapClass(false, VisibilityInternal, false),
	"logging.redact_auth_tokens":          bootstrapClass(false, VisibilityInternal, false),
	"logging.suppress_unauthenticated_logs": bootstrapClass(false, VisibilityInternal, false),

	// --- Bootstrap: secrets provider ---
	"secrets.provider":           bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_address":      bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_path":         bootstrapClass(false, VisibilityInternal, false),
	"secrets.vault_token":        bootstrapClass(false, VisibilityInternal, true),
	"secrets.aws_region":         bootstrapClass(false, VisibilityInternal, false),
	"secrets.aws_secret_name":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.azure_vault_url":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.gcp_project_id":     bootstrapClass(false, VisibilityInternal, false),
	"secrets.gcp_secret_name":    bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_compartment_id": bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_vault_id":       bootstrapClass(false, VisibilityInternal, false),
	"secrets.oci_secret_name":    bootstrapClass(false, VisibilityInternal, false),

	// --- Shared: embedding profile (text) ---
	"timmy.text_embedding_model":    sharedEmbeddingClass(false),
	"timmy.text_embedding_base_url": sharedEmbeddingClass(false),
	"timmy.text_embedding_api_key":  func() ConfigClass {
		// The API key is a secret; it is NOT stamped into the envelope —
		// it is resolved from a mounted secret. Classified bootstrap.
		return bootstrapClass(false, VisibilityInternal, true)
	}(),
}

// prefixClassifications handles repeating provider keys
// (auth.oauth.providers.*, auth.saml.providers.*, content_oauth.providers.*).
// The list is ordered; the first matching prefix wins.
var prefixClassifications = []prefixClass{
	{
		prefix: "auth.oauth.providers.",
		// OAuth provider config is operational. client_secret keys are also
		// matched here; mark the whole provider subtree Secret-safe by
		// classifying it as a secret-bearing operational group: individual
		// non-secret keys remain operational, and secret keys are masked by
		// the Secret flag on the MigratableSetting itself (kept from the
		// existing helpers). The Class.Secret here is the conservative
		// default; see the note below.
		class: operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "auth.saml.providers.",
		class:  operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "content_oauth.providers.",
		class:  operationalClass(VisibilityAdminOnly, true),
	},
	{
		prefix: "content_extractors.",
		class:  operationalClass(VisibilityAdminOnly, false),
	},
	{
		prefix: "content_sources.",
		class:  operationalClass(VisibilityAdminOnly, false),
	},
	{
		prefix: "timmy.",
		// All remaining timmy.* keys (chunk size, top-k, timeouts) are
		// operational, monolith-relayed to the chunk-embed worker.
		class: ConfigClass{
			Category:   CategoryOperational,
			ValueKind:  ValueKindInline,
			Delivery:   &Delivery{StampedIntoEnvelope: true},
			Visibility: VisibilityAdminOnly,
			Mutability: MutabilityHot,
			Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
		},
	},
}
```

**Important note for the implementer:** the `prefixClassifications` `Secret: true` on the OAuth/SAML prefixes is a *conservative blanket* — it would make the validation suite reject a non-secret provider key under `VisibilityAdminOnly` only if that key were also `VisibilityPublic`, which it is not, so it is safe. But it does interact with the `ValueKindReference` rule. If a provider key needs `ValueKindReference`, that is fine (`Reference ⟹ Secret` holds). The blanket `Secret: true` does NOT incorrectly mask non-secret keys for API display, because API masking reads the per-`MigratableSetting` `Secret` field (set by the existing helpers), not `Class.Secret`. Keep `Class.Secret` and the helper-level `Secret` consistent in Task 4. If a finer split is wanted later, replace the prefix entry with exact keys — but YAGNI for now.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestClassificationFor`
Expected: PASS — all five `TestClassificationFor_*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/classification_registry.go internal/config/classification_registry_test.go
git commit -m "feat(config): add classification registry"
```

---

### Task 4: Attach the registry to every setting + the total-partition test

**Files:**
- Modify: `internal/config/migratable_settings.go`
- Test: `internal/config/classification_test.go` (add to it)

- [ ] **Step 1: Write the failing test**

Append to `internal/config/classification_test.go`:

```go
func TestGetMigratableSettings_EveryKeyClassified(t *testing.T) {
	c := getDefaultConfig()
	settings := c.GetMigratableSettings()
	if len(settings) == 0 {
		t.Fatal("GetMigratableSettings returned no settings")
	}
	for _, s := range settings {
		if s.Class.Category == CategoryUnclassified {
			t.Errorf("setting %q is unclassified — add it to the classification registry", s.Key)
		}
	}
}

func TestGetMigratableSettings_PassesValidationSuite(t *testing.T) {
	c := getDefaultConfig()
	if err := ValidateClassifications(c.GetMigratableSettings()); err != nil {
		t.Fatalf("default config fails classification validation:\n%v", err)
	}
}
```

`getDefaultConfig()` already exists in `config.go` (it is what `Load` starts from).

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestGetMigratableSettings`
Expected: FAIL — every setting has `CategoryUnclassified` because `GetMigratableSettings()` does not yet attach a `Class`.

- [ ] **Step 3: Attach the class in `GetMigratableSettings`**

In `internal/config/migratable_settings.go`, modify `GetMigratableSettings` (lines 27-44) to stamp the class onto every setting after assembling them:

```go
// GetMigratableSettings returns all settings from the config formatted for database storage.
// Secret fields are included with Secret=true so the API layer can mask their values.
// Every setting is stamped with its ConfigClass from the classification registry.
func (c *Config) GetMigratableSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	settings = append(settings, c.getMigratableServerSettings()...)
	settings = append(settings, c.getMigratableDatabaseSettings()...)
	settings = append(settings, c.getMigratableAuthSettings()...)
	settings = append(settings, c.getMigratableFeatureFlags()...)
	settings = append(settings, c.getMigratableOAuthSettings()...)
	settings = append(settings, c.getMigratableSAMLSettings()...)
	settings = append(settings, c.getMigratableRuntimeSettings()...)
	settings = append(settings, c.getMigratableLoggingSettings()...)
	settings = append(settings, c.getMigratableSecretsSettings()...)
	settings = append(settings, c.getMigratableAdministratorsSettings()...)
	settings = append(settings, c.getMigratableTimmySettings()...)

	for i := range settings {
		settings[i].Class = classificationFor(settings[i].Key)
		// Keep the legacy per-setting Secret flag and Class.Secret consistent.
		if settings[i].Class.Secret {
			settings[i].Secret = true
		}
	}

	return settings
}
```

**Note:** the existing `GetMigratableSettings` does NOT currently emit `timmy.*` keys — verify with `rg "getMigratableTimmy" internal/config/`. If `getMigratableTimmySettings` does not exist, create it now so the embedding profile keys (`timmy.text_embedding_model`, `timmy.text_embedding_base_url`, `timmy.text_embedding_api_key`, and the other `timmy.*` operational keys) are emitted. Add this helper to `migratable_settings.go`:

```go
// getMigratableTimmySettings returns Timmy AI assistant settings, including
// the shared embedding profile keys.
func (c *Config) getMigratableTimmySettings() []MigratableSetting {
	t := c.Timmy
	return []MigratableSetting{
		{Key: "timmy.enabled", Value: strconv.FormatBool(t.Enabled), Type: "bool", Description: "Timmy AI assistant enabled", Source: settingSource("TMI_TIMMY_ENABLED")},
		{Key: "timmy.text_embedding_provider", Value: t.TextEmbeddingProvider, Type: "string", Description: "Text embedding provider", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_PROVIDER")},
		{Key: "timmy.text_embedding_model", Value: t.TextEmbeddingModel, Type: "string", Description: "Text embedding model — shared invariant between ingest and query", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_MODEL")},
		{Key: "timmy.text_embedding_base_url", Value: t.TextEmbeddingBaseURL, Type: "string", Description: "Text embedding API base URL — shared invariant", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_BASE_URL")},
		{Key: "timmy.text_embedding_api_key", Value: t.TextEmbeddingAPIKey, Type: "string", Description: "Text embedding API key", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_API_KEY"), Secret: true},
		{Key: "timmy.chunk_size", Value: strconv.Itoa(t.ChunkSize), Type: "int", Description: "Embedding chunk size", Source: settingSource("TMI_TIMMY_CHUNK_SIZE")},
		{Key: "timmy.chunk_overlap", Value: strconv.Itoa(t.ChunkOverlap), Type: "int", Description: "Embedding chunk overlap", Source: settingSource("TMI_TIMMY_CHUNK_OVERLAP")},
	}
}
```

If `getMigratableTimmySettings` (or an equivalently-named helper) already exists, do not duplicate it — instead ensure the three `text_embedding_*` keys are present and add them if missing. Adjust the `timmy.*` registry prefix entry in Task 3 if other `timmy.*` keys are emitted (each emitted key must be classified).

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestGetMigratableSettings`
Expected: PASS — both tests. If `TestGetMigratableSettings_EveryKeyClassified` names an unclassified key, add that key to `exactClassifications` or a prefix in `classification_registry.go` and re-run.

- [ ] **Step 5: Run the full config package test and build**

Run: `make test-unit name=TestGetMigratableSettings count1=true` then `make build-server`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/config/migratable_settings.go internal/config/classification_test.go
git commit -m "feat(config): classify every migratable setting; add total-partition test"
```

---

### Task 5: Delete the infrastructure-key prefix denylist

**Files:**
- Delete: `internal/config/infrastructure_keys.go`
- Delete: `internal/config/infrastructure_keys_test.go`
- Modify: `cmd/dbtool/config.go:34`

- [ ] **Step 1: Replace the caller in `cmd/dbtool/config.go`**

In `cmd/dbtool/config.go`, change the classification loop (lines 33-39):

```go
	for _, s := range allSettings {
		if s.Class.Category == config.CategoryBootstrap {
			infraSettings = append(infraSettings, s)
		} else {
			dbSettings = append(dbSettings, s)
		}
	}
```

- [ ] **Step 2: Delete the old files**

```bash
git rm internal/config/infrastructure_keys.go internal/config/infrastructure_keys_test.go
```

- [ ] **Step 3: Verify no remaining references**

Run: `rg -n "IsInfrastructureKey|infrastructureKey" --type go`
Expected: no output (zero references).

- [ ] **Step 4: Build and test**

Run: `make build-server` then `make build-dbtool` then `make test-unit name=TestConfig count1=true`
Expected: clean build, tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/dbtool/config.go internal/config/infrastructure_keys.go internal/config/infrastructure_keys_test.go
git commit -m "refactor(config): replace IsInfrastructureKey denylist with ConfigClass category"
```

---

### Task 6: The StampedConfig types and provider interface

**Files:**
- Create: `internal/config/stamped_config.go`
- Test: `internal/config/stamped_config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/stamped_config_test.go`:

```go
package config

import (
	"encoding/json"
	"testing"
)

func TestStampedConfig_JSONRoundTrip(t *testing.T) {
	in := StampedConfig{
		Embedding: EmbeddingProfile{
			Model:     "text-embedding-3-large",
			Endpoint:  "https://api.openai.com/v1",
			Dimension: 3072,
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out StampedConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Embedding != in.Embedding {
		t.Errorf("round trip mismatch: got %+v, want %+v", out.Embedding, in.Embedding)
	}
}

func TestEmbeddingProfile_Valid(t *testing.T) {
	good := EmbeddingProfile{Model: "m", Endpoint: "https://e", Dimension: 768}
	if err := good.Validate(); err != nil {
		t.Errorf("valid profile rejected: %v", err)
	}
	bad := EmbeddingProfile{Model: "", Endpoint: "https://e", Dimension: 768}
	if err := bad.Validate(); err == nil {
		t.Error("profile with empty model should be invalid")
	}
	badDim := EmbeddingProfile{Model: "m", Endpoint: "https://e", Dimension: 0}
	if err := badDim.Validate(); err == nil {
		t.Error("profile with zero dimension should be invalid")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestStampedConfig`
Expected: FAIL — `undefined: StampedConfig`.

- [ ] **Step 3: Write the implementation**

Create `internal/config/stamped_config.go`:

```go
package config

import (
	"context"
	"fmt"
)

// EmbeddingProfile is the shared, correctness-invariant embedding configuration.
// The same profile MUST be used to embed documents at ingest (by the
// tmi-chunk-embed worker) and to embed the user's query at search time (by the
// monolith). Disagreement makes vector search silently wrong.
//
// The API key is deliberately NOT part of this struct: it is a secret,
// resolved independently on each side from its own secret source, never
// carried in a job envelope.
type EmbeddingProfile struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Dimension int    `json:"dimension"`
}

// Validate returns an error if the profile is missing a required field.
func (p EmbeddingProfile) Validate() error {
	if p.Model == "" {
		return fmt.Errorf("embedding profile: model is required")
	}
	if p.Endpoint == "" {
		return fmt.Errorf("embedding profile: endpoint is required")
	}
	if p.Dimension <= 0 {
		return fmt.Errorf("embedding profile: dimension must be positive, got %d", p.Dimension)
	}
	return nil
}

// StampedConfig is the subset of operational configuration the monolith stamps
// into every job envelope. It carries only non-secret values; secrets are
// resolved from mounted secret sources by each consumer.
type StampedConfig struct {
	Embedding EmbeddingProfile `json:"embedding"`
}

// StampedConfigProvider is the single read point for stamped configuration.
// Both the monolith's job-envelope builder and the monolith's own Timmy query
// path read through this interface, which is what makes the shared-invariant
// guarantee structural rather than a matter of discipline.
type StampedConfigProvider interface {
	Get(ctx context.Context) (StampedConfig, error)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestStampedConfig` then `make test-unit name=TestEmbeddingProfile`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/stamped_config.go internal/config/stamped_config_test.go
git commit -m "feat(config): add StampedConfig types and provider interface"
```

---

### Task 7: The concrete StampedConfigProvider over SettingsService

**Files:**
- Create: `api/stamped_config_provider.go`
- Test: `api/stamped_config_provider_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/stamped_config_provider_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

// fakeStringGetter implements the minimal settings-read surface the provider needs.
type fakeStringGetter struct {
	vals map[string]string
}

func (f fakeStringGetter) GetString(ctx context.Context, key string) (string, error) {
	return f.vals[key], nil
}
func (f fakeStringGetter) GetInt(ctx context.Context, key string) (int, error) {
	switch f.vals[key] {
	case "768":
		return 768, nil
	case "3072":
		return 3072, nil
	}
	return 0, nil
}

func TestStampedConfigProvider_Get(t *testing.T) {
	g := fakeStringGetter{vals: map[string]string{
		"timmy.text_embedding_model":    "text-embedding-3-large",
		"timmy.text_embedding_base_url": "https://api.openai.com/v1",
		"timmy.embedding_dimension":     "3072",
	}}
	p := NewStampedConfigProvider(g)
	sc, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := config.EmbeddingProfile{
		Model:     "text-embedding-3-large",
		Endpoint:  "https://api.openai.com/v1",
		Dimension: 3072,
	}
	if sc.Embedding != want {
		t.Errorf("Get() embedding = %+v, want %+v", sc.Embedding, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestStampedConfigProvider`
Expected: FAIL — `undefined: NewStampedConfigProvider`.

- [ ] **Step 3: Write the implementation**

Create `api/stamped_config_provider.go`:

```go
package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
)

// settingsReader is the minimal read surface NewStampedConfigProvider needs.
// *SettingsService satisfies it.
type settingsReader interface {
	GetString(ctx context.Context, key string) (string, error)
	GetInt(ctx context.Context, key string) (int, error)
}

// stampedConfigProvider reads stamped configuration from the DB-backed
// settings service. It is the concrete config.StampedConfigProvider.
type stampedConfigProvider struct {
	settings settingsReader
}

// NewStampedConfigProvider builds a config.StampedConfigProvider that reads
// through the given settings reader (normally *SettingsService).
func NewStampedConfigProvider(settings settingsReader) config.StampedConfigProvider {
	return &stampedConfigProvider{settings: settings}
}

// Get assembles the current StampedConfig from the settings service. It is the
// single read point for stamped configuration in the monolith.
func (p *stampedConfigProvider) Get(ctx context.Context) (config.StampedConfig, error) {
	model, err := p.settings.GetString(ctx, "timmy.text_embedding_model")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding model: %w", err)
	}
	endpoint, err := p.settings.GetString(ctx, "timmy.text_embedding_base_url")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding endpoint: %w", err)
	}
	dim, err := p.settings.GetInt(ctx, "timmy.embedding_dimension")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding dimension: %w", err)
	}
	return config.StampedConfig{
		Embedding: config.EmbeddingProfile{
			Model:     model,
			Endpoint:  endpoint,
			Dimension: dim,
		},
	}, nil
}
```

**Note:** the test reads `timmy.embedding_dimension`. Add that key to `getMigratableTimmySettings` in Task 4's helper and to the classification registry as a `sharedEmbeddingClass(false)` exact key. If the dimension is not currently a config field, add `EmbeddingDimension int` to `TimmyConfig` in `internal/config/timmy.go` with `yaml:"embedding_dimension" env:"TMI_TIMMY_EMBEDDING_DIMENSION"` and a sensible default (e.g. `0` meaning "auto-detect"); the validation in `EmbeddingProfile.Validate()` will catch a misconfigured `0` at use time. Update Task 4's helper to emit `timmy.embedding_dimension` and Task 3's `exactClassifications` to include it.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestStampedConfigProvider`
Expected: PASS.

- [ ] **Step 5: Build**

Run: `make build-server`
Expected: clean build.

- [ ] **Step 6: Commit**

```bash
git add api/stamped_config_provider.go api/stamped_config_provider_test.go internal/config/timmy.go internal/config/migratable_settings.go internal/config/classification_registry.go
git commit -m "feat(config): add SettingsService-backed StampedConfigProvider"
```

---

### Task 8: Route the monolith's Timmy query embedding through StampedConfigProvider

**Files:**
- Modify: `api/timmy_session_manager.go` (the `searchIndexRaw` function, around line 864-900)
- Test: `api/timmy_session_manager_test.go` (add a test, or create if absent)

This task makes the shared-invariant guarantee structural: the monolith's query path reads the embedding model from the same `StampedConfigProvider` the envelope builder uses.

- [ ] **Step 1: Write the failing test**

Add to `api/timmy_session_manager_test.go` (create the file if it does not exist, with `package api`):

```go
func TestSearchIndexRaw_UsesStampedEmbeddingModel(t *testing.T) {
	// The expected model for an index lookup must come from the stamped
	// config provider, not directly from TimmyConfig — so that ingest and
	// query cannot disagree.
	g := fakeStringGetter{vals: map[string]string{
		"timmy.text_embedding_model":    "stamped-model",
		"timmy.text_embedding_base_url": "https://e",
		"timmy.embedding_dimension":     "768",
	}}
	p := NewStampedConfigProvider(g)
	sc, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("provider Get: %v", err)
	}
	if sc.Embedding.Model != "stamped-model" {
		t.Fatalf("provider returned model %q, want %q", sc.Embedding.Model, "stamped-model")
	}
	// The TimmySessionManager must expose a way to obtain the stamped model.
	// expectedEmbeddingModel returns the model the query path will use.
	sm := &TimmySessionManager{stampedConfig: p}
	got, err := sm.expectedEmbeddingModel(context.Background(), IndexTypeText)
	if err != nil {
		t.Fatalf("expectedEmbeddingModel: %v", err)
	}
	if got != "stamped-model" {
		t.Errorf("expectedEmbeddingModel = %q, want %q", got, "stamped-model")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSearchIndexRaw_UsesStampedEmbeddingModel`
Expected: FAIL — `TimmySessionManager` has no `stampedConfig` field and no `expectedEmbeddingModel` method.

- [ ] **Step 3: Add the field, the accessor, and rewire `searchIndexRaw`**

In `api/timmy_session_manager.go`:

1. Add a field to the `TimmySessionManager` struct:

```go
	// stampedConfig is the single read point for the shared embedding profile.
	// The query path reads the embedding model through it so that ingest and
	// query cannot diverge.
	stampedConfig config.StampedConfigProvider
```

(ensure `internal/config` is imported)

2. Add the accessor:

```go
// expectedEmbeddingModel returns the embedding model the query path must use
// for the given index type, read through the stamped config provider.
func (sm *TimmySessionManager) expectedEmbeddingModel(ctx context.Context, indexType string) (string, error) {
	if sm.stampedConfig == nil {
		// Fallback for code paths that have not been wired with a provider
		// (e.g. some unit tests): use the static config.
		if indexType == IndexTypeCode {
			return sm.config.CodeEmbeddingModel, nil
		}
		return sm.config.TextEmbeddingModel, nil
	}
	sc, err := sm.stampedConfig.Get(ctx)
	if err != nil {
		return "", err
	}
	return sc.Embedding.Model, nil
}
```

3. In `searchIndexRaw` (lines ~881-883), replace:

```go
	expectedModel := sm.config.TextEmbeddingModel
	if indexType == IndexTypeCode {
		expectedModel = sm.config.CodeEmbeddingModel
	}
```

with:

```go
	expectedModel, err := sm.expectedEmbeddingModel(ctx, indexType)
	if err != nil {
		logger.Warn("Failed to read stamped embedding model for %s search: %v", indexType, err)
		return nil
	}
```

(the `err` variable already exists in scope from the `EmbedTexts` call above; if Go complains about `:=` redeclaration, use `=` and a fresh declaration as needed — verify by reading the surrounding function).

4. Wire `stampedConfig` wherever `TimmySessionManager` is constructed. Find the constructor with `rg -n "TimmySessionManager{" api/` and pass the provider built in `cmd/server/main.go` after `settingsService` is created (around `main.go:601`):

```go
	stampedConfigProvider := api.NewStampedConfigProvider(settingsService)
```

Thread it into the session-manager constructor call. If the constructor signature must change, update all callers.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSearchIndexRaw_UsesStampedEmbeddingModel`
Expected: PASS.

- [ ] **Step 5: Build and run the Timmy unit tests**

Run: `make build-server` then `make test-unit name=TestTimmy count1=true`
Expected: clean build; existing Timmy tests still pass.

- [ ] **Step 6: Commit**

```bash
git add api/timmy_session_manager.go api/timmy_session_manager_test.go cmd/server/main.go
git commit -m "feat(timmy): read query embedding model through StampedConfigProvider"
```

---

### Task 9: The worker bootstrap package

**Files:**
- Create: `internal/config/bootstrap/bootstrap.go`
- Test: `internal/config/bootstrap/bootstrap_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/bootstrap/bootstrap_test.go`:

```go
package bootstrap

import (
	"strings"
	"testing"
)

func TestLoadWorker_Success(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "nats://localhost:4222")
	t.Setenv("TMI_WORKER_HEARTBEAT_SUBJECT", "workers.heartbeat.probe")
	t.Setenv("TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY", "/var/run/secrets/embedding/key")
	t.Setenv("TMI_WORKER_LOG_LEVEL", "debug")

	wb, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if wb.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL = %q", wb.NATSURL)
	}
	if wb.HeartbeatSubject != "workers.heartbeat.probe" {
		t.Errorf("HeartbeatSubject = %q", wb.HeartbeatSubject)
	}
	if wb.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", wb.LogLevel)
	}
	if got := wb.SecretMounts["embedding-api-key"]; got != "/var/run/secrets/embedding/key" {
		t.Errorf("SecretMounts[embedding-api-key] = %q", got)
	}
}

func TestLoadWorker_MissingNATSURLFails(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "TMI_WORKER_NATS_URL") {
		t.Fatalf("want missing-NATS-URL error, got %v", err)
	}
}

func TestLoadWorker_LogLevelDefaults(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "nats://localhost:4222")
	t.Setenv("TMI_WORKER_HEARTBEAT_SUBJECT", "workers.heartbeat.probe")
	t.Setenv("TMI_WORKER_LOG_LEVEL", "")
	wb, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if wb.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want %q", wb.LogLevel, "info")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestLoadWorker`
Expected: FAIL — package `bootstrap` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/config/bootstrap/bootstrap.go`:

```go
// Package bootstrap provides the minimal, environment-only configuration a
// TMI worker component needs to start and reach the point where it can accept
// a job. It is deliberately separate from internal/config: a worker MUST NOT
// import the monolith's configuration cascade. Everything else a worker needs
// arrives in the job envelope (config.StampedConfig) or is resolved from a
// mounted secret.
package bootstrap

import (
	"fmt"
	"os"
	"strings"
)

// WorkerBootstrap is the complete startup configuration of a worker.
type WorkerBootstrap struct {
	// NATSURL is the JetStream connection URL. Required — a worker cannot
	// receive a job without it.
	NATSURL string
	// HeartbeatSubject is the NATS subject the worker publishes liveness on.
	HeartbeatSubject string
	// SecretMounts maps a logical secret name to the filesystem path of a
	// mounted Kubernetes Secret. The worker reads secret values from these
	// paths; secret values never travel over NATS or the config cascade.
	SecretMounts map[string]string
	// LogLevel is the worker log level; defaults to "info".
	LogLevel string
}

// secretMountEnvPrefix is the env-var prefix for a mounted-secret path.
// TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY -> SecretMounts["embedding-api-key"].
const secretMountEnvPrefix = "TMI_WORKER_SECRET_MOUNT_"

// LoadWorker builds a WorkerBootstrap from environment variables only.
// It reads no YAML and touches no database.
func LoadWorker() (*WorkerBootstrap, error) {
	natsURL := os.Getenv("TMI_WORKER_NATS_URL")
	if natsURL == "" {
		return nil, fmt.Errorf("worker bootstrap: required env var TMI_WORKER_NATS_URL is not set")
	}

	logLevel := os.Getenv("TMI_WORKER_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	wb := &WorkerBootstrap{
		NATSURL:          natsURL,
		HeartbeatSubject: os.Getenv("TMI_WORKER_HEARTBEAT_SUBJECT"),
		LogLevel:         logLevel,
		SecretMounts:     map[string]string{},
	}

	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(name, secretMountEnvPrefix) || value == "" {
			continue
		}
		logical := strings.ToLower(strings.TrimPrefix(name, secretMountEnvPrefix))
		logical = strings.ReplaceAll(logical, "_", "-")
		wb.SecretMounts[logical] = value
	}

	return wb, nil
}

// ReadSecret reads the secret value for a logical name from its mounted path.
func (wb *WorkerBootstrap) ReadSecret(logicalName string) (string, error) {
	path, ok := wb.SecretMounts[logicalName]
	if !ok {
		return "", fmt.Errorf("worker bootstrap: no secret mount for %q", logicalName)
	}
	b, err := os.ReadFile(path) //nolint:gosec // path comes from operator-controlled env
	if err != nil {
		return "", fmt.Errorf("worker bootstrap: read secret %q: %w", logicalName, err)
	}
	return strings.TrimSpace(string(b)), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestLoadWorker`
Expected: PASS — all three tests.

- [ ] **Step 5: Add a `ReadSecret` test**

Append to `internal/config/bootstrap/bootstrap_test.go`:

```go
func TestReadSecret(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/key"
	if err := os.WriteFile(p, []byte("  s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wb := &WorkerBootstrap{SecretMounts: map[string]string{"embedding-api-key": p}}
	got, err := wb.ReadSecret("embedding-api-key")
	if err != nil {
		t.Fatalf("ReadSecret: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("ReadSecret = %q, want %q", got, "s3cr3t")
	}
	if _, err := wb.ReadSecret("missing"); err == nil {
		t.Error("ReadSecret for an unmounted name should error")
	}
}
```

Add `"os"` to the test file imports.

Run: `make test-unit name=TestReadSecret`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/bootstrap/
git commit -m "feat(config): add env-only worker bootstrap package"
```

---

### Task 10: The worker-probe stub binary

**Files:**
- Create: `cmd/worker-probe/main.go`
- Modify: `scripts/build-server.py` (add the `worker-probe` component)
- Modify: `Makefile` (add `build-worker-probe`)

The probe connects to NATS, publishes a heartbeat, consumes one job envelope, deserializes the `StampedConfig`, resolves a mounted secret, and echoes a result. Reuse `internal/worker` for the NATS connection.

- [ ] **Step 1: Inspect the reusable worker primitives**

Run: `rg -n "^func " internal/worker/nats.go internal/worker/heartbeat.go internal/worker/consumer.go`
Read the constructor for the NATS `Conn`, the heartbeat publisher, and the consumer. The probe uses these — do not reimplement NATS plumbing.

- [ ] **Step 2: Write the probe**

Create `cmd/worker-probe/main.go`. The exact NATS calls depend on the `internal/worker` API surface read in Step 1; the structure is fixed:

```go
// Command worker-probe is a stub TMI worker. It exists to prove the worker
// bootstrap + job-envelope + secret-mount contract end to end before #347's
// real workers depend on it. It is a test fixture, not production code.
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/config/bootstrap"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
)

// jobEnvelope is the subset of the #347 job envelope the probe needs: the
// stamped config block. #347 owns the full schema.
type jobEnvelope struct {
	JobID  string              `json:"job_id"`
	Config config.StampedConfig `json:"config"`
}

// probeResult is what the probe echoes back, proving each contract leg.
type probeResult struct {
	JobID             string `json:"job_id"`
	BootstrapOK       bool   `json:"bootstrap_ok"`
	StampedConfigSeen bool   `json:"stamped_config_seen"`
	SecretResolved    bool   `json:"secret_resolved"`
}

func main() {
	logger := slogging.Get()

	wb, err := bootstrap.LoadWorker()
	if err != nil {
		logger.Error("worker-probe: bootstrap failed: %v", err)
		os.Exit(1)
	}
	logger.Info("worker-probe: bootstrap OK (nats=%s)", wb.NATSURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to NATS using the internal/worker primitives.
	conn, err := worker.Connect(ctx, wb.NATSURL) // adjust to the actual constructor name from Step 1
	if err != nil {
		logger.Error("worker-probe: NATS connect failed: %v", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Publish a heartbeat on the bootstrap-supplied subject.
	if wb.HeartbeatSubject != "" {
		if err := conn.Publish(wb.HeartbeatSubject, []byte("worker-probe alive")); err != nil {
			logger.Warn("worker-probe: heartbeat publish failed: %v", err)
		}
	}

	// Consume exactly one probe job from "jobs.probe".
	msg, err := conn.NextOnSubject(ctx, "jobs.probe") // adjust to the actual API from Step 1
	if err != nil {
		logger.Error("worker-probe: did not receive a probe job: %v", err)
		os.Exit(1)
	}

	var env jobEnvelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		logger.Error("worker-probe: bad job envelope: %v", err)
		os.Exit(1)
	}

	res := probeResult{
		JobID:             env.JobID,
		BootstrapOK:       true,
		StampedConfigSeen: env.Config.Embedding.Model != "",
	}

	// Resolve the embedding API key from its mounted secret.
	if _, secErr := wb.ReadSecret("embedding-api-key"); secErr == nil {
		res.SecretResolved = true
	} else {
		logger.Warn("worker-probe: secret resolution failed: %v", secErr)
	}

	out, _ := json.Marshal(res)
	if err := conn.Publish("jobs.result."+env.JobID, out); err != nil {
		logger.Error("worker-probe: result publish failed: %v", err)
		os.Exit(1)
	}
	logger.Info("worker-probe: contract proven: %s", string(out))
}
```

**Implementer note:** the calls `worker.Connect`, `conn.Close`, `conn.Publish`, `conn.NextOnSubject` are placeholders for the *actual* `internal/worker` API found in Step 1. Replace each with the real function/method name and signature. If `internal/worker` has no simple "get next message on a subject" helper, use a JetStream pull consumer per `internal/worker/consumer.go`, or a plain `nats.go` subscription with a channel — the probe needs exactly one message. Do not add new exported helpers to `internal/worker` unless #347's design calls for them; a local `nats.go` subscription inside `cmd/worker-probe` is acceptable for a test fixture.

- [ ] **Step 3: Register the build component**

In `scripts/build-server.py`, add to the `COMPONENTS` dict:

```python
    "worker-probe": {
        "output": "bin/worker-probe",
        "package": "github.com/ericfitz/tmi/cmd/worker-probe",
        "tags": [],
        "ldflags": False,
    },
```

- [ ] **Step 4: Add the Makefile target**

In the `Makefile`, after `build-dbtool-oci` (around line 91), add:

```makefile
build-worker-probe:  ## Build the worker-probe stub (proves the #415 worker bootstrap contract)
	@uv run scripts/build-server.py --component worker-probe
```

- [ ] **Step 5: Build the probe**

Run: `make build-worker-probe`
Expected: `bin/worker-probe` produced, clean build. Fix compile errors against the real `internal/worker` API.

- [ ] **Step 6: Commit**

```bash
git add cmd/worker-probe/main.go scripts/build-server.py Makefile
git commit -m "feat(worker): add worker-probe stub proving the bootstrap contract"
```

---

### Task 11: The worker-probe integration test

**Files:**
- Create: `test/integration/worker_probe_integration_test.go`

This test runs the probe against a real NATS server and a monolith-side envelope builder. NATS runs as a GitHub Actions `services:` container in CI; locally it requires a NATS server.

- [ ] **Step 1: Confirm how integration tests reach NATS**

Run: `rg -n "nats" internal/worker/*_integration_test.go internal/worker/pipeline_integration_test.go`
Read how `pipeline_integration_test.go` obtains a NATS server URL (env var, testcontainers, or a `services:` container). Mirror that exact mechanism.

- [ ] **Step 2: Write the integration test**

Create `test/integration/worker_probe_integration_test.go`. The test function name MUST end in `_Integration` (project rule) and the file MUST end in `_integration_test.go`:

```go
package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/config"
)

// TestWorkerProbe_ContractEndToEnd_Integration runs the worker-probe stub
// against a real NATS server, publishes a job envelope carrying a StampedConfig
// and a mounted secret, and asserts the probe proves every leg of the
// worker bootstrap contract.
func TestWorkerProbe_ContractEndToEnd_Integration(t *testing.T) {
	natsURL := os.Getenv("TMI_TEST_NATS_URL")
	if natsURL == "" {
		t.Skip("TMI_TEST_NATS_URL not set; skipping worker-probe integration test")
	}

	// 1. Prepare a mounted-secret file.
	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, "embedding-key")
	if err := os.WriteFile(secretPath, []byte("integration-test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Build the probe binary.
	probeBin := filepath.Join(t.TempDir(), "worker-probe")
	build := exec.Command("go", "build", "-o", probeBin, "github.com/ericfitz/tmi/cmd/worker-probe")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build worker-probe: %v", err)
	}

	// 3. Start the probe with a worker bootstrap environment.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, probeBin)
	probe.Env = append(os.Environ(),
		"TMI_WORKER_NATS_URL="+natsURL,
		"TMI_WORKER_HEARTBEAT_SUBJECT=workers.heartbeat.probe",
		"TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY="+secretPath,
	)
	probe.Stdout = os.Stdout
	probe.Stderr = os.Stderr
	if err := probe.Start(); err != nil {
		t.Fatalf("start worker-probe: %v", err)
	}
	defer func() { _ = probe.Process.Kill() }()

	// 4. Connect a monolith-side publisher and subscriber to NATS, then
	//    publish a job envelope on jobs.probe and await jobs.result.<id>.
	//    Use the same NATS client mechanism confirmed in Step 1.
	jobID := "probe-job-1"
	env := struct {
		JobID  string               `json:"job_id"`
		Config config.StampedConfig `json:"config"`
	}{
		JobID: jobID,
		Config: config.StampedConfig{
			Embedding: config.EmbeddingProfile{
				Model: "text-embedding-3-large", Endpoint: "https://e", Dimension: 3072,
			},
		},
	}
	envBytes, _ := json.Marshal(env)

	resultCh := publishProbeJobAndAwaitResult(t, natsURL, "jobs.probe", "jobs.result."+jobID, envBytes)

	select {
	case raw := <-resultCh:
		var res struct {
			BootstrapOK       bool `json:"bootstrap_ok"`
			StampedConfigSeen bool `json:"stamped_config_seen"`
			SecretResolved    bool `json:"secret_resolved"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatalf("bad probe result: %v", err)
		}
		if !res.BootstrapOK {
			t.Error("probe: bootstrap_ok = false")
		}
		if !res.StampedConfigSeen {
			t.Error("probe: stamped_config_seen = false — envelope-carried config not received")
		}
		if !res.SecretResolved {
			t.Error("probe: secret_resolved = false — mounted secret not read")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for probe result")
	}
}
```

- [ ] **Step 3: Implement the NATS publish/await helper**

In the same file, add `publishProbeJobAndAwaitResult` using the NATS client mechanism confirmed in Step 1 (plain `nats.go` connection is fine here — the test is the monolith side of the contract):

```go
// publishProbeJobAndAwaitResult connects to NATS, subscribes to resultSubject,
// publishes envelope on jobSubject, and returns a channel that yields the
// first result message. Implemented with github.com/nats-io/nats.go.
func publishProbeJobAndAwaitResult(t *testing.T, natsURL, jobSubject, resultSubject string, envelope []byte) <-chan []byte {
	t.Helper()
	// Implement with nats.Connect(natsURL); subscribe to resultSubject into a
	// buffered channel; give the subscription a moment to register; publish
	// the envelope on jobSubject; return the channel. Close the connection
	// via t.Cleanup. See internal/worker for the exact import if a wrapper
	// is preferred.
	ch := make(chan []byte, 1)
	// ... nats.go implementation ...
	return ch
}
```

Write the real `nats.go` body — `nats.Connect`, `nc.ChanSubscribe` or `nc.Subscribe` with a handler that pushes to `ch`, a 200ms sleep for subscription registration, then `nc.Publish(jobSubject, envelope)`, and `t.Cleanup(func(){ nc.Close() })`.

- [ ] **Step 4: Run the integration test**

Run: `make test-integration` (NATS must be reachable; if `TMI_TEST_NATS_URL` is unset the test skips cleanly).
Expected: PASS, or a clean SKIP if NATS is unavailable locally. Verify it does not FAIL.

- [ ] **Step 5: Commit**

```bash
git add test/integration/worker_probe_integration_test.go
git commit -m "test(worker): add worker-probe contract integration test"
```

---

### Task 12: Bootstrap-isolation enforcement in SettingsService

**Files:**
- Modify: `api/settings_service.go` (the `Get` method, lines 133+)
- Test: `api/settings_service_test.go` (add a test)

The settings service must refuse to serve a `CategoryBootstrap` key from the database — bootstrap config is file/env only.

- [ ] **Step 1: Write the failing test**

Add to `api/settings_service_test.go`:

```go
func TestSettingsService_RefusesBootstrapKeyFromDB(t *testing.T) {
	// A CategoryBootstrap key must never be served from the DB path.
	svc := NewSettingsService(nil, nil) // no DB, no Redis — exercises the guard
	_, err := svc.Get(context.Background(), "database.url")
	if err == nil {
		t.Fatal("Get(database.url) should refuse: bootstrap keys are not DB-served")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSettingsService_RefusesBootstrapKeyFromDB`
Expected: FAIL — `Get` does not yet guard bootstrap keys (it will likely panic on a nil DB or return a different error).

- [ ] **Step 3: Add the guard**

In `api/settings_service.go`, at the top of `Get` (after the logger line, line ~134), add:

```go
	// Bootstrap settings are file/env only and must never be served from the
	// database. classificationFor returns CategoryBootstrap for them.
	if config.ClassificationCategoryFor(key) == config.CategoryBootstrap {
		return nil, fmt.Errorf("setting %q is a bootstrap key: read it from config/env, not the database", key)
	}
```

`classificationFor` is unexported. Add an exported wrapper to `internal/config/classification_registry.go`:

```go
// ClassificationCategoryFor returns the Category of a setting key. It is the
// exported entry point for callers outside the config package that need to
// know whether a key is bootstrap or operational.
func ClassificationCategoryFor(key string) Category {
	return classificationFor(key).Category
}
```

Ensure `api/settings_service.go` imports `github.com/ericfitz/tmi/internal/config`.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSettingsService_RefusesBootstrapKeyFromDB`
Expected: PASS.

- [ ] **Step 5: Run the full settings-service tests**

Run: `make test-unit name=TestSettingsService count1=true`
Expected: PASS. If an existing test reads a bootstrap key through `Get`, that test was relying on wrong behavior — change it to read an operational key, or to read the bootstrap key through the config provider instead. Document the change in the commit.

- [ ] **Step 6: Commit**

```bash
git add api/settings_service.go api/settings_service_test.go internal/config/classification_registry.go
git commit -m "feat(config): SettingsService refuses to serve bootstrap keys from the DB"
```

---

### Task 13: Operational-config DB seeding

**Files:**
- Modify: `api/models/system_setting.go` (the `DefaultSystemSettings()` function, line 46) OR a new seed source — see Step 1.
- Test: `api/settings_service_test.go` or `api/models/system_setting_test.go`

The operational config that previously lived in `config-*.yml` must seed into the DB on first run.

- [ ] **Step 1: Read the current seeding path**

Run: `rg -n "DefaultSystemSettings" api/` and read `api/models/system_setting.go:43-46+` and `api/settings_service.go:381-396` (`SeedDefaults`).
Decide: `DefaultSystemSettings()` returns `[]SystemSetting` seeded by `SeedDefaults`. The cleanest extension is to make `DefaultSystemSettings()` derive its rows from the classification registry — every `CategoryOperational` setting with a non-empty default becomes a seed row. This keeps one source of truth.

- [ ] **Step 2: Write the failing test**

Add to `api/models/system_setting_test.go` (create if absent, `package models`):

```go
func TestDefaultSystemSettings_IncludesOperationalDefaults(t *testing.T) {
	defaults := DefaultSystemSettings()
	if len(defaults) == 0 {
		t.Fatal("DefaultSystemSettings returned nothing")
	}
	// A representative operational key must be present.
	found := false
	for _, s := range defaults {
		if string(s.SettingKey) == "websocket.inactivity_timeout_seconds" {
			found = true
		}
		// No bootstrap key may be a default seed row.
		if string(s.SettingKey) == "database.url" {
			t.Errorf("bootstrap key %q must not be a default seed row", s.SettingKey)
		}
	}
	if !found {
		t.Error("expected websocket.inactivity_timeout_seconds among default settings")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `make test-unit name=TestDefaultSystemSettings_IncludesOperationalDefaults`
Expected: FAIL if the current `DefaultSystemSettings()` does not include that key. (If it already does, adjust the assertion to a key that is genuinely new — pick any `CategoryOperational` key not currently seeded.)

- [ ] **Step 4: Derive operational seed rows from a default config**

`DefaultSystemSettings()` is in package `models`, which cannot import `internal/config` (check for an import cycle with `go list`). If a cycle exists, place the derivation in `internal/config` instead: add `func DefaultOperationalSettings() []MigratableSetting` to `internal/config/migratable_settings.go` that calls `getDefaultConfig().GetMigratableSettings()` and filters to `Class.Category == CategoryOperational`. Then have `SeedDefaults` (in `api/settings_service.go`) consume `config.DefaultOperationalSettings()` and convert each to a `models.SystemSetting`, in addition to whatever `DefaultSystemSettings()` already returns.

Concretely, modify `SeedDefaults` in `api/settings_service.go` to also seed operational config:

```go
// SeedDefaults seeds default system settings, including operational config
// derived from the classification registry. Bootstrap keys are never seeded.
func (s *SettingsService) SeedDefaults(ctx context.Context) error {
	defaults := models.DefaultSystemSettings()

	for _, ms := range config.DefaultOperationalSettings() {
		if ms.Value == "" {
			continue // nothing to seed for an empty default
		}
		desc := ms.Description
		defaults = append(defaults, models.SystemSetting{
			SettingKey:  models.DBVarchar(ms.Key),
			Value:       models.DBText(ms.Value),
			SettingType: models.DBVarchar(ms.Type),
			Description: models.NewNullableDBText(&desc),
			ModifiedAt:  time.Now(),
		})
	}

	// ... existing SeedDefaults body that writes `defaults` idempotently ...
}
```

Add `DefaultOperationalSettings` to `internal/config/migratable_settings.go`:

```go
// DefaultOperationalSettings returns the operational-category settings from a
// default Config. It is the seed source for the DB-backed settings service.
func DefaultOperationalSettings() []MigratableSetting {
	all := getDefaultConfig().GetMigratableSettings()
	out := make([]MigratableSetting, 0, len(all))
	for _, s := range all {
		if s.Class.Category == CategoryOperational {
			out = append(out, s)
		}
	}
	return out
}
```

Read the existing `SeedDefaults` body before editing and preserve its idempotency logic (skip-if-exists). Do not seed a row for an existing key.

- [ ] **Step 5: Run tests**

Run: `make test-unit name=TestDefaultSystemSettings count1=true` then `make test-unit name=TestSettingsService count1=true` then `make build-server`
Expected: PASS and clean build.

- [ ] **Step 6: Oracle review checkpoint**

This task changes DB seeding. Before committing, invoke the `oracle-db-admin` skill and dispatch the subagent to review the seeding change (new rows into `system_settings`, no schema change expected). Address any BLOCKING findings.

- [ ] **Step 7: Commit**

```bash
git add api/settings_service.go internal/config/migratable_settings.go api/models/system_setting.go api/models/system_setting_test.go
git commit -m "feat(config): seed operational config into the DB from the classification registry"
```

---

### Task 14: Generated config-example.yml + the config-*.yml collapse

**Files:**
- Create: `internal/config/example_gen.go`
- Create: `cmd/genconfig/main.go`
- Modify: `scripts/build-server.py`, `Makefile`
- Test: `internal/config/example_gen_test.go`
- Modify: `config-development.yml`, `config-test.yml`, `config-production.yml`
- Delete: the six per-backend config files

- [ ] **Step 1: Write the failing test**

Create `internal/config/example_gen_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestGenerateExampleConfig_ContainsBootstrapKeys(t *testing.T) {
	out, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	s := string(out)
	for _, want := range []string{"database:", "server:", "logging:"} {
		if !strings.Contains(s, want) {
			t.Errorf("generated example missing %q", want)
		}
	}
}

func TestGenerateExampleConfig_OmitsOperationalKeys(t *testing.T) {
	out, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	// Operational config does not belong in the bootstrap example file.
	if strings.Contains(string(out), "inactivity_timeout_seconds") {
		t.Error("generated example should not contain operational keys")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestGenerateExampleConfig`
Expected: FAIL — `undefined: GenerateExampleConfig`.

- [ ] **Step 3: Write the generator**

Create `internal/config/example_gen.go`:

```go
package config

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GenerateExampleConfig produces the contents of config-example.yml from the
// classification registry. Only CategoryBootstrap keys are included — the
// example file documents the bootstrap-only config, since operational config
// lives in the database. Secret values are emitted as reference placeholders.
func GenerateExampleConfig() ([]byte, error) {
	all := getDefaultConfig().GetMigratableSettings()

	bootstrap := make([]MigratableSetting, 0, len(all))
	for _, s := range all {
		if s.Class.Category == CategoryBootstrap {
			bootstrap = append(bootstrap, s)
		}
	}
	sort.Slice(bootstrap, func(i, j int) bool { return bootstrap[i].Key < bootstrap[j].Key })

	root := map[string]any{}
	for _, s := range bootstrap {
		value := any(s.Value)
		if s.Class.Secret {
			value = "vault://replace-me/" + strings.ReplaceAll(s.Key, ".", "/")
		}
		setNested(root, strings.Split(s.Key, "."), value)
	}

	header := fmt.Sprintf(
		"# TMI Example Configuration (bootstrap keys only)\n"+
			"# GENERATED by `make generate-config-example` on %s — do not edit by hand.\n"+
			"# Operational and shared configuration lives in the database settings service.\n"+
			"# Secret values are shown as vault:// reference placeholders.\n\n",
		time.Now().UTC().Format(time.RFC3339))

	body, err := yaml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal example config: %w", err)
	}
	return append([]byte(header), body...), nil
}

// setNested writes value into a nested map following the dotted key path.
func setNested(root map[string]any, path []string, value any) {
	cur := root
	for i, part := range path {
		if i == len(path)-1 {
			cur[part] = value
			return
		}
		next, ok := cur[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestGenerateExampleConfig`
Expected: PASS.

- [ ] **Step 5: Write the genconfig binary and wire the make target**

Create `cmd/genconfig/main.go`:

```go
// Command genconfig writes config-example.yml from the classification registry.
package main

import (
	"os"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

func main() {
	logger := slogging.Get()
	out, err := config.GenerateExampleConfig()
	if err != nil {
		logger.Error("genconfig: %v", err)
		os.Exit(1)
	}
	if err := os.WriteFile("config-example.yml", out, 0o644); err != nil { //nolint:gosec // example file, not secret
		logger.Error("genconfig: write config-example.yml: %v", err)
		os.Exit(1)
	}
	logger.Info("genconfig: wrote config-example.yml")
}
```

In `scripts/build-server.py` `COMPONENTS`, add:

```python
    "genconfig": {
        "output": "bin/genconfig",
        "package": "github.com/ericfitz/tmi/cmd/genconfig",
        "tags": [],
        "ldflags": False,
    },
```

In the `Makefile`, add:

```makefile
build-genconfig:  ## Build the config-example.yml generator
	@uv run scripts/build-server.py --component genconfig

generate-config-example: build-genconfig  ## Regenerate config-example.yml from the classification registry
	@./bin/genconfig
```

- [ ] **Step 6: Regenerate config-example.yml and add a drift test**

Run: `make generate-config-example`

Append to `internal/config/example_gen_test.go`:

```go
func TestConfigExampleFile_MatchesRegistry(t *testing.T) {
	generated, err := GenerateExampleConfig()
	if err != nil {
		t.Fatalf("GenerateExampleConfig: %v", err)
	}
	onDisk, err := os.ReadFile("../../config-example.yml")
	if err != nil {
		t.Fatalf("read config-example.yml: %v", err)
	}
	// Compare ignoring the generated timestamp line.
	strip := func(b []byte) string {
		var keep []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "GENERATED by") {
				continue
			}
			keep = append(keep, line)
		}
		return strings.Join(keep, "\n")
	}
	if strip(generated) != strip(onDisk) {
		t.Error("config-example.yml is stale — run `make generate-config-example`")
	}
}
```

Add `"os"` to the test imports. Run: `make test-unit name=TestConfigExampleFile_MatchesRegistry` — Expected: PASS.

- [ ] **Step 7: Collapse the config files**

For `config-development.yml`, `config-test.yml`, `config-production.yml`: remove every block whose keys are `CategoryOperational` (the OAuth `providers:` subtree, `saml:`, `content_sources:`, `content_extractors:`, `content_oauth:`, `timmy:`, `operator:`, `administrators:`, `webhooks:`, `websocket:`). Keep only `server:`, `database:`, `logging:`, `observability:`, `secrets:`, and the bootstrap `auth:` keys (`build_mode`, `jwt.secret`, `jwt.signing_method`). Use `config-development.yml` as the worked example; `config-test.yml` and `config-production.yml` follow the same shape. In `config-production.yml`, replace secret values with `vault://` reference placeholders.

Delete the per-backend files:

```bash
git rm config-development-sqlite.yml config-development-mysql.yml config-development-sqlserver.yml config-development-oci.yml config-test-integration-pg.yml config-test-integration-oci.yml
```

- [ ] **Step 8: Repoint anything that referenced the deleted files**

Run: `rg -n "config-development-sqlite|config-development-mysql|config-development-sqlserver|config-development-oci|config-test-integration-pg|config-test-integration-oci"`
For each hit — Makefile targets, `scripts/*.py`, test setup — change it to use `config-development.yml` / `config-test.yml` plus a `TMI_DATABASE_URL` environment override for the backend. In particular, update `scripts/run-integration-tests.py` so `--target pg` and `--target oci` set `TMI_DATABASE_URL` instead of selecting a file.

- [ ] **Step 9: Update the yaml validation fixture test**

`auth/yaml_validation_fixture_test.go` lists config files. Run `rg -n "config-" auth/yaml_validation_fixture_test.go` and update the fixture list to the three remaining files.

- [ ] **Step 10: Build and full unit test**

Run: `make build-server` then `make build-genconfig` then `make build-worker-probe` then `make test-unit count1=true`
Expected: clean builds; all unit tests pass. Fix fallout (tests that loaded a deleted config file).

- [ ] **Step 11: Commit**

```bash
git add internal/config/example_gen.go internal/config/example_gen_test.go cmd/genconfig/ scripts/build-server.py Makefile config-example.yml config-development.yml config-test.yml config-production.yml auth/yaml_validation_fixture_test.go scripts/run-integration-tests.py
git add -u
git commit -m "refactor(config): collapse config-*.yml to 3 bootstrap-only files; generate config-example.yml"
```

---

### Task 15: Legacy config import (dbtool) and the load-time drift warning

**Files:**
- Create: `cmd/dbtool/config_legacy.go`
- Modify: `cmd/dbtool/main.go`
- Modify: `internal/config/config.go` (the `Load` function — add the drift warning)
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test for the drift warning**

The existing `Load` does not warn on operational keys in YAML. Add a function `OperationalKeysInFile(path string) ([]string, error)` and test it. Add to `internal/config/config_test.go`:

```go
func TestOperationalKeysInFile_DetectsDrift(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/c.yml"
	// websocket.inactivity_timeout_seconds is an operational key.
	yaml := "server:\n  port: \"8080\"\nwebsocket:\n  inactivity_timeout_seconds: 300\n"
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := OperationalKeysInFile(p)
	if err != nil {
		t.Fatalf("OperationalKeysInFile: %v", err)
	}
	found := false
	for _, k := range keys {
		if k == "websocket.inactivity_timeout_seconds" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected websocket.inactivity_timeout_seconds in drift list, got %v", keys)
	}
}
```

Add `"os"` to the test imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestOperationalKeysInFile_DetectsDrift`
Expected: FAIL — `undefined: OperationalKeysInFile`.

- [ ] **Step 3: Implement `OperationalKeysInFile` and the warning**

Add to `internal/config/config.go`:

```go
// OperationalKeysInFile returns the operational-category setting keys present
// in a YAML config file. Operational config belongs in the database; finding
// it in a file indicates drift during the bootstrap-only migration.
func OperationalKeysInFile(path string) ([]string, error) {
	raw := map[string]any{}
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	var found []string
	walkYAMLKeys(raw, "", func(dotted string) {
		if classificationFor(dotted).Category == CategoryOperational {
			found = append(found, dotted)
		}
	})
	return found, nil
}

// walkYAMLKeys invokes fn with the dotted path of every leaf key in m.
func walkYAMLKeys(m map[string]any, prefix string, fn func(string)) {
	for k, v := range m {
		dotted := k
		if prefix != "" {
			dotted = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			walkYAMLKeys(child, dotted, fn)
		} else {
			fn(dotted)
		}
	}
}
```

Ensure `config.go` imports `gopkg.in/yaml.v3` and `os` (it almost certainly already does — verify).

In the `Load` function, after `loadFromYAML` succeeds (inside the `if configFile != ""` block), add the drift warning:

```go
		if opKeys, dErr := OperationalKeysInFile(configFile); dErr == nil && len(opKeys) > 0 {
			slogging.Get().Warn(
				"config %s contains %d operational keys that should live in the database settings service: %v — see #415; this will become an error in a future release",
				configFile, len(opKeys), opKeys)
		}
```

Ensure `config.go` imports `github.com/ericfitz/tmi/internal/slogging`. If importing `slogging` into `config` creates an import cycle, instead return the drift list from `Load` via a side channel the caller logs, or have `Load` accept the warning being printed by `cmd/server/main.go` after `Load` returns by calling `OperationalKeysInFile` there. Prefer the `slogging` import; only fall back if `go build` reports a cycle.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestOperationalKeysInFile_DetectsDrift`
Expected: PASS.

- [ ] **Step 5: Write the legacy importer**

Create `cmd/dbtool/config_legacy.go`. It reuses `runConfigSeed`'s machinery but is named for the migration intent — import operational keys from a legacy file into the settings table:

```go
package main

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runLegacyConfigImport imports the operational-category settings from a
// legacy config-*.yml into the database settings table. Bootstrap keys are
// left in the file. It is the one-time migration path for the #415
// bootstrap-only config collapse.
func runLegacyConfigImport(db *testdb.TestDB, inputFile string, overwrite, dryRun bool) error {
	log := slogging.Get()

	opKeys, err := config.OperationalKeysInFile(inputFile)
	if err != nil {
		return fmt.Errorf("scan legacy config %s: %w", inputFile, err)
	}
	log.Info("Legacy config %s contains %d operational keys to import", inputFile, len(opKeys))

	// runConfigSeed already splits bootstrap vs operational by Class.Category
	// and writes operational settings to the DB. Reuse it: outputFile "" lets
	// it derive a migrated bootstrap-only file path.
	return runConfigSeed(db, inputFile, "", overwrite, dryRun)
}
```

- [ ] **Step 6: Register the subcommand in `cmd/dbtool/main.go`**

In `cmd/dbtool/main.go`, add a flag near the others (lines 27-34):

```go
	importLegacy := flag.Bool("import-legacy", false, "Import operational settings from a legacy config file into the database")
	flag.BoolVar(importLegacy, "l", false, "Import operational settings from a legacy config file (short)")
```

Add `*importLegacy` to the `boolCount` call (line 93):

```go
	opCount := boolCount(*schema, *importConfig, *importTestData, *importLegacy)
```

Add a dispatch case in the `switch` (after the `*importTestData` case):

```go
	case *importLegacy:
		if *inputFile == "" {
			runErr = fmt.Errorf("--input-file / -f is required for --import-legacy")
		} else {
			if !*dryRun {
				if migrateErr := ensureSchema(db); migrateErr != nil {
					log.Warn("Schema migration skipped or failed: %v", migrateErr)
				}
			}
			runErr = runLegacyConfigImport(db, *inputFile, *overwrite, *dryRun)
		}
```

- [ ] **Step 7: Build and test**

Run: `make build-dbtool` then `make build-server` then `make test-unit name=TestConfig count1=true`
Expected: clean builds; tests pass.

- [ ] **Step 8: Oracle review checkpoint**

`runLegacyConfigImport` writes to `system_settings`. Invoke the `oracle-db-admin` skill and dispatch the subagent to review the import path. Address BLOCKING findings.

- [ ] **Step 9: Commit**

```bash
git add cmd/dbtool/config_legacy.go cmd/dbtool/main.go internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add dbtool config import-legacy and load-time drift warning"
```

---

### Task 16: API visibility wiring (public /config and admin /admin/settings)

**Files:**
- Modify: `api/config_handlers.go` — `GetClientConfig` and `ListSystemSettings`
- Test: `api/config_handlers_test.go`

The public `/config` endpoint must return exactly `VisibilityPublic` settings and no secrets; `/admin/settings` returns `VisibilityAdminOnly ∪ VisibilityPublic`.

- [ ] **Step 1: Read the current handlers**

Read `api/config_handlers.go` — `GetClientConfig` (the public `/config`) and `ListSystemSettings` (admin). Note how each currently selects which settings to return.

- [ ] **Step 2: Write the failing test**

Add to `api/config_handlers_test.go`:

```go
func TestVisibilityFilter_PublicExcludesSecretsAndNonPublic(t *testing.T) {
	settings := []config.MigratableSetting{
		{Key: "operator.name", Value: "Acme", Class: config.ClassificationFor("operator.name")},
		{Key: "auth.jwt.secret", Value: "shh", Class: config.ClassificationFor("auth.jwt.secret")},
		{Key: "websocket.inactivity_timeout_seconds", Value: "300", Class: config.ClassificationFor("websocket.inactivity_timeout_seconds")},
	}
	pub := filterByVisibility(settings, config.VisibilityPublic)
	for _, s := range pub {
		if s.Class.Visibility != config.VisibilityPublic {
			t.Errorf("public filter leaked non-public key %q", s.Key)
		}
		if s.Class.Secret {
			t.Errorf("public filter leaked secret key %q", s.Key)
		}
	}
	// operator.name is public; it must be present.
	if len(pub) == 0 {
		t.Error("public filter dropped operator.name")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `make test-unit name=TestVisibilityFilter`
Expected: FAIL — `undefined: filterByVisibility` and `config.ClassificationFor`.

- [ ] **Step 4: Add the exported `ClassificationFor` and the filter**

In `internal/config/classification_registry.go`, add:

```go
// ClassificationFor returns the full ConfigClass for a setting key. It is the
// exported entry point for callers outside the config package.
func ClassificationFor(key string) ConfigClass {
	return classificationFor(key)
}
```

In `api/config_handlers.go`, add:

```go
// filterByVisibility returns the settings whose visibility is at least the
// requested level. A public request returns only VisibilityPublic settings;
// an admin request returns VisibilityAdminOnly and VisibilityPublic.
// Secret settings are never returned for a public request.
func filterByVisibility(settings []config.MigratableSetting, level config.Visibility) []config.MigratableSetting {
	out := make([]config.MigratableSetting, 0, len(settings))
	for _, s := range settings {
		switch level {
		case config.VisibilityPublic:
			if s.Class.Visibility == config.VisibilityPublic && !s.Class.Secret {
				out = append(out, s)
			}
		case config.VisibilityAdminOnly:
			if s.Class.Visibility == config.VisibilityAdminOnly || s.Class.Visibility == config.VisibilityPublic {
				out = append(out, s)
			}
		}
	}
	return out
}
```

- [ ] **Step 5: Apply the filter in the handlers**

In `GetClientConfig`, ensure the response is built from `filterByVisibility(allSettings, config.VisibilityPublic)`. In `ListSystemSettings`, ensure the admin response uses `filterByVisibility(allSettings, config.VisibilityAdminOnly)` (admin still masks `Secret` values per existing behavior — keep that masking). Read the current handler bodies and apply the filter at the point where they assemble the response set; do not change the response JSON shape, only which settings populate it. If `GetClientConfig` currently hand-picks fields (operator info, SAML flag), keep that shape but verify every field it exposes maps to a `VisibilityPublic` key — if one does not, either reclassify the key (and re-run the validation suite) or stop exposing it.

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit name=TestVisibilityFilter` then `make test-unit name=TestConfig count1=true` then `make test-unit name=TestGetClientConfig count1=true`
Expected: PASS.

- [ ] **Step 7: Build**

Run: `make build-server`
Expected: clean build.

- [ ] **Step 8: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go internal/config/classification_registry.go
git commit -m "feat(config): drive /config and /admin/settings visibility from the classification registry"
```

---

### Task 17: Full verification, OpenAPI check, and issue closure

**Files:** none new — verification and cleanup.

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: clean. Fix every issue. `make lint` includes `make check-unsafe-union-methods`; this change touches none, so that check is incidental.

- [ ] **Step 2: Build everything**

Run: `make build-server` then `make build-dbtool` then `make build-worker-probe` then `make build-genconfig`
Expected: all four binaries build clean.

- [ ] **Step 3: Full unit test suite**

Run: `make test-unit count1=true`
Expected: all pass. The classification validation suite (`TestGetMigratableSettings_PassesValidationSuite`, `TestGetMigratableSettings_EveryKeyClassified`) is the key gate — it must pass.

- [ ] **Step 4: Integration tests**

Run: `make test-integration`
Expected: pass (the worker-probe test SKIPs cleanly if NATS is unavailable; it must not FAIL). If `TMI_TEST_NATS_URL` can be set locally, set it and confirm `TestWorkerProbe_ContractEndToEnd_Integration` passes rather than skips.

- [ ] **Step 5: OpenAPI check**

This change did not alter `api-schema/tmi-openapi.json` — the `/config` and `/admin/settings` endpoints keep their existing request/response shapes (only the *contents* of the settings list changed, not the schema). Confirm:

Run: `git status api-schema/tmi-openapi.json`
Expected: unmodified. If it is modified, run `make validate-openapi` and `make generate-api`, then rebuild and re-test.

- [ ] **Step 6: Security regression scan**

This change touches secret handling (the `ValueKind`/`Secret` classification, the public-endpoint visibility filter) and config loading. Invoke the `security-regression` skill and the `security-review` skill per the project completion workflow. If either reports an issue, stop and report it to the user before proceeding.

- [ ] **Step 7: Final Oracle review**

Invoke the `oracle-db-admin` skill once more for a whole-change review covering the cumulative DB-touching surface (operational seeding in Task 13, legacy import in Task 15). Confirm the verdict and address any BLOCKING items.

- [ ] **Step 8: Update the design spec status**

In `docs/superpowers/specs/2026-05-18-config-three-category-model-design.md`, change the `Status:` line from `Design — approved in brainstorming, pending written-spec review` to `Implemented`.

```bash
git add docs/superpowers/specs/2026-05-18-config-three-category-model-design.md
git commit -m "docs(config): mark #415 design as implemented"
```

- [ ] **Step 9: Push to dev/1.4.0**

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin/dev/1.4.0"
```

- [ ] **Step 10: Close issue #415**

The work landed on `dev/1.4.0`, so `Closes #415` does not auto-close. Close it manually:

```bash
gh issue comment 415 --repo ericfitz/tmi --body "Implemented on dev/1.4.0. Three-category config classification model with a self-enforcing validation suite, envelope-stamped shared config (StampedConfigProvider), worker bootstrap package + worker-probe contract proof, and config-*.yml collapsed from 10 to 3 bootstrap-only files. Unblocks #347."
gh issue close 415 --repo ericfitz/tmi --reason completed
```

---

## Self-Review

**Spec coverage check** — every spec section maps to a task:

| Spec section | Task(s) |
|---|---|
| Classification properties (`Category`, `Secret`, `ValueKind`, `Delivery`, `Visibility`, `Mutability`, `Consumers`, `Required`) | 1 |
| `MigratableSetting` gains `Class` | 2 |
| Validation suite (total/disjoint, all cross-property rules) | 2, 4 |
| Classification registry (every key classified) | 3, 4 |
| `infrastructure_keys.go` deleted | 5 |
| `StampedConfig` / `EmbeddingProfile` / `StampedConfigProvider` | 6 |
| Concrete provider over `SettingsService` | 7 |
| Shared-invariant guarantee (Timmy query path through the same accessor) | 8 |
| Worker bootstrap package (`internal/config/bootstrap`, env-only) | 9 |
| `cmd/worker-probe` stub | 10 |
| Probe integration test | 11 |
| Bootstrap isolation in `SettingsService` | 12 |
| Operational config → DB seed | 13 |
| `config-*.yml` 10→3 collapse, generated `config-example.yml` | 14 |
| Legacy migration (`dbtool config import-legacy`, drift warning) | 15 |
| API visibility (public `/config`, admin `/admin/settings`) | 16 |
| Oracle review, OpenAPI check, lint/build/test gates, issue closure | 13, 15, 17 |

No spec section is unaddressed.

**Placeholder scan** — the plan contains two deliberate, bounded "implement against the real API" notes (Task 10 Step 2 and Task 11 Step 3) for the NATS client calls, because the exact `internal/worker` surface must be read at implementation time; both are scoped with a concrete fallback (a plain `nats.go` subscription) and a preceding inspection step. These are not open-ended placeholders. Every code-producing step shows complete code.

**Type consistency** — `ConfigClass`, `Category`, `ValueKind`, `Visibility`, `Mutability`, `Consumer`, `Delivery`, `MigratableSetting.Class`, `EmbeddingProfile`, `StampedConfig`, `StampedConfigProvider`, `WorkerBootstrap`, `classificationFor`/`ClassificationFor`/`ClassificationCategoryFor`, `ValidateClassifications`, `filterByVisibility`, `DefaultOperationalSettings`, `OperationalKeysInFile` are named consistently across all tasks. `classificationFor` (unexported) is used inside package `config`; `ClassificationFor` and `ClassificationCategoryFor` (exported) are used from package `api`. `getMigratableTimmySettings` is introduced in Task 4 and reused in Task 7. The `timmy.embedding_dimension` key is introduced in Task 7 and must be added to both the Task 4 helper and the Task 3 registry at that point — Task 7's note states this explicitly.
