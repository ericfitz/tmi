# #347 Plan 3 — Monolith Integration for the Sandboxed Extractor Pipeline

**Date:** 2026-06-03
**Issue:** [#347](https://github.com/ericfitz/tmi/issues/347) — feat(infra): isolate document/content extractors in a sandboxed worker container (T12/T3)
**Status:** Design — approved in brainstorming; implementation deferred
**Milestone:** 1.4.0
**Parent spec:** [`2026-05-16-extractor-component-isolation-design.md`](2026-05-16-extractor-component-isolation-design.md)

---

## Context

Issue #347 is implemented in three plans. The parent spec (2026-05-16) defines the full architecture. The first two plans are **already complete and merged on `dev/1.4.0`**:

- **Plan 1 — Component platform foundation** (✅ complete): `TMIComponent` CRD (`api/platform/v1alpha1/`), the custom controller and its renderers (`internal/platform/controller/`), and the K8s platform manifests (`deployments/k8s/platform/` — kind cluster, Calico CNI, NATS JetStream, KEDA).
- **Plan 2 — Extractor & chunk-embed workers** (✅ complete): the canonical job envelope (`pkg/jobenvelope/`), the relocated extractor library (`pkg/extract/`), the worker runtime (`internal/worker/` — NATS bootstrap, durable consumer, heartbeat, env reader, naming), both worker binaries (`cmd/extractor/`, `cmd/chunkembed/`), the two `TMIComponent` CRs, and the monolith re-wiring via an alias layer so existing callers compile unchanged.

**Plan 3 — this spec — is the remaining work: the monolith integration layer.** The workers can run today, but their results never reach the monolith, the request path is still synchronous, and there is no job-state table. Plan 3 closes that gap behind a runtime flag, with the existing inline extraction path remaining the default until cutover.

### What Plan 3 does NOT cover

The kind/Calico **cluster acceptance-test suite** (the five acceptance criteria in the parent spec, lines ~330–336) and the full **"always K8s" migration of the dev environment** are deferred to **Plan 4**. Plan 3 is testable entirely in process-mode (workers as containers/processes against a NATS container), which is also the CI tier.

---

## Scope

### In scope (Plan 3)

1. **`extraction_jobs` table** — the monolith's internal job-state authority. New GORM model, `AutoMigrate` schema, `cmd/dbtool` support. Reviewed by the `oracle-db-admin` subagent.
2. **Monolith NATS/JetStream connection** — the `Server` acquires and owns an `internal/worker.Conn` for publishing jobs and running the result-consumer.
3. **`extraction.async_enabled` operational setting** — a single DB-backed `Migratable` setting (default `false`) that flips both extraction callers between the inline path and the worker path. Runtime-toggleable via the settings service shipped in #415. Fails safe to inline when NATS is unavailable.
4. **`make start-dev` brings up NATS + both workers** — extend the existing standalone-Docker dev environment so the async path runs and is testable locally. (Docker shape retained; kind/k3d migration deferred to Plan 4.)
5. **Result-consumer goroutine** — a new long-lived goroutine subscribing to `jobs.result.*` (and the dead-letter subject). Sole writer of `extraction_jobs` terminal states; updates document `access_status`, writes embedding records, emits webhook events, deletes Object Store blobs.
6. **Flag-gated publish seam** in `ContentPipeline`, exercised by both callers:
   - `api/access_poller.go` — background loop becomes a job submitter when the flag is on.
   - `api/document_sub_resource_handlers.go` — request path returns **`202 Accepted`** with a `job_id` when the flag is on.
7. **OpenAPI changes** — the `202 Accepted` response on the triggering endpoint(s); two new webhook event types. No new job-status endpoint (clients poll the existing document `access_status`).
8. **Webhook event types** — `document.extraction_completed` and `document.extraction_failed`.
9. **Process-mode tests** — unit + `_Integration` tests against a NATS service container.

### Out of scope (→ Plan 4)

- The five kind+Calico **cluster acceptance tests** (crash isolation, egress denial against a real CNI, wall-clock timeout, cgroup OOM, dead-letter).
- The **kind/k3d migration** of the full dev environment (CRD + controller + KEDA in the dev loop) and the **retirement of the standalone-Docker `make start-dev` path** — the parent spec's "always K8s" breaking change.
- `source-locator` input mode and `fetch-controlled` egress (reserved in the CRD; future code extractor).
- The general config-system rework (owned by #415, now landed; Plan 3 consumes the operational-settings mechanism).

---

## Architecture

### Integration data flow

The code-level integration centers on five pieces: the monolith NATS connection, the `extraction_jobs` table, the result-consumer goroutine, the flag-gated publish seam, and the `ContentPipeline` seam. The remaining in-scope items (the `extraction.async_enabled` setting, the `make start-dev` change, the OpenAPI change, the webhook event types, and the tests) support and exercise these.

```
                       extraction.async_enabled = true
                                    │
  caller (access_poller │ doc handler)
        │ 1. write bytes → JetStream Object Store
        │ 2. publish jobs.extract.<type>   { job_id, content_type, input{object_ref}, limits, deadline }
        │ 3. INSERT extraction_jobs (job_id, status=queued)   [OnConflict DoNothing]
        │ 4. request path: return 202 Accepted { job_id };  poller: move on
        ▼
   [tmi-extractor]  → jobs.chunkembed.<job_id>
   [tmi-chunk-embed] → jobs.result.<job_id>
        ▼
  monolith result-consumer (jobs.result.* + DLQ)
        │ validate envelope → ClassifyExtractionError(typed)
        │ UPSERT extraction_jobs (status=completed|failed, reason_code, completed_at)  [OnConflict DoUpdates]
        │ UpdateAccessStatusWithDiagnostics(document, access_status, reason_code)
        │ write document/embedding records
        │ emit document.extraction_completed | document.extraction_failed webhook
        │ delete the job's Object Store blobs
        ▼
  client learns outcome by polling document access_status (or via webhook)
```

When `extraction.async_enabled = false` (default), both callers use today's inline path byte-for-byte; the result-consumer goroutine is inert (no jobs arrive).

### Monolith NATS connection

The monolith reuses the **already-exported** `internal/worker` NATS API rather than introducing a second bootstrap path:

- `internal/worker.ConfigFromEnv()` → `internal/worker.Connect(ctx, cfg)` → `*internal/worker.Conn`.
- `Conn` already exposes `JetStream()`, `Config()`, `Close()`, `PutPayload`, `GetPayload`, `Publish`, `PublishCore`.
- **Addition required:** `DeletePayload(ctx, ref)` on `Conn`, for the result-consumer's blob cleanup. This is the only change to the (otherwise complete) Plan 2 worker package.

The `Server` owns the connection: opened at startup if NATS is configured, closed on graceful shutdown. One connection is shared by the publish-side seam and the result-consumer.

### `ContentPipeline` as the stable seam

`ContentPipeline` keeps its Go interface. Its internals branch on `extraction.async_enabled`:

- **flag off** → in-process dispatch (today's behavior).
- **flag on** → publish-and-return (write blob, publish `jobs.extract.*`, insert `queued` row).

Callers do not change shape; only their observed timing and (for the request path) status code change.

---

## Data model

### `extraction_jobs` table

The monolith's internal job-state authority. **Not client-facing** — clients never read this table directly; they poll the document `access_status`.

| Column | Type | Notes |
|---|---|---|
| `job_id` | string/UUID, **PK** | Idempotency key; equals the envelope `job_id`. |
| `document_ref` | string/UUID, **indexed** | The document being extracted. **No database-level FK** (see below). |
| `status` | string | `queued` → `extracting` → `chunk_embedding` → `completed` \| `failed`. |
| `reason_code` | string, nullable | Reuses existing `Reason*` constants on failure. |
| `stage` | string | Last-known pipeline stage, for observability. |
| `attempts` | int | Incremented on JetStream redelivery. |
| `created_at` | timestamp | Set on insert. |
| `updated_at` | timestamp | Set on every write. |
| `completed_at` | timestamp, **nullable** | Set on terminal transition. |

### Writer / reader rules

- **The result-consumer is the sole writer of terminal states** (`completed`/`failed`).
- The two publish-side callers only ever **insert** the initial `queued` row, idempotently (`OnConflict DoNothing` on `job_id`, because at-least-once delivery and retries mean the row may already exist).
- **Components (workers) never touch this table.** They communicate only via NATS.

### Status transitions actually written by the monolith

Plan 3 writes only **two** transition points:

1. `queued` — at publish time (insert).
2. `completed` / `failed` — when the terminal result lands on `jobs.result.*` (upsert).

The intermediate `extracting` / `chunk_embedding` values exist in the column's value space for forward-compatibility but are **not actively written** in Plan 3 — the workers do not report mid-pipeline progress; only the terminal result returns. The plan does not fabricate progress updates.

### No database-level foreign key

`document_ref` is an indexed column with **no DB-level FK**. Rationale:

- Avoids the PostgreSQL-vs-Oracle cascade-semantics divergence that the project explicitly guards against.
- Lets the result-consumer handle a document deleted mid-job gracefully (log + drop the result) instead of erroring on a constraint violation during a delete race.

### Idempotent upsert (PG + Oracle ADB)

The upsert uses GORM's `clause.OnConflict{Columns: [{Name: "job_id"}], DoUpdates: ...}`, which GORM translates per dialect (`ON CONFLICT` on PostgreSQL, `MERGE` on Oracle).

**Oracle verification gate (for `oracle-db-admin`):**

1. Confirm the `oracle-samples/gorm-oracle` dialect actually emits a correct `MERGE` for `clause.OnConflict`. **If it does not**, fall back to an explicit read-then-write inside a transaction (`SELECT ... FOR UPDATE` on `job_id`, then INSERT or UPDATE), which is dialect-agnostic. This fallback is the documented contingency, not the default.
2. Review the generated upsert SQL on both dialects.
3. Review nullable `completed_at` timestamp handling on both dialects.
4. Confirm the no-FK indexed `document_ref` is acceptable.

`cmd/dbtool/` is updated for the new table per the project schema-change rule.

---

## Configuration: `extraction.async_enabled`

A single **operational, monolith-local** `Migratable` setting (the middle config category from #415):

- **Key:** `extraction.async_enabled` (boolean, default `false`).
- **Source of truth:** the DB-backed settings service.
- **Runtime-toggleable** via the settings service shipped in #415 — no restart, consistent with the runtime-toggle pattern used for the content-source registry (#427).
- **Flips both callers together** — the background poller and the request path are never in mixed states.

### Fail-safe when NATS is unavailable

Turning the flag on requires a reachable NATS connection. If the monolith has no NATS connection (e.g., local non-platform dev where `make start-dev` did not bring up NATS), the flag check **fails safe to the inline path and logs a warning** rather than dropping extractions. This guarantees extractions are never silently lost by a misconfiguration. (Confirmed design posture: fail-safe-to-inline + warn, **not** a hard startup error.)

### Shared config (embedding profile)

`tmi-chunk-embed` and the monolith's Timmy query path must agree on embedding model/endpoint/dimension/key, or vector queries are silently wrong. #347 consumes the shared-config mechanism for this one profile; the mechanism itself is owned by #415 (now landed). Plan 3 wires the chunk-embed worker's embedding configuration through the shared-config object rather than worker-local env where #415 provides the projection.

---

## Error handling, failure model, and retention

### Failure classes → outcomes

| Failure | How it arrives | Result-consumer action |
|---|---|---|
| Parse failure (malformed file) | `jobs.result.*`, typed error, `extraction_failed` reason | Upsert `failed`; update document `access_status` + `reason_code`; emit `document.extraction_failed`; delete blobs |
| Wall-clock / ack-wait timeout | `jobs.result.*`, `extraction_timeout` reason | Same, with `extraction_timeout` |
| Pod death (OOM / segfault / crash) | **No result published** → ack-wait lapses → redelivery → max-deliver exceeded → **dead-letter subject** (`jobs.dlq`) | The result-consumer also subscribes to the DLQ subject and treats a dead-lettered job as `extraction_failed` |

### Zero-500 compliance

The request-path handler returns `202 Accepted` **before** any extraction outcome exists. No extraction failure can produce an HTTP 500 — all failures land asynchronously as a `failed` row + `access_status` + webhook. The `access_poller` path is a background loop (never HTTP). This is structurally 500-proof, consistent with the zero-500 policy.

### Result-consumer robustness

The result-consumer is a long-lived goroutine that must never crash the monolith:

- Each message is processed under a `recover`-guarded handler. A panic on one message logs and Naks (or Terms to DLQ), never taking down the goroutine.
- A malformed/unparseable result envelope → log + **Term** (do not redeliver forever); it is an internal contract violation, not a user error.
- A transient DB upsert failure → **Nak** for redelivery, bounded by the `attempts` column and JetStream max-deliver.
- **Idempotency:** a redelivered terminal result for an already-`completed`/`failed` job is a no-op on terminal state; blob deletion is best-effort and idempotent.
- A result for a since-deleted document → log + drop (no FK to violate).

### Blob retention / cleanup

- The result-consumer deletes the job's Object Store blobs after the result is persisted (requires the new `Conn.DeletePayload`).
- The Object Store bucket also carries a **TTL / max-age** as a backstop so abandoned jobs (whose results never arrive) self-clean. The plan confirms the bucket TTL is set in the Plan 1/2 NATS config and adds it if missing.

---

## Monolith request path

### `api/document_sub_resource_handlers.go` (request path)

- **flag off:** today's behavior (inline extraction; existing success response).
- **flag on:** publish the job, insert the `queued` row, return **`202 Accepted`** with a body carrying `job_id`. The client learns the outcome by polling the existing document `access_status` (updated by the result-consumer) or via the optional webhook callback.

The handler never blocks on extraction and never returns 500 for an extraction outcome.

### `api/access_poller.go` (background loop)

- **flag off:** today's behavior — inline `ExtractForDocument`, classify, `UpdateAccessStatusWithDiagnostics` (current seam at `access_poller.go:162`).
- **flag on:** write bytes to the Object Store, publish `jobs.extract.*`, insert the `queued` row, and move on. The result-consumer performs the `access_status` update when the result lands.

### `ClassifyExtractionError` relocation

The monolith-side classifier `ClassifyExtractionError` and the `AccessStatus*` / `Reason*` constants live authoritatively in `api/content_pipeline.go` (note: `pkg/extract/errors.go` has a *separate* worker-side classifier returning a different `Classification` type — do not conflate them). The result-consumer invokes the monolith-side `ClassifyExtractionError` against the typed errors arriving over `jobs.result.*`, instead of against in-process error chains. The existing `AccessStatus*` / `Reason*` constants are reused unchanged, preserving caller and DB-schema continuity.

---

## OpenAPI surface

`api-schema/tmi-openapi.json` is the source of truth → `make validate-openapi` + `make generate-api`.

- The document-extraction triggering endpoint(s) gain a **`202 Accepted`** response with a body carrying `job_id`. The existing success response remains for the inline/flag-off path.
- The webhook event-type enum gains `document.extraction_completed` and `document.extraction_failed`.
- **No new job-status path** — clients poll the existing document `access_status`.
- All operations retain their `x-tmi-authz` annotations (enforced by the `make lint` gate).

---

## `make start-dev` (dev environment)

`make start-dev` is extended so the async path runs and is testable locally:

- Bring up **NATS/JetStream** and **both worker containers** (`tmi-extractor`, `tmi-chunk-embed`) alongside the existing Postgres / Redis / server containers.
- The workers run against the same NATS the monolith connects to, so toggling `extraction.async_enabled` on locally exercises the full publish → worker → result-consumer → DB round-trip.
- The Docker dev shape is retained. Plan 2 already provides the Chainguard worker images and their Makefile build targets; Plan 3 wires them into the dev-environment orchestration.

**Implementation note:** follow the existing `make start-dev` orchestration (e.g. `scripts/manage-server.py` and any dev compose/container setup) rather than inventing a new mechanism. Per project policy, all container operations go through make targets.

The full kind/k3d migration and the retirement of the standalone-Docker path are **deferred to Plan 4**.

---

## Testing (process-mode only)

| Tier | Where | Covers |
|---|---|---|
| **Unit** | `make test-unit` | `extraction_jobs` repository (queued-insert idempotency, `OnConflict` upsert, terminal-state no-op); result-consumer handler with synthetic result envelopes (completed / failed / timeout / malformed-envelope / deleted-document / redelivery-idempotency); the flag-routing branch in `ContentPipeline`; the `202` handler shape |
| **Integration** (`_Integration` suffix) | `make test-integration`, NATS service container (the tier `internal/worker/pipeline_integration_test.go` already uses) | Full publish → worker → result-consumer → DB round-trip; idempotency on redelivered `job_id`; DLQ → `failed`; flag-off path unchanged |

No kind/Calico e2e in Plan 3 — the five cluster acceptance criteria are deferred to Plan 4.

---

## Implementation sequencing

Most-foundational first; each step independently testable.

1. **`extraction_jobs` model + `AutoMigrate` + `cmd/dbtool`** → dispatch `oracle-db-admin` for the schema, upsert, nullable timestamp, and no-FK review.
2. **Monolith NATS connection** on `Server` (reuse `internal/worker`; add `Conn.DeletePayload`) + **`extraction.async_enabled` Migratable setting** with the NATS-availability fail-safe.
3. **`make start-dev` brings up NATS + both workers** (so the rest of Plan 3 can be developed against a live local async path).
4. **Result-consumer goroutine** (`api/result_consumer.go`) + invoke the relocated `ClassifyExtractionError`.
5. **Flag-gated publish seam** in `ContentPipeline` + `api/access_poller.go`.
6. **Request-path `202 Accepted`** in `api/document_sub_resource_handlers.go` + OpenAPI change + `make generate-api`.
7. **Webhook event types** `document.extraction_completed` / `document.extraction_failed` (`api/events.go` + OpenAPI enum).
8. **Full test pass** (unit + integration), `make lint` / `make build-server`, and **`oracle-db-admin` sign-off** before completion.

---

## Decision log (Plan 3)

| Decision | Choice |
|---|---|
| Plan 3 scope | Integration code + `make start-dev` NATS/workers; cluster acceptance tests + kind migration → Plan 4 |
| Cutover flag | One DB-backed `Migratable` setting `extraction.async_enabled` (default false), both callers, runtime-toggleable |
| Flag + no NATS | Fail safe to inline + warn (not a hard startup error) |
| Client outcome surface | Reuse document `access_status` (+ webhook); no dedicated job-status endpoint |
| Webhook events | New `document.extraction_completed` / `document.extraction_failed` |
| Upsert mechanism | GORM `clause.OnConflict` on `job_id`; explicit-transaction fallback if gorm-oracle does not emit MERGE |
| `document_ref` ↔ documents | Plain indexed column, **no DB FK**; result-consumer tolerates deleted documents |
| Monolith NATS | Reuse `internal/worker` bootstrap; add `Conn.DeletePayload` |
| `make start-dev` | Extend existing Docker env with NATS + both workers; full kind migration deferred |
| Testing | Process-mode unit + `_Integration` only |
| Status writes | Only `queued` and terminal `completed`/`failed`; no fabricated progress |

---

## Open items carried into implementation

- **Oracle MERGE verification** — confirm `gorm-oracle` emits a correct `MERGE` for `clause.OnConflict`; adopt the explicit-transaction fallback if not. (Gated by `oracle-db-admin`.)
- **Object Store bucket TTL** — confirm a TTL/max-age is configured in the Plan 1/2 NATS bucket spec; add it if missing.
- **`Conn.DeletePayload`** — add to `internal/worker` (the only change to the completed Plan 2 package).
- **Embedding shared-config wiring** — wire `tmi-chunk-embed`'s embedding profile through the #415 shared-config projection where available.
