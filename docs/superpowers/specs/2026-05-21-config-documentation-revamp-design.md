# Configuration Documentation Revamp — Design

**Date:** 2026-05-21
**Status:** Approved (brainstorming) — pending implementation plan
**Branch:** dev/1.4.0
**Related:** #415 (three-category config model), 2026-05-18-config-three-category-model-design.md, 2026-05-21-timmy-runtime-db-config-design.md

---

## Problem

TMI's configuration wiki documentation describes a model that no longer exists. The current config pages were written before the #415 refactor (2026-05-18) and the runtime-DB-config work (2026-05-21), and they assert architecture that was explicitly removed:

- `Configuration-Management.md` documents a **three-tier priority cascade** (env > config file > database) where every setting can come from any of three sources. The current model is **two orthogonal categories** — *Bootstrap* (file/env only, read at startup) and *Operational* (DB-backed, runtime-editable) — not a per-key three-way override.
- `Configuration-Management.md` and `Config-Migration-Guide.md` document **"infrastructure keys"** as a string-prefix denylist (the `infrastructure_keys.go` / `IsInfrastructureKey()` mechanism). That file was **deleted** in #415 and replaced by an explicit `Category == CategoryBootstrap` classification.
- The docs describe **10 `config-*.yml` files** and a `--import-config` migration with priority-override rollback semantics. The current reality is **3 bootstrap-only files**, a generated `config-example.yml`, `vault://`/`env://`/`file://` secret references, and a `dbtool --import-legacy` path.
- `Configuration-Reference.md` (34 KB) is an exhaustive hand-maintained env-var/YAML key table. It has already drifted and has no drift guard.

The result: an operator or developer following the current docs would configure TMI against a mental model that produces wrong behavior (e.g. expecting a config-file value to override a DB value; expecting a `timmy:` block in `config-production.yml`).

## Goal

Revamp all configuration documentation in the wiki to reflect the current implementation, optimized for two audiences — **production operators** and **local developers** — and covering how to bootstrap production environments. Make the per-key reference **generated from the classification registry** so it cannot drift again.

## Audience

Primary: **production operators** and **local developers**. The integrator/registry-internals audience (people adding new config keys) is served incidentally by the Configuration Model page but is not the primary optimization target.

---

## Approach

**Approach A — concept hub + task guides + generated reference (chosen).** Five focused pages, each with one job: one concept page that explains the model once, three by-scenario task guides (local dev, production bootstrap, runtime management), and one generated reference. Rejected alternatives: two big pages (one comprehensive guide reproduces the "everything in one page, hard to keep current" failure mode that let the current docs rot); audience-split pages (duplicates the identical model across a developer page and an operator page).

The per-key reference is **generated from the classification registry** (`internal/config` `GetMigratableSettings()`), the same source of truth that drives `config-example.yml`. A reusable generator binary plus a `make` target plus a drift unit test means the reference is regenerated, not hand-maintained, and CI fails if a key is added without regenerating.

---

## Page set

Five pages, mapped onto the existing wiki sidebar sections (Getting Started / Deployment / Operation / Reference).

| Page | Status | Sidebar section | Job |
|---|---|---|---|
| `Configuration-Model` | New | Reference (top of config group) | Concept hub: two-category model, the per-category cascade, secret references, hot vs static, visibility, where each kind of config lives. Everything links here. |
| `Configuring-Local-Development` | New | Operation | Task guide: dev setup, `config-development.yml`, OAuth stub, `TMI_*` overrides, make targets, where logs/settings live in dev. |
| `Bootstrapping-Production` | New | Deployment | Task guide: bootstrap-only skeleton, secret references, first-run DB seeding, legacy migration, OCI/Heroku/K8s + worker bootstrap, post-bootstrap checklist. |
| `Managing-Operational-Settings` | Rewrite of `Configuration-Management` | Operation | Task guide: `/admin/settings` API, hot vs static in practice, runtime-toggle Timmy/content sources, `dbtool` import flags, key rotation. |
| `Configuration-Reference` | Rewrite (generated) | Reference | Full per-key table from the registry, grouped by category. |

### Retirements (with redirect stubs)

GitHub wiki has no native page rename, so a "rename" is: create the new page, convert the old page to a one-line redirect stub pointing at the new page (other pages and the sidebar link to the old names).

- `Config-Migration-Guide` → content folds into **Managing-Operational-Settings** (the legacy-import path is now `dbtool --import-legacy`, and the priority-override rollback semantics no longer exist). Old page becomes a redirect stub.
- `Configuration-Management` → rewritten as **Managing-Operational-Settings**. Old page becomes a redirect stub.

### Kept, lightly cross-linked (not rewritten)

- `Content-Extractors-Limits-and-Overrides` — content-specific, already recent. Add a pointer to the Configuration Model page and verify its claims still hold against current code; do not restructure.

### Sidebar edits

Add the 3 new pages to their sections; point the two retired entries at the new pages; keep `Configuration Reference` in Reference.

---

## The reference generator

A small Go binary mirroring the existing `cmd/genconfig` pattern, plus a make target, plus a drift test.

- **Library function:** `config.GenerateReferenceMarkdown()` in `internal/config/` (beside `example_gen.go`), reusing `getDefaultConfig().GetMigratableSettings()`.
- **Binary:** `cmd/genconfigdocs/main.go` — calls the library function and writes the Markdown to `config-reference.md` at the **repo root**, a sibling of the generated `config-example.yml` (the existing `genconfig` binary writes its artifact to the repo root the same way; this avoids the `docs/` directory, which is deprecated and must not receive new content per the project CLAUDE.md). The artifact is committed so the drift test has a baseline. The wiki `Configuration-Reference.md` is produced by copying this file's body under the wiki page heading.
- **Make targets:** `build-genconfigdocs` and `generate-config-docs`, following the exact shape of `build-genconfig` / `generate-config-example` in the Makefile.
- **Drift guard:** a unit test (mirroring the existing `config-example.yml` drift test) asserting the committed generated reference matches a fresh generation, run under `make test-unit`.

### Output shape

Two tables, grouped by `Category`, sorted by key.

**Bootstrap settings** (file/env only, read at startup) — columns: Key, Env var, Type, Default, Required, Secret, Description. Operators need the override env var name and what is mandatory at startup.

**Operational settings** (DB-backed, runtime-editable) — columns: Key, Type, Default, Mutability, Visibility, Secret, Description. Operators need to know whether editing a setting does anything (hot vs static) and who can read it (internal/admin/public).

Secrets render their default as a `vault://…` placeholder, never a real value.

### The `EnvVar` field addition

The env var name is currently passed as a literal argument to `settingSource("TMI_…")` at each `MigratableSetting` literal site and consumed to compute `Source`, but **not stored** on the setting. The key→env-var mapping is irregular (`server.port`→`TMI_SERVER_PORT` but `auth.build_mode`→`TMI_BUILD_MODE`, `auth.jwt.refresh_token_days`→`TMI_REFRESH_TOKEN_DAYS`) and cannot be reliably derived.

Add an `EnvVar string` field to `MigratableSetting` and populate it at each literal site (mechanical edit across the literals in `migratable_settings.go`). This makes the env var name first-class registry data — consistent with the #415 principle that "the literal is the whole truth about the setting" — and yields a correct, drift-proof Env var column.

This touches the settings pipeline, so the change runs through the `oracle-db-admin` gate and the `security-regression` scan before commit.

---

## Per-page content

### Configuration-Model (concept hub)

- **Two categories.** Bootstrap = file/env only, read once at startup, cannot come from the DB (DB connection, JWT secret, server listener, logging, secrets provider). Operational = DB-backed, seeded from registry defaults on first run, runtime-editable (OAuth providers, timeouts, feature flags, Timmy, operator info).
- **The per-category cascade.** Bootstrap: defaults → `config-*.yml` → `TMI_*` env (env wins). Operational: registry defaults seed the DB on first run; thereafter the DB is authoritative and editable via `/admin/settings`.
- **Secret references.** `vault://`, `env://`, `file://` resolved at startup per value; inline values pass through unchanged. Where each scheme resolves from (`vault://` via the `secrets.provider`, `env://` from the environment, `file://` from disk).
- **Secrets are not all bootstrap (worked example: Timmy).** "Secret" and "bootstrap" are orthogonal. Bootstrap secrets (`database.url`, `auth.jwt.secret`, `database.redis.password`) are resolved at startup from env/file/vault references. Operational secrets (all `timmy.*_api_key`, OAuth provider `client_secret`) are stored **encrypted in `system_settings`**, managed via `/admin/settings`, and rotated via `reencrypt`. Timmy is entirely an operational subsystem — there is no `timmy:` block in the bootstrap config files; it is seeded from registry defaults and fully runtime-tunable. (This is precisely why a dev instance with no `timmy:` config and no `TMI_TIMMY_*` env vars relied on DB-seeded values.)
- **Hot vs static (Mutability).** What a runtime edit actually does: `MutabilityHot` settings are re-read at use time and take effect immediately; `MutabilityStatic` settings are read once at boot and need a restart.
- **Visibility.** internal / admin / public — who can read a setting via API. A public secret is a build failure (validation suite rule).
- **The config files.** 3 bootstrap-only files (`config-development.yml`, `config-test.yml`, `config-production.yml`); the DB backend is selected by the `database.url` scheme; `config-example.yml` is generated; the working files are gitignored and carry real secrets.
- A simple diagram: where a value comes from at rest and at read time, per category.

### Configuring-Local-Development (task guide)

- Copy `config-example.yml` → `config-development.yml`; populate secret placeholders locally.
- `make start-dev` (DB + Redis + server); `TMI_*` overrides for fast iteration.
- OAuth stub flow for auth-requiring work; `login_hint` users (alice/bob/charlie convention).
- Where dev logs go (`logs/tmi.log`); how operational settings get seeded in dev on first run.
- How to inspect/change a setting locally (admin API or the `/db` skill).

### Bootstrapping-Production (task guide)

- The bootstrap-only `config-production.yml` skeleton; what must be set; required-at-startup validation (`ValidateRequired`).
- Wiring secrets: `secrets.provider` (env/aws/oci/vault), `vault://`-style references, K8s Secret mounts; the three-phase resolution order (VaultToken first, then provider, then remaining fields).
- **First-run DB seeding:** empty settings table → operational defaults seeded from the registry; what to verify after first boot.
- Migrating an existing pre-#415 deployment: `dbtool --import-legacy` (legacy YAML → DB), the warn-then-error transition window for operational keys still present in YAML.
- Platform specifics: OCI/ADB (wallet, `oracle://` URL), Heroku, K8s + the worker bootstrap contract (env-only `LoadWorker`, stamped config in the job envelope, secret mounts).
- Post-bootstrap checklist: admin user, TLS, `server.base_url`, verifying required settings validated at startup.

### Managing-Operational-Settings (rewrite of Configuration-Management)

- `/admin/settings` CRUD + `reencrypt`; the `source` field; secret masking in responses.
- Hot vs static in practice; runtime-toggle Timmy and content sources (no restart) — the 2026-05-21 runtime-DB-config behavior.
- `dbtool` flags: `--import-legacy` (with `--no-rewrite`, `--no-backup`), `--import-config`, `--dry-run`, `--overwrite`, `--output`. Folds in the old Config-Migration-Guide content.
- Key rotation via `reencrypt`.

### Configuration-Reference (generated)

- Generated preamble + the two category tables; a "regenerate with `make generate-config-docs`" note; a link back to the Model page for what the columns mean.

---

## Accuracy pass

Before writing each page, verify claims against current code the way the Timmy classification was verified during brainstorming: CLI flags (`internal/config/cli.go`), dbtool flags (`cmd/dbtool/main.go`), make targets (`Makefile`), endpoint paths (`api/`), and the classification registry (`internal/config/classification_registry.go`). Do not carry forward a stale assertion.

---

## Testing / verification

Main-repo changes (the generator + `EnvVar` field) are Go code and run the full gates:

| Gate | Scope |
|---|---|
| `make build-genconfigdocs` + `make generate-config-docs` | Generator builds and runs clean. |
| New drift unit test (`make test-unit`) | Committed generated reference matches a fresh generation. |
| `make lint`, `make build-server`, `make test-unit` | All green. |
| `oracle-db-admin` subagent | The `EnvVar` field addition touches the settings pipeline. |
| `security-regression` skill | The change touches config secret-adjacent code; scan before commit. |

Wiki changes are Markdown only — no build/test gates; verified by the per-page accuracy pass and cross-link check.

---

## Landing

- **Main repo (`dev/1.4.0`):** the `EnvVar` field on `MigratableSetting`, `cmd/genconfigdocs`, `config.GenerateReferenceMarkdown()`, the `build-genconfigdocs` / `generate-config-docs` make targets, and the drift test → normal flow, conventional commit, push. Lands on `dev/1.4.0` (no main merge).
- **Wiki repo (`/Users/efitz/Projects/tmi.wiki`):** all five page edits + the sidebar + the two redirect stubs. The generated reference Markdown is copied into `Configuration-Reference.md`. Committed and pushed directly to the wiki repo (separate git repo; lower risk than the main branch).

---

## Out of scope

- The broader threat-model / component-platform docs (#347, #414) beyond what the worker-bootstrap section of Bootstrapping-Production needs.
- Non-config wiki pages.
- Restructuring `Content-Extractors-Limits-and-Overrides` beyond a cross-link and an accuracy check.
- Any new `secrets.provider` backend or config-model behavior change — this is documentation plus a reference generator, not a config-system change (the `EnvVar` field is additive metadata).

---

## Summary of decisions

| Decision | Choice |
|---|---|
| Restructure scope | Full restructure around the current two-category model |
| Audience | Production operators + local developers |
| Page set | Approach A: concept hub + 3 task guides + generated reference |
| Retirements | `Config-Migration-Guide` and `Configuration-Management` → redirect stubs pointing at new pages |
| Per-key reference | Generated from the classification registry; drift-guarded by a unit test |
| Generator | `cmd/genconfigdocs` + `config.GenerateReferenceMarkdown()` + `make generate-config-docs` |
| Env var column | Add `EnvVar` field to `MigratableSetting`, populated at each literal site |
| Timmy classification | Entirely operational (no bootstrap keys); used as the worked example for "secrets are not all bootstrap" |
| Main-repo gates | lint, build, test-unit, oracle-db-admin, security-regression |
| Wiki delivery | Edited, committed, and pushed directly to the wiki repo |
