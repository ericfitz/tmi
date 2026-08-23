# Config model redesign: bootstrap in files, everything else in the database

**Date:** 2026-08-22
**Status:** Approved (design)
**Supersedes parts of:** #415 (three-category model)
**Closes:** #793 (as obsolete)
**Depends on:** #807 (dbtool schema preflight) as a hard prerequisite for Phase C

## Problem

TMI's configuration has four layers (defaults → config file → environment →
database) and, for any given setting, up to four *declaration sites*: the
`Config` struct's yaml/env tags, `GetMigratableSettings()`,
`classification_registry.go`, and `DefaultSystemSettings()`. Nothing structural
keeps those four in agreement.

Two live bugs are symptoms of the same defect:

- **#793** — seven keys export from the database but are silently dropped on
  import, because export walks database rows while import walks
  `GetMigratableSettings()`. A customized value for any of them is
  unrecoverable by #792's snapshot/restore.
- **The `rate_limit.*` 404** — `rate_limit.requests_per_minute` and
  `rate_limit.requests_per_hour` are seeded into `system_settings` but have no
  classification entry (neither exact nor prefix). Unclassified resolves to
  `VisibilityInternal`, so `GET`/`DELETE /admin/settings/{key}` returns 404 for
  keys the LIST endpoint displays. This is the same list/get inconsistency
  commit `8f7b5125` fixed for five sibling keys and did not sweep up. It is live
  in production today.

The guardrail that should have caught both cannot: `ValidateClassifications`
takes a `[]MigratableSetting` and validates whatever list it is handed — in
practice `GetMigratableSettings()`, which by construction only contains keys
that already have a `Config` struct field. Keys seeded straight into the
database are invisible to it.

Underneath that, the env-vs-database precedence question has now been answered
three times (#415, #767, #794) because two sources of truth for one value is a
contest that has no stable resolution. #794 converged the rules onto one table;
this design removes the contest.

## Goals

1. Config file and environment variables are available for **bootstrap settings
   only**, and for **all** bootstrap settings.
2. Everything not needed for bootstrap is **database-only**.
3. A templating mechanism dumps current database settings to a file and imports
   a template file into a database.
4. Per-environment templates exist and are used whenever a database is cleared
   or an environment is deployed or rebuilt.
5. Precedence is simple and exists only for bootstrap: **env > file > compiled
   default**.
6. Every setting has a classification, enforced by guardrails that make an
   unclassified setting impossible rather than merely detectable.

## Non-goals

- Changing how workers receive projected shared config (the component-platform
  projection from #415 is unaffected).
- Changing settings-at-rest encryption (#547).
- Adding any new `system_settings` column. The design deliberately avoids a
  schema migration.

---

## 1. The model

### Two categories, each with exactly one delivery path

**Bootstrap** — everything needed to start the process and reach a usable,
administrable service. Source: config file and environment only. Never stored in
the database; this is already true today (`cmd/server/startup_checks.go:143`
skips bootstrap keys because "there is no DB row to diverge from").

Precedence: **env > file > compiled default**. No origin tracking, no
explicit-vs-seeded distinction, no divergence warning.

**Operational** — everything else. Source: the database only. No yaml path, no
env var, no `Config` struct field. Read through the settings service by key.

### The identity floor

The built-in `tmi` provider is bootstrap and lives in its own sub-tree,
deliberately **not** under `auth.oauth.providers.*`:

```
auth.tmi_provider.enabled       bootstrap, bool,   default false
auth.tmi_provider.callback_url  bootstrap, string, optional
```

Keeping it out of `auth.oauth.providers.*` means that prefix denotes exactly one
thing — a real IdP, database-only — with no carve-out, which is what makes the
prefix classification honest.

`auth.tmi_provider.callback_url` is optional and derives from `server.base_url`
when empty. It must exist as an explicit override because derivation is provably
insufficient: `deployments/k8s/dev/oauth-providers.env.example:48-57` documents
that k3s requires `https://tmi.efitz.net/api/oauth2/callback`, where the `/api`
prefix belongs to tmi-ux's ingress rather than to the server. No derivation from
`base_url` could produce it, and requesting the underived URL returns the SPA's
index.html instead of the callback handler.

**Translation, not special-casing.** One adapter function converts the
`auth.tmi_provider.*` bootstrap config into the same provider struct every other
IdP produces. The OAuth core is unchanged and never learns that two delivery
paths exist.

**`tmi` is a reserved provider id.** Writes to `auth.oauth.providers.tmi.*` are
rejected by the admin API with a 400 explaining that it is bootstrap-configured;
any such row already present is ignored at load with a warning. Without this,
two definitions can exist for one id and resolution order silently picks a
winner.

**Production caveat, stated plainly.** In a production build the tmi provider
refuses the authorization-code/PKCE flow (`auth/test_provider.go:87-93`) and
supports only the Client Credentials Grant; CC-grant tokens are service-account
tokens, which are categorically denied on `/admin/*` (`api/auth_helpers.go:52`,
per #399). So the tmi provider is a genuine identity floor in dev/test builds
and a machine-auth switch in production. **Production's identity floor is the
template import**, which is why goal 3 must exist before goal 2 can be true.

### `server.base_url`

Bootstrap, and **required for any non-dev build**, validated at startup rather
than silently inferred. Today `Config.GetBaseURL()`
(`internal/config/config.go:1348-1369`) falls back to inferring from `Interface`,
`Port`, and `TLSEnabled` — the *bind* address. Behind the AWS ALB every
component of that inference is wrong (scheme `http` because TLS terminates at
the load balancer, host `0.0.0.0` because the server binds all interfaces, port
`8080` because that is the container port), yielding `http://0.0.0.0:8080` where
the truth is `https://api.tmi.dev`.

The adjacent `GetCookieDomain()` carries the scar from the same class of bug:
deriving the cookie Domain from the bind address produced `Domain=0.0.0.0`,
which browsers rejected, silently discarding auth cookies (#497).

The inference path survives only in dev/test builds, where the server really is
the public endpoint, and fails closed elsewhere.

**Inference from `X-Forwarded-Host`/`X-Forwarded-Proto` is explicitly
rejected.** For a value that determines where OAuth redirects land, deriving it
from a client-suppliable header turns a spoofed header into a callback hijack.

### Restart semantics

The registry's existing `Mutability` field is promoted from decorative to
load-bearing. Every operational setting declares `Hot` or `RestartRequired`. The
admin API reports it, and a `PUT` to a restart-required setting returns 200 with
an explicit "takes effect on restart" signal rather than appearing to work.

---

## 2. One registry, one declaration per setting

Each setting is declared exactly once:

```go
{
  Key:         "auth.jwt.expiration_seconds",
  Category:    Bootstrap,            // or Operational
  Type:        Int,
  Default:     3600,
  Visibility:  AdminOnly,
  Secret:      false,
  Mutability:  RestartRequired,      // operational only
  Consumers:   []Consumer{Monolith},
  Description: "...",
  // bootstrap only:
  YAMLPath:    "auth.jwt.expiration_seconds",
  EnvVar:      "TMI_JWT_EXPIRATION_SECONDS",
}
```

Category determines which fields are legal, and validation enforces it:

- **Bootstrap** entries must have both `YAMLPath` and `EnvVar`, and no
  `Delivery`. This is goal 1's "always available for ALL bootstrap settings",
  enforced rather than hoped for. It matters because the key→env-var mapping is
  irregular and cannot be derived.
- **Operational** entries must have neither `YAMLPath` nor `EnvVar`, and must
  have a `Default` and a `Mutability`.

### Derived consumers

Three things derive from the registry instead of duplicating it:

- `DefaultSystemSettings()` becomes a projection of the operational entries. The
  database seed list stops being a hand-kept parallel list, which is what
  stranded `rate_limit.*` outside classification.
- `genconfig` and `genconfigdocs` re-point at the registry as their source (they
  generate `config-example.yml` and the config reference from the `Config`
  struct today).
- The `Config` struct keeps typed bootstrap fields for startup code, with a test
  asserting struct fields and bootstrap registry entries are in exact bijection.

`ValidateClassifications` is promoted from "validates the list you hand it" to
"validates the registry, which is the whole set by construction."

---

## 3. The template mechanism

### The artifact

One YAML file per environment. Every operational setting, with secret-classified
values represented as **references**, never material:

```yaml
# tmi-config-template v1
environment: aws-public
settings:
  auth.oauth.providers.google.client_id:     "1234....apps.googleusercontent.com"
  auth.oauth.providers.google.client_secret: !secret aws-sm://tmi/aws-public/oauth/google/client_secret
  auth.oauth.callback_url:                   "https://api.tmi.dev/oauth2/callback"
  administrators:                            ["google:...:admin@example.com"]
```

`!secret` resolves at **import** time through the existing
`internal/secrets.Provider` interface, which already implements AWS Secrets
Manager, OCI Vault, and env backends and is already constructed by dbtool. The
server never learns references exist; it reads resolved, encrypted-at-rest
database rows exactly as it does now.

### Export never emits secret material

Not even behind a flag — that flag is how plaintext ends up on a laptop. For
secret-classified keys, export emits:

- the reference from a prior template when one is supplied via
  `--template <existing>`, or
- a `TODO` placeholder that import **refuses** to resolve.

Fail-closed: a half-authored template errors loudly rather than quietly
installing an empty client secret.

**Rejected alternative:** storing the reference in a new `system_settings`
column so export could round-trip it unaided. It buys the same thing at the cost
of a schema migration and an Oracle migration review, when the file can carry
it.

### Export modes

- `--from-db` — current rows. The steady-state mode.
- `--effective` — rows *plus* currently env-resolved operational values. This is
  the cutover bridge: it captures AWS's provider config from the running system
  instead of requiring it to be hand-typed. **Temporary by construction** — once
  no operational setting has an env path it degenerates into `--from-db`, and it
  is deleted in Phase E.

### Import semantics

Convergent and idempotent. Default is upsert-what-is-in-the-template. `--prune`
(off by default) additionally deletes operational rows the template does not
mention, which is what makes a template a complete environment definition rather
than a patch. `--prune` never touches bootstrap.

Import **requires** an explicit `--environment <name>` and refuses when the
file's declared `environment:` does not match. Importing `aws-public.yaml`
against a dev database — or the reverse — costs an error message, not an outage.

Every write lands in the audit trail attributed to the template import, so
`system_audit_entries` still answers "who set this".

### `origin` is repurposed, not deleted

#794's `system_settings.origin` column was built to referee env-vs-database
precedence, a contest this design removes. It survives with a better job: export
takes only rows marked **explicit**, skipping seeded defaults. A template
therefore captures *operator intent* rather than a snapshot of whatever the
defaults happened to be — so when a default changes in a later release, restored
environments pick up the new default instead of being frozen at the old one by
their own template.

---

## 4. Per-environment templates and deploy integration

### Location, split by ownership rather than by secrecy

Because templates carry no secret material, they can be tracked. The split is
whether the environment belongs to the project or to an individual.

**Tracked** — environments the project deploys:

```
deployments/config-templates/aws-public.yaml     # production
deployments/config-templates/oci.yaml            # when it exists
```

**Untracked** — anything belonging to one developer's machine or lab, free-form
names:

```
.local/config-templates/k3s-rp.yaml              # a personal dev cluster
.local/config-templates/docker-desktop.yaml      # per-developer local
```

Resolution searches `.local/config-templates/` **first**, then the tracked
directory, so a developer can shadow a tracked template locally without editing
a tracked file. That is also how a production config change gets tested safely.

### Deploy integration

Both hooks already exist and are re-pointed rather than invented:

- `deploy-aws.sh` has a `--config-export FILE` argument that already runs a
  dbtool import as Phase 7. It targets the environment template and becomes
  non-optional for a fresh environment.
- `scripts/devenv.py` already snapshots before every teardown and restores after
  `start()` (#792).

### Ordering

The server owns schema migration, so:

```
apply workloads
  → server boots (migrates schema; no providers yet, not yet usable)
  → import template
  → restart (or wait out the settings cache)
  → verify identity
```

The "not yet usable" window is inherent to database-only identity and is the
price of goal 2.

This makes **#807 a hard prerequisite, not a nicety**: dbtool must preflight the
target schema version and refuse to import against a database the current server
has not migrated. Once template import is the only path into a new environment,
a silent failure there produces an environment nobody can log in to.

---

## 5. What gets deleted

- ~48 operational fields from the `Config` struct, with their yaml paths and env
  tags.
- The env-vs-database precedence machinery from #794. `GetResolvedString`
  collapses to a plain database read for operational and a plain config read for
  bootstrap. The dual-accessor hazard that PR eliminated stops being *possible*
  rather than being refereed.
- `warnOnConfigDatabaseDivergence` and its startup warning — there is no longer
  a contest to warn about.
- `nonRoundTrippingKeys` in `cmd/dbtool/config_export.go`.

### The three derived aliases, resolved

`nonRoundTrippingKeys` names three keys as "derived display values", each
shadowing a real source. Under one registry a key is either a real setting or it
is not; a third state called "display alias that silently does not import" is
precisely what produced #793. Resolutions:

- **`session.timeout_minutes`** — delete as a setting. It is a computed view of
  `auth.jwt.expiration_seconds`. If tmi-ux needs it on `/config`, `/config`
  computes it.
- **`features.saml_enabled`** — promote to a real operational setting with
  `Mutability: RestartRequired`, gating SAML manager construction in place of
  `TMI_SAML_ENABLED`. This resolves the #794 asymmetry (where the env var gated
  construction regardless of the database row) rather than documenting it.
  Per-provider `enabled` flags remain separate rows.
- **`auth.oauth_callback_url`** — two names for one value. Keep
  `auth.oauth.callback_url`, consistent with the `auth.oauth.providers.*`
  sub-tree; migrate existing rows and delete the alias.

---

## 6. Cutover

| Phase | What | Risk |
| --- | --- | --- |
| A | Build the registry + total-coverage guardrail. No behavior change. | None — pure refactor, tests prove equivalence |
| B | Build the template tool: references, `--effective`, `--environment` guard. #807 preflight lands here. | None — new tool, nothing depends on it yet |
| C | Per environment: export `--effective` → import → restart → verify identity, **env vars still present** | Reversible; env remains the live source until D |
| D | Per environment: remove the operational env vars | The real cutover; one environment at a time, k3s before AWS |
| E | Delete operational config/env code paths, `--effective`, and the #794 precedence machinery | None — dead by then |

A and B are safe to land in any release. C and D are per-environment and
independently revertible. E happens only after every environment has completed
D, which the Phase-E guardrail test asserts directly.

---

## 7. Testing

### Registry guardrails (these are goal 6, as build-failing tests)

- **Total coverage:** every key reachable by the server — `Config` struct field,
  registry entry, or seeded row — resolves to a registry entry with a non-zero
  `Category`. This is the test that would have caught `rate_limit.*`.
- **Category legality:** bootstrap entries have both `YAMLPath` and `EnvVar` and
  no `Delivery`; operational entries have neither, plus a `Default` and a
  `Mutability`.
- **Bijection:** bootstrap registry entries ↔ `Config` struct fields, exactly.
- **Goal 2's enforcement:** no operational key has an `env` or `yaml` tag
  anywhere in the `Config` struct. This doubles as the Phase-E completion gate —
  when it passes, the cutover is provably done.
- `tmi` is reserved: writes to `auth.oauth.providers.tmi.*` are rejected.

### Template round-trip

- export → import → export is byte-identical.
- **Fail-closed secrecy test:** seed known secret values, export, assert the file
  contains none of them. This is the test that stops a future refactor from
  quietly reintroducing plaintext export.
- A `TODO` placeholder refuses to import rather than installing an empty secret.
- `--environment` mismatch refuses.
- `--prune` deletes only unmentioned operational rows and never touches
  bootstrap.
- CI check: any *tracked* template whose secret-classified key holds anything
  but a `!secret` reference fails the build.

### Behavior

- Bootstrap precedence table test: env > file > default, including the
  empty-string-vs-unset distinction that has bitten this repo on Oracle.
- Restart-required settings return the "takes effect on restart" signal from the
  admin API rather than appearing to apply.

### Integration — the test that actually matters

Fresh database + template import + restart yields a **working login**. Every
other test here can pass while the system is unusable. This is what proves
Phases C and D are safe, and it runs against k3s before AWS is touched.

### Oracle

`oracle-db-admin` review is mandatory before completion. Making
`DefaultSystemSettings()` a projection changes seeding behavior, and the
explicit-only export changes which rows are read — both are DB-touching. The
design adds **no new column**, which keeps this a behavior review rather than a
migration review.

---

## 8. Issue disposition

- **#793** — close as obsolete. The export/import registry mismatch cannot exist
  once `DefaultSystemSettings()` is a projection of the one registry.
- **`rate_limit.*` 404** — file separately and fix independently. It is live in
  production now and this work spans several releases.
- **#807** — promoted from follow-up to a hard prerequisite for Phase C.
- **#803** (expose `origin` via the admin settings API) — still wanted; `origin`
  survives this design with a new purpose.
- **#805** (`ReEncryptAll` stamps `modified_by`, blunting it as an intent
  signal) — becomes more relevant, since explicit-vs-seeded now decides what a
  template captures.
