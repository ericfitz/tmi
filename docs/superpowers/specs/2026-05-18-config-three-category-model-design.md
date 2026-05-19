# #415 — Definitive Config Classification Model + Platform-Shared Config

**Date:** 2026-05-18
**Issue:** [#415](https://github.com/ericfitz/tmi/issues/415) — refactor(config): three-category config model + platform-shared config; shrink config-*.yml toward bootstrap-only
**Status:** Design — approved in brainstorming, pending written-spec review
**Milestone:** 1.4.0

---

## Context and intent

TMI's configuration system has accreted complexity: a four-layer cascade (defaults → `config-*.yml` → environment → DB-stored settings) and ten `config-*.yml` files (one per environment, several per DB backend). The TMI Component Platform (#414) adds a fifth consumer — external worker components — and a new requirement: some configuration must be **shared** between the monolith and workers with a *correctness invariant*.

This issue is a **dependency of #347** (the first platform tenant). #347 *consumes* the shared-config mechanism for the embedding profile only; it does not build the general system. This issue builds the general system.

### The problem the platform exposes

Embeddings are only comparable if produced by the **same model**. The `tmi-chunk-embed` worker embeds documents at ingest; the monolith embeds the user's Timmy query at search time. If they disagree on embedding model / endpoint / dimension, vector queries are **silently wrong** — no error, just bad results. The same shape recurs for a future content-fetching worker that needs the monolith's OAuth client config. "Shared config" is a category, not a one-off.

### Scope decision

The project owner chose the **full, definitive classification**: every configuration item in TMI is classified by an explicit, orthogonal property set, and config behavior (DB seeding, masking, redaction, envelope-stamping, public/admin API exposure, secret resolution) is **derived from the classification** rather than hand-maintained anywhere. The stated goal is to **never revisit configuration again** — a half-classified model would force a revisit, so the model is deliberately complete.

### What already exists (and what this reuses)

- `internal/config/config.go` — the four-layer cascade and the `Config` struct.
- `internal/config/migratable_settings.go` — `GetMigratableSettings()` translates the entire `Config` into `MigratableSetting` rows. **Every setting is already a typed literal here.** This issue adds properties to literals that already exist.
- `internal/config/infrastructure_keys.go` — `IsInfrastructureKey()`, a string-prefix denylist marking the bootstrap set. **This file is deleted** and replaced by an explicit typed category.
- `api/settings_service.go` — the DB-backed `SettingsService` with Redis + in-memory caching. Reused unchanged for operational config.
- `crypto.SettingsEncryptor` and the `secrets.provider` config (Vault / AWS / Azure / GCP / OCI). **Reused** as the backend for `Reference`-kind secret resolution — no new secrets provider is introduced.
- `api/seed/` — the unified seeder (`2026-04-10-unified-seeder-design.md`). Extended to seed operational config.

---

## Approach

**Approach A — category as typed data on each setting.** Keep the single `Config` struct and the `MigratableSetting` pipeline. Replace the `IsInfrastructureKey()` prefix heuristic with an explicit property set declared *per setting* in `migratable_settings.go`. Categorization lives next to the setting, as data. Every config mechanism reads the properties and behaves accordingly; no mechanism keeps a hand-maintained list.

Rejected alternatives: physically splitting `Config` into three structs (invasive across ~15 consumers; the bootstrap/operational split is artificial at the struct level because mixed-category sub-structs exist, e.g. `auth.jwt.secret` is bootstrap while `auth.jwt.expiration_seconds` is operational); spec-only direction (under-delivers the chosen full refactor and leaves the fragile prefix denylist in place).

---

## The classification model

A configuration item is described by **orthogonal properties**. Each property answers exactly one question. Every `MigratableSetting` carries the full set.

### Properties

**1. `Category` — where does the value come from at rest?**

```go
type Category int
const (
    CategoryBootstrap   Category = iota // file/env only, never DB; consumed at startup
    CategoryOperational                  // DB-backed, runtime-editable
)
```

`Bootstrap` settings are loaded from file/env before the settings service exists (chicken-and-egg: DB connection settings cannot themselves come from the DB). `Operational` settings are DB-backed.

**2. `Secret` — is the value sensitive?** (`bool`) Drives API masking, log redaction, and audit redaction. Already present on `MigratableSetting` as an ad-hoc bool; formalized as a first-class classification property.

**3. `ValueKind` — is the stored value the secret itself, or a pointer to it?**

```go
type ValueKind int
const (
    ValueKindInline    ValueKind = iota // the field holds the actual value
    ValueKindReference                  // the field holds a locator dereferenced at use time
)
```

A reference value is a locator with a scheme prefix — `vault://PATH`, `env://VARNAME`, or `file://PATH` — that is dereferenced **at startup, per value**. `internal/config/secret_reference.go` implements this: `IsSecretReference` inspects the value's scheme prefix, and `ResolveSecretValue(ctx, value, vault)` dereferences it — `vault://` through the existing `secrets.provider` (via a `SecretResolver` interface, because `internal/config` cannot import `internal/secrets`), `env://` from the environment, `file://` from disk. A value with no recognized scheme prefix (including a plain secret literal or a `postgres://` URL) is **inline** and returned unchanged. This is the property that lets a production deployment store *references* while the actual secrets live in Vault / K8s Secrets.

`ValueKind` in the classification registry records the **default (inline) kind** of a key. Because resolution is per-value, the same key can be inline in dev and a reference in prod with **no registry change** — `ResolveSecretValue` handles both. `ValueKindReference` in the registry is reserved for a key that is *always* a reference in every deployment; today no key is classified that way, so the `ValueKindReference ⟹ Secret` validation rule holds vacuously. Only meaningful when `Secret = true`.

**4. `Delivery` — how does an operational setting reach a process that cannot ask the monolith over HTTP?** (operational settings only; bootstrap settings carry no `Delivery`)

```go
type Delivery struct {
    StampedIntoEnvelope bool // monolith copies this into job envelopes
    SharedInvariant     bool // monolith ALSO consumes it; ingest & monolith must agree
}
```

`SharedInvariant` implies `StampedIntoEnvelope` — you cannot hold an invariant on a value you do not send. The two are stored as separate booleans (not a flat enum) precisely so the validation suite can reject the illegal combination `SharedInvariant && !StampedIntoEnvelope`.

**5. `Visibility` — who may read it through the API?**

```go
type Visibility int
const (
    VisibilityInternal  Visibility = iota // server-side only, never in any API response
    VisibilityAdminOnly                   // visible to admins via /admin config endpoints
    VisibilityPublic                      // exposed on the unauthenticated discovery/config endpoint
)
```

`VisibilityPublic` with `Secret = true` is illegal — the validation suite rejects it, making "a secret accidentally exposed on a public endpoint" a build failure. TMI-UX reads `Public` settings (operator name, OAuth provider display data, `server.base_url`) the way any HTTP client does — it is not a platform component and needs no delivery role.

**6. `Mutability` — can it change after startup?**

```go
type Mutability int
const (
    MutabilityStatic Mutability = iota // read once at boot; a change needs a restart
    MutabilityHot                      // re-read at use time; a runtime edit takes effect immediately
)
```

Answers "does editing this setting in the admin UI do anything, or silently require a restart?" — a question that is currently undocumented.

**7. `Consumers` — which processes read it?** A closed enum (typo = build error):

```go
type Consumer int
const (
    ConsumerMonolith Consumer = iota
    ConsumerTMIUX
    ConsumerWorkerExtractor
    ConsumerWorkerChunkEmbed
    // Add a value here when a new component type is introduced.
)
```

`[]Consumer` per setting. This is documentation-as-data: it makes the `SharedInvariant` property *derivable* (a `SharedInvariant` setting must list `ConsumerMonolith` plus at least one `ConsumerWorker*`) and turns a future component's config audit into a query rather than an investigation. `ConsumerTMIUX` records that TMI-UX reads a setting via the public config API — it documents audience, not a delivery role; TMI-UX receives nothing through the cascade or the envelope.

**8. `Required`.** `Required bool`. A `Required` setting must have a non-empty effective value at startup. This is enforced by `Config.ValidateRequired()`, which `config.Load()` calls right after `Validate()` succeeds: it walks `GetMigratableSettings()` and fails `Load()` with a single error naming every `Required` setting whose post-cascade `Value` is empty, rather than producing a confusing downstream nil. The validation suite additionally enforces `Required ⟹ CategoryBootstrap` — a required setting must be knowable at startup; an operational setting comes from the DB seed, where "required" has no enforcement point.

There is **no `Default` field** on `ConfigClass` / `MigratableSetting`. Default *values* are not classification data: bootstrap defaults come from `getDefaultConfig()` and operational defaults from `DefaultOperationalSettings()` (itself derived from `getDefaultConfig()`). Those two functions are the single source of truth for default values; adding a `Default` field to the classification would create a second, divergent source — the opposite of the "never revisit config" goal.

### `MigratableSetting` — the complete shape

```go
type MigratableSetting struct {
    Key         string
    Type        string        // existing: string|int|bool|json
    Description string
    Value       string
    Source      string        // existing: "config" | "environment"

    Category    Category
    Secret      bool
    ValueKind   ValueKind      // meaningful only when Secret
    Delivery    *Delivery      // nil for Bootstrap; set for Operational
    Visibility  Visibility
    Mutability  Mutability
    Consumers   []Consumer
    Required    bool
    // No Default field — default values come from getDefaultConfig()
    // (bootstrap) and DefaultOperationalSettings() (operational).
}
```

Every config item in TMI is one such literal. The literal is the *whole truth* about the setting.

### The four roles, as a consequence

`Category` and `Delivery` together yield four operational roles:

| Role | `Category` | `StampedIntoEnvelope` | `SharedInvariant` | Example |
|---|---|---|---|---|
| **Bootstrap** | `Bootstrap` | — (no `Delivery`) | — | `database.url`, `auth.jwt.secret`, `server.port`, worker `NATS_URL` |
| **Operational, monolith-only** | `Operational` | false | false | feature flags, WebSocket timeout, OAuth provider config, operator name |
| **Operational, worker-relayed** | `Operational` | true | false | `tmi-chunk-embed` wall-clock budget, worker concurrency limit |
| **Shared** | `Operational` | true | true | embedding `model` / `endpoint` / `dimension` |

---

## The validation suite

A unit test over the `MigratableSetting` registry, run as part of `make test-unit`. It is the mechanism that makes the model **complete and self-enforcing** — a setting that is added without full, consistent classification fails the build. Rules:

- **Total and disjoint partition.** Every setting has exactly one `Category`.
- `Category == Bootstrap` ⟹ `Delivery == nil`, and the key is never DB-served or DB-seeded.
- `Delivery.SharedInvariant` ⟹ `Delivery.StampedIntoEnvelope`.
- `Delivery.SharedInvariant` ⟹ `Consumers` contains `ConsumerMonolith` and at least one `ConsumerWorker*`.
- `ValueKind == ValueKindReference` ⟹ `Secret == true`.
- `Visibility == VisibilityPublic` ⟹ `Secret == false`.
- `Required == true` ⟹ `Category == CategoryBootstrap`. A required setting must be knowable at startup; an operational setting comes from the DB seed, where "required" has no enforcement point. Empty-value enforcement itself is done at runtime by `Config.ValidateRequired()` in `config.Load()`, not by this suite (the suite has no effective `Config`, only the classification registry).
- Every setting has a non-empty `Description` and at least one `Consumer`.

Config behavior is derived from these properties everywhere. After this issue, "dealing with config" is: add a literal with its properties; the suite proves consistency; every mechanism does the right thing automatically. That is the "never revisit" property.

---

## Shared config: the stamped-config object and envelope mechanism

Non-secret shared config travels to workers **in the job envelope**, not through a controller projection. The monolith reads it from the DB-backed settings service and stamps it into each job. This kills the chicken-and-egg dependency on the unbuilt K8s controller and makes the same-model invariant *structural*: the monolith decides the embedding profile for both ingest and query, and literally cannot disagree with itself.

### The typed object

```go
type EmbeddingProfile struct {
    Model     string // e.g. "text-embedding-3-large"
    Endpoint  string // base URL
    Dimension int
    // API key is NOT here — it is a secret; see "Secrets" below.
}

type StampedConfig struct {
    Embedding EmbeddingProfile
    // worker-relayed operational tunables join here over time
}
```

`StampedConfig` is the serialized subset of operational settings with `Delivery.StampedIntoEnvelope == true`.

### The provider

```go
type StampedConfigProvider interface {
    Get(ctx context.Context) (StampedConfig, error)
}
```

A concrete implementation reads through the existing `SettingsService` (DB-backed, cache-served). It is the **single read point** — the monolith never assembles envelope config ad hoc.

### Envelope-stamping

The monolith's job-envelope builder calls `StampedConfigProvider.Get()` and copies the result into a `config` block on the envelope. #347 owns the envelope *schema*; this issue owns the *producing side* — the `StampedConfig` type and the provider.

### The shared-invariant guarantee

`SharedInvariant` settings (the embedding profile) have a second consumer inside the monolith: the Timmy query path embeds the user's search text. Both the envelope stamp and the Timmy query path read through the **same** `StampedConfigProvider.Get()` — one accessor, one DB read path, one cache. They cannot disagree because they call the same function. The invariant is structural, not enforced by discipline.

### Secrets

The embedding API key is **not** in `StampedConfig`, never in the envelope, and never stored as DB plaintext. It is a `Secret` setting. For the worker it is `CategoryBootstrap` — a mounted K8s `Secret` at a known path, resolved from the worker's `SecretMounts` (see Worker bootstrap). For the monolith it is `CategoryBootstrap` resolved from file/env or the `secrets.provider`. `StampedConfig` carries only the non-secret coordinates; each side resolves the key value independently from its own secret source. Rotation is one `Secret` object, two mounts.

Secret reference resolution is **implemented**. At startup, after `config.Load`, `cmd/server/main.go` calls `resolveSecretReferences`, which walks every bootstrap secret field of the loaded `Config` (`Auth.JWT.Secret`, `Database.URL`, `Database.Redis.URL`, `Database.Redis.Password`, `Server.TLSKeyFile`, `Secrets.VaultToken`, the Timmy API keys) and dereferences any `vault://`/`env://`/`file://` value in place via `ResolveSecretValue`; inline values pass through unchanged. Resolution runs **before** `runServer` connects the database and initializes auth, so those consumers see resolved values. Ordering is three-phase: `Secrets.VaultToken` is resolved first with no provider (`env://`/`file://` only — a `vault://` token is rejected), then `secrets.NewProvider` is built from the resolved `Secrets` block, then the remaining fields are resolved with the provider-backed vault leg. A resolution failure is fatal (`os.Exit(1)`) — a config that references an unresolvable secret cannot run — and is reported as a typed error, never a panic. The `internal/config` ↔ `internal/secrets` import cycle is avoided by the `config.SecretResolver` interface plus the `internal/configsecrets` adapter package.

---

## The `config-*.yml` collapse

### Before and after

Ten files collapse to **three**, holding only `CategoryBootstrap` settings:

- `config-development.yml`
- `config-test.yml`
- `config-production.yml`

The per-backend files (`config-development-sqlite.yml`, `-mysql.yml`, `-sqlserver.yml`, `config-development-oci.yml`, `config-test-integration-pg.yml`, `config-test-integration-oci.yml`) are removed. Inspection of the diffs confirmed they do not express a different DB — they are stripped-down copies that *omit* operational config (OAuth providers, SAML, content extractors, Timmy, administrators) and have drifted. The DB backend is selected entirely by the `database.url` value (`postgres://`, `sqlite://`, `mysql://`, `oracle://`), overridable by `TMI_DATABASE_URL`.

### Where operational and shared config goes

Everything `CategoryOperational` moves out of YAML and into **DB seed data**. The unified seeder gains a per-environment operational-config seed set. First run seeds the DB; thereafter the DB is authoritative and runtime-editable.

### Generated example config

`config-example.yml` is **generated** from the `MigratableSetting` registry — every setting is a typed literal with `Description`, `Required`, `Category`, and a `Value` (the default value, supplied by `getDefaultConfig()`), so the example file can be emitted by a `make` target rather than hand-maintained. A test asserts the regenerated file matches the committed file (drift = test failure). `config-production.yml` shrinks to a bootstrap skeleton whose secret fields are `Reference`-kind placeholders (`vault://…`) — it is a template, not a populated file.

### Integration-test configs

`config-test-integration-pg.yml` and `-oci.yml` collapse into `config-test.yml` plus a `TMI_DATABASE_URL` override set by the `make test-integration-pg` / `make test-integration-oci` targets. The test bootstrap is identical across backends; only the URL differs.

### Migration and cutover

This is a behavior change for any existing deployment — operational config that lived in YAML must now be in the DB. The cutover:

1. The seeder, on an empty settings table, seeds operational defaults (existing first-run behavior, unchanged).
2. For an existing deployment, a one-time `dbtool config import-legacy` command imports operational keys from a legacy `config-*.yml` into the settings table.
3. `config.Load()` **warns** (does not fail) when it finds operational keys in a YAML file — surfacing drift during the transition window.
4. After a release window the warning becomes an error.

`cmd/dbtool/` is updated for the settings-table seeding and the legacy-import path, per the project schema-change rule.

---

## Worker bootstrap and the stub worker

### The worker bootstrap contract

A worker needs the absolute minimum to start and reach the point where it can accept a job. New package `internal/config/bootstrap`, **separate from the monolith's `config.Load()`** so the type system enforces that a worker cannot import the monolith cascade:

```go
package bootstrap

type WorkerBootstrap struct {
    NATSURL          string            // required — cannot receive a job without it
    SecretMounts     map[string]string // logical name -> filesystem path of a mounted K8s Secret
    LogLevel         string            // optional, default "info"
    HeartbeatSubject string            // NATS subject for liveness
}

// LoadWorker reads environment variables only — no YAML, no DB.
func LoadWorker() (*WorkerBootstrap, error)
```

Everything else a worker needs arrives in the job envelope (`StampedConfig`) or is resolved from a secret mount (the embedding API key: the worker reads the file at `SecretMounts["embedding-api-key"]`). The worker never reads a `config-*.yml`, never reaches the DB, never sees an operational setting that is not stamped. Each `WorkerBootstrap` field is a `CategoryBootstrap` setting in the registry with `Consumers` including the relevant `ConsumerWorker*` value.

This is the contract #347's `cmd/extractor` and `cmd/chunkembed` will import.

### The stub worker

`cmd/worker-probe/main.go` — a minimal binary whose sole purpose is to prove the contract end-to-end before #347 depends on it:

1. Calls `bootstrap.LoadWorker()` — proves env-only bootstrap, including a clean failure on a missing required variable.
2. Connects to NATS, subscribes to a probe subject, publishes a heartbeat — proves the liveness shape.
3. Receives a job envelope and deserializes its `config` block into `StampedConfig` — proves envelope-carried config.
4. Resolves a secret from `SecretMounts` — proves the secret-mount path without the value ever touching NATS or the DB.
5. Echoes `{bootstrap_ok, stamped_config_seen, secret_resolved}` on a result subject.

It is a test fixture, but a real binary with a `make build-worker-probe` target, exercised by an integration test (`worker-probe` ↔ NATS ↔ a monolith-side envelope builder). When #347 builds the real workers the contract is already proven; the probe's integration test becomes the regression guard. The probe stays in the tree as living documentation of the worker contract.

### Explicitly not in this issue

No `TMIComponent` CRD, no custom controller, no KEDA, no K8s manifests, no real extractor / chunk-embed logic — all of that is #347. This issue builds the config *contract and its proof*.

---

## Consumer migration

The migration is mechanical because the categories mirror what the code already does:

- **Bootstrap consumers** (startup sequencing, DB connection, JWT init) already read from `config.Config`; unchanged.
- **Operational consumers** already read from `SettingsService`; unchanged.
- **New:** the monolith's job-envelope builder calls `StampedConfigProvider.Get()`; the Timmy query path switches to the same accessor.
- **Deleted:** `internal/config/infrastructure_keys.go` and `IsInfrastructureKey()` — replaced by `Category == CategoryBootstrap`. Its single caller (`cmd/dbtool/config.go`) switches to the category check.

The ~15 call sites of `config.Config` are otherwise untouched — the single `Config` struct is retained (Approach A).

---

## Testing

All tests run under `make` targets per project rules (`make test-unit`, `make test-integration`).

| Test | Covers |
|---|---|
| **Validation suite** | Every classification rule above; a misclassified or unclassified setting fails the build. The "never revisit" guarantee. |
| **Bootstrap isolation** | `SettingsService` refuses to DB-serve a `Bootstrap` key; the seeder skips it. |
| **Shared-invariant** | The value the monolith stamps equals the value the monolith's Timmy query path reads — both via `StampedConfigProvider.Get()`. |
| **Secret resolution** | Implemented per-value at startup by `ResolveSecretValue`: an inline secret returns its value unchanged; a `vault://` value dereferences through the `secrets.provider`, `env://` from the environment, `file://` from disk; a resolution failure is a typed, fatal error, not a panic. |
| **Visibility** | The public config endpoint returns exactly the `Public` set and no `Secret`; the admin endpoint returns `AdminOnly ∪ Public`. |
| **Generated example config** | `config-example.yml` regenerated from the registry matches the committed file. |
| **Worker bootstrap** | `LoadWorker()` succeeds on a complete env set, fails cleanly on a missing required variable, never touches YAML or the DB. |
| **Probe integration test** | `worker-probe` ↔ NATS ↔ monolith envelope builder: bootstrap, stamped-config, and secret-mount proven end-to-end. NATS runs as a GitHub Actions `services:` container. |

---

## OpenAPI surface

This issue does **not** define the job-envelope schema — that is #347. It produces the Go-side `StampedConfig` type and the public / admin config endpoints. If the public discovery/config endpoint or the `/admin` config endpoints are new or change shape, `api-schema/tmi-openapi.json` is the source of truth and `make validate-openapi` + `make generate-api` apply. The exact endpoint surface is finalized during planning against the existing `api/config_handlers.go`.

---

## Oracle Database compatibility

Settings-table seeding, the `dbtool config import-legacy` path, and any settings-schema change are DB-touching. The `oracle-db-admin` subagent reviews these before the issue is marked complete. Verdicts are handled per the project rule (`APPROVED` / `APPROVED WITH NOTES` / `BLOCKING ISSUES`).

---

## Task-completion gates

`make lint`, `make build-server`, `make test-unit`, `make test-integration`. `make validate-openapi` + `make generate-api` if the OpenAPI spec is touched. `oracle-db-admin` subagent review for the DB-touching changes. The commit references the issue (`Closes #415`); the issue is closed manually if the commit does not land directly on `main`.

---

## Out of scope / deferred

- `TMIComponent` CRD, custom controller, KEDA, K8s manifests, real extractor / chunk-embed workers — **#347**.
- Any new `secrets.provider` backend — the existing Vault / AWS / Azure / GCP / OCI plumbing is reused as-is.
- The job-envelope schema itself — **#347**.

---

## Summary of decisions

| Decision | Choice |
|---|---|
| Scope | Full, definitive classification of every config item; never revisit config |
| Approach | Category as typed data on each `MigratableSetting` (Approach A); single `Config` struct retained |
| Role model | `Category` (Bootstrap / Operational) × `Delivery` (`StampedIntoEnvelope`, `SharedInvariant`) → four roles |
| Classification properties | `Category`, `Secret`, `ValueKind`, `Delivery`, `Visibility`, `Mutability`, `Consumers`, `Required` (no `Default` field — default values come from `getDefaultConfig()` / `DefaultOperationalSettings()`; `Required` is enforced by `ValidateRequired()` at startup and the suite rule `Required ⟹ CategoryBootstrap`) |
| Self-enforcement | A validation suite over the registry; misclassification fails the build |
| Shared config delivery | Envelope-stamped by the monolith; no controller-projection dependency |
| Shared-invariant guarantee | Structural — one `StampedConfigProvider.Get()` accessor for both stamp and Timmy query |
| Secrets | Per-value `vault://`/`env://`/`file://` references resolved at startup by `ResolveSecretValue` (`vault://` reuses the existing `secrets.provider`); the registry's `ValueKind` records only the default inline kind; secrets never in envelopes, DB plaintext, or public endpoints (build-enforced) |
| `config-*.yml` | Collapse 10 → 3 bootstrap-only files; DB backend by URL; operational config moves to DB seed |
| Example config | Generated from the registry; drift is a test failure |
| Legacy migration | `dbtool config import-legacy`; `config.Load()` warns then errors on operational keys in YAML |
| Worker bootstrap | New `internal/config/bootstrap` package, env-only `LoadWorker()` |
| Contract proof | `cmd/worker-probe` stub worker + integration test, before #347 depends on the contract |
| `infrastructure_keys.go` | Deleted; replaced by `Category == CategoryBootstrap` |
