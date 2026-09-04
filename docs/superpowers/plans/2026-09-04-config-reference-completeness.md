# Config Reference Completeness (#810) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `config-reference.md` (the project's `TMI_*` allowlist) document every environment variable any TMI binary reads, and add a build-time gate that keeps it that way.

**Architecture:** The reference generator stops consuming the database-seeding projection (`GetMigratableSettings`) and iterates the registry (`AllSettingDefs`) directly, so empty-default settings are documented instead of omitted. Variables read outside the registry entirely (the ten SSRF overrides `cmd/server` reads itself, workers, admin bootstrap, cloud logging, secret prefixes) are declared in a hand-maintained `[]ProcessEnvVar` table rendered as a new "Process environment" section, and a test scans all non-test Go source for `TMI_*` tokens and fails on any that neither the registry nor the table nor a justified exclusion list explains.

**Tech Stack:** Go 1.x, testify, Make targets (`make test-unit`, `make lint`, `make build-server`, `make generate-config-docs`, `make generate-config-example`).

**Spec:** `docs/superpowers/specs/2026-09-04-config-reference-completeness-design.md` (verbatim copy of the two approved design comments on #810). **Amendment (human decision, 2026-09-04):** design point 2, the SSRF consolidation, is dropped. `buildURIValidator` keeps its `os.Getenv` reads unchanged, the `SSRF` structs get no env tags, the ten `ssrf.*` defs get no `EnvVar`; the ten `TMI_SSRF_*` names are documented as process-environment variables instead.

## Global Constraints

- Branch: `dev/1.9.16/config-reference-completeness` off `main`. No DB change, so no Oracle review.
- **Always use Make targets.** Never run `go test`, `go run`, `go build` or `./bin/*` directly. Single test: `make test-unit name=TestName count1=true` (the `name` value must not contain `|`; the Makefile passes it to `-run`, so a plain prefix like `TestGenerateReferenceMarkdown` runs every test with that prefix). Full suite: `make test-unit`. Lint: `make lint`. Build: `make build-server`. Docs: `make generate-config-docs`, `make generate-config-example`.
- gofmt every Go file you touch (`gofmt -w <file>` is fine; `make lint` also catches it).
- **SEM markers:** every new function/type gets `// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: <one-line intent>` directly above it (that is `main` HEAD at plan time; substituting the current `git rev-parse HEAD` is fine). When you change an existing function's behaviour, update the description text of its existing `SEM@` line and keep its sha.
- Never import the standard `log` package; use `internal/slogging` (no new logging is needed in this plan).
- Conventional commits, imperative mood. The Task 3 commit body carries `Fixes #810`. **Every** commit ends with this trailer:

```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
```

- Regenerated artifacts (`config-reference.md`) are committed in the same commit as the generator change that produced them, so the freshness test `TestConfigReferenceFile_MatchesRegistry` passes at every commit.
- Design decisions taken by this plan where the spec is silent or the code disagrees with it (each is explained where it applies): the category/secret columns come from `SettingDef.Class`, not `classificationFor(key)` (Task 1); empty defaults keep the existing `_(none)_` placeholder rather than a literally blank cell (Task 1); the allowlist gate and the dead-SAML-tag deletion are one task because the gate is the failing test that the deletion turns green (Task 3).

---

## File map

| File | Change |
|---|---|
| `internal/config/reference_gen.go` | `GenerateReferenceMarkdown` iterates the registry via new `referenceSettings`; new `processEnvSection` |
| `internal/config/reference_gen_test.go` | completeness test rewritten over the registry; secret-leak test over the registry; new process-env and empty-default tests |
| `config-reference.md` | regenerated (+46 rows, new section) |
| `internal/config/process_env.go` | new: `ProcessEnvVar`, `processEnvVars`, `ProcessEnvVars()` |
| `internal/config/process_env_test.go` | new: table well-formedness test; repo-wide `TMI_*` allowlist gate |
| `internal/config/config.go` | 20 dead `TMI_SAML_*` env tags deleted from `SAMLProviderConfig` |

Untouched by design: `cmd/server/main.go` (`buildURIValidator` and its `os.Getenv` reads), `SSRFConfig`/`SSRFURIConfig`, the ten `ssrf.*` defs, `GenerateExampleConfig`.

---

### Task 1: Reference generator iterates the registry

**Files:**
- Modify: `internal/config/reference_gen.go:16-31` (`GenerateReferenceMarkdown`), add `referenceSettings`
- Modify: `internal/config/reference_gen_test.go:33-55` (`TestGenerateReferenceMarkdown_NeverLeaksSecretDefault`), `:107-138` (replace `TestGenerateReferenceMarkdown_CoversEveryEmittedEnvVar`)
- Regenerate: `config-reference.md`

**Interfaces:**
- Consumes: `AllSettingDefs() []SettingDef`, `SettingDef.Get func(*Config) string`, `SettingDef.Class ConfigClass`, `getDefaultConfig() *Config`, `MigratableSetting` (row shape the existing `bootstrapRow`/`operationalRow` take), `declaredMutability(MigratableSetting) string` (unchanged).
- Produces: `referenceSettings(cfg *Config) []MigratableSetting` (unexported; Task 2's tests reuse it).

Why `d.Class` and not `classificationFor(d.Key)`: for 152 of the 154 config-delivered defs they agree on Category/Visibility/Secret/Required; the two that differ are `database.oracle_wallet_location` and `content_token_encryption_key`, which have no `exactClassifications` entry, so `classificationFor` returns `CategoryUnclassified` and they would land in neither table. `SettingDef.Class` is where the registry declared them bootstrap (and the encryption key secret). Mutability still goes through `declaredMutability`, which returns the same declared value.

Why `_(none)_` stays: `defaultCell` already renders 15 empty defaults that way today; the spec's "rendered blank" means "no value shown", and a literally empty Markdown cell is ambiguous next to a real empty string.

- [ ] **Step 1: Create the branch**

```bash
git checkout main && git pull --rebase
git checkout -b dev/1.9.16/config-reference-completeness
```

- [ ] **Step 2: Write the failing tests**

In `internal/config/reference_gen_test.go`, add `"sort"` to the imports, replace the whole of `TestGenerateReferenceMarkdown_CoversEveryEmittedEnvVar` (lines 107-138, comment included) with:

```go
// TestGenerateReferenceMarkdown_CoversEveryRegistryEnvVar is the allowlist-
// completeness guardrail: config-reference.md doubles as this project's
// TMI_* environment-variable allowlist, so every env var the registry
// declares must be named in the generated reference. It checks the whole
// registry, not the GetMigratableSettings projection — that projection
// omits empty-valued settings for database-seeding reasons (see
// OmitWhenEmpty on SettingDef) that do not apply to documentation, and a
// setting with an empty default is exactly the one an operator needs to be
// told about (#810). There is deliberately no exception list.
func TestGenerateReferenceMarkdown_CoversEveryRegistryEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)

	var missing []string
	for _, d := range AllSettingDefs() {
		if d.EnvVar == "" {
			continue
		}
		if !strings.Contains(s, "`"+d.EnvVar+"`") {
			missing = append(missing, d.EnvVar)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("generated reference is missing %d registry env vars (it doubles as the TMI_* allowlist): %v", len(missing), missing)
	}
}

// TestGenerateReferenceMarkdown_DocumentsEveryConfigDeliveredDef pins the
// three omission classes #810 found: an empty default (server.base_url), a
// conditional on another field (server.tls_cert_file, previously skipped
// because the default config has TLS off), and a key deliberately excluded
// from the settings path (content_token_encryption_key, in
// ExpectedMigratableKeysSkipped). All three are real config/env settings
// and must be documented.
func TestGenerateReferenceMarkdown_DocumentsEveryConfigDeliveredDef(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)

	for _, key := range []string{"server.base_url", "server.tls_cert_file", "content_token_encryption_key", "ssrf.webhook.allowlist"} {
		if !strings.Contains(s, "| `"+key+"` |") {
			t.Errorf("reference has no row for %s", key)
		}
	}

	var rows int
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "| `") && !strings.HasPrefix(line, "| `TMI_") && !strings.HasPrefix(line, "| `SAML_") {
			rows++
		}
	}
	var want int
	for _, d := range AllSettingDefs() {
		if d.Get != nil {
			want++
		}
	}
	if rows != want {
		t.Errorf("reference renders %d setting rows, registry has %d config-delivered defs", rows, want)
	}
}
```

(The `TMI_`/`SAML_` prefix guards keep the row count honest once Task 2 adds the process-environment tables, whose rows start with an env var name rather than a dotted key.)

Then change `TestGenerateReferenceMarkdown_NeverLeaksSecretDefault` so it iterates what the generator now renders. Replace lines 39-40:

```go
	cfg := getDefaultConfig()
	for _, ms := range cfg.GetMigratableSettings() {
```

with:

```go
	cfg := getDefaultConfig()
	cfg.Server.TLSSubjectName = "localhost"
	for _, ms := range referenceSettings(cfg) {
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `make test-unit name=TestGenerateReferenceMarkdown count1=true`

Expected: compile failure `undefined: referenceSettings`. (After Step 4's helper exists but before the generator uses it, `CoversEveryRegistryEnvVar` fails listing 35 env vars starting with `TMI_ALERTING_WEBHOOK_SECRET` ... and `DocumentsEveryConfigDeliveredDef` reports `108` rows vs `154` defs.)

- [ ] **Step 4: Implement `referenceSettings` and switch the generator to it**

In `internal/config/reference_gen.go`, replace lines 10-19 (the doc comment, SEM line, and the first three lines of `GenerateReferenceMarkdown` up to and including `all := cfg.GetMigratableSettings()`) with:

```go
// GenerateReferenceMarkdown produces the per-key configuration reference from
// the classification registry. It emits a precedence explainer plus two
// tables — bootstrap and operational — so the wiki Configuration-Reference
// page is generated, not hand-maintained. Secret defaults are shown as
// vault:// placeholders, never real values.
//
// Rows come from referenceSettings, i.e. the registry itself, not from
// GetMigratableSettings: that projection omits empty-valued and
// conditionally-emitted settings for database-seeding reasons, and the
// reference doubles as the TMI_* env-var allowlist, so it must name every
// setting the server can read (#810).
// SEM@e6cee63c3a07d38f471e0ebfb81722849f36085e: build the wiki configuration reference as Markdown from the full registry, with a precedence explainer and bootstrap/operational tables (pure)
func GenerateReferenceMarkdown() ([]byte, error) {
	cfg := getDefaultConfig()
	cfg.Server.TLSSubjectName = "localhost" // deterministic — must not embed the build host's name
	all := referenceSettings(cfg)
```

Then add, directly after `GenerateReferenceMarkdown` (before `precedenceSection`):

```go
// referenceSettings projects every registry def that has a config/env
// delivery path (Get != nil) into the row shape the table renderers take,
// reading each default from cfg. Unlike GetMigratableSettings it applies no
// OmitWhenEmpty, skiplist or TLS-enabled filter — those exist for database
// seeding (see OmitWhenEmpty on SettingDef) — and it takes Class from the
// def rather than classificationFor(key), because the two keys the registry
// declares with a fallback class (database.oracle_wallet_location,
// content_token_encryption_key) have no classification entry and would
// otherwise land in neither table.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: project every config-delivered registry setting into documentation rows, unfiltered (pure)
func referenceSettings(cfg *Config) []MigratableSetting {
	defs := AllSettingDefs()
	out := make([]MigratableSetting, 0, len(defs))
	for _, d := range defs {
		if d.Get == nil {
			continue // database-only: no config/env delivery to document here
		}
		out = append(out, MigratableSetting{
			Key:         d.Key,
			Value:       d.Get(cfg),
			Type:        d.Type,
			Description: d.Description,
			EnvVar:      d.EnvVar,
			Class:       d.Class,
		})
	}
	return out
}
```

- [ ] **Step 5: Regenerate the reference and run the tests**

Run: `make generate-config-docs`
Run: `make test-unit name=TestGenerateReferenceMarkdown count1=true`
Run: `make test-unit name=TestConfigReferenceFile_MatchesRegistry count1=true`

Expected: all PASS. `git diff --stat config-reference.md` shows roughly 46 added lines (35 env-var rows plus the 10 `ssrf.*` rows and `administrators`, which have no env var). Spot-check that `git diff config-reference.md` adds rows for `server.base_url`, `database.redis.url` (`TMI_REDIS_URL`), `server.tls_cert_file`, `server.tls_key_file`, `content_token_encryption_key` and `database.oracle_wallet_location`, and that the `content_token_encryption_key` row's default cell reads `_(secret)_`.

- [ ] **Step 6: Lint and commit**

Run: `make lint`
Expected: clean.

```bash
git add internal/config/reference_gen.go internal/config/reference_gen_test.go config-reference.md
git commit -m "fix(config): generate the config reference from the full registry

GenerateReferenceMarkdown iterated GetMigratableSettings, whose
OmitWhenEmpty, skiplist and TLS-enabled filters exist for database
seeding and silently dropped 35 env vars from config-reference.md, the
project's TMI_* allowlist. It now projects AllSettingDefs directly; the
completeness test checks the whole registry with no exception list.

Refs #810

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 2: Process-environment table and section

**Files:**
- Create: `internal/config/process_env.go`
- Create: `internal/config/process_env_test.go` (well-formedness test; Task 3 adds the gate to the same file)
- Modify: `internal/config/reference_gen.go` (`GenerateReferenceMarkdown` appends the section; new `processEnvSection`)
- Modify: `internal/config/reference_gen_test.go` (new coverage test)
- Regenerate: `config-reference.md`

**Interfaces:**
- Consumes: `sanitizeCell(string) string`, `yesNo(bool) string` from `reference_gen.go`.
- Produces: `type ProcessEnvVar struct { Name, Binary, Purpose string; Secret, Pattern bool }`; `ProcessEnvVars() []ProcessEnvVar`. For a `Pattern` entry, `Name` is the documented shape with the operator-supplied part in angle brackets (`TMI_SECRET_<KEY>`), and the text before the first `<` is the prefix code scans for; Task 3's gate relies on that.

The table is populated from the spec's inventory: 32 names not in the registry (the ten `TMI_SSRF_*` overrides `cmd/server`'s `buildURIValidator` reads with `os.Getenv`, plus 22 others) and `TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET` (registered for the server, but also read directly by the extractor worker), so 33 fixed rows; then 3 `TMI_` prefix patterns and `SAML_PROVIDERS_<ID>_<FIELD>` as the documented non-`TMI_` pattern. `Purpose` strings must not contain `<`, `>`, backticks or `|`: angle brackets outside a code span disappear as HTML in rendered Markdown, and `sanitizeCell` turns backticks into quotes. `TMI_NATS_URL` is listed once, under workers (where it is required), with the server and component-controller readers named in its purpose.

- [ ] **Step 1: Write the failing tests**

Create `internal/config/process_env_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

// TestProcessEnvVars_WellFormed keeps the hand-maintained inventory
// renderable: every row is complete, names are unique, Pattern agrees with
// the <placeholder> convention the allowlist gate relies on, and Purpose
// cannot break the Markdown table or vanish as HTML.
func TestProcessEnvVars_WellFormed(t *testing.T) {
	vars := ProcessEnvVars()
	if len(vars) == 0 {
		t.Fatal("ProcessEnvVars is empty")
	}
	seen := map[string]bool{}
	for _, p := range vars {
		if p.Name == "" || p.Binary == "" || p.Purpose == "" {
			t.Errorf("%+v: Name, Binary and Purpose are all required", p)
		}
		if seen[p.Name] {
			t.Errorf("%s is declared twice", p.Name)
		}
		seen[p.Name] = true
		if p.Pattern != strings.Contains(p.Name, "<") {
			t.Errorf("%s: Pattern must be true exactly when Name carries a <placeholder>", p.Name)
		}
		if p.Pattern && strings.HasPrefix(p.Name, "<") {
			t.Errorf("%s: a pattern needs a literal prefix before its first placeholder", p.Name)
		}
		if strings.ContainsAny(p.Purpose, "<>`|\n") {
			t.Errorf("%s: Purpose must not contain <, >, backticks, | or newlines", p.Name)
		}
	}
}

func TestProcessEnvVars_ReturnsACopy(t *testing.T) {
	a := ProcessEnvVars()
	a[0].Name = "MUTATED"
	if ProcessEnvVars()[0].Name == "MUTATED" {
		t.Error("ProcessEnvVars must return a copy, not the backing slice")
	}
}
```

In `internal/config/reference_gen_test.go`, add:

```go
// TestGenerateReferenceMarkdown_CoversEveryProcessEnvVar: variables read
// straight from the process environment (the SSRF overrides, workers, admin
// bootstrap, cloud logging, secret prefixes) never pass through the
// registry, so the reference — the TMI_* allowlist — must render the
// hand-maintained ProcessEnvVars table in full (#810).
func TestGenerateReferenceMarkdown_CoversEveryProcessEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "## Process environment") {
		t.Fatal("reference missing section \"## Process environment\"")
	}
	for _, p := range ProcessEnvVars() {
		if !strings.Contains(s, "`"+p.Name+"`") {
			t.Errorf("process env var %s missing from the reference", p.Name)
		}
	}
	for _, want := range []string{"### server", "### workers", "### Prefix patterns"} {
		if !strings.Contains(s, want) {
			t.Errorf("reference missing process-environment group %q", want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `make test-unit name=TestProcessEnvVars count1=true`
Expected: compile failure `undefined: ProcessEnvVars`.

- [ ] **Step 3: Create the table**

Create `internal/config/process_env.go`:

```go
package config

// ProcessEnvVar documents one environment variable that a TMI binary reads
// straight from its process environment — outside the Config struct, the
// SettingDef registry and the config file. The registry cannot see these,
// so they are declared here by hand and rendered into config-reference.md
// (which doubles as the TMI_* allowlist) by GenerateReferenceMarkdown. The
// gate test in process_env_test.go fails when a TMI_* token appears in Go
// source without being declared here or in the registry (#810).
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: describe an env var a binary reads outside the config registry (pure)
type ProcessEnvVar struct {
	// Name is the exact variable name or, for a Pattern, the documented
	// shape with the operator-supplied part in angle brackets, e.g.
	// "TMI_SECRET_<KEY>"; the text before the first '<' is the prefix the
	// code scans for.
	Name string
	// Binary names the reader: "server" or "workers" (chunkembed,
	// extractor, worker-probe, component-controller).
	Binary string
	// Purpose is the one-line operator-facing description. No angle
	// brackets, backticks or pipes — it is rendered into a Markdown table.
	Purpose string
	// Secret marks a value that must never be logged or echoed.
	Secret bool
	// Pattern marks Name as a prefix pattern rather than a fixed name.
	Pattern bool
}

// processEnvVars is the hand-maintained inventory from #810. Fixed names
// are grouped by Binary in first-appearance order when rendered; patterns
// render in their own table.
var processEnvVars = []ProcessEnvVar{
	// --- server ---
	{Name: "TMI_ADMIN_PROVIDER", Binary: "server", Purpose: "Bootstrap administrator: identity provider. Setting it appends one entry to administrators at load time (internal/config)"},
	{Name: "TMI_ADMIN_PROVIDER_ID", Binary: "server", Purpose: "Bootstrap administrator: the subject's provider id"},
	{Name: "TMI_ADMIN_SUBJECT_TYPE", Binary: "server", Purpose: "Bootstrap administrator: subject type, user or group (default user)"},
	{Name: "TMI_ADMIN_EMAIL", Binary: "server", Purpose: "Bootstrap administrator: email address"},
	{Name: "TMI_ADMIN_GROUP_NAME", Binary: "server", Purpose: "Bootstrap administrator: group name when the subject type is group"},
	{Name: "TMI_JWT_KEY_ID", Binary: "server", Purpose: "JWKS key id; falls back to JWT_KEY_ID, then 1. Read by auth/config.go rather than internal/config"},
	{Name: "TMI_CLOUD_LOG_ENABLED", Binary: "server", Purpose: "Enable the cloud log writer when set to true"},
	{Name: "TMI_CLOUD_LOG_PROVIDER", Binary: "server", Purpose: "Cloud log provider; only oci is supported"},
	{Name: "TMI_CLOUD_LOG_LEVEL", Binary: "server", Purpose: "Minimum log level forwarded to the cloud log writer"},
	{Name: "TMI_OCI_LOG_ID", Binary: "server", Purpose: "OCI Logging log OCID the cloud log writer sends to"},
	{Name: "TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING", Binary: "server", Purpose: "Test-only: force auth-flow rate limiting on. Honoured only when TMI_BUILD_MODE is test"},
	// SSRF overrides: cmd/server/main.go buildURIValidator reads these with
	// os.Getenv at startup, on top of the ssrf.* config settings.
	{Name: "TMI_SSRF_ISSUE_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.issue_uri.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_ISSUE_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.issue_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_DOCUMENT_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.document_uri.allowlist setting (also used for content OAuth); read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_DOCUMENT_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.document_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_REPOSITORY_URI_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.repository_uri.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_REPOSITORY_URI_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.repository_uri.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_TIMMY_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.timmy.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_TIMMY_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.timmy.schemes setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_WEBHOOK_ALLOWLIST", Binary: "server", Purpose: "Env override of the ssrf.webhook.allowlist setting; read by cmd/server buildURIValidator at startup"},
	{Name: "TMI_SSRF_WEBHOOK_SCHEMES", Binary: "server", Purpose: "Env override of the ssrf.webhook.schemes setting; read by cmd/server buildURIValidator at startup"},

	// --- workers: chunkembed, extractor, worker-probe, component-controller ---
	{Name: "TMI_NATS_URL", Binary: "workers", Purpose: "NATS endpoint. Required by every worker; also read by the server (extraction wiring) and component-controller (JetStream provisioning)"},
	{Name: "TMI_NATS_CREDS", Binary: "workers", Purpose: "Path to a NATS credentials file used when connecting (the file is secret; the path is not)"},
	{Name: "TMI_COMPONENT_NAME", Binary: "workers", Purpose: "This worker's TMIComponent name; required by chunkembed, extractor and worker-probe"},
	{Name: "TMI_HEARTBEAT_INTERVAL", Binary: "workers", Purpose: "Worker heartbeat period as a Go duration (chunkembed, extractor)"},
	{Name: "TMI_JOB_ACK_WAIT", Binary: "workers", Purpose: "JetStream ack wait per job as a Go duration (chunkembed, extractor); also a TMIComponent spec.config key read by component-controller"},
	{Name: "TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET", Binary: "workers", Purpose: "Extraction wall-clock cap read directly by the extractor worker; the same name is a registry setting for the server"},
	{Name: "TMI_EMBEDDING_MODEL", Binary: "workers", Purpose: "Embedding model name (chunkembed)"},
	{Name: "TMI_EMBEDDING_BASE_URL", Binary: "workers", Purpose: "Embedding API base URL (chunkembed)"},
	{Name: "TMI_EMBEDDING_API_KEY", Binary: "workers", Purpose: "Embedding API key (chunkembed)", Secret: true},
	{Name: "TMI_WORKER_NATS_URL", Binary: "workers", Purpose: "NATS URL for the worker bootstrap config; required (internal/config/bootstrap)"},
	{Name: "TMI_WORKER_LOG_LEVEL", Binary: "workers", Purpose: "Worker log level (internal/config/bootstrap)"},
	{Name: "TMI_WORKER_HEARTBEAT_SUBJECT", Binary: "workers", Purpose: "NATS subject worker heartbeats are published to (internal/config/bootstrap)"},

	// --- prefix patterns: the operator supplies the part in angle brackets ---
	{Name: "TMI_SECRET_<KEY>", Binary: "server", Pattern: true, Secret: true, Purpose: "Environment secrets provider: logical secret key, upper-cased, e.g. TMI_SECRET_JWT_SECRET or TMI_SECRET_SETTINGS_ENCRYPTION_KEY. Every value is a secret"},
	{Name: "TMI_WORKER_SECRET_MOUNT_<NAME>", Binary: "workers", Pattern: true, Purpose: "Filesystem path to a mounted secret file, exposed to the worker under the logical name, e.g. TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY"},
	{Name: "TMI_CONTENT_OAUTH_PROVIDERS_<ID>_<FIELD>", Binary: "server", Pattern: true, Purpose: "Per-provider content OAuth config, discovered from the _ENABLED field. FIELD is one of CLIENT_ID, CLIENT_SECRET (secret), AUTH_URL, TOKEN_URL, USERINFO_URL, REVOCATION_URL, REQUIRED_SCOPES"},
	{Name: "SAML_PROVIDERS_<ID>_<FIELD>", Binary: "server", Pattern: true, Purpose: "Per-provider SAML config, discovered from the _ENABLED field; note there is no TMI_ prefix. FIELD is a SAMLProviderConfig yaml key upper-cased, e.g. ENTITY_ID, ACS_URL, SP_PRIVATE_KEY (secret), IDP_METADATA_B64XML (secret)"},
}

// ProcessEnvVars returns a copy of the process-environment inventory, so
// callers cannot mutate it.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: list a defensive copy of the process-environment env var inventory (pure)
func ProcessEnvVars() []ProcessEnvVar {
	out := make([]ProcessEnvVar, len(processEnvVars))
	copy(out, processEnvVars)
	return out
}
```

If `make lint` later reports gosec G101 on this file, add `// #nosec G101 -- env-var names, not credentials` on its own line directly above `var processEnvVars` (use `#nosec`, not `//nolint:gosec`; CI runs standalone gosec which ignores nolint).

- [ ] **Step 4: Run the table tests**

Run: `make test-unit name=TestProcessEnvVars count1=true`
Expected: PASS (both).

- [ ] **Step 5: Render the section**

In `internal/config/reference_gen.go`, in `GenerateReferenceMarkdown`, replace

```go
	for _, s := range operational {
		b.WriteString(operationalRow(s))
	}

	return []byte(b.String()), nil
```

with

```go
	for _, s := range operational {
		b.WriteString(operationalRow(s))
	}

	b.WriteString(processEnvSection())

	return []byte(b.String()), nil
```

and add, after `precedenceSection`:

```go
// processEnvSection renders the hand-maintained ProcessEnvVars inventory as
// the "Process environment" section: fixed names grouped by binary in
// first-appearance order, then the prefix patterns in their own table.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: render env vars read outside the config registry as Markdown tables grouped by binary (pure)
func processEnvSection() string {
	var order []string
	byBinary := map[string][]ProcessEnvVar{}
	var patterns []ProcessEnvVar
	for _, p := range ProcessEnvVars() {
		if p.Pattern {
			patterns = append(patterns, p)
			continue
		}
		if _, seen := byBinary[p.Binary]; !seen {
			order = append(order, p.Binary)
		}
		byBinary[p.Binary] = append(byBinary[p.Binary], p)
	}

	var b strings.Builder
	b.WriteString("\n## Process environment\n\n")
	b.WriteString("Environment variables a TMI binary reads directly from its process environment, " +
		"bypassing the config file, the settings registry above and the database. They have no " +
		"dotted key and cannot be set through `/admin/settings`; they are listed here because this " +
		"file is the complete `TMI_*` allowlist.\n")
	for _, bin := range order {
		fmt.Fprintf(&b, "\n### %s\n\n", bin)
		b.WriteString("| Env var | Purpose | Secret |\n")
		b.WriteString("|---------|---------|--------|\n")
		for _, p := range byBinary[bin] {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", p.Name, sanitizeCell(p.Purpose), yesNo(p.Secret))
		}
	}
	b.WriteString("\n### Prefix patterns\n\n")
	b.WriteString("The operator supplies the part in angle brackets.\n\n")
	b.WriteString("| Pattern | Binary | Purpose | Secret |\n")
	b.WriteString("|---------|--------|---------|--------|\n")
	for _, p := range patterns {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", p.Name, p.Binary, sanitizeCell(p.Purpose), yesNo(p.Secret))
	}
	return b.String()
}
```

- [ ] **Step 6: Regenerate and run the reference tests**

Run: `make generate-config-docs`
Run: `make test-unit name=TestGenerateReferenceMarkdown count1=true`
Run: `make test-unit name=TestConfigReferenceFile_MatchesRegistry count1=true`
Expected: all PASS. `tail -50 config-reference.md` shows the new section with `### server` (21 rows), `### workers` (12 rows), `### Prefix patterns` (4 rows). The `Env var` cells and the `Pattern` cells are backticked, so `<KEY>` survives rendering.

Also confirm the example file did not change (it still uses the seeding projection by design): `make generate-config-example` then `git diff --stat config-example.yml` should show only the timestamp line, if anything. Do not stage it.

- [ ] **Step 7: Lint and commit**

Run: `make lint`
Expected: clean.

```bash
git add internal/config/process_env.go internal/config/process_env_test.go \
        internal/config/reference_gen.go internal/config/reference_gen_test.go config-reference.md
git commit -m "feat(config): document process-environment env vars in the config reference

The SSRF overrides cmd/server reads itself, the workers, the admin
bootstrap, cloud logging and the secret prefixes read TMI_* variables
straight from the process environment, outside Config and the
registry, so no generator could see them. Declare them in a
hand-maintained ProcessEnvVar table and render it as a Process
environment section grouped by binary, with the prefix patterns and
the non-TMI SAML_PROVIDERS_<ID>_<FIELD> pattern.

Refs #810

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 3: Repo-wide `TMI_*` allowlist gate, and the dead `TMI_SAML_*` tags it catches

**Files:**
- Modify: `internal/config/process_env_test.go` (add the gate and `tmiTokenExclusions`)
- Modify: `internal/config/config.go:226-253` (`SAMLProviderConfig`)

**Interfaces:**
- Consumes: `AllSettingDefs()`, `ProcessEnvVars()` and the `Name`-before-first-`<` prefix convention from Task 2.
- Produces: nothing new; the gate is the deliverable.

Why one task: the 20 `env:"TMI_SAML_*"` tags on `SAMLProviderConfig` are dead code. `overrideStructWithEnv` recurses into struct-typed fields and special-cases the map field named `Providers`, but `SAMLProviderConfig` exists only as that map's value type, so its `env` tags are never consulted; the real per-provider reads are `overrideSAMLProviders` (`internal/config/config.go`, `SAML_PROVIDERS_<ID>_<FIELD>`, discovered with `envutil.DiscoverProviders("SAML_PROVIDERS_", "_ENABLED")`). Those 20 names are exactly what the new gate reports on its first run, which makes the gate its own red test and the tag deletion the change that turns it green. No runtime behaviour changes.

- [ ] **Step 1: Write the failing gate**

Append to `internal/config/process_env_test.go` (extend the import block to `"io/fs"`, `"os"`, `"path/filepath"`, `"regexp"`, `"sort"`, `"strings"`, `"testing"`):

```go
// tmiTokenExclusions lists TMI_-prefixed tokens that occur in Go source but
// are not environment variables. Every entry carries its justification;
// adding one is a deliberate act.
var tmiTokenExclusions = map[string]string{
	"TMI_DLQ":                    "JetStream stream name (internal/worker/names.go)",
	"TMI_DLQ_ADVISORY":           "JetStream stream name (internal/worker/names.go)",
	"TMI_RESULTS":                "JetStream stream name (internal/worker/names.go)",
	"TMI_PAYLOADS":               "JetStream KV bucket name (internal/worker/names.go)",
	"TMI_SCHEMA_VERSIONS":        "Oracle upper-folded table name (internal/dbschema/schema_version.go)",
	"TMI_THREAT_MODEL_ALIAS_SEQ": "Oracle sequence name (internal/dbschema/alias_sequence.go)",
	"TMI_TMI_EXTRACTOR":          "derived JetStream consumer name in a doc comment (internal/worker/names.go)",
	"TMI_CHUNK_EMBED_CONSUMER":   "derived JetStream consumer name in a doc comment (internal/worker/names.go)",
	"TMI_SERVER_URL":             "read only by the integration-test framework under test/integration/, never by a shipped binary",
	"TMI_CONTENT_EXTRACTORS_":    "bare prefix in a doc comment (cmd/extractor/main.go); every concrete TMI_CONTENT_EXTRACTORS_* name is a registry EnvVar",
	// The prefix argument cmd/server/main.go passes to buildURIValidator,
	// which appends _ALLOWLIST / _SCHEMES; the ten resulting names are
	// ProcessEnvVars.
	"TMI_SSRF_ISSUE_URI":      "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_DOCUMENT_URI":   "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_REPOSITORY_URI": "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_TIMMY":          "buildURIValidator env-var prefix (cmd/server/main.go)",
	"TMI_SSRF_WEBHOOK":        "buildURIValidator env-var prefix (cmd/server/main.go)",
}

// TestRepoTMIEnvTokens_AreAllDocumented is the allowlist gate for #810.
// Every TMI_[A-Z0-9_]+ token in the repository's non-test Go source must be
// a registry EnvVar, a ProcessEnvVar name, an instance of a ProcessEnvVar
// prefix pattern, or a justified tmiTokenExclusions entry. Anything else is
// an env var — or a new non-env-var naming collision — that
// config-reference.md, the TMI_* allowlist, does not know about.
func TestRepoTMIEnvTokens_AreAllDocumented(t *testing.T) {
	known := map[string]bool{}
	for _, d := range AllSettingDefs() {
		if d.EnvVar != "" {
			known[d.EnvVar] = true
		}
	}
	var prefixes []string
	for _, p := range ProcessEnvVars() {
		if p.Pattern {
			prefixes = append(prefixes, p.Name[:strings.Index(p.Name, "<")])
			continue
		}
		known[p.Name] = true
	}
	documented := func(tok string) bool {
		if known[tok] || tmiTokenExclusions[tok] != "" {
			return true
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(tok, pre) {
				return true
			}
		}
		return false
	}

	const root = "../.." // this package is internal/config
	tokenRe := regexp.MustCompile(`TMI_[A-Z0-9_]+`)
	offenders := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 -- walking the repository's own source tree
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, tok := range tokenRe.FindAllString(string(src), -1) {
			if !documented(tok) {
				offenders[tok+"  ("+rel+")"] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}

	var list []string
	for o := range offenders {
		list = append(list, o)
	}
	sort.Strings(list)
	if len(list) > 0 {
		t.Errorf("%d TMI_* tokens in Go source that config-reference.md does not document; "+
			"add a SettingDef EnvVar, a ProcessEnvVar, or a justified tmiTokenExclusions entry:\n  %s",
			len(list), strings.Join(list, "\n  "))
	}
}
```

- [ ] **Step 2: Run it to verify it fails on exactly the dead SAML tags**

Run: `make test-unit name=TestRepoTMIEnvTokens_AreAllDocumented count1=true`
Expected: FAIL listing 20 tokens, all `TMI_SAML_*` in `internal/config/config.go` (`TMI_SAML_ACS_URL`, `TMI_SAML_ALLOW_IDP_INITIATED`, `TMI_SAML_EMAIL_ATTRIBUTE`, `TMI_SAML_ENTITY_ID`, `TMI_SAML_FAMILY_NAME_ATTRIBUTE`, `TMI_SAML_FORCE_AUTHN`, `TMI_SAML_GIVEN_NAME_ATTRIBUTE`, `TMI_SAML_GROUPS_ATTRIBUTE`, `TMI_SAML_IDP_METADATA_B64XML`, `TMI_SAML_IDP_METADATA_URL`, `TMI_SAML_METADATA_URL`, `TMI_SAML_METADATA_XML`, `TMI_SAML_NAME_ATTRIBUTE`, `TMI_SAML_NAME_ID_ATTRIBUTE`, `TMI_SAML_SIGN_REQUESTS`, `TMI_SAML_SLO_URL`, `TMI_SAML_SP_CERTIFICATE`, `TMI_SAML_SP_CERTIFICATE_PATH`, `TMI_SAML_SP_PRIVATE_KEY`, `TMI_SAML_SP_PRIVATE_KEY_PATH`). `TMI_SAML_ENABLED` is a registry EnvVar and must not be listed; the `TMI_SSRF_*` tokens in `cmd/server/main.go` must not be listed either (the five prefixes are excluded, `TMI_SSRF_WEBHOOK_ALLOWLIST` in a comment there and in `test/integration/framework/webhook_receiver.go` is a table name). If anything else is listed, it is a token this plan's inventory missed: classify it (registry EnvVar, `ProcessEnvVar`, or a justified exclusion) rather than widening the regex.

- [ ] **Step 3: Delete the dead tags**

In `internal/config/config.go`, replace lines 226-253 (`SAMLProviderConfig` from its comment line through the closing brace) with:

```go
// SAMLProviderConfig holds configuration for a SAML provider. It is only ever
// a value of SAMLConfig.Providers, which overrideStructWithEnv hands to
// overrideSAMLProviders; that reads SAML_PROVIDERS_<ID>_<FIELD> (no TMI_
// prefix), so env tags on these fields would never be consulted and there
// are none.
// SEM@78155d54490599e00095eb72b817575bb1e8da5b: configuration struct for a single SAML provider's SP/IdP metadata, keys, and attribute mappings (pure)
type SAMLProviderConfig struct {
	ID                  string `yaml:"id"`
	Name                string `yaml:"name"`
	Enabled             bool   `yaml:"enabled"`
	Icon                string `yaml:"icon"`
	EntityID            string `yaml:"entity_id"`
	MetadataURL         string `yaml:"metadata_url"`
	MetadataXML         string `yaml:"metadata_xml"`
	ACSURL              string `yaml:"acs_url"`
	SLOURL              string `yaml:"slo_url"`
	SPPrivateKey        string `yaml:"sp_private_key"`
	SPPrivateKeyPath    string `yaml:"sp_private_key_path"`
	SPCertificate       string `yaml:"sp_certificate"`
	SPCertificatePath   string `yaml:"sp_certificate_path"`
	IDPMetadataURL      string `yaml:"idp_metadata_url"`
	IDPMetadataB64XML   string `yaml:"idp_metadata_b64xml"` // Base64-encoded IdP metadata XML
	AllowIDPInitiated   bool   `yaml:"allow_idp_initiated"`
	ForceAuthn          bool   `yaml:"force_authn"`
	SignRequests        bool   `yaml:"sign_requests"`
	NameIDAttribute     string `yaml:"name_id_attribute"`
	EmailAttribute      string `yaml:"email_attribute"`
	NameAttribute       string `yaml:"name_attribute"`
	GivenNameAttribute  string `yaml:"given_name_attribute"`
	FamilyNameAttribute string `yaml:"family_name_attribute"`
	GroupsAttribute     string `yaml:"groups_attribute"`
}
```

`SAMLConfig.Enabled` keeps its `env:"TMI_SAML_ENABLED"` tag; it is bound directly on `SAMLConfig` and claimed by the `features.saml_enabled` def.

- [ ] **Step 4: Run the gate and the SAML tests to verify green**

Run: `make test-unit name=TestRepoTMIEnvTokens_AreAllDocumented count1=true`
Run: `make test-unit name=TestSettingDefs count1=true`
Run: `make test-unit name=SAML count1=true`
Expected: all PASS. (`envTaggedKeys` in the bijection test never walks into the map value type, so it is unaffected either way; the SAML tests prove `overrideSAMLProviders` still populates providers from `SAML_PROVIDERS_*`.)

- [ ] **Step 5: Lint, build, commit**

Run: `make lint`
Run: `make build-server`
Expected: clean; build succeeds.

```bash
git add internal/config/process_env_test.go internal/config/config.go
git commit -m "test(config): gate every TMI_* token in Go source against the config reference

Scan all non-test Go sources for TMI_[A-Z0-9_]+ and fail unless each
token is a registry EnvVar, a ProcessEnvVar, an instance of a declared
prefix pattern, or a justified non-env-var exclusion (JetStream and
Oracle object names, doc-comment fragments, the buildURIValidator
prefixes, the integration-test TMI_SERVER_URL). The gate's first run
reported the 20 env:\"TMI_SAML_*\" tags on SAMLProviderConfig, which are
dead: it exists only as a map value, which overrideStructWithEnv never
tag-walks, and the real reads are SAML_PROVIDERS_<ID>_<FIELD> in
overrideSAMLProviders. Deleted.

Fixes #810

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv"
```

---

### Task 4: Final regeneration and gates

**Files:**
- Possibly modify: `config-reference.md` (only if a regeneration differs beyond the timestamp)

- [ ] **Step 1: Regenerate both artifacts and check for drift**

Run: `make generate-config-docs`
Run: `make generate-config-example`
Run: `git diff --stat`

Expected: only the two `GENERATED by ... on <timestamp>` lines differ. If `config-reference.md` differs beyond its timestamp, a task above skipped a regeneration: commit it as `docs(config): regenerate config-reference.md` with the standard trailer. Never commit a timestamp-only change; run `git checkout -- config-reference.md config-example.yml` to drop it.

- [ ] **Step 2: Freshness and full unit suite**

Run: `make test-unit name=TestConfigReferenceFile_MatchesRegistry count1=true`
Run: `make test-unit name=TestConfigExampleFile_MatchesRegistry count1=true`
Run: `make test-unit`
Expected: all PASS.

- [ ] **Step 3: Lint and build one last time**

Run: `make lint`
Run: `make build-server`
Expected: clean; build succeeds.

- [ ] **Step 4: Confirm the branch state**

```bash
git status --short      # must be empty (untracked PROGRESS.md / HANDOFF.md aside)
git log --oneline main..HEAD
```

Expected: three commits (Tasks 1-3), the last of which carries `Fixes #810`. `rg -n 'env:"TMI_SAML_' internal/config/config.go` returns only `TMI_SAML_ENABLED`; `git diff main -- cmd/server/main.go` is empty (the SSRF reads were deliberately left alone).

Do not push or open the PR from this plan; that is the session's landing step (`git pull --rebase && git push`, then a PR titled `fix(config): document every TMI_* env var in the config reference (#810)` so the automatic version bump is a PATCH).

---

## Self-review

**Spec coverage.**
- Design point 1 (registry-driven reference, `d.Get(getDefaultConfig())`, empty rendered as placeholder, `OmitWhenEmpty`/skiplist/TLS filters no longer apply to docs, `GenerateExampleConfig` untouched, completeness test over the registry with no exception list) → Task 1.
- Design point 2 (SSRF consolidation) → dropped by human decision on 2026-09-04 (see the Spec amendment in the header). The ten `TMI_SSRF_*` names are documented as process-environment variables in Task 2, and the five `TMI_SSRF_<CLASS>` prefix strings `buildURIValidator` composes them from are justified gate exclusions in Task 3. No code under `cmd/server` or the SSRF structs/defs changes.
- Design point 3 (hand-maintained table rendered grouped by binary) → Task 2. Row count: server 21 (11 + the 10 SSRF overrides), workers 12, so 33 fixed rows = 32 names absent from the registry + the extractor's direct wall-clock read; patterns 4 (3 `TMI_` + `SAML_PROVIDERS_<ID>_<FIELD>`).
- Design point 4 (gate over non-test Go source, subtracting registry, table, patterns and the exclusion list, failing with token and file) → Task 3. The exclusion list is the spec's "Excluded as not env vars" table minus the pattern instantiations and the `TMI_CONTENT_OAUTH_PROVIDERS_` fragment (both covered by prefix matching), plus the five `buildURIValidator` prefixes that exist because design point 2 was dropped; 15 entries in all. Accounting for the 64 non-registry `TMI_*` tokens in today's non-test Go source: 20 SAML tags deleted, 5 SSRF prefixes excluded, 1 SSRF name in comments (`TMI_SSRF_WEBHOOK_ALLOWLIST`, a table name), 6 tokens covered by pattern prefixes, 10 spec exclusions, 22 literal table names. The other 9 SSRF table names never appear as literal tokens (`buildURIValidator` composes them), so they add rows to the reference without adding tokens to the gate.
- Design point 5 (delete the 20 dead `TMI_SAML_*` tags, `overrideSAMLProviders` is the real reader) → Task 3.
- Design point 6 (`make generate-config-docs`, freshness tests, unit tests; no DB change, no Oracle review) → every task's regenerate step plus Task 4.
- Issue-body items: the 31 empty-default vars, the 2 TLS vars and the 2 `ExpectedMigratableKeysSkipped` vars are all covered by `referenceSettings` applying none of the three filters; `TestGenerateReferenceMarkdown_DocumentsEveryConfigDeliveredDef` pins one of each class.

**Placeholder scan.** Every code step carries the full code; the only conditional instruction is the gosec `#nosec` note in Task 2 Step 3, which names the exact line to add and where.

**Type consistency.** `referenceSettings(cfg *Config) []MigratableSetting` is defined in Task 1 and used in Task 1's secret test; `ProcessEnvVar{Name, Binary, Purpose string; Secret, Pattern bool}` and `ProcessEnvVars() []ProcessEnvVar` are used identically in Task 2's renderer and tests and Task 3's gate; the `Name`-before-first-`<` prefix convention is asserted by `TestProcessEnvVars_WellFormed` and consumed by the gate; `tmiTokenExclusions map[string]string` is defined and consumed in the same test file.
