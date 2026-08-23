# Config Registry (Phase A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make one registry the single authoritative declaration of every TMI configuration setting, with guardrails that make an unclassified setting impossible rather than merely detectable.

**Architecture:** A new `SettingDef` list in `internal/config` declares every setting exactly once — key, classification, type, default, description, and (for settings still delivered by file/env) the yaml path, env var, and a typed accessor. Existing consumers stop holding their own parallel lists: `GetMigratableSettings()` becomes a projection over the registry, `DefaultSystemSettings()` becomes a projection over its operational entries, and `genconfig`/`genconfigdocs` read the registry instead of the `Config` struct. Phase A changes **no runtime behavior** — it is a consolidation whose correctness is proven by equivalence tests.

**Tech Stack:** Go 1.26 (go.mod pin), testify, reflection in tests only.

**Spec:** `docs/superpowers/specs/2026-08-22-config-model-redesign-design.md`

## Global Constraints

- **No behavior change in Phase A.** Every existing test must pass unmodified. The only intended user-visible change is that `rate_limit.requests_per_minute` and `rate_limit.requests_per_hour` become classified (Task 5), which fixes #809.
- **No schema migration.** No new `system_settings` column, per the spec's non-goals.
- **Operational settings keep their config/env paths in Phase A.** They are marked `Transitional: true` and removed in Phase E. A validation rule that forbade them outright would fail the whole build on day one.
- **Reflection is permitted in tests only**, never in production code paths.
- **Logging:** `github.com/ericfitz/tmi/internal/slogging` only. Never the standard `log` package, never `fmt.Println`.
- **Never log setting values**, only keys and outcomes (`feedback_never_log_values`; the #794 leak).
- **Test command is `make test-unit`**, never `go test` directly. Single test: `make test-unit name=TestName`.
- **Lint gate:** `make lint` must report 0 issues before every commit.
- **Go style:** `gofmt`; imports grouped stdlib / external / internal; exported symbols get godoc comments.
- **SEM markers:** every new or behavior-changed function gets a `SEM@` marker line. Run `/sem-annotate --update <files>` *after* the code commit (`sem blame` cannot anchor a file not yet in HEAD).

---

## Execution deviations

This section records where the executed implementation deliberately departed from
the plan text below. The task text past this point is kept as originally written
for historical continuity; where it conflicts with what was actually built, this
section is authoritative. The full set of execution rulings (nineteen of them) is
recorded in the session ledger; the ledger is git-ignored scratch, so this section
is the durable record of the ones that matter to a future reader.

1. **Emission stayed conditional, not unconditional.** Task 7 (below) called for
   `GetMigratableSettings` to emit every declared key, including ones with empty
   values, instead of omitting them. That would have broken two consumers:
   `api/settings_service.go`'s `SeedDefaults` iterates
   `config.DefaultOperationalSettings()` (itself derived from
   `GetMigratableSettings()`), so unconditional emission would seed a new row into
   `system_settings` on every fresh database; and
   `internal/dbschema/system_setting_origin_backfill.go:86` uses the same set to
   decide seeded-vs-explicit, which drives env-vs-database precedence on
   **existing** databases. Instead, the conditionality was encoded explicitly as
   `SettingDef.OmitWhenEmpty` (set on 45 defs), with `server.tls_cert_file` and
   `server.tls_key_file` special-cased because their original guard tested
   `server.tls_enabled` — a different field than the one being emitted. This is
   pinned by `TestDefaultOperationalSettings_MatchesPreRegistryBaseline`.

2. **`SeedableOperationalDefs()` filters on an explicit `Seeded` flag**, not on
   "every operational def" as the plan assumed. Returning every operational def
   would have seeded roughly 110 rows instead of the 9 that exist today. The
   seeded set cannot be inferred from any other property on a def —
   `session.timeout_minutes` and `features.saml_enabled` are both seeded *and*
   config-delivered — so an explicit flag is the only rule that reproduces
   today's set.

3. **Task 3 and Task 4 were executed together, not sequentially.** Task 3's
   bijection test walks every env-tagged `Config` field, which includes the
   operational fields Task 4 declares, so Task 3's acceptance test could not pass
   on its own until Task 4's work was also done.

4. **The Task 6 coverage test keys on `YAMLPath` as well as `Key`, and uses no
   allowlist.** Matching only on `Key` made correctly-declared defs whose `Key`
   deliberately differs from their `YAMLPath` (the rename cases, e.g.
   `features.saml_enabled` / `auth.saml.enabled`) look uncovered. See
   `internal/config/registry_coverage_test.go`.

---

### Task 1: The `SettingDef` type and registry container

**Files:**
- Create: `internal/config/setting_def.go`
- Test: `internal/config/setting_def_test.go`

**Interfaces:**
- Consumes: `ConfigClass`, `Category`, `Visibility`, `Mutability`, `Consumer`, `Delivery` from `internal/config/classification.go` (unchanged).
- Produces: `type SettingDef struct{...}`; `func AllSettingDefs() []SettingDef`; `func DefFor(key string) (SettingDef, bool)`; package-level `settingDefs []SettingDef` (empty in this task, populated in Tasks 3-5).

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefFor_ReturnsRegisteredDef(t *testing.T) {
	defs := []SettingDef{
		{
			Key:         "test.example",
			Type:        "string",
			Description: "an example",
			Class: ConfigClass{
				Category:   CategoryBootstrap,
				Visibility: VisibilityInternal,
				Consumers:  []Consumer{ConsumerMonolith},
			},
			YAMLPath: "test.example",
			EnvVar:   "TMI_TEST_EXAMPLE",
			Get:      func(c *Config) string { return "v" },
		},
	}
	idx := indexDefs(defs)

	got, ok := idx["test.example"]
	require.True(t, ok)
	assert.Equal(t, "string", got.Type)
	assert.Equal(t, CategoryBootstrap, got.Class.Category)
}

func TestDefFor_UnknownKeyReturnsFalse(t *testing.T) {
	_, ok := DefFor("no.such.key.anywhere")
	assert.False(t, ok)
}

func TestAllSettingDefs_ReturnsACopy(t *testing.T) {
	a := AllSettingDefs()
	require.NotNil(t, a)
	if len(a) == 0 {
		t.Skip("registry not yet populated; covered from Task 3 onward")
	}
	a[0].Key = "mutated"
	b := AllSettingDefs()
	assert.NotEqual(t, "mutated", b[0].Key, "AllSettingDefs must not expose the backing array")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestDefFor`
Expected: FAIL — `undefined: SettingDef`, `undefined: indexDefs`, `undefined: DefFor`.

- [ ] **Step 3: Write minimal implementation**

```go
package config

// SettingDef is the single authoritative declaration of one configuration
// setting. Every setting the server can read — bootstrap or operational,
// config-delivered or database-seeded — has exactly one SettingDef.
//
// Which fields are legal depends on Class.Category; ValidateSettingDefs
// enforces that (see setting_def_validation.go).
type SettingDef struct {
	// Key is the canonical dotted setting key, e.g. "server.port".
	Key string
	// Class is the full classification: category, visibility, secrecy,
	// mutability, consumers, delivery.
	Class ConfigClass
	// Type is the value's canonical type: "string", "bool", "int",
	// "float", or "json".
	Type string
	// Description is human-readable and required; it feeds generated docs
	// and the admin API.
	Description string
	// Default is the canonical string form of the compiled-in default.
	// Required for operational settings, which have no config file to
	// fall back to.
	Default string

	// YAMLPath, EnvVar and Get are populated only for settings currently
	// delivered by config file or environment: all bootstrap settings, plus
	// operational settings still in transition (see Transitional).
	YAMLPath string
	EnvVar   string
	// Get extracts this setting's current value from a loaded Config as a
	// canonical string. Nil for settings with no config-file path.
	Get func(*Config) string

	// Transitional marks an operational setting that still has a config/env
	// delivery path during the Phase A-E cutover described in the spec.
	// Every Transitional entry is scheduled for removal in Phase E; the
	// ratchet test in Task 9 prevents new ones from being added.
	Transitional bool
}

// IsSecret reports whether this setting's value must never appear in an API
// response or a log line.
//
// This answers the question only for STATICALLY DECLARED settings. The
// per-provider keys under auth.oauth.providers., auth.saml.providers. and
// content_oauth.providers. are generated per configured provider and have no
// SettingDef; for those, Class.Secret is deliberately false (a blanket true
// would mis-mask non-secret sub-keys like .client_id) and secrecy is carried
// only by the per-setting Secret flag that the provider helpers set on
// .client_secret, .sp_private_key and .idp_metadata_b64xml. Never reach for
// this method to decide secrecy for a provider key — use
// MigratableSetting.IsSecret(), which ORs both flags.
func (d SettingDef) IsSecret() bool {
	return d.Class.Secret
}

// settingDefs is the authoritative registry. Populated in Tasks 3-5.
var settingDefs []SettingDef

// settingDefIndex is the by-key lookup, built once from settingDefs.
var settingDefIndex = indexDefs(settingDefs)

// indexDefs builds a by-key lookup map from a slice of definitions.
func indexDefs(defs []SettingDef) map[string]SettingDef {
	idx := make(map[string]SettingDef, len(defs))
	for _, d := range defs {
		idx[d.Key] = d
	}
	return idx
}

// DefFor returns the declaration for a setting key, and whether one exists.
func DefFor(key string) (SettingDef, bool) {
	d, ok := settingDefIndex[key]
	return d, ok
}

// AllSettingDefs returns a copy of the registry, so callers cannot mutate it.
func AllSettingDefs() []SettingDef {
	out := make([]SettingDef, len(settingDefs))
	copy(out, settingDefs)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestDefFor`
Then: `make test-unit name=TestAllSettingDefs`
Expected: PASS (the third test skips while the registry is empty).

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/setting_def.go internal/config/setting_def_test.go
git commit -m "feat(config): add SettingDef, the single authoritative setting declaration"
```

---

### Task 2: Validation rules over `SettingDef`

**Files:**
- Create: `internal/config/setting_def_validation.go`
- Test: `internal/config/setting_def_validation_test.go`
- Reference (do not modify yet): `internal/config/classification_validation.go`

**Interfaces:**
- Consumes: `SettingDef`, `AllSettingDefs()` from Task 1.
- Produces: `func ValidateSettingDefs(defs []SettingDef) error` — returns an error naming every problem found, one per line, or nil.

The rules below preserve every rule `ValidateClassifications` already enforces, and add the category-legality rules the spec requires. Port the existing rules rather than reinventing them: read `internal/config/classification_validation.go:13-89` and carry each one across.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validBootstrapDef() SettingDef {
	return SettingDef{
		Key:         "server.port",
		Type:        "string",
		Description: "HTTP server port",
		Class: ConfigClass{
			Category:   CategoryBootstrap,
			Visibility: VisibilityInternal,
			Mutability: MutabilityStatic,
			Consumers:  []Consumer{ConsumerMonolith},
		},
		YAMLPath: "server.port",
		EnvVar:   "TMI_SERVER_PORT",
		Get:      func(c *Config) string { return c.Server.Port },
	}
}

func validOperationalDef() SettingDef {
	return SettingDef{
		Key:         "ui.default_theme",
		Type:        "string",
		Description: "Default UI theme",
		Default:     "auto",
		Class: ConfigClass{
			Category:   CategoryOperational,
			Visibility: VisibilityPublic,
			Mutability: MutabilityHot,
			Delivery:   &Delivery{},
			Consumers:  []Consumer{ConsumerMonolith, ConsumerTMIUX},
		},
	}
}

func TestValidateSettingDefs_AcceptsValidSet(t *testing.T) {
	err := ValidateSettingDefs([]SettingDef{validBootstrapDef(), validOperationalDef()})
	assert.NoError(t, err)
}

func TestValidateSettingDefs_RejectsUnclassified(t *testing.T) {
	d := validBootstrapDef()
	d.Class.Category = CategoryUnclassified
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclassified")
}

func TestValidateSettingDefs_RejectsDuplicateKeys(t *testing.T) {
	err := ValidateSettingDefs([]SettingDef{validBootstrapDef(), validBootstrapDef()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValidateSettingDefs_BootstrapRequiresEnvVarAndYAMLPath(t *testing.T) {
	d := validBootstrapDef()
	d.EnvVar = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EnvVar")

	d = validBootstrapDef()
	d.YAMLPath = ""
	err = ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "YAMLPath")
}

func TestValidateSettingDefs_BootstrapRequiresGetter(t *testing.T) {
	d := validBootstrapDef()
	d.Get = nil
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Get")
}

func TestValidateSettingDefs_BootstrapMustNotCarryDelivery(t *testing.T) {
	d := validBootstrapDef()
	d.Class.Delivery = &Delivery{}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Delivery")
}

func TestValidateSettingDefs_BootstrapMustNotBeTransitional(t *testing.T) {
	d := validBootstrapDef()
	d.Transitional = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Transitional")
}

func TestValidateSettingDefs_OperationalRequiresDefault(t *testing.T) {
	d := validOperationalDef()
	d.Default = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Default")
}

func TestValidateSettingDefs_NonTransitionalOperationalMustHaveNoConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.EnvVar = "TMI_UI_DEFAULT_THEME"
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-transitional")
}

func TestValidateSettingDefs_TransitionalOperationalRequiresConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transitional")
}

func TestValidateSettingDefs_TransitionalOperationalAcceptsConfigPath(t *testing.T) {
	d := validOperationalDef()
	d.Transitional = true
	d.EnvVar = "TMI_UI_DEFAULT_THEME"
	d.YAMLPath = "ui.default_theme"
	d.Get = func(c *Config) string { return "auto" }
	assert.NoError(t, ValidateSettingDefs([]SettingDef{d}))
}

func TestValidateSettingDefs_RejectsUnknownType(t *testing.T) {
	d := validBootstrapDef()
	d.Type = "widget"
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type")
}

func TestValidateSettingDefs_RequiresDescriptionAndConsumers(t *testing.T) {
	d := validBootstrapDef()
	d.Description = ""
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Description")

	d = validBootstrapDef()
	d.Class.Consumers = nil
	err = ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Consumers")
}

func TestValidateSettingDefs_PublicCannotBeSecret(t *testing.T) {
	d := validOperationalDef()
	d.Class.Secret = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "public")
}

func TestValidateSettingDefs_RequiredImpliesBootstrap(t *testing.T) {
	d := validOperationalDef()
	d.Class.Required = true
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Required")
}

func TestValidateSettingDefs_SharedInvariantImpliesStamped(t *testing.T) {
	d := validOperationalDef()
	d.Class.Delivery = &Delivery{SharedInvariant: true}
	d.Class.Consumers = []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed}
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StampedIntoEnvelope")
}

func TestValidateSettingDefs_ReferenceImpliesSecret(t *testing.T) {
	d := validBootstrapDef()
	d.Class.ValueKind = ValueKindReference
	d.Class.Secret = false
	err := ValidateSettingDefs([]SettingDef{d})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference")
}

func TestValidateSettingDefs_RegistryItselfIsValid(t *testing.T) {
	assert.NoError(t, ValidateSettingDefs(AllSettingDefs()))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestValidateSettingDefs`
Expected: FAIL — `undefined: ValidateSettingDefs`.

- [ ] **Step 3: Write minimal implementation**

```go
package config

import (
	"fmt"
	"strings"
)

// validSettingTypes is the closed set of canonical value types.
var validSettingTypes = map[string]bool{
	"string": true,
	"bool":   true,
	"int":    true,
	"float":  true,
	"json":   true,
}

// ValidateSettingDefs checks the registry's internal consistency and returns
// an error naming every problem found. It is the enforcement point for the
// rule that every setting must be classified: a definition whose Category is
// the zero value is rejected.
func ValidateSettingDefs(defs []SettingDef) error {
	var problems []string
	add := func(key, msg string) {
		problems = append(problems, fmt.Sprintf("%s: %s", key, msg))
	}

	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Key == "" {
			problems = append(problems, "(empty key): a definition has no Key")
			continue
		}
		if seen[d.Key] {
			add(d.Key, "duplicate key in registry")
			continue
		}
		seen[d.Key] = true

		c := d.Class

		if c.Category == CategoryUnclassified {
			add(d.Key, "unclassified — Category is the zero value")
			continue
		}
		if d.Description == "" {
			add(d.Key, "empty Description")
		}
		if len(c.Consumers) == 0 {
			add(d.Key, "no Consumers declared")
		}
		if !validSettingTypes[d.Type] {
			add(d.Key, fmt.Sprintf("unknown type %q", d.Type))
		}
		if c.ValueKind == ValueKindReference && !c.Secret {
			add(d.Key, "ValueKindReference is only valid on a Secret setting")
		}
		if c.Visibility == VisibilityPublic && c.Secret {
			add(d.Key, "a public setting must not be Secret")
		}
		if c.Required && c.Category != CategoryBootstrap {
			add(d.Key, "Required implies bootstrap")
		}

		hasConfigPath := d.YAMLPath != "" || d.EnvVar != "" || d.Get != nil

		switch c.Category {
		case CategoryBootstrap:
			if c.Delivery != nil {
				add(d.Key, "bootstrap setting must not carry a Delivery")
			}
			if d.Transitional {
				add(d.Key, "bootstrap setting must not be Transitional")
			}
			if d.YAMLPath == "" {
				add(d.Key, "bootstrap setting must declare a YAMLPath")
			}
			if d.EnvVar == "" {
				add(d.Key, "bootstrap setting must declare an EnvVar")
			}
			if d.Get == nil {
				add(d.Key, "bootstrap setting must declare a Get accessor")
			}
		case CategoryOperational:
			if d.Default == "" {
				add(d.Key, "operational setting must declare a Default")
			}
			if c.Delivery != nil && c.Delivery.SharedInvariant {
				if !c.Delivery.StampedIntoEnvelope {
					add(d.Key, "SharedInvariant implies StampedIntoEnvelope")
				}
				if !hasWorkerConsumer(c.Consumers) {
					add(d.Key, "SharedInvariant needs at least one worker consumer")
				}
			}
			if d.Transitional {
				if !hasConfigPath {
					add(d.Key, "transitional operational setting must keep its YAMLPath, EnvVar and Get until Phase E")
				}
				if d.YAMLPath == "" || d.EnvVar == "" || d.Get == nil {
					add(d.Key, "transitional operational setting needs all of YAMLPath, EnvVar and Get")
				}
			} else if hasConfigPath {
				add(d.Key, "non-transitional operational setting must have no YAMLPath, EnvVar or Get")
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid setting definitions:\n  %s", strings.Join(problems, "\n  "))
}

// hasWorkerConsumer reports whether any consumer is a worker process.
func hasWorkerConsumer(consumers []Consumer) bool {
	for _, c := range consumers {
		if c == ConsumerWorkerExtractor || c == ConsumerWorkerChunkEmbed {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestValidateSettingDefs`
Expected: PASS, all cases.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/setting_def_validation.go internal/config/setting_def_validation_test.go
git commit -m "feat(config): validate SettingDef category legality, including the transition marker"
```

---

### Task 3: Port the bootstrap settings into the registry

**Files:**
- Create: `internal/config/setting_defs_bootstrap.go`
- Test: `internal/config/setting_defs_bijection_test.go`
- Read for source data: `internal/config/config.go` (env/yaml tags), `internal/config/classification_registry.go:100-282` (classes), `internal/config/migratable_settings.go` (descriptions, types, accessors)

**Interfaces:**
- Consumes: `SettingDef` (Task 1), `ValidateSettingDefs` (Task 2).
- Produces: `var bootstrapSettingDefs []SettingDef`, appended into `settingDefs`.

The bijection test is the specification for this task: it enumerates exactly which keys are missing or extra, so the port is complete when the test passes.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// envTaggedKeys walks the Config struct and returns every yaml path that
// carries an env tag, as "a.b.c" joined from the yaml tags along the path.
// Reflection is used here deliberately and only in tests.
func envTaggedKeys(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	var walk func(rt reflect.Type, prefix []string)
	walk = func(rt reflect.Type, prefix []string) {
		if rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			yamlTag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if yamlTag == "-" {
				continue
			}
			path := prefix
			if yamlTag != "" {
				path = append(append([]string{}, prefix...), yamlTag)
			}
			if env := f.Tag.Get("env"); env != "" {
				out[strings.Join(path, ".")] = env
			}
			ft := f.Type
			if ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, path)
			}
		}
	}
	walk(reflect.TypeOf(Config{}), nil)
	return out
}

func TestBootstrapDefs_BijectWithConfigStructEnvTags(t *testing.T) {
	structKeys := envTaggedKeys(t)

	declared := map[string]string{}
	for _, d := range AllSettingDefs() {
		if d.YAMLPath == "" {
			continue // no config path: database-only operational setting
		}
		declared[d.YAMLPath] = d.EnvVar
	}

	var missing, extra []string
	for k := range structKeys {
		if _, ok := declared[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range declared {
		if _, ok := structKeys[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	assert.Empty(t, missing, "Config struct fields with an env tag that have no SettingDef")
	assert.Empty(t, extra, "SettingDefs claiming a config path that the Config struct does not bind")
}

func TestBootstrapDefs_EnvVarNamesMatchStructTags(t *testing.T) {
	structKeys := envTaggedKeys(t)
	for _, d := range AllSettingDefs() {
		if d.YAMLPath == "" {
			continue
		}
		want, ok := structKeys[d.YAMLPath]
		if !ok {
			continue // reported by the bijection test
		}
		assert.Equal(t, want, d.EnvVar,
			"SettingDef %q declares EnvVar %q but the Config struct binds %q",
			d.Key, d.EnvVar, want)
	}
}

func TestBootstrapDefs_GetReturnsConfiguredValue(t *testing.T) {
	c := &Config{}
	c.Server.Port = "9999"

	d, ok := DefFor("server.port")
	assert.True(t, ok, "server.port must be declared")
	if ok {
		assert.Equal(t, "9999", d.Get(c))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestBootstrapDefs`
Expected: FAIL. `TestBootstrapDefs_BijectWithConfigStructEnvTags` lists every env-tagged key as missing — that list is the work queue for Step 3.

- [ ] **Step 3: Write the implementation**

Create `internal/config/setting_defs_bootstrap.go` declaring one `SettingDef` per bootstrap key. Source each field from the existing code rather than inventing it:

- `Key` and `YAMLPath` — the dotted yaml path (they are equal for bootstrap settings).
- `EnvVar` — copy verbatim from the `env:` struct tag in `internal/config/config.go`. **Do not derive it**; the mapping is irregular (`server.disable_rate_limiting` binds `TMI_DISABLE_RATE_LIMITING`, not `TMI_SERVER_DISABLE_RATE_LIMITING`), and a derived name marks a struct default as explicit, which is exactly the class of bug #794 fixed.
- `Class` — copy from `exactClassifications` in `classification_registry.go`.
- `Type` and `Description` — copy from the corresponding entry in `migratable_settings.go`.
- `Get` — a closure returning the canonical string form, matching how `migratable_settings.go` formats that field (`strconv.FormatBool`, `strconv.Itoa`, `.String()` for durations, `json.Marshal` for slices).

The file's shape, with three worked examples covering the three formatting cases:

```go
package config

import (
	"encoding/json"
	"strconv"
)

// bootstrapSettingDefs declares every setting delivered by config file and
// environment. Bootstrap settings are never stored in the database.
var bootstrapSettingDefs = []SettingDef{
	{
		Key:         "server.port",
		YAMLPath:    "server.port",
		EnvVar:      "TMI_SERVER_PORT",
		Type:        "string",
		Description: "HTTP server port",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Get:         func(c *Config) string { return c.Server.Port },
	},
	{
		Key:         "server.tls_enabled",
		YAMLPath:    "server.tls_enabled",
		EnvVar:      "TMI_SERVER_TLS_ENABLED",
		Type:        "bool",
		Description: "TLS enabled",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Get:         func(c *Config) string { return strconv.FormatBool(c.Server.TLSEnabled) },
	},
	{
		Key:         "server.cors.allowed_origins",
		YAMLPath:    "server.cors.allowed_origins",
		EnvVar:      "TMI_CORS_ALLOWED_ORIGINS",
		Type:        "json",
		Description: "CORS allowed origins",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Get: func(c *Config) string {
			b, err := json.Marshal(c.Server.CORS.AllowedOrigins)
			if err != nil {
				return "[]"
			}
			return string(b)
		},
	},
	// ... one entry per remaining env-tagged bootstrap key; the bijection
	// test in setting_defs_bijection_test.go names any that are missing.
}
```

Then register them by changing the `settingDefs` declaration in `setting_def.go`:

```go
// settingDefs is the authoritative registry.
var settingDefs = concatDefs(bootstrapSettingDefs)

// concatDefs joins definition groups into the single registry slice.
func concatDefs(groups ...[]SettingDef) []SettingDef {
	var out []SettingDef
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}
```

Note `settingDefIndex` is initialised from `settingDefs` at package init; because Go initialises package-level variables in dependency order, no change is needed there.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestBootstrapDefs`
Expected: PASS — both `missing` and `extra` empty.
Then: `make test-unit name=TestValidateSettingDefs_RegistryItselfIsValid`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/setting_defs_bootstrap.go internal/config/setting_defs_bijection_test.go internal/config/setting_def.go
git commit -m "feat(config): declare every bootstrap setting in the registry, bijective with the Config struct"
```

---

### Task 4: Port the operational settings as transitional

**Files:**
- Create: `internal/config/setting_defs_operational.go`
- Modify: `internal/config/setting_def.go` (add the group to `concatDefs`)
- Test: `internal/config/setting_defs_operational_test.go`
- Read for source data: `internal/config/classification_registry.go:100-282`, `internal/config/migratable_settings.go`

**Interfaces:**
- Consumes: `SettingDef` (Task 1), `ValidateSettingDefs` (Task 2), `concatDefs` (Task 3).
- Produces: `var operationalSettingDefs []SettingDef`.

Every setting classified `operationalClass(...)` today still has a `Config` struct field, so each is declared `Transitional: true` with its `YAMLPath`, `EnvVar` and `Get` retained. Phase E removes them.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalDefs_KnownKeysAreDeclaredOperational(t *testing.T) {
	// A representative sample across the operational sections. Full coverage
	// is proven by TestRegistry_CoversEveryReachableKey in Task 6.
	for _, key := range []string{
		"auth.everyone_is_a_reviewer",
		"auth.oauth.client_callback_allowlist",
		"features.saml_enabled",
		"websocket.inactivity_timeout_seconds",
		"operator.name",
		"administrators",
		"observability.enabled",
		"ssrf.webhook.allowlist",
		"webhooks.allow_http_targets",
		"timmy.embedding_dimension",
	} {
		d, ok := DefFor(key)
		require.True(t, ok, "%s must be declared in the registry", key)
		assert.Equal(t, CategoryOperational, d.Class.Category, "%s must be operational", key)
		assert.NotEmpty(t, d.Default, "%s must declare a Default", key)
	}
}

func TestOperationalDefs_AreTransitionalWhileConfigDelivered(t *testing.T) {
	d, ok := DefFor("auth.everyone_is_a_reviewer")
	require.True(t, ok)
	assert.True(t, d.Transitional, "operational settings still have a config path in Phase A")
	assert.NotEmpty(t, d.EnvVar)
	assert.NotNil(t, d.Get)
}

func TestOperationalDefs_DeclareMutability(t *testing.T) {
	// features.saml_enabled gates SAML manager construction at startup, so a
	// database edit cannot take effect without a restart. The spec makes
	// Mutability load-bearing; this pins the known case.
	d, ok := DefFor("features.saml_enabled")
	require.True(t, ok)
	assert.Equal(t, MutabilityStatic, d.Class.Mutability,
		"features.saml_enabled gates startup wiring and is restart-required")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestOperationalDefs`
Expected: FAIL — the keys are not yet declared.

- [ ] **Step 3: Write the implementation**

Create `internal/config/setting_defs_operational.go`, one entry per key currently classified with `operationalClass(...)` or `sharedEmbeddingClass(...)`. Two worked examples:

```go
package config

import "strconv"

// operationalSettingDefs declares every database-backed setting. Entries
// marked Transitional still have a config/env delivery path, removed in
// Phase E of the config model redesign.
var operationalSettingDefs = []SettingDef{
	{
		Key:          "auth.everyone_is_a_reviewer",
		YAMLPath:     "auth.everyone_is_a_reviewer",
		EnvVar:       "TMI_EVERYONE_IS_A_REVIEWER",
		Type:         "bool",
		Description:  "Grant every authenticated user the reviewer role",
		Default:      "false",
		Transitional: true,
		Class:        operationalClass(VisibilityAdminOnly, false),
		Get:          func(c *Config) string { return strconv.FormatBool(c.Auth.EveryoneIsAReviewer) },
	},
	{
		Key:          "features.saml_enabled",
		YAMLPath:     "auth.saml.enabled",
		EnvVar:       "TMI_SAML_ENABLED",
		Type:         "bool",
		Description:  "Enable SAML authentication",
		Default:      "false",
		Transitional: true,
		Class: ConfigClass{
			Category:   CategoryOperational,
			Visibility: VisibilityPublic,
			Mutability: MutabilityStatic, // gates SAML manager construction at startup
			Delivery:   &Delivery{},
			Consumers:  []Consumer{ConsumerMonolith, ConsumerTMIUX},
		},
		Get: func(c *Config) string { return strconv.FormatBool(c.Auth.SAML.Enabled) },
	},
	// ... one entry per remaining operational key.
}
```

Set `Mutability` deliberately per entry: `MutabilityHot` where the value is re-read at use time, `MutabilityStatic` where it is captured at startup. When unsure, read the consuming code before choosing; do not copy the value from `operationalClass`, whose default was never load-bearing.

Then extend the registry in `setting_def.go`:

```go
var settingDefs = concatDefs(bootstrapSettingDefs, operationalSettingDefs)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestOperationalDefs`
Then: `make test-unit name=TestValidateSettingDefs_RegistryItselfIsValid`
Then: `make test-unit name=TestBootstrapDefs`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/setting_defs_operational.go internal/config/setting_defs_operational_test.go internal/config/setting_def.go
git commit -m "feat(config): declare operational settings in the registry, marked transitional"
```

---

### Task 5: Declare the database-seeded keys and make `DefaultSystemSettings()` a projection

**Files:**
- Modify: `internal/config/setting_defs_operational.go`
- Modify: `api/models/system_setting.go:106-165` (`DefaultSystemSettings`)
- Create: `internal/config/seed_projection.go`
- Test: `internal/config/seed_projection_test.go`
- Test: `api/models/system_setting_test.go`

**Interfaces:**
- Consumes: `AllSettingDefs()` (Task 1), `operationalSettingDefs` (Task 4).
- Produces: `func SeedableOperationalDefs() []SettingDef` — the operational entries that should be seeded into `system_settings`, sorted by key.

This is the task that fixes **#809**: `rate_limit.requests_per_minute` and `rate_limit.requests_per_hour` are seeded today but classified nowhere, so `GET`/`DELETE /admin/settings/{key}` 404s on keys the list endpoint shows.

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedableOperationalDefs_IncludesPreviouslyUnclassifiedKeys(t *testing.T) {
	// #809: these two were seeded into system_settings but had no
	// classification entry, so they resolved to VisibilityInternal and 404'd
	// on GET/DELETE while appearing in the LIST response.
	for _, key := range []string{
		"rate_limit.requests_per_minute",
		"rate_limit.requests_per_hour",
	} {
		d, ok := DefFor(key)
		require.True(t, ok, "%s must be declared", key)
		assert.Equal(t, CategoryOperational, d.Class.Category)
		assert.Equal(t, VisibilityAdminOnly, d.Class.Visibility,
			"%s is not consumed by tmi-ux, so admin-only rather than public", key)
		assert.False(t, d.Transitional, "%s has no config path", key)
	}
}

func TestSeedableOperationalDefs_AreSortedAndNonEmpty(t *testing.T) {
	defs := SeedableOperationalDefs()
	require.NotEmpty(t, defs)
	for i := 1; i < len(defs); i++ {
		assert.Less(t, defs[i-1].Key, defs[i].Key, "SeedableOperationalDefs must be sorted by key")
	}
	for _, d := range defs {
		assert.Equal(t, CategoryOperational, d.Class.Category)
		assert.NotEmpty(t, d.Default)
	}
}
```

And in `api/models/system_setting_test.go`:

```go
func TestDefaultSystemSettings_MatchesRegistryProjection(t *testing.T) {
	defs := config.SeedableOperationalDefs()
	seeds := DefaultSystemSettings()

	require.Equal(t, len(defs), len(seeds),
		"DefaultSystemSettings must be a projection of the registry, not a parallel list")

	byKey := map[string]SystemSetting{}
	for _, s := range seeds {
		byKey[string(s.SettingKey)] = s
	}
	for _, d := range defs {
		s, ok := byKey[d.Key]
		require.True(t, ok, "registry declares %s but DefaultSystemSettings does not seed it", d.Key)
		assert.Equal(t, d.Default, s.Value, "seed value for %s must be the registry Default", d.Key)
		assert.Equal(t, d.Description, s.Description.String, "seed description for %s", d.Key)
	}
}

func TestDefaultSystemSettings_SeedsPreviouslyUnclassifiedRateLimitKeys(t *testing.T) {
	keys := map[string]bool{}
	for _, s := range DefaultSystemSettings() {
		keys[string(s.SettingKey)] = true
	}
	assert.True(t, keys["rate_limit.requests_per_minute"])
	assert.True(t, keys["rate_limit.requests_per_hour"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSeedableOperationalDefs`
Expected: FAIL — `rate_limit.*` not declared; `SeedableOperationalDefs` undefined.
Run: `make test-unit name=TestDefaultSystemSettings`
Expected: FAIL — counts differ.

- [ ] **Step 3: Write the implementation**

Add the nine keys currently seeded by `DefaultSystemSettings()` to `operationalSettingDefs`, non-transitional (they have no `Config` struct field), preserving each existing default value and description exactly so no seeded database changes:

```go
	{
		Key:         "rate_limit.requests_per_minute",
		Type:        "int",
		Description: "Maximum API requests per minute per user",
		Default:     "100",
		Class:       operationalClass(VisibilityAdminOnly, false),
	},
	{
		Key:         "rate_limit.requests_per_hour",
		Type:        "int",
		Description: "Maximum API requests per hour per user",
		Default:     "1000",
		Class:       operationalClass(VisibilityAdminOnly, false),
	},
	{
		Key:         "ui.default_theme",
		Type:        "string",
		Description: "Default UI theme (auto, light, dark)",
		Default:     "auto",
		Class:       operationalClass(VisibilityPublic, false, ConsumerMonolith, ConsumerTMIUX),
	},
	// ... and the remaining seeded keys: websocket.max_participants,
	// upload.max_file_size_mb, features.webhooks_enabled,
	// features.websocket_enabled. Note session.timeout_minutes and
	// features.saml_enabled are already declared (Task 4) — do not duplicate
	// them; the duplicate-key validation rule will catch it if you do.
```

Create `internal/config/seed_projection.go`:

```go
package config

import "sort"

// SeedableOperationalDefs returns the operational settings that should be
// seeded into system_settings on database initialisation, sorted by key.
//
// This is the single source for the seed list. DefaultSystemSettings() in
// api/models projects it, rather than maintaining a parallel list — the
// parallel list is what left rate_limit.* seeded but unclassified (#809).
func SeedableOperationalDefs() []SettingDef {
	var out []SettingDef
	for _, d := range settingDefs {
		if d.Class.Category == CategoryOperational {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
```

Rewrite `DefaultSystemSettings()` in `api/models/system_setting.go` as a projection:

```go
// DefaultSystemSettings returns the default system settings seeded when the
// database is initialised. It projects the config package's registry rather
// than maintaining a parallel list.
func DefaultSystemSettings() []SystemSetting {
	desc := func(s string) NullableDBText { return NullableDBText{String: s, Valid: true} }

	defs := config.SeedableOperationalDefs()
	out := make([]SystemSetting, 0, len(defs))
	for _, d := range defs {
		out = append(out, SystemSetting{
			SettingKey:  DBVarchar(d.Key),
			Value:       d.Default,
			SettingType: settingTypeFor(d.Type),
			Description: desc(d.Description),
		})
	}
	return out
}

// settingTypeFor maps a registry type name to the stored setting_type.
func settingTypeFor(t string) string {
	switch t {
	case "bool":
		return SystemSettingTypeBool
	case "int":
		return SystemSettingTypeInt
	case "float":
		return SystemSettingTypeFloat
	case "json":
		return SystemSettingTypeJSON
	default:
		return SystemSettingTypeString
	}
}
```

Check the exact constant names and the `SettingKey`/`SettingType` field types against `api/models/system_setting.go:1-60` before writing; adjust the conversions to match. If `SystemSettingTypeFloat` or `SystemSettingTypeJSON` does not exist, map those types to the string constant and note it in the commit message.

Watch the import direction: `api/models` may import `internal/config`, but not the reverse. Verify with `go build ./...` that no import cycle results; if one does, move `SeedableOperationalDefs` consumption into a small adapter in `api/models` rather than inverting the dependency.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestSeedableOperationalDefs`
Run: `make test-unit name=TestDefaultSystemSettings`
Run: `make build-server`
Run: `make test-unit`
Expected: PASS, full suite green (2678 tests as of 1.8.22).

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/setting_defs_operational.go internal/config/seed_projection.go internal/config/seed_projection_test.go api/models/system_setting.go api/models/system_setting_test.go
git commit -m "fix(config): classify rate_limit.* and project DefaultSystemSettings from the registry

The seed list was a hand-kept parallel list, so rate_limit.requests_per_minute
and rate_limit.requests_per_hour were seeded into system_settings while having
no classification entry. Unclassified resolves to VisibilityInternal, so
GET/DELETE /admin/settings/{key} returned 404 for keys the LIST endpoint
displayed.

Fixes #809"
```

---

### Task 6: The total-coverage guardrail

**Files:**
- Create: `internal/config/registry_coverage_test.go`

**Interfaces:**
- Consumes: `AllSettingDefs()`, `DefFor()` (Task 1), `SeedableOperationalDefs()` (Task 5), `exactClassifications` and `prefixClassifications` (existing, `classification_registry.go`).

This is the test that would have caught #809, and the enforcement point for the spec's goal 6.

- [ ] **Step 1: Write the test**

```go
package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRegistry_CoversEveryReachableKey asserts that every setting key the
// server can reach — from the Config struct, the classification registry, or
// the database seed list — has a SettingDef with a real Category.
//
// The previous guardrail could not do this: ValidateClassifications took
// whatever slice it was handed, in practice GetMigratableSettings(), which by
// construction contains only keys that already have a Config struct field,
// and which emits conditionally so the set changed with runtime values.
func TestRegistry_CoversEveryReachableKey(t *testing.T) {
	declared := map[string]bool{}
	for _, d := range AllSettingDefs() {
		declared[d.Key] = true
	}

	reachable := map[string]string{} // key -> where it came from

	for k := range envTaggedKeys(t) {
		reachable[k] = "Config struct env tag"
	}
	for k := range exactClassifications {
		reachable[k] = "classification registry"
	}
	for _, d := range SeedableOperationalDefs() {
		reachable[d.Key] = "database seed list"
	}

	var uncovered []string
	for k, src := range reachable {
		if !declared[k] {
			uncovered = append(uncovered, k+" (from "+src+")")
		}
	}
	sort.Strings(uncovered)

	assert.Empty(t, uncovered,
		"every reachable setting key must have a SettingDef; add one to internal/config/setting_defs_*.go")
}

func TestRegistry_NoKeyIsDeclaredTwice(t *testing.T) {
	seen := map[string]int{}
	for _, d := range AllSettingDefs() {
		seen[d.Key]++
	}
	var dupes []string
	for k, n := range seen {
		if n > 1 {
			dupes = append(dupes, k)
		}
	}
	sort.Strings(dupes)
	assert.Empty(t, dupes, "a setting must be declared exactly once")
}

func TestRegistry_EveryDefHasANonZeroCategory(t *testing.T) {
	for _, d := range AllSettingDefs() {
		assert.NotEqual(t, CategoryUnclassified, d.Class.Category,
			"%s has the zero Category", d.Key)
	}
}

func TestRegistry_PassesValidation(t *testing.T) {
	assert.NoError(t, ValidateSettingDefs(AllSettingDefs()))
}
```

- [ ] **Step 2: Run tests**

Run: `make test-unit name=TestRegistry`
Expected: PASS if Tasks 3-5 are complete. Any failure names the exact missing keys — add a `SettingDef` for each and re-run.

- [ ] **Step 3: Verify the guardrail actually bites**

Temporarily add a seeded key with no `SettingDef` — append to `operationalSettingDefs` a `SettingDef` with `Key: "temp.guardrail.probe"` and then delete it from the registry while adding `"temp.guardrail.probe"` to `exactClassifications`. Run `make test-unit name=TestRegistry_CoversEveryReachableKey` and confirm it FAILS naming that key. Revert the probe.

This step is not optional: a coverage test that cannot fail is worse than none, and the previous guardrail's whole defect was that it validated a set that excluded the problem.

- [ ] **Step 4: Lint and commit**

```bash
make lint
git add internal/config/registry_coverage_test.go
git commit -m "test(config): assert every reachable setting key has a classification"
```

---

### Task 7: Project `GetMigratableSettings()` from the registry

**Files:**
- Modify: `internal/config/migratable_settings.go:60-105` (`GetMigratableSettings`) and delete the per-section builders it calls
- Test: `internal/config/migratable_settings_equivalence_test.go`

**Interfaces:**
- Consumes: `AllSettingDefs()`, `SettingDef.Get` (Tasks 1, 3, 4).
- Produces: `GetMigratableSettings()` with an unchanged signature — `func (c *Config) GetMigratableSettings() []MigratableSetting`.

The equivalence test is written **before** the rewrite and pins current output, so the refactor is provably behaviour-preserving.

- [ ] **Step 1: Write the equivalence test against current behaviour**

```go
package config

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleConfig returns a Config with enough fields populated to exercise the
// conditional emission paths in the pre-refactor implementation.
func sampleConfig() *Config {
	c := &Config{}
	c.Server.Port = "8080"
	c.Server.Interface = "0.0.0.0"
	c.Server.BaseURL = "https://api.example.test"
	c.Server.TLSEnabled = true
	c.Server.TLSCertFile = "/tmp/cert.pem"
	c.Server.TLSKeyFile = "/tmp/key.pem"
	c.Server.CORS.AllowedOrigins = []string{"https://ui.example.test"}
	return c
}

func keysOf(settings []MigratableSetting) []string {
	out := make([]string, 0, len(settings))
	for _, s := range settings {
		out = append(out, s.Key)
	}
	sort.Strings(out)
	return out
}

func TestGetMigratableSettings_EmitsEveryDeclaredConfigPathKey(t *testing.T) {
	c := sampleConfig()
	got := keysOf(c.GetMigratableSettings())

	var want []string
	for _, d := range AllSettingDefs() {
		if d.Get != nil {
			want = append(want, d.Key)
		}
	}
	sort.Strings(want)

	assert.Equal(t, want, got,
		"GetMigratableSettings must emit exactly the registry's config-path keys")
}

func TestGetMigratableSettings_ValuesComeFromConfig(t *testing.T) {
	c := sampleConfig()
	byKey := map[string]MigratableSetting{}
	for _, s := range c.GetMigratableSettings() {
		byKey[s.Key] = s
	}

	require.Contains(t, byKey, "server.port")
	assert.Equal(t, "8080", byKey["server.port"].Value)
	assert.Equal(t, "TMI_SERVER_PORT", byKey["server.port"].EnvVar)

	require.Contains(t, byKey, "server.tls_enabled")
	assert.Equal(t, "true", byKey["server.tls_enabled"].Value)

	require.Contains(t, byKey, "server.cors.allowed_origins")
	assert.Equal(t, `["https://ui.example.test"]`, byKey["server.cors.allowed_origins"].Value)
}

func TestGetMigratableSettings_CarriesClassAndSecrecy(t *testing.T) {
	c := sampleConfig()
	for _, s := range c.GetMigratableSettings() {
		// Per-provider keys are generated, not statically declared, and have
		// no SettingDef by design. Their secrecy lives on the per-setting
		// Secret flag rather than on Class.Secret, so comparing them against
		// a SettingDef would be both impossible and wrong.
		if isGeneratedProviderKey(s.Key) {
			assert.True(t, s.Class.Category != CategoryUnclassified,
				"generated provider key %s must still be prefix-classified", s.Key)
			continue
		}
		d, ok := DefFor(s.Key)
		require.True(t, ok, "emitted key %s has no SettingDef", s.Key)
		assert.Equal(t, d.Class.Category, s.Class.Category, "class mismatch for %s", s.Key)
		assert.Equal(t, d.IsSecret(), s.IsSecret(), "secrecy mismatch for %s", s.Key)
	}
}

// isGeneratedProviderKey reports whether a key comes from the per-provider
// generators rather than from a static SettingDef.
func isGeneratedProviderKey(key string) bool {
	for _, p := range []string{
		"auth.oauth.providers.",
		"auth.saml.providers.",
		"content_oauth.providers.",
	} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func TestGetMigratableSettings_ExplicitTracksEnvAndFile(t *testing.T) {
	t.Setenv("TMI_SERVER_PORT", "9090")
	c := sampleConfig()
	for _, s := range c.GetMigratableSettings() {
		if s.Key == "server.port" {
			assert.Equal(t, "environment", s.Source)
			assert.True(t, s.Explicit, "an env-set key must be Explicit")
		}
	}
}
```

- [ ] **Step 2: Run the tests against the current implementation**

Run: `make test-unit name=TestGetMigratableSettings`
Expected: `TestGetMigratableSettings_EmitsEveryDeclaredConfigPathKey` FAILS, because the current implementation emits `server.base_url`, `server.tls_cert_file`, `server.tls_key_file` and `server.cors.allowed_origins` only when their values are non-empty, whereas the registry declares them unconditionally.

**Executed differently — see "Execution deviations" above.** Unconditional emission was not adopted; it was replaced with the explicit `SettingDef.OmitWhenEmpty` flag, for the reasons given there.

**Record the diff before changing anything.** Conditional emission is a real behaviour difference, not a test bug: a key that vanishes when empty is why `ValidateClassifications` saw a different set on every boot. The other tests should pass, confirming values, classes and explicitness are already equivalent.

- [ ] **Step 3: Rewrite `GetMigratableSettings` as a projection**

**Executed differently — see "Execution deviations" above.** The code sample
below is the plan's original proposal and was not what was built; the shipped
`GetMigratableSettings` honors `SettingDef.OmitWhenEmpty` instead of emitting
every key unconditionally.

```go
// GetMigratableSettings returns every setting that has a config-file or
// environment delivery path, with its current value read from this Config.
//
// This projects the registry (setting_defs_*.go) rather than maintaining a
// parallel list. Emission is unconditional: a key with an empty value is
// emitted with an empty value, rather than being omitted. The previous
// implementation omitted several keys when unset, which made the set of
// settings vary with runtime values and left the classification guardrail
// validating a different set on every boot.
func (c *Config) GetMigratableSettings() []MigratableSetting {
	defs := AllSettingDefs()
	settings := make([]MigratableSetting, 0, len(defs))

	for _, d := range defs {
		if d.Get == nil {
			continue // database-only: no config path to read from
		}
		settings = append(settings, MigratableSetting{
			Key:         d.Key,
			Value:       d.Get(c),
			Type:        d.Type,
			Description: d.Description,
			Secret:      d.Class.Secret,
			Source:      settingSource(d.EnvVar),
			EnvVar:      d.EnvVar,
			Class:       d.Class,
		})
	}

	for i := range settings {
		settings[i].Explicit = settings[i].Source == "environment" ||
			c.explicitFileKeys[settings[i].Key]
	}

	return settings
}
```

Delete the per-section builders this replaces (`getMigratableServerSettings`, `getMigratableAuthSettings`, and their siblings) along with any helper now unreferenced. Keep `settingSource`, `MigratableSetting`, and `IsSecret`.

The provider subtrees (`auth.oauth.providers.*`, `auth.saml.providers.*`, `content_oauth.providers.*`) are generated per configured provider rather than declared statically. Keep their existing generator functions and append their output after the loop above; they are prefix-classified and out of scope for Phase A. **Preserve the per-setting `Secret` flag those helpers set on `.client_secret`, `.sp_private_key` and `.idp_metadata_b64xml`** — `Class.Secret` is deliberately false for those prefixes, so dropping the per-key flag would expose real OAuth client secrets.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make test-unit name=TestGetMigratableSettings`
Run: `make test-unit`
Expected: PASS, full suite.

If a pre-existing test now fails because a key is emitted that previously was not, that is the intended behaviour change — update the test and note it in the commit message. If a test fails because a *value* differs, stop: that is a regression, not a behaviour change.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/migratable_settings.go internal/config/migratable_settings_equivalence_test.go
git commit -m "refactor(config): project GetMigratableSettings from the registry

Emission is now unconditional. Keys that were previously omitted when their
value was empty (server.base_url, the TLS file paths, CORS origins) are always
emitted, so the set of settings no longer varies with runtime values."
```

**Executed differently — see "Execution deviations" above.** This proposed
commit message describes the plan's original intent, not the shipped commit:
emission stayed conditional via `SettingDef.OmitWhenEmpty` rather than
becoming unconditional.

---

### Task 8: Re-point `genconfig` and `genconfigdocs` at the registry

**Files:**
- Modify: `cmd/genconfig/main.go`
- Modify: `cmd/genconfigdocs/main.go`
- Test: `cmd/genconfig/main_test.go`
- Test: `cmd/genconfigdocs/main_test.go`

**Interfaces:**
- Consumes: `config.AllSettingDefs()` (Task 1).
- Produces: no new exported API; generated output must be equivalent to today's.

Read both files first — they generate `config-example.yml` and the config reference documentation from the `Config` struct today. The generated config reference doubles as the `TMI_*` env-var allowlist, so an omitted env var silently narrows it.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestGenerated_CoversEveryBootstrapEnvVar(t *testing.T) {
	out := generate() // the package's existing generation entry point

	var missing []string
	for _, d := range config.AllSettingDefs() {
		if d.EnvVar == "" {
			continue
		}
		if !strings.Contains(out, d.EnvVar) {
			missing = append(missing, d.EnvVar)
		}
	}
	assert.Empty(t, missing,
		"generated output must name every declared env var; it doubles as the TMI_* allowlist")
}

func TestGenerated_OmitsDatabaseOnlySettings(t *testing.T) {
	out := generate()
	for _, d := range config.AllSettingDefs() {
		if d.Class.Category == config.CategoryOperational && !d.Transitional {
			assert.NotContains(t, out, d.Key,
				"%s is database-only and must not appear in a config file template", d.Key)
		}
	}
}
```

Adapt `generate()` to whatever the entry point is actually named in each command; if generation currently writes straight to a file, extract a function returning the rendered string first, in this same task, and have `main` call it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestGenerated`
Expected: FAIL — either `generate` is undefined (extract it first) or database-only keys appear in the output.

- [ ] **Step 3: Rewrite generation to walk the registry**

Replace the `Config`-struct reflection walk with a loop over `config.AllSettingDefs()`, emitting an entry for each def where `d.YAMLPath != ""` (bootstrap plus transitional operational), grouped by the first dotted segment of `YAMLPath` and sorted by key within each group. Emit `d.Description` as the comment, `d.EnvVar` as the documented override, and `d.Default` as the sample value.

Skip defs with `Class.Category == CategoryOperational && !Transitional` entirely: they have no config-file representation.

- [ ] **Step 4: Run tests and regenerate**

Run: `make test-unit name=TestGenerated`
Run the generators and inspect the diff against the committed `config-example.yml` and config reference. The diff should be limited to ordering and to keys that were conditionally omitted before. **If a `TMI_*` variable disappears from the reference, stop** — that silently narrows the env-var allowlist.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add cmd/genconfig cmd/genconfigdocs config-example.yml
git commit -m "refactor(config): generate config template and reference from the registry"
```

---

### Task 9: The transitional ratchet

**Files:**
- Create: `internal/config/transitional_ratchet_test.go`

**Interfaces:**
- Consumes: `AllSettingDefs()` (Task 1).

A golden list of the operational keys that still have a config/env path. It can only shrink. This is what stops Phase A from quietly becoming permanent, and its emptiness is the Phase E completion gate.

- [ ] **Step 1: Write the test**

```go
package config

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// transitionalKeys is the golden list of operational settings that still have
// a config-file or environment delivery path, pending Phase E of the config
// model redesign (docs/superpowers/specs/2026-08-22-config-model-redesign-design.md).
//
// This list may only SHRINK. Adding a key here means adding a new config/env
// path for a database-only setting, which is the thing the redesign exists to
// remove. When it reaches zero, Phase E is complete and goal 2 is enforced.
//
// Populate it in Step 2 from the test's own failure output.
var transitionalKeys = []string{
	// filled in from the first run
}

func TestTransitionalKeys_MatchGoldenListExactly(t *testing.T) {
	var actual []string
	for _, d := range AllSettingDefs() {
		if d.Transitional {
			actual = append(actual, d.Key)
		}
	}
	sort.Strings(actual)

	want := append([]string{}, transitionalKeys...)
	sort.Strings(want)

	assert.Equal(t, want, actual,
		"the transitional list may only shrink: removing a key means its config/env "+
			"path is gone (good); adding one means a new config/env path was introduced "+
			"for a database-only setting (not allowed)")
}

func TestTransitionalKeys_AreAllOperational(t *testing.T) {
	for _, d := range AllSettingDefs() {
		if d.Transitional {
			assert.Equal(t, CategoryOperational, d.Class.Category,
				"%s is Transitional but not operational", d.Key)
		}
	}
}
```

- [ ] **Step 2: Run, then populate the golden list from the failure**

Run: `make test-unit name=TestTransitionalKeys_MatchGoldenListExactly`
Expected: FAIL, printing the actual list. Copy those keys verbatim into `transitionalKeys`, one per line, sorted.

- [ ] **Step 3: Re-run to verify it passes**

Run: `make test-unit name=TestTransitionalKeys`
Expected: PASS.

- [ ] **Step 4: Verify the ratchet bites in the addition direction**

Temporarily set `Transitional: true` on one non-transitional operational def and confirm the test FAILS naming it. Revert.

- [ ] **Step 5: Lint and commit**

```bash
make lint
git add internal/config/transitional_ratchet_test.go
git commit -m "test(config): ratchet the list of operational settings still delivered by config/env"
```

---

### Task 10: Full verification and Oracle review

**Files:**
- No source changes expected. Fix whatever the gates surface.

- [ ] **Step 1: Run the full local gate**

```bash
make lint
make build-server
make test-unit
```
Expected: 0 lint issues; clean build; full unit suite green.

- [ ] **Step 2: Run the integration suite**

```bash
make test-integration
```
Expected: 85 passed / 0 failed (the 1.8.22 baseline).

If `TestIdentityLink` or `TestIdentityLink_SecondConfirmRejected` fail, check for foreign listeners on `:8080`/`:6379` before blaming the diff (#778) — but do not assume it. Prove it against a clean `main` worktree if in doubt.

- [ ] **Step 3: Dispatch the Oracle review**

`DefaultSystemSettings()` changing from a hand-written list to a registry projection changes seeding behaviour, which is DB-touching. Invoke the `oracle-db-admin` skill and dispatch the subagent with the full diff.

Address every BLOCKING finding before proceeding; fold APPROVED WITH NOTES items in or file follow-ups. Two things to raise explicitly:
- The seed list gains two rows (`rate_limit.*` are already seeded today, so in fact it gains none — confirm that against the current row set rather than assuming).
- `SeedDefaults` skips existing rows, so a projection change does not rewrite existing databases. Confirm that is still true, and that the `origin` stamping from #794 is unaffected.

- [ ] **Step 4: Refresh SEM markers**

```bash
/sem-annotate --update internal/config/setting_def.go internal/config/setting_def_validation.go internal/config/setting_defs_bootstrap.go internal/config/setting_defs_operational.go internal/config/seed_projection.go internal/config/migratable_settings.go api/models/system_setting.go
```

Markers must follow the code commits — `sem blame` cannot anchor a file that is not yet in HEAD.

- [ ] **Step 5: Commit and open the PR**

```bash
make lint
git add -u
git commit -m "chore(config): refresh SEM markers for the registry consolidation"
git push -u origin dev/1.9.0/config-model-redesign
```

Open a PR titled `feat(config): make one registry the authoritative declaration of every setting` — `feat:` drives the minor bump to 1.9.0. Expect to approve the workflow runs by hand after the version-bump commit lands (#797), and expect "Version Check" to fail on the pre-bump run and pass on the next.

---

## Out of scope for this plan

These are spec'd but belong to later plans:

- **Phase B** — the template tool: `!secret` references, `--effective` export, `--environment` guard, `--prune`, and #807's schema preflight.
- **Phases C/D** — per-environment cutover. An operational runbook, not code.
- **Phase E** — deleting the operational config/env paths, the #794 precedence machinery, `warnOnConfigDatabaseDivergence`, and `nonRoundTrippingKeys`; resolving the three derived aliases (`session.timeout_minutes`, `features.saml_enabled`, `auth.oauth_callback_url`).
- **The `auth.tmi_provider.*` sub-tree and the reserved `tmi` provider id** — these change auth behaviour and belong with Phase B, where the template becomes the production identity path.
- **`server.base_url` becoming required for non-dev builds** — a behaviour change, deferred to Phase E.
- **Surfacing `Mutability` through the admin API** — Task 4 *declares* hot vs restart-required per setting, which is the prerequisite. Reporting it on `GET /admin/settings/{key}` and returning the "takes effect on restart" signal from `PUT` is an API change requiring an OpenAPI spec edit and regeneration, so it lands with Phase B alongside #803's `origin` exposure — one OpenAPI regen instead of two, since `api/api.go` rebases badly.
