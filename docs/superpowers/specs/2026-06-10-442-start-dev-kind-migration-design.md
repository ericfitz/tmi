# Design: Migrate `make start-dev` to a local kind cluster

**Issue:** #442 (remainder of #347 Plan 4) · **Milestone:** 1.4.0 · **Date:** 2026-06-10

## Summary

Rework `make start-dev` so the local development environment runs as a single
**kind** cluster that hosts the *entire* stack — the monolith server, Postgres,
Redis, NATS JetStream, KEDA, the `TMIComponent` controller, and the two worker
components. The pre-existing dev path (server + workers as host Go processes
against containerized Postgres/Redis/NATS) is retired. "Always K8s" — dev and
prod share one shape.

This is a **breaking change to the contributor workflow**. The project has a
single active contributor today, so no coordination is required; all future
contributors experience only the kind-based workflow.

## Goals

- `make start-dev` brings up a fully working dev environment in one kind cluster.
- The server is reachable at a stable `http://localhost:8080` on the host, so
  curl, tmi-ux, the OAuth callback stub, postman/newman, and wstest all keep
  working unchanged.
- The async-extraction pipeline is exercised end-to-end: an extraction job
  round-trips through `tmi-extractor`, which KEDA scales from zero.
- A plain `make restart-dev` rebuilds the server image and rolls the pod with no
  new dev tooling (no Tilt/Skaffold).
- Contributor docs (GitHub Wiki) updated with the new onramp and a
  breaking-change callout.

## Non-goals / scope boundaries

- **Test targets are not migrated.** `make test-integration` keeps spinning its
  own ephemeral Postgres container; `make test-workers` keeps its own NATS
  container; `make e2e-platform-up` / `make test-e2e-acceptance` are untouched.
  This issue migrates the **dev workflow only**.
- The standalone-container `manage-*.py` scripts are **not deleted** — the test
  targets still use them. Only their use in the dev *start path* is retired.
- Heroku and cloud (OCI/AWS/Azure/GCP) build targets are unchanged.
- Closing the in-cluster chunk-embed embedding-reachability gap is out of scope;
  it is tracked separately in **#443**.

## Key decisions (locked during brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Dev topology | **Full in-cluster** (server + Postgres + Redis all in kind) | Literal "always K8s — dev and prod share one shape." |
| Inner loop | **Plain make**: rebuild image → `kind load` → `rollout restart` | Zero new dev dependencies, fully transparent in the Makefile. |
| Worker components | **Deploy both** `tmi-extractor` + `tmi-chunk-embed` CRs | Full prod shape; KEDA keeps them at zero until a job arrives. |
| Postgres persistence | **PVC** (kind local-path provisioner) | Data survives pod restarts and server rollouts for the cluster's lifetime. |
| Orchestration | **Rewrite `start-dev.py`** | Keep the Python orchestrator + `tmi_common` logging/preflight helpers. |
| Namespace | **`tmi-platform`** (alongside the workers) | One namespace, simplest wiring; components already live there. |

## Architecture

### Cluster

One kind cluster named `tmi-platform`, reusing the existing
`deployments/k8s/platform/kind-cluster.yml` (Calico CNI, `disableDefaultCNI`).
`start-dev` runs the existing platform-base steps (Calico → NATS → KEDA → CRD →
controller) and then layers a **dev overlay**.

`kind-cluster.yml` gains `extraPortMappings`: containerPort `30080` → hostPort
`8080`. This is additive and harmless to the e2e flow that shares this file.

### New manifests — `deployments/k8s/dev/`

All in namespace `tmi-platform`.

- **`postgres.yml`** — Deployment + ClusterIP Service `postgres:5432` + a `PVC`
  (default `standard` / local-path StorageClass) + a Secret/env carrying the
  dev creds from `config-development.yaml` (`tmi_dev` / `dev123` / `tmi_dev`).
  Image: reuse `Dockerfile.postgres` (or the Chainguard postgres base it builds
  from).
- **`redis.yml`** — Deployment + ClusterIP Service `redis:6379`. Image: reuse
  `Dockerfile.redis` / Chainguard redis.
- **`server.yml`** — Deployment + **NodePort** Service (nodePort `30080`,
  targetPort `8080`). Runs `tmi-server:dev`. An init-container blocks until
  Postgres accepts connections; GORM `AutoMigrate` then builds the schema on
  boot. Readiness/liveness probe on `GET /` (the root endpoint — there is no
  `/health`).

No dev-specific NetworkPolicy is required: Calico is allow-all except where the
controller's deny-by-default policies *select component pods*. The dev
server/pg/redis pods are unselected, so they communicate freely.

### Config & secrets

`config-development.yaml` remains the single baseline (not forked). The server
Deployment overrides only host/URL values via `TMI_*` env, which the config
loader already honors:

- `TMI_DATABASE_POSTGRES_HOST=postgres`
- `TMI_DATABASE_REDIS_HOST=redis`
- `TMI_NATS_URL=nats://nats.tmi-platform.svc:4222`
- `TMI_SERVER_INTERFACE=0.0.0.0`

The OAuth provider URLs in `config-development.yaml` reference
`http://localhost:8080/...`; because the host port maps to the in-cluster
server, these resolve correctly from the host-driven OAuth flow (browser/stub).

The `tmi-embedding` Secret (`api-key`) is created from the `TMI_EMBEDDING_API_KEY`
environment variable (defaulting to the `sk-e2e-placeholder` sentinel), exactly
as `make test-e2e-workers` does today. It backs both `tmi-chunk-embed` and the
monolith's Timmy query path. With the placeholder value, chunk-embed cannot
reach a real embedding API — which is consistent with the #443 gap; a developer
exercising real chunk-embed sets `TMI_EMBEDDING_API_KEY` before `start-dev`.

### Server image — `tmi-server:dev`

Add a `BUILD_TAGS` build-arg to `Dockerfile.server` so the dev image is built
with `-tags=dev` (enables `login_hint` and the test OAuth provider). The image
reuses the existing tmi-client staging that the prod container build performs.

## Make targets / orchestration

`start-dev.py` is rewritten to drive the kind sequence (keeping the
`tmi_common` logging + verbosity helpers and adding a preflight that checks for
`kind`, `kubectl`, and `docker`):

**`make start-dev`**
1. Preflight: `kind`, `kubectl`, `docker` present.
2. If the cluster is down, create it + the platform base (reuse the
   `e2e-platform-up` steps: Calico, NATS, KEDA, CRD).
3. Build & `kind load`: `tmi-server:dev`, `tmi-extractor:dev`,
   `tmi-chunk-embed:dev` (reuse the worker image build from `test-e2e-workers`).
4. Deploy the controller.
5. Apply the dev overlay (`deployments/k8s/dev/`), the component CRs
   (`deployments/k8s/platform/components/`), and the `tmi-embedding` secret.
6. Wait for the `tmi-server` rollout; print `http://localhost:8080`.

**`make restart-dev`** (the chosen inner loop)
- `docker build tmi-server:dev` → `kind load` → `kubectl rollout restart
  deploy/tmi-server` → wait for rollout. Expected ~30–90 s per server change.

**`make stop-dev`**
- `kind delete cluster --name tmi-platform`.

The `--no-workers` flag is repointed to "do not apply the component CRs" (the
workers are scale-to-zero now, so the distinction is mostly about whether the
CRs exist). `stop-all` and the per-service stop targets reconcile so the dev
teardown is `kind delete`.

## Data flow (unchanged from prod shape)

```
host curl / tmi-ux / oauth-stub
        │  http://localhost:8080  (kind extraPortMapping → NodePort 30080)
        ▼
   tmi-server (pod) ──► postgres (svc)        GORM AutoMigrate on boot
        │           └─► redis (svc)
        │  TMI_NATS_URL
        ▼
   NATS JetStream (svc) ──► KEDA scales ──► tmi-extractor / tmi-chunk-embed (0→N)
        ▲                                         │
        └──────────── jobs.result.* ◄────────────┘
```

## Error handling / edge cases

- **Postgres not ready at server boot** → server init-container blocks until
  Postgres accepts connections; the server pod does not start early.
- **Cluster already up** → `start-dev` is idempotent: it detects the existing
  cluster and re-applies manifests / re-rolls images rather than recreating.
- **Missing prerequisites** → preflight fails fast with an actionable message
  (which of `kind`/`kubectl`/`docker` is missing).
- **chunk-embed embedding unreachable in-cluster (#443)** → surfaces only when a
  real chunk-embed job runs; extraction through `tmi-extractor` is unaffected.
  Documented as a known gap, not a `start-dev` failure.

## Testing / acceptance criteria

- `make start-dev` → `curl http://localhost:8080/` returns the server version.
- A full OAuth login via the stub (tmi provider, `login_hint=alice`) completes
  end-to-end against the in-cluster server and yields a usable JWT.
- An async extraction job round-trips through `tmi-extractor` (KEDA scales it
  from zero, result consumer updates `access_status`).
- `make restart-dev` picks up a server source change (version/string change
  visible at `/`).
- `make stop-dev` deletes the cluster cleanly.
- `make test-integration` and `make test-e2e-acceptance` still pass (proving the
  scope boundary held — test provisioning untouched).

## Documentation

Update the **GitHub Wiki** contributor onramp:
- New prerequisites: `kind`, `kubectl`, a container runtime able to host a kind
  cluster; resource expectations (Calico + KEDA + NATS + workers + app).
- New commands: `make start-dev` / `make restart-dev` / `make stop-dev`.
- Explicit **breaking-change callout**: the standalone-Docker/host-process dev
  path is retired.

Per project rules, the `docs/` directory is not modified.

## Risks

- **Inner-loop latency**: docker build + `kind load` + rollout is ~30–90 s per
  server change vs near-instant `go build && run`. Accepted.
- **chunk-embed #443 gap** persists in dev until that issue lands.
- **Server dev image build** depends on the tmi-client staging the prod
  container build performs; the dev build path must reuse that plumbing.

## Closing #347

#442 is the last open Plan 4 item for #347. When this lands, #347 can close.
Because the commit lands on `dev/1.4.0` (not `main`), the issues must be closed
explicitly (comment + `gh issue close`), not via a commit trailer.
