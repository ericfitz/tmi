# Timmy Runtime DB-Backed Config — Design

**Date:** 2026-05-21
**Status:** Approved (brainstorming) — pending implementation plan
**Branch:** dev/1.4.0

## Problem

The Timmy AI assistant cannot be turned on or reconfigured without a server
restart, and its configuration is effectively frozen at startup even though the
`timmy.*` settings already live in the database.

The entire Timmy subsystem is wired from the **frozen `config.TimmyConfig`
struct loaded at boot**:

- `api.TimmyEnabledMiddleware(config.Timmy)` captures the config by value
  ([cmd/server/main.go:912](../../../cmd/server/main.go)).
- `initializeTimmySubsystem` hard-returns when `cfg.Timmy.Enabled` is false at
  boot and builds the LLM service, session manager, and vector manager once from
  `cfg.Timmy` ([cmd/server/main.go:1122](../../../cmd/server/main.go)).
- `NewTimmyLLMService(cfg.Timmy, ...)` constructs live LangChainGo clients
  (`chatModel`, `textEmbedder`) at startup.

Consequences observed in the dev instance:

- The DB has `timmy.enabled = true` plus an OpenAI provider/model, but the
  server logs `Timmy middleware configured (enabled=false, configured=false)`
  because the startup config (no `timmy:` block in `config-development.yml`, no
  `TMI_TIMMY_*` env vars) defaults `Enabled` to `false`.
- The `timmy.llm_api_key` / `timmy.text_embedding_api_key` rows do not exist.
- `embedding_dimension` is `0` and `llm_model` is the placeholder
  `gpt-5.4-cyber`.

This contradicts the #415 config-refactor goal ("operational config is
DB-backed, runtime-tunable, no restart required") and `config-development.yml`'s
own header, which states that Timmy config "lives in the database."

## Goal

Make the **Timmy AI core** fully DB-backed and runtime-tunable:

- Turn Timmy on/off without a restart.
- Rotate the API key, change the model/provider/base URL/embedding dimension
  without a restart.
- Adjust tuning knobs (top-k, timeouts, rate limits, history) without a restart.

## Scope

### In scope — the "Timmy AI core"

- **Enable gate** (`timmy.enabled`): read from the DB per request in the
  middleware.
- **LLM + embedding clients** (provider, model, base_url, api_key, dimension):
  **lazy rebuild on next request** when a wiring-setting change is detected via a
  stable hash of the wiring fields.
- **Vector index manager**: rebuilt as part of the same lazy reload.
- **Tuning knobs** (top-k, rate limits, conversation history, chunk size/overlap,
  session caps): read **live per request**, independently of the wiring-hash
  rebuild. A knob change does NOT trigger an LLM-client rebuild (knobs are
  excluded from the wiring hash); instead the session manager reads them through
  a live-config closure (`cfgFor(ctx)`) and the rate limiter reads its thresholds
  through a `limits()` closure on each check, preserving sliding-window state.
  (`llm_timeout_seconds` and `operator_system_prompt` are the exceptions — they
  ARE baked into the client/prompt at build time, so they live in the wiring hash
  and take effect on the next rebuild.)
- **Precedence**: config-first preserved — `env/config file > database`,
  identical to every other operational key (`SettingsService.GetString/GetBool/
  GetInt` already implement this). In dev (no Timmy YAML/env), the DB wins,
  giving runtime-tunable behavior. A stray `TMI_TIMMY_*` env var shadows the DB,
  same rule as all other settings — chosen for consistency over special-casing.
- **Data load**: real values imported into the DB via
  `dbtool --import-legacy` (`enabled=true`, `llm_model=gpt-5.5`,
  `embedding_dimension=3072`, base URLs, API key from `~/Desktop/lmk`).

### Out of scope — deferred to a follow-up issue

- The content-source registry (Google Workspace / Confluence / Microsoft
  delegated sources) and the access-poller goroutine remain **startup-wired**;
  changing those still requires a restart. Their `os.Exit(1)` startup
  validations are untouched. The embedding cleanup goroutine already runs
  unconditionally (independent of `timmy.enabled`), so it is unaffected.

### Separate issue (not this spec)

- A build-mode-aware startup warning: when any Secret-classified setting has a
  non-empty value in `system_settings` while settings encryption is disabled,
  emit a log line — `dev` build mode → WARN, `production` → ERROR — but never
  fail startup. This applies to all secrets, not just Timmy.

## Architecture

### New components

#### `TimmyConfigProvider` (`api/timmy_config_provider.go`)

- `Current(ctx) config.TimmyConfig` — reads all `timmy.*` keys via
  `SettingsServiceInterface` (honoring config-first precedence, since
  `GetString/GetBool/GetInt` already do that) and assembles a
  `config.TimmyConfig`. Reuses the existing struct so downstream signatures do
  not change.
- `WiringHash(cfg) string` — a stable hash over only the *wiring* fields
  (provider, model, base_url, api_key, dimension for LLM + text embedding + code
  embedding + rerank, plus `llm_timeout_seconds` and `operator_system_prompt`
  which are baked in at client/prompt construction). Used to detect "needs
  rebuild." Tuning-knob changes do NOT change the hash — they take effect via
  the live-config closures wired into the session manager and rate limiter (see
  below), so they apply without an LLM-client rebuild.
- Cheap reads ride the existing settings cache (60s TTL) and its invalidation on
  `PUT /admin/settings`.

#### `TimmyCore` (`api/timmy_core.go`) — rebuildable holder

- Fields: `llmService`, `vectorManager`, `sessionManager`, plus the
  `wiringHash` they were built from, behind a `sync.RWMutex`.
- `Get(ctx) (*TimmyRuntime, error)`:
  - RLock; if the cached `WiringHash` matches the current config's hash, return
    the live `*TimmyRuntime` (fast path).
  - If it differs (or the core is empty), upgrade to WLock, double-check the
    hash (prevents two concurrent rebuilds), rebuild the LLM/embedder/vector/
    session objects from `provider.Current(ctx)`, swap them in, store the new
    hash, release.
  - Rebuild failure (bad key, unreachable endpoint at construction, malformed
    base_url) returns a typed error → handler maps to 503. The bad config does
    NOT poison the cache; the next request retries the rebuild, so fixing the
    setting recovers without a restart.
- In-flight safety: a request holds its `*TimmyRuntime` pointer for the duration
  of the call. A swap replaces the holder's pointer but never mutates an object
  an in-flight request is using. Old objects are GC'd once their last request
  returns.

#### `TimmyEnabledMiddleware` (modified)

- Takes the `TimmyConfigProvider` instead of a frozen `config.TimmyConfig`.
- Per request: `provider.Current(ctx).Enabled` → 404 if false;
  `.IsConfigured()` → 503 if not configured.

#### Handlers (`chat/sessions`, `admin/timmy`)

- Obtain the session manager via `server.timmyCore.Get(ctx)` instead of a
  startup-injected `timmySessionManager`.

#### Live tuning knobs (no rebuild)

- The session manager holds an optional live-config reader
  (`SetLiveConfig(func(ctx) config.TimmyConfig)`) and resolves knob reads
  (`MaxSessionsPerThreatModel`, `TextRetrievalTopK`, `CodeRetrievalTopK`,
  `MaxConversationHistory`, `ChunkSize`/`ChunkOverlap`) through `cfgFor(ctx)`,
  falling back to its frozen build-time config when no reader is wired (unit
  tests). The chunker is built on demand per ingest from the live values.
- The rate limiter holds its three thresholds behind a `limits()` closure read
  on each check; the sliding-window + concurrency state is preserved across knob
  edits. Both closures read the same `TimmyConfigProvider` the core uses, so a
  knob-only change (which leaves the wiring hash unchanged and serves the same
  `*TimmyRuntime`) is still picked up live on the next request.

### What stays the same

`TimmyLLMService`, `TimmySessionManager`, and `VectorIndexManager` keep their
current constructors and internals. `TimmyCore` calls those constructors on
rebuild instead of `main.go` calling them once. Content sources, access poller,
and the embedding cleanup goroutine are unchanged and remain startup-wired.

### Startup flow change

`initializeTimmySubsystem` no longer hard-returns when `cfg.Timmy.Enabled` is
false at boot. It always constructs the `TimmyConfigProvider` + an empty
`TimmyCore` and wires the middleware and handlers to them. The first Timmy
request after enablement triggers the first build. The content-source block
inside `initializeTimmySubsystem` continues to read the boot-time config (out of
scope, startup-wired).

## Data Flow

Request flow for e.g. `POST /chat/sessions`:

1. `TimmyEnabledMiddleware` → `provider.Current(ctx)` → reads `timmy.enabled`.
   - false → `404 "Timmy AI assistant is not enabled"`.
   - `!IsConfigured()` → `503`.
2. Handler → `server.timmyCore.Get(ctx)`:
   - `provider.Current(ctx)` assembles the live `TimmyConfig`; compute
     `WiringHash`.
   - Hash matches cached → return existing `*TimmyRuntime` (RLock only).
   - Hash differs/empty → WLock, rebuild, store new hash, return new runtime.
3. Handler uses the returned `*TimmyRuntime` for the whole request.

## Error Handling

- **Rebuild failure** (bad api_key, malformed base_url, provider construction
  error): `Get` returns a typed error; handler maps to
  `503 "Timmy is temporarily unavailable"` (no stack traces — Zero-500 policy).
  The bad config does not poison the cache; the next request retries, so fixing
  the setting recovers without a restart.
- **Settings read failure** (DB down): "cannot determine config" → 503, logged
  at WARN.
- **Concurrent rebuilds**: the WLock serializes; the in-write-lock hash
  double-check prevents two requests both rebuilding.
- **Disabled mid-session**: in-flight requests finish on their held runtime; new
  requests get 404 once the flag flips and the cache refreshes (≤60s TTL, or
  immediately after a `PUT /admin/settings` cache invalidation).

## Data Migration (`dbtool --import-legacy`)

1. Create a throwaway import file (e.g. `/tmp/timmy-import.yml`, **not
   committed**) with a `timmy:` block carrying the real values: `enabled: true`,
   `llm_provider: openai`, `llm_model: gpt-5.5`, `llm_base_url`,
   `text_embedding_provider: openai`,
   `text_embedding_model: text-embedding-3-large`, `text_embedding_base_url`,
   `embedding_dimension: 3072`, and `llm_api_key` + `text_embedding_api_key`
   from `~/Desktop/lmk`.
2. Run `bin/tmi-dbtool --import-legacy -f /tmp/timmy-import.yml` with
   `--no-rewrite` so the tool does not rewrite a committed config file with the
   secret embedded.
3. Delete `/tmp/timmy-import.yml`.

Result: `timmy.*` rows in `system_settings` carry the real values (plaintext in
dev, since encryption is off — flagged for the separate warning issue).
`gpt-5.4-cyber` → `gpt-5.5`, `embedding_dimension` `0` → `3072`, API-key rows
created.

## Testing

### Unit

- `TimmyConfigProvider.Current` assembles the correct `TimmyConfig` from a mock
  settings service.
- `WiringHash` changes iff a wiring field changes (not when a tuning knob
  changes).
- `TimmyCore.Get` returns the same runtime when the hash is stable, rebuilds
  when it changes, returns a typed error on rebuild failure, and retries on the
  next call.
- `TimmyEnabledMiddleware` 404/503 matrix (disabled, enabled-but-unconfigured,
  enabled-and-configured).

### Integration

- Enable Timmy via settings → `POST /chat/sessions` succeeds without restart.
- Flip `timmy.enabled=false` via `PUT /admin/settings` → next call 404s.

### Live verification

- Import values via `dbtool --import-legacy`; confirm the server log shows Timmy
  reachable and a Timmy endpoint responds.

## Follow-up issues (filed)

1. **#427 — Content-source + access-poller runtime toggling**: make the
   content-source registry (Google Workspace / Confluence / Microsoft) and the
   access-poller goroutine come up / tear down on runtime `timmy.enabled`
   changes; convert their `os.Exit(1)` startup validations to graceful per-source
   disable.
2. **#428 — Build-mode-aware secrets-at-rest startup warning**: when any
   Secret-classified setting has a non-empty DB value while settings encryption
   is disabled, log WARN (dev) / ERROR (prod) without failing startup.
