# #347 — Sandboxed Extractor as First Tenant of the TMI Component Platform

**Date:** 2026-05-16
**Issue:** [#347](https://github.com/ericfitz/tmi/issues/347) — feat(infra): isolate document/content extractors in a sandboxed worker container (T12/T3)
**Status:** Design — approved in brainstorming, pending written-spec review
**Milestone:** 1.4.0

---

## Context and intent

Issue #347 as originally written is a contained task: stand up a sandboxed extractor container with a gRPC/HTTP `Extract()` API, no network egress, cgroup caps, hard timeouts, and swap the inline `api/content_extractor_*.go` calls to a client.

During design the scope was **intentionally and substantially expanded** by the project owner. #347 is now the **first tenant** of a broader architectural shift: TMI moves from a monolith-with-deployment-variability to a Kubernetes-native system of cooperating components. #347 is designed as the *proof case* — designing it concretely surfaces the real contracts the platform needs.

This produces **three GitHub issues**:

1. **Umbrella roadmap** — the TMI Component Platform architecture (separate tracking issue).
2. **Config-system rework** — three config categories, shared cross-component config, shrink `config-*.yml` toward bootstrap-only (separate server issue).
3. **#347 itself** — this spec. Depends on (2) for shared config and on the **T3 issue** for the shared egress-guard library.

This document specifies **#347**. The umbrella and config issues are summarized here only where #347 touches them.

### Motivation

The server binary has accreted significant complexity — most recently content providers with their authorization, fetching, chunking, and embedding logic. The monolith is becoming large, and the configuration burden has grown with it. The goal is a generic component model that lets functionality be refactored out of the monolith over time, starting with content handling. Standardizing on container + Kubernetes deployment also removes the current deployment-shape variability (local Docker, Heroku, multiple cloud registries).

---

## The umbrella architecture (summary — full detail in the roadmap issue)

TMI becomes a Kubernetes-native system where:

- **The monolith is the stateful coordinator** — sole DB writer, fetch/egress owner, job-state authority, result-consumer. It does not dissolve; it becomes a coordinator.
- **Components are stateless workers** — declared as `TMIComponent` custom resources.
- **NATS JetStream** is the spine for asynchronous job work — durable subjects, fan-out, and the autoscaling signal.
- **A custom controller** reconciles each `TMIComponent` CR into a Deployment + KEDA `ScaledObject` + NetworkPolicy + NATS stream/consumer wiring.
- **KEDA** autoscales workers on JetStream queue depth, scale-to-zero capable. Neither the monolith nor the worker decides scaling.
- **Workers heartbeat** on the bus so the monolith can distinguish "type declared, no healthy instance yet" from "instances present."
- A **synchronous REST-style interaction pattern** for latency-sensitive future components (OAuth provider, webhook dispatch) is **deferred**, with a stated direction: reuse TMI's existing REST/HTTP API style rather than introduce a second protocol. The `TMIComponent` CRD is designed to accept an `interactionType` field later without a breaking change.

### `TMIComponent` CRD — the contract

A `TMIComponent` custom resource is the durable, runtime-editable declaration of a component *type*. An admin registers a new component type with `kubectl apply` — no monolith redeploy. The CRD's OpenAPI schema validates the declaration before any pod runs. Fields relevant to #347:

| Field | Purpose |
|---|---|
| `jobSubjects` | JetStream subjects this component consumes |
| `inputMode` | `content-ref` \| `source-locator` — how the component receives input |
| `egress` | `none` \| `fetch-controlled` \| `allowlist` — drives the rendered NetworkPolicy |
| `spec.config` | Non-secret component-local config values; validated by the CRD OpenAPI schema |
| `secretRefs` | References to K8s `Secret` objects; controller wires them into the pod. Secrets are **never** inlined in the CR |
| `resources` | CPU / memory limits → pod cgroup caps |
| `timeouts` | Per-job wall-clock budgets |
| `scratchVolume` | Optional capped `emptyDir` size, when a writable scratch path is needed |
| `scaling` | KEDA bounds (min/max replicas, queue-depth target) |

The controller **always** renders the cluster-layer NetworkPolicy as a backstop, regardless of any in-code egress guarding.

---

## #347 scope

### In scope

- The component platform foundation: `TMIComponent` CRD, custom controller, KEDA install, NATS/JetStream install, deployment manifests under `deployments/k8s/`.
- Two `TMIComponent` instances:
  - **`tmi-extractor`** — sandboxed parse worker (`egress: none`, `inputMode: content-ref`).
  - **`tmi-chunk-embed`** — chunk + embed worker (`egress: allowlist`, `inputMode: content-ref`).
- Worker binaries `cmd/extractor/main.go` and `cmd/chunkembed/main.go`.
- Relocation of extractor logic (`api/content_extractor_*.go`, the registry, bounded-extractor, wall-clock budget) into the extractor worker binary — **relocation, not rewrite**.
- The single job-envelope schema, including `source-locator` fields **reserved but not exercised**.
- Payload-by-reference via JetStream Object Store.
- The new `extraction_jobs` DB table and the monolith-side result-consumer.
- The monolith request-path change: triggering endpoint returns `202 Accepted` with a job reference; completion/failure surfaced via `access_status` + `reason_code` and the existing webhook mechanism.
- OpenAPI spec changes for the `202` response and the job-status surface.
- Three-tier test plan (process-mode CI, gated kind+CNI e2e, local kind dev).
- Flag-gated cutover from the in-process extraction path.

### Out of scope (reserved / deferred / delegated)

- **`source-locator` input mode** and **`fetch-controlled` egress** — defined in the CRD schema but not exercised. The future code extractor is the first tenant of these.
- **The shared egress-guard library** — owned by the **T3 issue**. #347 declares a dependency: `fetch-controlled`'s in-code guard requires it. #347's `content-ref` workers do not.
- **The general config-system rework** — owned by the separate config issue. #347 *consumes* the shared-config mechanism for the embedding profile only.
- **Synchronous REST-style components** — deferred (OAuth provider, webhook dispatch extraction).
- **Three-way extract/chunk/embed split** — #347 uses a two-component split (extractor + chunk-embed) on the security boundary.

### Dependencies

| Dependency | Owner | What #347 needs |
|---|---|---|
| Shared-config mechanism (embedding profile) | Config-system issue | `tmi-chunk-embed` and the monolith's Timmy query path must agree on embedding model/endpoint/dimension/key |
| Shared egress-guard library | T3 issue | Only for the reserved `fetch-controlled` posture; not needed for #347's `content-ref` workers |

---

## Architecture

### Component topology

Two `TMIComponent` instances, both new code (no migration risk — a deliberate first-tenant property):

**`tmi-extractor`** — sandboxed parse worker. Consumes `jobs.extract.<type>` (ooxml/pdf/html/plaintext). Hosts the relocated `ContentExtractorRegistry` extractors unchanged, with their registry, bounded-extractor detection, and wall-clock budget logic. `egress: none`. On success publishes `jobs.chunkembed.<job_id>`; on failure publishes `jobs.result.<job_id>` with a typed error.

**`tmi-chunk-embed`** — chunk + embed worker. Consumes `jobs.chunkembed.<job_id>`, calls the external embedding API (`egress: allowlist`), publishes final results to `jobs.result.<job_id>`.

The split is on the **security boundary**: the extractor is the hardened, untrusted-input-facing sandbox; chunk+embed run together in a second, network-capable component. This isolates the CVE-prone parsing code without over-fragmenting the rest.

### Job flow

```
monolith: ContentPipeline.Extract(uri)
   -> fetch bytes (existing egress helper, stays in the monolith)
   -> write bytes to JetStream Object Store
   -> publish jobs.extract.<type>      { job_id, content_type, input{object_ref,...}, limits, deadline }

tmi-extractor:
   -> read object_ref, parse, write extracted-text blob
   -> publish jobs.chunkembed.<job_id> { job_id, input{object_ref=extracted_text_ref}, metadata }

tmi-chunk-embed:
   -> read object_ref, chunk + embed, write result blob
   -> publish jobs.result.<job_id>     { job_id, status, output{result_ref} | error, reason_code }

monolith: result-consumer
   -> classify, upsert extraction_jobs row, write document/embedding records
   -> emit webhook event (completed | failed)
   -> delete the job's Object Store blobs
```

The monolith's `ContentPipeline` keeps its Go **interface** as the stable seam; its **internals** change from in-process dispatch to publish-and-await-result.

---

## Component model details

### Isolation contract

Read-only root filesystem is a **hard invariant for every component, no exceptions**. Writable space, when needed, is always an explicit, size-capped `emptyDir` — never a writable root, never `hostPath`, never a PersistentVolume. `emptyDir` is per-pod and ephemeral, preserving "per-call state wiped on pod termination."

Egress is the only axis that varies, through the CRD `egress` field with three defined values:

| Posture | Used by | NetworkPolicy |
|---|---|---|
| `none` | `tmi-extractor` | `egress: []` — no DNS, no `169.254.169.254`, no DB, no Redis, no internet. NATS only. |
| `allowlist` | `tmi-chunk-embed` | Egress to the embedding API host(s) only; credential via `secretRef`. |
| `fetch-controlled` | *reserved* (future code extractor) | Controlled outbound fetch; in-code guard from the T3 shared library + cluster-layer NetworkPolicy backstop denying metadata IP / RFC1918. |

Per-component isolation matrix:

| Property | `tmi-extractor` | `tmi-chunk-embed` | future `tmi-code-extractor` |
|---|---|---|---|
| `egress` | `none` | `allowlist` | `fetch-controlled` |
| Root FS | read-only | read-only | read-only |
| Scratch volume | none | none (or small `emptyDir`) | capped `emptyDir` |
| `runAsNonRoot` / caps dropped / `seccompProfile` | yes | yes | yes |
| Input bounding | per-doc byte / zip-member / XML-depth caps | n/a (text input) | clone depth + total-size + file-count + per-file caps |
| cgroup CPU / memory | 500m / 256 MiB | CR-declared | CR-declared (higher) |
| Wall-clock | 30s OOXML / 60s PDF | CR-declared | CR-declared |

### Defense layering

The existing in-code caps — 50 MiB fetch cap and per-member stream caps in `api/content_extractor_ooxml_common.go`, the XML element depth limit, the 10 MiB HTML cap in `api/timmy_content_provider_http.go` — **move into the extractor worker and stay**. The container sandbox is a *second* wall, not a replacement:

- A novel zip-bomb that slips the in-code cap hits the **cgroup memory limit** → OOM-kill.
- A parser CVE that hangs hits the **wall-clock budget** (and a JetStream ack-wait ceiling as backstop).
- A parser CVE that attempts exfiltration hits **`egress: []`**.

### Failure model

The worker never lets a crash escape as a stack trace. Failure classes map to typed results:

| Failure | Result |
|---|---|
| Parse failure (malformed file) | `jobs.result.*` with `reason_code` for `extraction_failed` |
| Timeout (wall-clock or JetStream ack-wait expiry) | `jobs.result.*` with `extraction_timeout` reason code |
| Pod death (cgroup OOM, segfault, crash) | No result published. JetStream ack-wait lapses → redelivery up to a max-deliver limit → **dead-letter subject**, which the monolith treats as `extraction_failed`. |

The existing `ClassifyExtractionError` classifier (`api/content_pipeline.go`) **moves to the monolith's result-consumer** — it classifies typed errors arriving over `jobs.result.*` instead of in-process error chains. The existing `AccessStatus*` / `Reason*` constants are reused unchanged, so callers and the DB schema see continuity.

**Crash isolation** — because the extractor is a separate pod with no shared memory or process, an extractor crash on a malformed file cannot affect the API server. This is #347's headline acceptance criterion, satisfied structurally.

### Discovery, scaling, liveness

- **Discovery** — the `TMIComponent` CR is the durable declaration; the monolith routes by reading CRs. A component type exists even with zero running instances.
- **Scaling** — KEDA `ScaledObject` (rendered by the controller) scales worker Deployments on JetStream queue depth, scale-to-zero capable.
- **Liveness** — workers heartbeat on the bus. The monolith distinguishes "type declared, no healthy instance yet" (enqueue and wait for KEDA) from "instances present" (route normally).

---

## Data flow and job payloads

### Single job envelope

One stable envelope schema for every stage and every component — no discriminator. Input mode is declared per-CR; the monolith populates the matching fields and validates envelope-against-contract at publish time (`source-locator` + `egress: none` is rejected).

```
job envelope (one shape, all stages):
  job_id          (idempotency key)
  content_type
  limits
  deadline
  reason_code     (on the return path)
  input:
    object_ref         (set for content-ref mode — bytes pre-supplied in Object Store)
    byte_size
    source_url         (set for source-locator mode — RESERVED, not used by #347)
    source_secret_ref  (RESERVED)
    fetch_limits       (RESERVED)
  output:
    result_ref
```

Moving the fetch in or out of the monolith later is a CR edit plus a change to which input field the monolith populates — **no envelope-schema change, no consumer migration**.

### Payload-by-reference

Fetched documents can be up to 50 MiB; JetStream messages are practically capped (~1 MiB default). Large payloads therefore travel **by reference**, never in NATS messages:

- The monolith writes fetched bytes to a **JetStream Object Store** bucket; the job message carries only `object_ref`.
- Each stage reads its input blob by `object_ref`, processes, writes its output blob, publishes the next envelope.
- Extracted text and embeddings also ride the Object Store; only the lightweight result envelope (status + reason code + ref) reaches the relational DB.
- Reading an `object_ref` is a NATS operation, not internet egress — `egress: none` holds for the extractor.

### Blob lifecycle

Blobs are transient. The Object Store bucket has a TTL/max-age. The monolith deletes a job's blobs once the result is consumed and persisted, so abandoned jobs self-clean.

### `extraction_jobs` table

New table (PostgreSQL + Oracle ADB — **must be reviewed by the `oracle-db-admin` subagent** before #347 completes; the upsert path needs PG/Oracle `MERGE` semantics review):

```
extraction_jobs:
  job_id        (PK)
  document_ref
  status        (queued -> extracting -> chunk_embedding -> completed | failed)
  reason_code   (reuses existing AccessStatus* / Reason* constants)
  stage
  attempts
  created_at
  updated_at
  completed_at
```

The monolith's `jobs.result.*` consumer is the **sole writer**. Components never access the DB.

`cmd/dbtool/` must be updated for the new table per the project schema-change rule.

### Idempotency

JetStream delivers at-least-once. `job_id` is the idempotency key: a worker that finds a result blob already present for a `job_id` skips reprocessing; the monolith's result-consumer upserts on `job_id`.

---

## Monolith-side request path

Extraction no longer completes inline. The two existing callers of `ContentPipeline` adapt differently:

**`api/access_poller.go`** — already a background loop. It becomes a pure job submitter: publishes a job and moves on, no longer blocking on extraction. The result-consumer writes the document record and updates `access_status` when the result lands.

**`api/document_sub_resource_handlers.go`** — the request-path caller. An HTTP request cannot block on a queued 60s parse, so the endpoint contract changes:

- The handler publishes a job, writes an `extraction_jobs` row as `queued`, and returns **`202 Accepted`** with the `job_id` and a status location.
- The client learns of completion by **polling** `access_status` / a job-status surface (reuses TMI's existing `access_status` + reason-code model) **or** via an **optional webhook callback** through TMI's existing webhook-dispatch mechanism.
- On result consumption the monolith updates the row to `completed` / `failed`, updates the document `access_status`, and emits the webhook event.

**Failure surface** — a failed job sets the `extraction_jobs` row to `failed` with a `reason_code` from the existing `Reason*` set, updates the document `access_status` (`extraction_failed` / `extraction_timeout`), and emits a job-failed webhook event carrying the typed `reason_code`. No HTTP 500 — the `202` was already returned. Consistent with the zero-500 policy.

**Result-consumer** — a new long-lived goroutine in the monolith subscribing to `jobs.result.*`: durable JetStream consumer, validates the envelope, runs the relocated `ClassifyExtractionError`, upserts `extraction_jobs`, writes the document/embedding records, emits webhook events, deletes the job's Object Store blobs.

**OpenAPI** — `api-schema/tmi-openapi.json` is the source of truth. The triggering endpoint's response becomes `202` with a job reference, and a job-status surface is added. This pulls in `make validate-openapi` and `make generate-api`.

---

## Configuration

The config-system rework is a **separate issue**. #347 settles the *direction* and consumes one slice of the mechanism.

### Three config categories

| Category | Examples | Source of truth | Consumers | DB-backed |
|---|---|---|---|---|
| **Bootstrap** | DB connection + creds, JWT signing secret, TLS, logging, listen port (the existing `internal/config/infrastructure_keys.go` set) | Local only — file/env (chicken-and-egg) | Monolith at startup; workers get their own minimal bootstrap (NATS address, secret-mount paths) | No |
| **Operational, monolith-local** | Feature flags, runtime tunables | DB-backed settings service (existing `Migratable` settings) | Monolith only | Yes |
| **Shared / cross-component** | Embedding profile (model, endpoint, dimension, API key); content-provider OAuth config | Platform-owned, one object | Monolith **and** components, with a correctness invariant | Yes (converges with the settings service) |

### Direction (settled; mechanism detailed in the config issue)

- **Shared config converges with the DB-backed settings service** — both are "operational config, single source of truth, runtime-editable." The controller projects the cross-component subset into worker pods.
- **`config-*.yml` shrinks toward bootstrap-only.** The ten environment/backend-specific config files collapse because most of what differs between them is operational config that should not live in files.
- **Workers never consume the monolith's config cascade.** A worker gets a minimal local bootstrap plus projected shared config — nothing else.

### Why shared config is needed for #347

Embeddings are only comparable if produced by the same model. `tmi-chunk-embed` embeds documents at ingest; the monolith embeds the user's Timmy query at search time. If they disagree on model/endpoint/dimension, vector queries are **silently wrong**. The embedding profile (model, endpoint, dimension, API-key `secretRef`) is therefore shared config with a correctness invariant — it must have one source of truth. #347 consumes the shared-config mechanism for this one profile; the config issue builds the mechanism.

### Secrets

Secrets are **never** literal values in a CR — a CRD field is stored in etcd, visible to anyone with `get` RBAC, and appears in `kubectl describe`, audit logs, and GitOps repos. The CR holds `secretRefs` pointing to K8s `Secret` objects; the controller wires the referenced Secret into the worker pod via `secretKeyRef` / volume mount. Non-secret config values are inline in `spec.config` and validated by the CRD OpenAPI schema. External Secrets Operator / Vault can populate the `Secret` objects later without touching the component contract.

The embedding API key is a **shared** secret (worker + monolith both need it): one `Secret`, referenced by the shared-config object, mounted by the controller into both — one key, one rotation point.

---

## Deployment, testing, and migration

### Deployment artifacts (new)

- `deployments/k8s/` — the `TMIComponent` CRD, the custom controller, KEDA install, NATS/JetStream install.
- Two `TMIComponent` CRs: `tmi-extractor`, `tmi-chunk-embed`.
- `cmd/extractor/main.go`, `cmd/chunkembed/main.go` — worker binaries. Each: NATS consumer loop, heartbeat publisher, the relocated extract / chunk / embed logic, typed-error production.
- Two Chainguard distroless images, `CGO_ENABLED=0`, matching existing container hardening.
- Makefile targets per worker (build / test), following the existing `make` discipline — no raw `go` / `docker`.

The extractor/chunk-embed config moves *out* of the monolith's config into the respective CRs and the shared-config object. The config-burden reduction is concrete and measurable: count the keys that leave `config-*.yml`.

### Test plan — three tiers

| Tier | Where | Covers | Frequency |
|---|---|---|---|
| **Unit / integration** | Process-mode; workers as plain processes against NATS as a GitHub Actions `services:` container | Worker logic, envelope (de)serialization, monolith↔NATS contract, `ClassifyExtractionError` on typed envelopes, idempotency on redelivered `job_id` | Every PR; extends `make test-unit` / `test-integration` |
| **E2E / cluster** | `kind` **with Calico or Cilium CNI** (kindnet does *not* enforce NetworkPolicy), CRD + controller + KEDA installed | The #347 acceptance criteria (below) | Gated merge check — PR label + pre-merge to a release branch, not every push |
| **Local dev** | `kind` / `k3d`, prod-shaped manifests | Everything, hands-on | Developer-driven |

`make start-dev` is reworked to bring up the local `kind` / `k3d` cluster instead of the current standalone Docker containers. The pre-existing standalone-Docker dev path is **retired** as part of this change — "always K8s" means dev and prod share one shape. This is the "always K8s" cost landing — an explicit work item, and a breaking change to the contributor workflow that the issue must call out.

**Critical test-design note:** kind's default CNI (kindnet) silently ignores NetworkPolicy. An egress-isolation test on a default kind cluster passes for the wrong reason. The e2e tier **must** install Calico or Cilium, or the egress acceptance criteria are not actually verified.

### Acceptance criteria (verified in the e2e tier)

- An extractor crash on a malformed file does not affect the main API server.
- The extractor pod has no DNS resolution and cannot reach `169.254.169.254`, the DB, or Redis — asserted against a real CNI.
- A wall-clock timeout test forces the extractor to hit the cap and reports a clean `extraction_timeout` result to the API.
- A memory cap test confirms a zip bomb is killed by cgroup OOM rather than allowed to expand.
- Killing the extractor mid-job dead-letters the job to a `failed` result; the API server is unaffected.

### Migration / cutover

The `ContentPipeline` Go interface is the seam. Strategy:

1. Build the workers and platform alongside the existing in-process extraction path.
2. Put the publish-vs-inline choice behind a flag.
3. Cut over per environment.
4. Delete the in-process extractor dispatch from the monolith.

The extractor *logic* moves into the worker binary — same code, new home. This is relocation, not rewrite, which bounds the risk.

---

## Open questions and deferred decisions

- **Synchronous component interaction pattern** — deferred until the first latency-sensitive tenant (OAuth provider, webhook dispatch). Stated direction: reuse TMI's existing REST/HTTP API style, not a second protocol. The `TMIComponent` CRD is designed to accept an `interactionType` field later without a breaking change.
- **Controller build vs. Helm templating** — #347 commits to a custom controller. If controller delivery proves too large for the 1.4.0 milestone, a Helm-templated interim (rendering Deployment / ScaledObject / NetworkPolicy from manifest values) is the documented fallback; the CRD-as-contract model and KEDA autoscaling are unchanged either way.

---

## Summary of decisions

| Decision | Choice |
|---|---|
| Sequencing | Umbrella sketch → #347 full design → umbrella revision |
| Component runtime model | Async workers behind a job queue |
| Queue technology | NATS JetStream |
| Component discovery | `TMIComponent` CRD as the contract; custom controller |
| Scaling | KEDA on JetStream queue depth, scale-to-zero |
| Liveness | Worker heartbeat on the bus |
| Pipeline split | Extract + chunk + embed external; fetch stays in the monolith |
| Component granularity | Two components — sandboxed extractor + chunk-embed worker |
| Embedding backend | External embedding API (`allowlist` egress) |
| Result delivery | JetStream result subject + `extraction_jobs` DB table |
| Job envelope | Single envelope, optional input fields, CR-declared input mode |
| Job completion notice | Polling (`access_status`) + optional webhook callback |
| Failure surface | `failed` row + `reason_code` + `access_status` + webhook event; no 500 |
| Egress model | Three-valued CRD posture (`none` / `fetch-controlled` / `allowlist`) |
| Filesystem | Read-only root always; writable scratch only as capped `emptyDir` |
| Config model | Three categories; shared config platform-owned; `config-*.yml` → bootstrap-only |
| Dev/test | Three tiers — process-mode CI / gated kind+CNI e2e / local kind dev |
| Issue structure | Three issues: umbrella roadmap, config-system rework, #347 |
| #347 dependencies | Config issue (shared config); T3 issue (egress-guard library) |
