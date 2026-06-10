# Configuration Documentation Revamp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Revamp all TMI configuration wiki documentation to reflect the post-#415 two-category config model, optimized for production operators and local developers, with a registry-generated per-key reference that cannot drift.

**Architecture:** Two work streams. (1) Main repo (`dev/1.4.0`): add an `EnvVar` field to `MigratableSetting`, build a `cmd/genconfigdocs` generator + make target that emits a Markdown reference from the classification registry, drift-guarded by a unit test. (2) Wiki repo (`/Users/efitz/Projects/tmi.wiki`): a new `Configuration-Model` concept hub, three task guides (local dev, production bootstrap, runtime settings), a generated `Configuration-Reference`, two redirect stubs, and sidebar edits.

**Tech Stack:** Go (registry + generator), Make + a uv-driven Python build script, Markdown (GitHub wiki), `make test-unit` / `make lint` / `make build-server` gates, `oracle-db-admin` subagent, `security-regression` skill.

**Spec:** `docs/superpowers/specs/2026-05-21-config-documentation-revamp-design.md`

---

## File Structure

**Main repo (`/Users/efitz/Projects/tmi`):**
- Modify: `internal/config/migratable_settings.go` — add `EnvVar` field to `MigratableSetting`; populate at literal sites.
- Create: `internal/config/reference_gen.go` — `GenerateReferenceMarkdown()`.
- Create: `internal/config/reference_gen_test.go` — content + drift tests.
- Create: `cmd/genconfigdocs/main.go` — generator binary.
- Modify: `scripts/build-server.py` — add `genconfigdocs` to `COMPONENTS`.
- Modify: `Makefile` — add `build-genconfigdocs` and `generate-config-docs` targets and their `.PHONY` entries.
- Create: `config-reference.md` (repo root) — the generated artifact, committed.

**Wiki repo (`/Users/efitz/Projects/tmi.wiki`):**
- Create: `Configuration-Model.md`
- Create: `Configuring-Local-Development.md`
- Create: `Bootstrapping-Production.md`
- Create: `Managing-Operational-Settings.md`
- Rewrite: `Configuration-Reference.md` (from `config-reference.md`)
- Rewrite as stub: `Configuration-Management.md`, `Config-Migration-Guide.md`
- Modify: `_Sidebar.md`
- Modify: `Content-Extractors-Limits-and-Overrides.md` (cross-link only)

---

## STREAM 1 — Main repo: registry generator

### Task 1: Add `EnvVar` field to `MigratableSetting`

**Files:**
- Modify: `internal/config/migratable_settings.go`
- Test: `internal/config/reference_gen_test.go` (created in Task 3; the field is exercised there)

- [ ] **Step 1: Add the field to the struct**

In `internal/config/migratable_settings.go`, change the struct (currently lines 10–18):

```go
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
	Secret      bool   // true = mask value in API responses (kept for back-compat; mirrors Class.Secret)
	Source      string // "config" or "environment"
	EnvVar      string // TMI_* environment variable that overrides this setting ("" if none)
	Class       ConfigClass
}
```

- [ ] **Step 2: Populate `EnvVar` at every literal that calls `settingSource`**

Each literal currently has `Source: settingSource("TMI_FOO")`. Add `EnvVar: "TMI_FOO"` to the same literal, reusing the exact string already passed to `settingSource`. Example transform:

```go
// before
{Key: "server.port", Value: c.Server.Port, Type: "string", Description: "HTTP server port", Source: settingSource("TMI_SERVER_PORT")},
// after
{Key: "server.port", Value: c.Server.Port, Type: "string", Description: "HTTP server port", Source: settingSource("TMI_SERVER_PORT"), EnvVar: "TMI_SERVER_PORT"},
```

Apply across every `settingSource("TMI_...")` call site in the file. For literals built with no `settingSource` call (e.g. `administrators` uses `Source: "config"`), leave `EnvVar` unset (empty string). Do not invent env var names — only copy the literal already present.

- [ ] **Step 3: Build to verify it compiles**

Run: `make build-server`
Expected: `[SUCCESS] Server binary built: bin/tmiserver`

- [ ] **Step 4: Run config unit tests**

Run: `make test-unit name=TestMigratableSettings_CoverEveryConfigField count1=true`
Expected: `Tests: 1 passed, 0 failed`. (Adding a field does not change classification coverage.)

- [ ] **Step 5: Commit**

```bash
git add internal/config/migratable_settings.go
git commit -m "feat(config): add EnvVar field to MigratableSetting

Records the TMI_* override variable as first-class registry data so the
generated config reference can show a correct per-key Env var column. The
key->env-var mapping is irregular and cannot be reliably derived, so the
name is copied verbatim from the existing settingSource() argument.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Implement `GenerateReferenceMarkdown()`

**Files:**
- Create: `internal/config/reference_gen.go`

- [ ] **Step 1: Write the generator**

Create `internal/config/reference_gen.go`:

```go
package config

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// GenerateReferenceMarkdown produces the per-key configuration reference from
// the classification registry. It emits two tables — bootstrap and operational
// — so the wiki Configuration-Reference page is generated, not hand-maintained.
// Secret defaults are shown as vault:// placeholders, never real values.
func GenerateReferenceMarkdown() ([]byte, error) {
	cfg := getDefaultConfig()
	cfg.Server.TLSSubjectName = "localhost" // deterministic — must not embed the build host's name
	all := cfg.GetMigratableSettings()

	var bootstrap, operational []MigratableSetting
	for _, s := range all {
		switch s.Class.Category {
		case CategoryBootstrap:
			bootstrap = append(bootstrap, s)
		case CategoryOperational:
			operational = append(operational, s)
		}
	}
	sort.Slice(bootstrap, func(i, j int) bool { return bootstrap[i].Key < bootstrap[j].Key })
	sort.Slice(operational, func(i, j int) bool { return operational[i].Key < operational[j].Key })

	var b strings.Builder
	fmt.Fprintf(&b,
		"# Configuration Reference\n\n"+
			"<!-- GENERATED by `make generate-config-docs` on %s — do not edit by hand. -->\n\n"+
			"Every TMI configuration key, grouped by category. See "+
			"[[Configuration-Model]] for what the categories and columns mean.\n\n",
		time.Now().UTC().Format(time.RFC3339))

	b.WriteString("## Bootstrap settings\n\n")
	b.WriteString("File/env only, read once at startup. Cannot come from the database.\n\n")
	b.WriteString("| Key | Env var | Type | Default | Required | Secret | Description |\n")
	b.WriteString("|-----|---------|------|---------|----------|--------|-------------|\n")
	for _, s := range bootstrap {
		b.WriteString(bootstrapRow(s))
	}

	b.WriteString("\n## Operational settings\n\n")
	b.WriteString("DB-backed, seeded from defaults on first run, editable at runtime via `/admin/settings`.\n\n")
	b.WriteString("| Key | Type | Default | Mutability | Visibility | Secret | Description |\n")
	b.WriteString("|-----|------|---------|------------|------------|--------|-------------|\n")
	for _, s := range operational {
		b.WriteString(operationalRow(s))
	}

	return []byte(b.String()), nil
}

func bootstrapRow(s MigratableSetting) string {
	return fmt.Sprintf("| `%s` | %s | %s | %s | %s | %s | %s |\n",
		s.Key, codeOrDash(s.EnvVar), s.Type, defaultCell(s),
		yesNo(s.Class.Required), yesNo(s.Class.Secret), escapePipes(s.Description))
}

func operationalRow(s MigratableSetting) string {
	return fmt.Sprintf("| `%s` | %s | %s | %s | %s | %s | %s |\n",
		s.Key, s.Type, defaultCell(s), mutabilityName(s.Class.Mutability),
		visibilityName(s.Class.Visibility), yesNo(s.Class.Secret), escapePipes(s.Description))
}

func defaultCell(s MigratableSetting) string {
	if s.Class.Secret {
		return "_(secret)_"
	}
	if s.Value == "" {
		return "_(none)_"
	}
	return "`" + escapePipes(s.Value) + "`"
}

func codeOrDash(v string) string {
	if v == "" {
		return "—"
	}
	return "`" + v + "`"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

func mutabilityName(m Mutability) string {
	if m == MutabilityHot {
		return "hot"
	}
	return "static"
}

func visibilityName(v Visibility) string {
	switch v {
	case VisibilityPublic:
		return "public"
	case VisibilityAdminOnly:
		return "admin"
	default:
		return "internal"
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `make build-server`
Expected: `[SUCCESS] Server binary built: bin/tmiserver`

- [ ] **Step 3: Commit**

```bash
git add internal/config/reference_gen.go
git commit -m "feat(config): generate per-key config reference from the registry

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Content tests for the generator (TDD)

**Files:**
- Create: `internal/config/reference_gen_test.go`

- [ ] **Step 1: Write the content tests**

Create `internal/config/reference_gen_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestGenerateReferenceMarkdown_HasBothCategoryTables(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	for _, want := range []string{"## Bootstrap settings", "## Operational settings"} {
		if !strings.Contains(s, want) {
			t.Errorf("reference missing section %q", want)
		}
	}
}

func TestGenerateReferenceMarkdown_BootstrapKeyHasEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	// server.port is bootstrap and overridable by TMI_SERVER_PORT.
	if !strings.Contains(string(out), "`TMI_SERVER_PORT`") {
		t.Error("reference missing env var TMI_SERVER_PORT for server.port")
	}
}

func TestGenerateReferenceMarkdown_NeverLeaksSecretDefault(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	// Operational secret timmy.* api keys must render as _(secret)_, never a value.
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "api_key") && strings.Contains(line, "| yes |") {
			if !strings.Contains(line, "_(secret)_") {
				t.Errorf("secret row appears to expose a default: %q", line)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `make test-unit name=TestGenerateReferenceMarkdown_HasBothCategoryTables count1=true`
Expected: `Tests: 1 passed, 0 failed`. (Implementation from Task 2 already satisfies them; this pins the behavior.)

- [ ] **Step 3: Commit**

```bash
git add internal/config/reference_gen_test.go
git commit -m "test(config): pin content + secret-masking of generated reference

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Generator binary `cmd/genconfigdocs`

**Files:**
- Create: `cmd/genconfigdocs/main.go`

- [ ] **Step 1: Write the binary** (mirrors `cmd/genconfig/main.go`)

Create `cmd/genconfigdocs/main.go`:

```go
// Command genconfigdocs writes config-reference.md from the classification registry.
package main

import (
	"os"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

func main() {
	logger := slogging.Get()
	out, err := config.GenerateReferenceMarkdown()
	if err != nil {
		logger.Error("genconfigdocs: %v", err)
		os.Exit(1)
	}
	if err := os.WriteFile("config-reference.md", out, 0o644); err != nil { //nolint:gosec // doc artifact, not secret
		logger.Error("genconfigdocs: write config-reference.md: %v", err)
		os.Exit(1)
	}
	logger.Info("genconfigdocs: wrote config-reference.md")
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/genconfigdocs/` (compile check only; the make target is wired in Task 5)
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/genconfigdocs/main.go
git commit -m "feat(config): add genconfigdocs binary for the config reference

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Wire build script + Makefile targets

**Files:**
- Modify: `scripts/build-server.py`
- Modify: `Makefile`

- [ ] **Step 1: Add `genconfigdocs` to the build script COMPONENTS dict**

In `scripts/build-server.py`, in the `COMPONENTS` dict, after the `genconfig` entry add:

```python
    "genconfigdocs": {
        "output": "bin/genconfigdocs",
        "package": "github.com/ericfitz/tmi/cmd/genconfigdocs",
        "tags": [],
        "ldflags": False,
    },
```

Also update the docstring/`--component` help line that lists components to include `genconfigdocs`.

- [ ] **Step 2: Add the Makefile targets**

In `Makefile`, immediately after the `generate-config-example` stanza, add:

```makefile
build-genconfigdocs:  ## Build the config-reference.md generator
	@uv run scripts/build-server.py --component genconfigdocs

generate-config-docs: build-genconfigdocs  ## Regenerate config-reference.md from the classification registry
	@./bin/genconfigdocs
```

In the same `Makefile`, find the `.PHONY` line that lists `build-genconfig generate-config-example` (the `.PHONY` block near line 78) and add `build-genconfigdocs generate-config-docs` to it.

- [ ] **Step 3: Generate the artifact**

Run: `make generate-config-docs`
Expected: `[SUCCESS]` build line then `genconfigdocs: wrote config-reference.md`. A new `config-reference.md` exists at the repo root.

- [ ] **Step 4: Sanity-check the artifact**

Run: `head -40 config-reference.md`
Expected: the `# Configuration Reference` heading, a `GENERATED by` comment, a `## Bootstrap settings` table containing `server.port` with `TMI_SERVER_PORT`, and a `## Operational settings` table.

- [ ] **Step 5: Commit**

```bash
git add scripts/build-server.py Makefile config-reference.md
git commit -m "build(config): add generate-config-docs target + generated reference

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Drift guard test

**Files:**
- Modify: `internal/config/reference_gen_test.go`

- [ ] **Step 1: Add the drift test** (mirrors `TestConfigExampleFile_MatchesRegistry`)

Append to `internal/config/reference_gen_test.go`:

```go
func TestConfigReferenceFile_MatchesRegistry(t *testing.T) {
	generated, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	onDisk, err := os.ReadFile("../../config-reference.md")
	if err != nil {
		t.Fatalf("read config-reference.md: %v", err)
	}
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
		t.Error("config-reference.md is stale — run `make generate-config-docs`")
	}
}
```

Add `"os"` to the import block at the top of the file (currently `"strings"` and `"testing"`).

- [ ] **Step 2: Run the drift test**

Run: `make test-unit name=TestConfigReferenceFile_MatchesRegistry count1=true`
Expected: `Tests: 1 passed, 0 failed` (the committed artifact matches a fresh generation).

- [ ] **Step 3: Commit**

```bash
git add internal/config/reference_gen_test.go
git commit -m "test(config): fail the build when config-reference.md is stale

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Stream-1 gates (lint, full unit tests, Oracle review, security regression)

**Files:** none (verification only)

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: `[SUCCESS] Lint passed`. Fix any issue before proceeding.

- [ ] **Step 2: Full unit suite**

Run: `make test-unit`
Expected: `[SUCCESS] All unit tests passed`.

- [ ] **Step 3: Oracle DB compatibility review**

Invoke the `oracle-db-admin` skill and dispatch the subagent with: the `EnvVar` field addition to `MigratableSetting` (`internal/config/migratable_settings.go`) and the new generator (additive metadata, no schema/SQL/FK/migration change; the field is never written to the DB). Address any BLOCKING finding; fold APPROVED WITH NOTES items in or file follow-ups.

- [ ] **Step 4: Security regression scan**

Invoke the `security-regression` skill. Scan the staged/branch changes (config secret-adjacent code). Expected: `VERDICT: PASS`. If REVIEW/BLOCK, stop and report to the user.

- [ ] **Step 5: Push Stream 1**

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin/dev/1.4.0"
```

---

## STREAM 2 — Wiki repo: page revamp

All wiki tasks operate in `/Users/efitz/Projects/tmi.wiki` (a separate git repo). Each page must be verified against current code before writing (the accuracy pass). Where a claim is not directly verified in this plan, run the named command first.

### Task 8: `Configuration-Model.md` (concept hub)

**Files:**
- Create: `/Users/efitz/Projects/tmi-wiki/Configuration-Model.md`

- [ ] **Step 1: Verify the model claims against code**

Run (from `/Users/efitz/Projects/tmi`):
- `rg -n 'CategoryBootstrap|CategoryOperational' internal/config/classification.go` — confirm the two categories.
- `rg -n 'IsSecretReference|vault://|env://|file://' internal/config/secret_reference.go` — confirm the three reference schemes.
- `rg -n 'timmy\.' internal/config/classification_registry.go` — confirm all `timmy.*` are operational (used for the worked example).

- [ ] **Step 2: Write the page**

Create `Configuration-Model.md` with these sections (prose, scaled to the spec's "Configuration-Model" content list):
1. **Two categories** — Bootstrap (file/env only, startup, can't be DB-served: DB connection, JWT secret, server listener, logging, secrets provider) vs Operational (DB-backed, seeded on first run, runtime-editable: OAuth, timeouts, feature flags, Timmy, operator info).
2. **Per-category cascade** — Bootstrap: defaults → `config-*.yml` → `TMI_*` env (env wins). Operational: registry defaults seed the DB on first run; thereafter DB authoritative, edited via `/admin/settings`.
3. **Secret references** — `vault://`, `env://`, `file://` resolved at startup per value; inline passes through; where each resolves from.
4. **Secrets are not all bootstrap (worked example: Timmy)** — bootstrap secrets (`database.url`, `auth.jwt.secret`, `database.redis.password`) vs operational secrets (all `timmy.*_api_key`, OAuth `client_secret`) stored encrypted in `system_settings`, managed via `/admin/settings`, rotated via `reencrypt`. Timmy is entirely operational; no `timmy:` block in bootstrap files.
5. **Hot vs static (Mutability)** — what a runtime edit does.
6. **Visibility** — internal/admin/public; public secret is a build failure.
7. **The config files** — 3 bootstrap-only files; DB backend by `database.url` scheme; `config-example.yml` generated; working files gitignored.
8. A simple ASCII/Mermaid diagram of where a value comes from at rest and at read time, per category.

Cross-link to `[[Configuring-Local-Development]]`, `[[Bootstrapping-Production]]`, `[[Managing-Operational-Settings]]`, `[[Configuration-Reference]]`.

- [ ] **Step 3: Verify links resolve to real page names**

Run: `ls /Users/efitz/Projects/tmi-wiki/*.md | xargs -n1 basename` and confirm every `[[...]]` target maps to a page that exists now or is created in this plan.

---

### Task 9: `Configuring-Local-Development.md`

**Files:**
- Create: `/Users/efitz/Projects/tmi-wiki/Configuring-Local-Development.md`

- [ ] **Step 1: Verify make targets + CLI behavior**

Run (from `/Users/efitz/Projects/tmi`):
- `rg -n 'start-dev|start-database|start-redis' Makefile | head` — confirm dev targets.
- `sed -n '70,95p' internal/config/cli.go` — confirm `--generate-config` help text and the "copy config-example.yml" workflow.

- [ ] **Step 2: Write the page**

Content per spec: copy `config-example.yml` → `config-development.yml` and populate placeholders; `make start-dev`; `TMI_*` overrides; OAuth stub flow with `login_hint` users (alice/bob/charlie); `logs/tmi.log`; first-run operational seeding in dev; inspecting/changing a setting locally (admin API or the `/db` skill). Cross-link to `[[Configuration-Model]]` and `[[Managing-Operational-Settings]]`.

---

### Task 10: `Bootstrapping-Production.md`

**Files:**
- Create: `/Users/efitz/Projects/tmi-wiki/Bootstrapping-Production.md`

- [ ] **Step 1: Verify the production claims against code**

Run (from `/Users/efitz/Projects/tmi`):
- `cat config-production.yml` — confirm the bootstrap-only skeleton + `vault://` placeholders.
- `rg -n 'resolveSecretReferences|VaultToken|NewProvider' cmd/server/main.go | head` — confirm the three-phase secret resolution order.
- `rg -n 'func.*SeedDefaults|DefaultOperationalSettings' api/settings_service.go` — confirm first-run seeding.
- `rg -n 'func LoadWorker|SecretMounts|NATSURL' internal/config/bootstrap/bootstrap.go` — confirm the worker bootstrap contract.

- [ ] **Step 2: Write the page**

Content per spec: bootstrap-only `config-production.yml`; required-at-startup validation (`ValidateRequired`); wiring secrets (`secrets.provider` env/aws/oci/vault, `vault://` refs, K8s mounts, three-phase order); first-run DB seeding + what to verify; migrating a pre-#415 deployment with `dbtool --import-legacy` and the warn-then-error window; platform specifics (OCI/ADB wallet + `oracle://`, Heroku, K8s + worker bootstrap contract); post-bootstrap checklist (admin user, TLS, `server.base_url`, required-settings validation). Cross-link to `[[Configuration-Model]]`, `[[Database-Setup]]`, `[[Managing-Operational-Settings]]`, `[[OCI-Container-Deployment]]`.

---

### Task 11: `Managing-Operational-Settings.md` (rewrite of Configuration-Management)

**Files:**
- Create: `/Users/efitz/Projects/tmi-wiki/Managing-Operational-Settings.md`

- [ ] **Step 1: Verify endpoints + dbtool flags**

Run (from `/Users/efitz/Projects/tmi`):
- `jq -r '.paths | keys[] | select(test("admin/settings"))' api-schema/tmi-openapi.json` — confirm `/admin/settings`, `/admin/settings/{key}`, `/admin/settings/reencrypt`.
- `sed -n '215,250p' cmd/dbtool/main.go` — confirm the exact dbtool flag set and help text.

- [ ] **Step 2: Write the page**

Content per spec: `/admin/settings` CRUD + `reencrypt`; the `source` field; secret masking; hot vs static in practice; runtime-toggle Timmy and content sources (no restart, per the 2026-05-21 work); the dbtool flags (`--import-legacy` with `--no-rewrite`/`--no-backup`, `--import-config`, `--dry-run`, `--overwrite`, `--output`); key rotation via `reencrypt`. Fold in the relevant Config-Migration-Guide content. Cross-link to `[[Configuration-Model]]`, `[[Configuration-Reference]]`, `[[Database-Tool-Reference]]`.

---

### Task 12: `Configuration-Reference.md` (generated)

**Files:**
- Rewrite: `/Users/efitz/Projects/tmi-wiki/Configuration-Reference.md`

- [ ] **Step 1: Regenerate the artifact**

Run (from `/Users/efitz/Projects/tmi`): `make generate-config-docs`
Then copy `config-reference.md` body into the wiki page:

Run: `cp /Users/efitz/Projects/tmi/config-reference.md /Users/efitz/Projects/tmi-wiki/Configuration-Reference.md`

- [ ] **Step 2: Verify the page renders the two tables**

Run: `head -30 /Users/efitz/Projects/tmi-wiki/Configuration-Reference.md`
Expected: `# Configuration Reference`, the GENERATED comment, `## Bootstrap settings`, a `server.port` row with `TMI_SERVER_PORT`.

---

### Task 13: Redirect stubs + sidebar + cross-link

**Files:**
- Rewrite as stub: `/Users/efitz/Projects/tmi-wiki/Configuration-Management.md`
- Rewrite as stub: `/Users/efitz/Projects/tmi-wiki/Config-Migration-Guide.md`
- Modify: `/Users/efitz/Projects/tmi-wiki/_Sidebar.md`
- Modify: `/Users/efitz/Projects/tmi-wiki/Content-Extractors-Limits-and-Overrides.md`

- [ ] **Step 1: Write the stubs**

`Configuration-Management.md` content:

```markdown
# Configuration Management

> This page has moved. Runtime settings management is now documented in **[[Managing-Operational-Settings]]**; the configuration model is in **[[Configuration-Model]]**.
```

`Config-Migration-Guide.md` content:

```markdown
# Config Migration Guide

> This page has moved. Migrating a pre-1.4.0 deployment's operational config into the database is documented in **[[Bootstrapping-Production]]** (legacy migration) and **[[Managing-Operational-Settings]]** (`dbtool` import flags).
```

- [ ] **Step 2: Edit the sidebar**

In `_Sidebar.md`:
- Under **Deployment**, add `- [Bootstrapping Production](Bootstrapping-Production)` (after `Database Setup`).
- Under **Operation**, replace the `Configuration Management` and `Config Migration Guide` lines with `- [Configuring Local Development](Configuring-Local-Development)` and `- [Managing Operational Settings](Managing-Operational-Settings)`.
- Under **Reference**, replace the `Configuration Reference` line with `- [Configuration Model](Configuration-Model)` followed by `- [Configuration Reference](Configuration-Reference)`.

- [ ] **Step 3: Add the cross-link to the content-extractors page**

Near the top of `Content-Extractors-Limits-and-Overrides.md`, add a line: `> Content extractor limits are operational settings. See [[Configuration-Model]] for how operational config is stored and edited, and [[Configuration-Reference]] for the `content_extractors.*` keys.` Verify its existing claims still match `internal/config/content_extractors.go` (run `rg -n 'content_extractors' internal/config/classification_registry.go`); fix any drifted statement.

- [ ] **Step 4: Verify no dangling wiki links**

Run: `rg -o '\[\[[^]]+\]\]' /Users/efitz/Projects/tmi-wiki/*.md | sort -u` and confirm every target page exists in the wiki dir.

---

### Task 14: Land the wiki changes

**Files:** none (commit + push of the wiki repo)

- [ ] **Step 1: Review the diff**

Run (from `/Users/efitz/Projects/tmi.wiki`): `git status && git diff --stat`
Confirm only the intended pages changed.

- [ ] **Step 2: Commit and push the wiki repo**

```bash
cd /Users/efitz/Projects/tmi-wiki
git add Configuration-Model.md Configuring-Local-Development.md Bootstrapping-Production.md \
        Managing-Operational-Settings.md Configuration-Reference.md \
        Configuration-Management.md Config-Migration-Guide.md _Sidebar.md \
        Content-Extractors-Limits-and-Overrides.md
git commit -m "docs(config): revamp configuration documentation for the two-category model

Rewrites config docs around the post-#415 model (Bootstrap vs Operational):
a Configuration-Model concept hub, task guides for local dev / production
bootstrap / runtime settings, and a registry-generated Configuration-Reference.
Retires Configuration-Management and Config-Migration-Guide to redirect stubs."
git push
git status   # MUST show up to date
```

---

## Self-Review

**Spec coverage:**
- Two-category model, cascade, secret refs, secret≠bootstrap (Timmy), hot/static, visibility, config files → Task 8.
- Generated reference + EnvVar field + drift guard → Tasks 1–6, 12.
- Local dev guide → Task 9. Production bootstrap → Task 10. Runtime management (incl. folded migration guide) → Task 11.
- Retirements + stubs + sidebar + content-extractors cross-link → Task 13.
- Main-repo gates (lint, build, test-unit, oracle-db-admin, security-regression) → Task 7. Wiki landing → Task 14.
- Repo-root artifact path (avoids deprecated `docs/`) → Tasks 4–6 (`config-reference.md`).
All spec sections map to a task.

**Placeholder scan:** No TBD/TODO. Code steps show full code. Wiki content steps enumerate the exact section list from the spec rather than "write the page" — acceptable because the spec section is the content contract and each is preceded by a code-verification step; prose pages cannot be literal-coded but every required section is named.

**Type consistency:** `GenerateReferenceMarkdown()`, `MigratableSetting.EnvVar`, `config-reference.md`, target names `build-genconfigdocs`/`generate-config-docs`, and `cmd/genconfigdocs` are used identically across Tasks 1–6, 12. `MutabilityHot`/`VisibilityAdminOnly`/`VisibilityPublic` match `internal/config/classification.go`.
