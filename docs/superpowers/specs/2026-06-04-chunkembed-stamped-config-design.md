# tmi-chunk-embed — Stamped-Config Embedding Profile + Self-Healing Secret Mount

**Date:** 2026-06-04
**Issue:** [#347](https://github.com/ericfitz/tmi/issues/347) — Plan 3 open item: "Embedding shared-config wiring"
**Status:** Design — awaiting user review
**Milestone:** 1.4.0
**Parent spec:** [`2026-06-03-347-plan3-monolith-integration-design.md`](2026-06-03-347-plan3-monolith-integration-design.md) (open item #4, line 302)
**Branch:** `dev/1.4.0`

---

## Problem

The `tmi-chunk-embed` worker reads its entire embedding configuration from worker-local
environment variables (`TMI_EMBEDDING_MODEL`, `TMI_EMBEDDING_BASE_URL`,
`TMI_EMBEDDING_API_KEY`) at startup, via `embedConfigFromEnv()`
([cmd/chunkembed/embedder.go:23](../../../cmd/chunkembed/embedder.go)). It also hardcodes
chunk sizing (`chunkMaxChars=512`, `chunkOverlap=50`) as package constants
([cmd/chunkembed/handler.go:42](../../../cmd/chunkembed/handler.go)).

This has two consequences:

1. **Operational breakage.** After the #415 config migration, the canonical embedding
   configuration lives in the DB (`system_settings`, keys `timmy.text_embedding_*` and
   `timmy.embedding_dimension`/`timmy.chunk_*`). Nothing projects those into the worker's
   `TMI_EMBEDDING_*` env vars in local dev, so the worker exits immediately at startup with
   `required env var TMI_EMBEDDING_MODEL is not set`.
2. **Correctness risk.** The worker's embedding profile and chunk sizing can silently
   diverge from the monolith's Timmy query path, which now reads the same values from the
   DB. Disagreement on model/dimension makes vector search silently wrong.

The fix is the work the codebase already half-built and explicitly deferred: route the
embedding **profile** through the `StampedConfig` job envelope (monolith DB-config →
envelope → worker), and resolve the API **key** from a mounted secret rather than a startup
env var.

## Goals

- Worker obtains embedding **profile** (model, endpoint, dimension) and **chunk sizing**
  (size, overlap) from the **job envelope**, sourced from the monolith's DB-config —
  giving a structural guarantee that document-ingest embedding and query embedding use the
  same profile.
- Worker resolves the API **key** from a **mounted secret file** (re-read per job), so a
  key update self-heals on the next job without a restart. The key never travels on NATS.
- Missing/unreadable key fails the in-flight job **terminally** with a typed reason and
  flips the worker heartbeat to **degraded**, so the condition is visible in the server log.
- Local `make start-dev` works again with no manual env exports.

## Non-goals

- Putting any secret in the job envelope (rejected; preserves the documented
  no-secret-on-NATS invariant in [stamped_config.go](../../../internal/config/stamped_config.go)).
- A shared monolith↔worker encryption scheme (none exists; not needed).
- Giving the worker DB access (rejected; preserves the worker egress allowlist).
- The broader Plan 3 integration (result-consumer, `extraction_jobs`, `202` path) — already
  covered by the parent spec.

---

## Architecture & data flow

```
  monolith ExtractionPublisher
    ├─ StampedConfigProvider.Get()  → reads timmy.text_embedding_* + timmy.chunk_* from DB
    ├─ map config.StampedConfig → jobenvelope.StampedConfig   (non-secret only)
    └─ publish jobs.extract.<kind>  { ..., stamped_config }
         │
         ▼
   [tmi-extractor]  forwards job.StampedConfig unchanged onto:
         └─ publish jobs.chunkembed.<id>  { ..., stamped_config }
              │
              ▼
   [tmi-chunk-embed] Handle(job):
        profile := job.StampedConfig            (model, endpoint, dimension, chunk size/overlap)
        apiKey  := bootstrap.ReadSecret("embedding-api-key")   ← mounted file, re-read per job
        client  := embedClient{profile + apiKey}               ← reassembled in-memory
        embedder := embedderCache.get(client)                  ← cached by full tuple
        chunk with profile.Chunk; embed with embedder
```

**Transport split, in-memory bundle.** The non-secret profile travels in the envelope; the
secret key travels via the mount. The worker reassembles both into a single `embedClient`
value so worker code reads as one cohesive config. This keeps the secret off NATS (no
at-rest/replayable plaintext, never seen by the extractor) while preserving the
ingest==query shared-invariant guarantee that `StampedConfig` exists to provide.

**Self-heal.** `bootstrap.ReadSecret` (`os.ReadFile` at call time —
[bootstrap.go:79](../../../internal/config/bootstrap/bootstrap.go)) is invoked per job, so a
changed key file is picked up on the next delivery. In k8s the key is a **mounted secret
volume** (kubelet live-refreshes the file in place); in local dev it is a temp file written
by `manage-workers.py`.

---

## Components & changes

### 1. Wire contract — `pkg/jobenvelope/envelope.go` + `validate.go`

- Add `StampedConfig *StampedConfig` (pointer, `json:"stamped_config,omitempty"`) to `Job`.
  Pointer so un-stamped envelopes (older producers, flag-off) deserialize cleanly and the
  worker can detect "not stamped."
- Define an **envelope-local** shape (no import of `internal/config` — `pkg` must not depend
  on `internal/config`):
  ```go
  type StampedConfig struct {
      Embedding EmbeddingProfile `json:"embedding"`
      Chunk     ChunkProfile     `json:"chunk"`
  }
  type EmbeddingProfile struct {
      Model     string `json:"model"`
      Endpoint  string `json:"endpoint"`
      Dimension int    `json:"dimension"`
  }
  type ChunkProfile struct {
      Size    int `json:"size"`
      Overlap int `json:"overlap"`
  }
  ```
- `validate.go`: a non-nil `StampedConfig` must satisfy: `Model != ""`, `Endpoint != ""`,
  `Dimension > 0`, `Chunk.Size > 0`, `0 <= Chunk.Overlap < Chunk.Size`.

### 2. `config.StampedConfig` — `internal/config/stamped_config.go`

- Add `Chunk ChunkProfile { Size, Overlap int }` alongside the existing `Embedding`.
  Extend `StampedConfig.Validate()` to validate the chunk profile.
- (The existing `EmbeddingProfile` here is unchanged; the API key remains deliberately
  excluded per the file's own doc comment.)

### 3. Monolith stamp — `api/stamped_config_provider.go` + `api/extraction_publisher.go`

- `stampedConfigProvider.Get()` additionally reads `timmy.chunk_size` and
  `timmy.chunk_overlap` (both already present in `system_settings` and surfaced by
  `migratable_settings.go`) and populates `StampedConfig.Chunk`.
- `ExtractionPublisher` gains a `stamped config.StampedConfigProvider` field (constructor
  param). In `Publish`, call `Get(ctx)`, map `config.StampedConfig → jobenvelope.StampedConfig`,
  and set it on the job. If `Get` fails, **fail the publish** with a clear error — never
  publish an unstamped job. `cmd/server/main.go` passes the already-constructed provider
  (`api.NewStampedConfigProvider(settingsService)`) into the publisher.

### 4. Extractor forward — `cmd/extractor/handler.go`

- One field added to the `next` envelope at
  [handler.go:91](../../../cmd/extractor/handler.go): `StampedConfig: job.StampedConfig`.
  The extractor neither inspects nor mutates it (and never sees the key — it is not in the
  envelope).

### 5. Worker — `cmd/chunkembed/`

- **`main.go`:** replace `worker.ConfigFromEnv()` + `embedConfigFromEnv()` + the startup
  `newEmbedder` call with `bootstrap.LoadWorker()`. No embedder is built at startup. The
  worker still requires `TMI_WORKER_NATS_URL` (already a `bootstrap.LoadWorker` requirement)
  and a configured `embedding-api-key` mount (`TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY`),
  but a *missing key value* no longer prevents startup — it is handled per job (§7).
  > **Note:** `bootstrap.LoadWorker` reads `TMI_WORKER_NATS_URL`/`TMI_WORKER_HEARTBEAT_SUBJECT`,
  > whereas the current worker uses `worker.ConfigFromEnv` (`TMI_NATS_URL`, `TMI_COMPONENT_NAME`).
  > The worker still needs the component name + a `*worker.Conn`. Implementation reconciles
  > the two readers: keep `worker.ConfigFromEnv()` for the NATS `Conn`/component identity and
  > use `bootstrap.LoadWorker()` only for `SecretMounts`/`ReadSecret`. (The two are
  > complementary, not mutually exclusive; this avoids re-plumbing the NATS connection.)
- **`embedder.go`:** delete `embedConfigFromEnv`. Add:
  - `type embedClient struct { Model, Endpoint string; Dimension int; apiKey string }` —
    the reassembled cohesive config.
  - A profile+key-keyed embedder cache: `map[string]embeddings.Embedder` guarded by a mutex,
    keyed by a string derived from `Model|Endpoint|Dimension|sha256(apiKey)` so a key
    rotation correctly rebuilds the client and a stale embedder is never reused.
  - `embedderFor(c embedClient) (embeddings.Embedder, error)` — cache lookup or build via the
    existing `newEmbedder` logic.
- **`handler.go`:**
  - Read `job.StampedConfig`; if nil or invalid → **terminal** failure with
    `extract.ReasonExtractionInternal` (producer bug; not retryable).
  - Read the key via `bootstrap.ReadSecret("embedding-api-key")`; if missing/unreadable →
    **terminal** failure with the new `extract.ReasonEmbeddingNotConfigured` and flip the
    heartbeat health to degraded (§6/§7).
  - Build the chunker from `StampedConfig.Chunk` (delete the `chunkMaxChars`/`chunkOverlap`
    constants).
  - Get the embedder via `embedderFor`, then chunk + embed as today.

### 6. Heartbeat health — `internal/worker/heartbeat.go`

- Add `Status string` (`"healthy"` | `"degraded"`) and `Detail string` to `Heartbeat`.
- `RunHeartbeat` gains a health-probe parameter — a `func() (status, detail string)` — so the
  worker can report current health each tick. The chunk-embed handler sets an atomic
  "degraded + reason" flag when a key read fails and clears it on the next successful key read
  (self-heal is observable: the heartbeat returns to healthy once the key is fixed).
- Existing callers (`tmi-extractor`) pass a probe that always returns `"healthy"` — no
  behavior change for workers that don't have a degraded mode.

### 7. Failure semantics & server-log reporting

| Condition | Failure mode | Reason | Heartbeat |
|---|---|---|---|
| `StampedConfig` missing/invalid | terminal (publish failed result) | `ReasonExtractionInternal` | unchanged |
| Key missing/unreadable | terminal (publish failed result) | `ReasonEmbeddingNotConfigured` (new) | degraded + detail |
| Embedding API error (5xx/rate-limit) | retryable (nak; existing behavior) | — | unchanged |

- **New typed reason** `ReasonEmbeddingNotConfigured` added to `pkg/extract` (the worker-side
  reason constants), used by the worker's `publishFailure`.
- The monolith's **result-consumer** already logs failed results and emits
  `document.extraction_failed` — a key-missing failure therefore surfaces in the server log
  and the events stream with the new reason code, no extra wiring beyond mapping the reason.
- **Heartbeat consumer (conditional):** a search of the current tree shows the monolith does
  **not** yet consume `components.heartbeat.*` (only the worker publishes it and
  `cmd/worker-probe` reads it). The degraded-status field is added to the `Heartbeat` message
  regardless (cheap, forward-compatible). Logging degraded status to the server log is wired
  **only if** a monolith heartbeat consumer exists or is being added; otherwise the
  guaranteed server-log path is the result-envelope failed-result above, and the heartbeat
  field is consumed by `worker-probe`/future health tooling. This keeps the reporting
  guarantee honest: the result-envelope path always surfaces the condition; the heartbeat
  path is the bonus "visible even when no jobs flow" channel where a consumer exists.
- Rationale for **terminal** (not retryable) on a missing key: key fixes typically take
  longer than the 3 `MaxDeliver` redeliveries, so naking would dead-letter real work and
  churn the queue. Terminal-fail-fast + degraded heartbeat is loud and cheap; self-heal still
  applies to the **next** job once the key file appears.

### 8. Local-dev glue — `scripts/manage-workers.py`

- Before launching `tmi-chunk-embed`:
  1. Read `timmy.text_embedding_api_key` from the dev DB (the value is already present in
     `system_settings`).
  2. Write it to a `0600` temp file under the project (e.g. `.local-secrets/embedding-api-key`,
     gitignored).
  3. Export `TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY=<that path>` and the worker's
     NATS/component env.
- Remove the old "optional embedding env vars not set: `TMI_EMBEDDING_*`" warning and the
  startup dependence on those vars — the profile now arrives via the envelope; the only
  worker-side requirement is NATS + the key mount.
- Result: `make start-dev` brings the worker up cleanly with the embedding config that is
  already in the dev DB, no manual exports.

### 9. k8s controller — `internal/platform/controller/render_deployment.go` + CRD + manifest

- The chunk-embed `TMIComponent` Secret moves from an **env projection**
  (`secretRefs → valueFrom.secretKeyRef`, fixed at pod start) to a **mounted secret volume**
  (file, kubelet live-refreshed) so the in-cluster path self-heals like local dev.
- `render_deployment.go` emits a `Volume` (secret) + `VolumeMount` for the embedding key, and
  sets `TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY` to the mount path.
- The `TMIComponent` CRD gains a `secretMounts` concept (logical-name → secretName/secretKey
  → mount path) distinct from the env-projecting `secretRefs`. `secretRefs` is retained for
  non-self-healing secrets.
- `deployments/k8s/platform/components/tmi-chunk-embed.yml`: drop
  `TMI_EMBEDDING_MODEL`/`TMI_EMBEDDING_BASE_URL` from `config` (profile now arrives via
  envelope), replace the `secretRefs` key entry with a `secretMounts` entry.

---

## Data model

No DB schema change. All values consumed already exist in `system_settings`
(`timmy.text_embedding_model`, `timmy.text_embedding_base_url`, `timmy.embedding_dimension`,
`timmy.text_embedding_api_key`, `timmy.chunk_size`, `timmy.chunk_overlap`). **No
`oracle-db-admin` gate is triggered** — there are no migrations, model/tag changes, raw SQL,
FK/cascade, or transaction/locking changes. (Reads go through the existing settings service.)

---

## Testing

| Tier | Where | Covers |
|---|---|---|
| **Unit** | `make test-unit` | `jobenvelope.StampedConfig` validation (valid / missing field / overlap≥size); `config.StampedConfig.Validate` with chunk; `stampedConfigProvider.Get` populates chunk; `ExtractionPublisher.Publish` stamps the envelope and fails when `Get` errors; worker `embedderFor` cache (hit/miss/rotation); worker `Handle` with synthetic jobs: valid stamped+key (success), missing stamped (terminal internal), missing key (terminal not-configured + degraded), key-appears-then-next-job-succeeds (self-heal); `Heartbeat` status field marshals; extractor forwards `StampedConfig` |
| **Integration** (`_Integration` suffix) | `make test-integration`, NATS service container | Full publish → extractor → chunk-embed round-trip carries the profile end-to-end; embedding produced with the stamped profile; key read from a mounted file path; degraded heartbeat observed when the file is absent and healthy after it appears |

`pkg/jobenvelope` is `pkg`-level and importable by tests without the monolith.

---

## Implementation sequencing

Each step independently buildable/testable.

1. **Envelope contract** — `jobenvelope.StampedConfig` + `Job` field + `validate.go` + unit tests.
2. **`config.StampedConfig` chunk extension** + `stampedConfigProvider.Get` chunk reads + unit tests.
3. **Monolith stamp** — `ExtractionPublisher` provider dependency + `Publish` stamping + `cmd/server/main.go` wiring + unit tests.
4. **Extractor forward** — one line + assertion in extractor test.
5. **Worker** — `embedClient` + cache + `Handle` rewrite (profile from envelope, key from mount, chunk from envelope) + `ReasonEmbeddingNotConfigured` + unit tests.
6. **Heartbeat health** — `Status`/`Detail` + probe param + extractor passes healthy probe + chunk-embed degraded wiring + monolith heartbeat-consumer log + unit tests.
7. **Local-dev glue** — `manage-workers.py` writes the key temp file, sets the mount env, drops the old `TMI_EMBEDDING_*` checks; verify `make start-dev` brings the worker up.
8. **k8s controller** — `render_deployment.go` mounted-secret-volume + CRD `secretMounts` + manifest update + render tests.
9. **Quality gates** — `make lint`, `make build-server`, `make test-unit`, `make test-integration`. (No `oracle-db-admin`; no DB-touching change.)

---

## Decision log

| Decision | Choice |
|---|---|
| Profile source | Job envelope (`StampedConfig`), stamped by monolith from DB-config — structural ingest==query guarantee |
| Key source | Mounted secret file, re-read per job via `bootstrap.ReadSecret` (self-healing) |
| Key on the wire? | No — preserves documented no-secret-on-NATS invariant; no encryption infra needed |
| Bundling | Split transport (profile=envelope, key=mount); reassembled into one in-memory `embedClient` |
| Embedder lifecycle | Built per job, cached by full `Model|Endpoint|Dimension|hash(key)` tuple |
| Chunk sizing | Also moved into `StampedConfig` (`timmy.chunk_*`), closing the other Plan-3 stub |
| Missing key behavior | Terminal job failure (`ReasonEmbeddingNotConfigured`) + degraded heartbeat; self-heal on next job |
| Missing/invalid profile | Terminal job failure (`ReasonExtractionInternal`) — producer bug, not retryable |
| Reporting | Result-envelope failed-result (→ server log + `document.extraction_failed`) is the guaranteed path; degraded heartbeat (→ server log) is added only where/if a monolith heartbeat consumer exists |
| k8s secret delivery | Mounted secret **volume** (kubelet live-refresh), not `valueFrom` env projection |
| DB review | Not required — no schema/SQL/FK/txn change; reads via existing settings service |

---

## Open items carried into implementation

- **`bootstrap.LoadWorker` vs `worker.ConfigFromEnv` reconciliation** (§5 note) — keep both:
  `ConfigFromEnv` for the NATS `Conn`/component identity, `LoadWorker` for secret mounts.
  Confirm env-var names don't collide.
- **CRD `secretMounts` shape** — finalize the field schema on `TMIComponent` and ensure the
  controller render tests cover both `secretRefs` (env) and `secretMounts` (volume).
- **Heartbeat-consumer existence** — confirm the monolith already consumes
  `components.heartbeat.*`; if not, the degraded-status log is added where heartbeats are
  consumed (or deferred with the result-envelope reporting as the guaranteed path).
