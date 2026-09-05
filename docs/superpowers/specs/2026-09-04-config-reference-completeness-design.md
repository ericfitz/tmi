<!-- Approved design for #810 (config-reference completeness). Verbatim copy of the two comments dated 2026-09-04 on https://github.com/ericfitz/tmi/issues/810. -->

## Design (approved 2026-09-04, human decision) — scope widened to every TMI_* read

**Scope decision:** cover both the 35 registry-declared vars the issue counts AND the 32 names + 3 prefix patterns read outside the registry (inventory below). Also found: the 20 `TMI_SAML_*` struct `env:` tags are dead code (`overrideStructWithEnv` never reaches `SAMLProviderConfig`, which exists only as a map value); the real reads use `SAML_PROVIDERS_<ID>_<FIELD>` via `overrideSAMLProviders` (`internal/config/config.go:750`).

1. **Registry-declared gap.** `GenerateReferenceMarkdown` (`internal/config/reference_gen.go`) iterates `AllSettingDefs()` instead of `GetMigratableSettings()`: every def with a `Get` (DB-only defs stay out), default computed by `d.Get(getDefaultConfig())`, empty default rendered blank. The `OmitWhenEmpty`, skiplist and TLS-enabled filters no longer apply to documentation. `GenerateExampleConfig` keeps the projection. `TestGenerateReferenceMarkdown_CoversEveryEmittedEnvVar` becomes "every registry def with an `EnvVar` names it in the reference", no exception list.
2. **SSRF vars (10, `TMI_SSRF_{ISSUE_URI,DOCUMENT_URI,REPOSITORY_URI,TIMMY,WEBHOOK}_{ALLOWLIST,SCHEMES}`).** Defs exist without `EnvVar`; `cmd/server/main.go` `buildURIValidator` reads them with its own `os.Getenv`. Consolidate: add `env:` tags on the `SSRF` struct fields and `EnvVar` on the defs; `buildURIValidator` reads the already-overridden config only. Same precedence, one mechanism.
3. **Process environment (bypasses `Config`): 22 names + 3 patterns.** Hand-maintained table in `internal/config` (name, binary, purpose, secret, pattern) rendered as a new "Process environment" section grouped by binary:
   - server: `TMI_ADMIN_{PROVIDER,PROVIDER_ID,SUBJECT_TYPE,EMAIL,GROUP_NAME}`, `TMI_JWT_KEY_ID`, `TMI_CLOUD_LOG_{ENABLED,PROVIDER,LEVEL}`, `TMI_OCI_LOG_ID`, `TMI_NATS_URL`, `TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING` (test-only)
   - workers (chunkembed / extractor / worker-probe / component-controller): `TMI_NATS_URL`, `TMI_NATS_CREDS`, `TMI_COMPONENT_NAME`, `TMI_HEARTBEAT_INTERVAL`, `TMI_JOB_ACK_WAIT`, `TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET` (also read directly by the extractor), `TMI_EMBEDDING_{MODEL,BASE_URL,API_KEY}` (API_KEY secret), `TMI_WORKER_{NATS_URL,LOG_LEVEL,HEARTBEAT_SUBJECT}`
   - patterns: `TMI_SECRET_<KEY>` (all secret), `TMI_WORKER_SECRET_MOUNT_<NAME>` (paths to mounted secrets), `TMI_CONTENT_OAUTH_PROVIDERS_<ID>_<FIELD>` (CLIENT_SECRET secret), plus `SAML_PROVIDERS_<ID>_<FIELD>` documented here despite lacking the prefix.
4. **Allowlist gate.** A test scans non-test Go sources for `TMI_[A-Z0-9_]+`, subtracts registry `EnvVar`s, the process-environment table, and a short exclusion list of non-env-var tokens (JetStream names `TMI_DLQ*/TMI_RESULTS/TMI_PAYLOADS`, Oracle names `TMI_SCHEMA_VERSIONS`/`TMI_THREAT_MODEL_ALIAS_SEQ`, doc-comment examples, prefix fragments). Anything left fails with token and file.
5. **Dead SAML tags:** delete the 20 `TMI_SAML_*` `env:` tags on `SAMLProviderConfig`. No runtime change.
6. **Gates:** `make generate-config-docs`, existing freshness tests, unit tests. No DB change, no Oracle review.

Full inventory with read sites is attached below.


---

# TMI_* env vars read outside the SettingDef registry

Method: `rg -o 'TMI_[A-Z0-9_]+'` over non-test Go sources (206 distinct tokens) minus the
`EnvVar:` values declared in `internal/config/setting_defs_*.go` (142 values), then each
remainder inspected at its read site.

**Final count: 32 concrete names** (33 table rows; the extra row,
`TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET`, *is* registered as an EnvVar but is additionally
read directly by the extractor binary outside Config). Plus **3 prefix patterns**.

## Table

| Name | Read at | Mechanism | Purpose | Secret | Binary | Config field? |
|---|---|---|---|---|---|---|
| TMI_ADMIN_PROVIDER | `internal/config/config.go:425` | os.Getenv | gate for building a single bootstrap administrator entry | no | server | yes, appended to `Config.Administrators` (slice element, no scalar path) |
| TMI_ADMIN_PROVIDER_ID | `internal/config/config.go:431` | os.Getenv | admin subject's provider id | no | server | same slice element |
| TMI_ADMIN_SUBJECT_TYPE | `internal/config/config.go:428` | envutil.Get (default `user`) | admin subject type user/group | no | server | same slice element |
| TMI_ADMIN_EMAIL | `internal/config/config.go:435` | os.Getenv | admin email | no | server | same slice element |
| TMI_ADMIN_GROUP_NAME | `internal/config/config.go:439` | os.Getenv | admin group name | no | server | same slice element |
| TMI_JWT_KEY_ID | `auth/config.go:195` | envutil.Get, falls back to `JWT_KEY_ID`, then `"1"` | JWKS key id | no | server | on `auth.Config.JWT.KeyID`, not on `internal/config.Config`, so no `Get(*Config)` reaches it |
| TMI_CLOUD_LOG_ENABLED | `cmd/server/main.go:1810` | os.Getenv | enable cloud log writer | no | server | bypasses Config |
| TMI_CLOUD_LOG_PROVIDER | `cmd/server/main.go:1814` | os.Getenv | cloud log provider, only `oci` supported | no | server | bypasses Config |
| TMI_CLOUD_LOG_LEVEL | `cmd/server/main.go:1841` | os.Getenv | min level for the cloud writer | no | server | bypasses Config |
| TMI_OCI_LOG_ID | `cmd/server/main.go:1820` | os.Getenv | OCI log OCID | no | server | bypasses Config |
| TMI_SSRF_ISSUE_URI_ALLOWLIST | `cmd/server/main.go:1752`, via `buildURIValidator(cfg.SSRF.IssueURI, "TMI_SSRF_ISSUE_URI")` at `:1509` | os.Getenv, name composed as `prefix+"_ALLOWLIST"` | env override of the issue-URI SSRF allowlist | no | server | yes, `SSRF.IssueURI.Allowlist`; SettingDef `ssrf.issue_uri.allowlist` exists with **no EnvVar** |
| TMI_SSRF_ISSUE_URI_SCHEMES | `cmd/server/main.go:1756` | same, `prefix+"_SCHEMES"` | permitted schemes | no | server | yes, def has no EnvVar |
| TMI_SSRF_DOCUMENT_URI_ALLOWLIST | `:1752` via `:1374`, `:1510`, `:1513` | same | document-URI allowlist, also reused for content OAuth | no | server | yes, def has no EnvVar |
| TMI_SSRF_DOCUMENT_URI_SCHEMES | `:1756` | same | schemes | no | server | yes, no EnvVar |
| TMI_SSRF_REPOSITORY_URI_ALLOWLIST | `:1752` via `:1511` | same | repository-URI allowlist | no | server | yes, no EnvVar |
| TMI_SSRF_REPOSITORY_URI_SCHEMES | `:1756` | same | schemes | no | server | yes, no EnvVar |
| TMI_SSRF_TIMMY_ALLOWLIST | `:1752` via `:1512` | same | Timmy URI allowlist | no | server | yes, no EnvVar |
| TMI_SSRF_TIMMY_SCHEMES | `:1756` | same | schemes | no | server | yes, no EnvVar |
| TMI_SSRF_WEBHOOK_ALLOWLIST | `:1752` via `:1873` | same | webhook destination allowlist | no | server | yes, no EnvVar |
| TMI_SSRF_WEBHOOK_SCHEMES | `:1756` | same | schemes | no | server | yes, no EnvVar |
| TMI_NATS_URL | `cmd/server/main.go:1301`, `cmd/component-controller/main.go:55`, `internal/worker/nats.go:26` | os.Getenv / worker.MustEnv | NATS endpoint for extraction wiring, JetStream provisioning, worker connect | no | server, component-controller, all workers | bypasses Config |
| TMI_NATS_CREDS | `internal/worker/nats.go:59` | os.Getenv | path to a NATS creds file | path to a secret | workers | bypasses Config |
| TMI_COMPONENT_NAME | `internal/worker/nats.go:30` | worker.MustEnv (required) | this worker's TMIComponent name | no | chunkembed, extractor, worker-probe | bypasses Config |
| TMI_HEARTBEAT_INTERVAL | `cmd/chunkembed/main.go:63`, `cmd/extractor/main.go:54` | worker.EnvDuration | worker heartbeat period | no | chunkembed, extractor | bypasses Config |
| TMI_JOB_ACK_WAIT | `cmd/chunkembed/main.go:73`, `cmd/extractor/main.go:64`; also a CR `spec.config` key at `internal/platform/controller/render_jetstream.go:73` | worker.EnvDuration | JetStream ack wait per job | no | chunkembed, extractor, component-controller | bypasses Config |
| TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET | `cmd/extractor/main.go:81` | worker.EnvDuration | extraction wall-clock cap inside the worker | no | extractor | registered as an EnvVar for the server; listed only because the extractor reads it directly, outside Config |
| TMI_EMBEDDING_MODEL | `cmd/chunkembed/embedder.go:25` | worker.MustEnv | embedding model name | no | chunkembed | bypasses Config |
| TMI_EMBEDDING_BASE_URL | `cmd/chunkembed/embedder.go:29` | worker.MustEnv | embedding API base URL | no | chunkembed | bypasses Config |
| TMI_EMBEDDING_API_KEY | `cmd/chunkembed/embedder.go:33` | worker.MustEnv | embedding API key | **yes** | chunkembed | bypasses Config |
| TMI_WORKER_NATS_URL | `internal/config/bootstrap/bootstrap.go:41` | os.Getenv, hard-required (error at `:43`) | NATS URL for the worker bootstrap config | no | workers | worker-local bootstrap struct, not `config.Config` |
| TMI_WORKER_LOG_LEVEL | `internal/config/bootstrap/bootstrap.go:46` | os.Getenv | worker log level | no | workers | worker bootstrap struct |
| TMI_WORKER_HEARTBEAT_SUBJECT | `internal/config/bootstrap/bootstrap.go:53` | os.Getenv | NATS subject for heartbeats | no | workers | worker bootstrap struct |
| TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING | `api/auth_flow_rate_limiter.go:48`, used at `:115` | os.Getenv | test-only override forcing auth-flow rate limiting on | no | server (non-test code path) | bypasses Config |

## Prefix patterns (document as patterns, not names)

- **`TMI_SECRET_<KEY>`** — a real pattern. `internal/secrets/env_provider.go:23` sets
  `prefix: "TMI_SECRET_"`; `GetSecret` uppercases the logical key and looks up
  `TMI_SECRET_<KEY>`, and `ListSecrets` scans `os.Environ()` for the prefix. Every value is a
  secret. Instantiations visible in the tree: `TMI_SECRET_JWT_SECRET` (doc example,
  `env_provider.go:29`) and `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` (remediation message,
  `cmd/server/startup_checks.go:81`). The key set is open-ended, so no fixed name list exists.
- **`TMI_WORKER_SECRET_MOUNT_<NAME>`** — `internal/config/bootstrap/bootstrap.go:35`, prefix
  scan over `os.Environ()`; each value is a filesystem path to a mounted secret. Example in
  comments: `TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY`.
- **`TMI_CONTENT_OAUTH_PROVIDERS_<ID>_<FIELD>`** — `internal/config/config.go:858` uses
  `envutil.DiscoverProviders` (an `os.Environ()` prefix/suffix scan) on `_ENABLED`, then reads
  `CLIENT_ID`, `CLIENT_SECRET`, `AUTH_URL`, `TOKEN_URL`, `USERINFO_URL`, `REVOCATION_URL`,
  `REQUIRED_SCOPES` per provider (`config.go:862-895`). `CLIENT_SECRET` is a secret. Only
  `TMI_CONTENT_OAUTH_CALLBACK_URL` is in the registry; none of the per-provider vars are.

## SAML structure, and a live bug

`SAMLConfig` (`internal/config/config.go:221`) and `SAMLProviderConfig`
(`internal/config/config.go:228`) both live in `internal/config/config.go`.

- `SAMLConfig`: `Enabled bool` with `yaml:"enabled" env:"TMI_SAML_ENABLED"`, and
  `Providers map[string]SAMLProviderConfig` with a yaml tag only.
- `SAMLProviderConfig`: yaml tags on every field, plus `env:"TMI_SAML_*"` tags on 20 of them
  (`TMI_SAML_ENTITY_ID`, `_METADATA_URL`, `_METADATA_XML`, `_ACS_URL`, `_SLO_URL`,
  `_SP_PRIVATE_KEY`, `_SP_PRIVATE_KEY_PATH`, `_SP_CERTIFICATE`, `_SP_CERTIFICATE_PATH`,
  `_IDP_METADATA_URL`, `_IDP_METADATA_B64XML`, `_ALLOW_IDP_INITIATED`, `_FORCE_AUTHN`,
  `_SIGN_REQUESTS`, `_NAME_ID_ATTRIBUTE`, `_EMAIL_ATTRIBUTE`, `_NAME_ATTRIBUTE`,
  `_GIVEN_NAME_ATTRIBUTE`, `_FAMILY_NAME_ATTRIBUTE`, `_GROUPS_ATTRIBUTE`).
- No struct-level defaults. The only defaulting is in `overrideSAMLProviders`: an empty `ID` or
  `Name` falls back to the provider key (`config.go:824-830`).

**Those 20 `TMI_SAML_*` env tags are dead.** `overrideStructWithEnv` (`config.go:584`) recurses
into struct fields and special-cases a map field named `Providers`; `SAMLProviderConfig` exists
only as a map value, so its env tags are never consulted. The real per-provider reads are in
`overrideSAMLProviders` (`config.go:750`) and use the prefix **`SAML_PROVIDERS_<ID>_`** — no
`TMI_` prefix at all — discovered via `envutil.DiscoverProviders("SAML_PROVIDERS_", "_ENABLED")`
(`config.go:761`, scanner at `internal/envutil/envutil.go:49`). The operative names are
`SAML_PROVIDERS_<ID>_ENTITY_ID`, `..._ACS_URL`, `..._SP_PRIVATE_KEY`, and so on. That is why
the SAML names fall out of the token diff: they are tags, not reads.

Registry SAML coverage: exactly one def, `features.saml_enabled`
(`internal/config/setting_defs_auth.go:197`, YAMLPath `auth.saml.enabled`, EnvVar
`TMI_SAML_ENABLED`). Nothing else SAML-related is registered.

## Excluded as not env vars

| Token | What it actually is |
|---|---|
| TMI_DLQ, TMI_DLQ_ADVISORY, TMI_RESULTS, TMI_PAYLOADS | JetStream stream and KV bucket names, `internal/worker/names.go:20-38` |
| TMI_SCHEMA_VERSIONS | Oracle upper-folded table name, `internal/dbschema/schema_version.go:44` |
| TMI_THREAT_MODEL_ALIAS_SEQ | Oracle sequence name, `internal/dbschema/alias_sequence.go:137` |
| TMI_TMI_EXTRACTOR, TMI_CHUNK_EMBED_CONSUMER | derived consumer names in doc comments only, `internal/worker/names.go:103,111` |
| TMI_SECRET_JWT_SECRET, TMI_SECRET_SETTINGS_ENCRYPTION_KEY, TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY | doc/message instantiations of the prefix patterns above, not standalone lookups |
| TMI_SERVER_URL | read only under `test/integration/` |
| TMI_CONTENT_EXTRACTORS_*, TMI_CONTENT_OAUTH_PROVIDERS_ (bare prefixes) | prefix fragments, not names; the 8 concrete `TMI_CONTENT_EXTRACTORS_*` env tags are all registered |

Python under `scripts/` (`devenv.py` and friends) writes `TMI_*` into manifests and `.env.dev`
rather than reading them at server runtime, so nothing there adds a name.


