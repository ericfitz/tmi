# Design: Migrate `make start-dev` to a local kind cluster

**Issue:** #442 (remainder of #347 Plan 4) · **Milestone:** 1.4.0 · **Date:** 2026-06-10

## Summary

Rework `make start-dev` so the local development environment runs the TMI app
and platform as a **kind** cluster instead of host Go processes. The cluster
hosts the monolith server, Redis, NATS JetStream, KEDA, the `TMIComponent`
controller, and the two worker components. **Postgres stays a host Docker
container** (its current `manage-database.py` shape) reached from the cluster as
an external service — because in production the database is always a managed
provider offering outside the app cluster, so an in-cluster Postgres would be
the wrong shape. The pre-existing dev path (server + workers as host Go
processes) is retired.

An **opt-in Tilt fast loop** (`make tilt-up`) gives a sub-5-second
server-only inner loop on top of the same cluster, without compromising the
prod-shaped default that `make start-dev` produces.

This is a **breaking change to the contributor workflow**. The project has a
single active contributor today, so no coordination is required; all future
contributors experience only the kind-based workflow.

## Goals

- `make start-dev` brings up a fully working dev environment: an in-cluster
  server + Redis + NATS + KEDA + controller + workers, plus a host Postgres
  container wired in as an external service.
- The server is reachable at a stable `http://localhost:8080` on the host, so
  curl, tmi-ux, the OAuth callback stub, postman/newman, and wstest all keep
  working unchanged.
- The async-extraction pipeline is exercised end-to-end: an extraction job
  round-trips through `tmi-extractor`, which KEDA scales from zero.
- A `make restart-dev` rebuilds the server image and rolls the pod with no new
  required tooling (plain make path).
- An **optional** `make tilt-up` provides a fast server-only loop (compile on
  host → sync binary → restart process) for tight iteration, restoring the
  prod-shaped server on `make tilt-down`.
- Contributor docs (GitHub Wiki) updated with the new onramp and a
  breaking-change callout.

## Non-goals / scope boundaries

- **Worker/integration test targets are not migrated.** `make test-workers`
  keeps its own NATS container; `make e2e-platform-up` / `make
  test-e2e-acceptance` are untouched. This issue migrates the **dev workflow
  only**.
- **Postgres provisioning is now shared, intentionally.** Both `make start-dev`
  and `make test-integration` use the host Postgres container via
  `manage-database.py`. This is alignment, not scope creep — the dev DB and the
  integration-test DB are the same shape.
- The standalone-container `manage-*.py` scripts are **not deleted**. The dev
  start path retires only the *host-process server/worker* orchestration;
  `manage-database.py` (Postgres) is retained and now also serves dev.
- Tilt is **optional**. Contributors who do not install it use the plain
  `make restart-dev` loop. Tilt is never a hard prerequisite.
- Heroku and cloud (OCI/AWS/Azure/GCP) build targets are unchanged.
- Closing the in-cluster chunk-embed embedding-reachability gap is out of scope;
  it is tracked separately in **#443**.

## Key decisions (locked during brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Server placement | **In-cluster** (Deployment) | Prod shape; the platform spine is in-cluster. |
| Postgres placement | **External host Docker container** (`manage-database.py`), wired via an `ExternalName` Service | Prod DB is always an external managed service; in-cluster PG is the wrong shape. Free persistence via the host volume. |
| Redis placement | **In-cluster** (Deployment/Service) | Treated as a cluster-local cache for dev. |
| NATS / KEDA / controller / workers | **In-cluster** | This *is* the prod component-platform shape. |
| Default inner loop | **Plain make**: rebuild image → `kind load` → `rollout restart` | Zero required dev dependencies. |
| Optional fast loop | **Tilt**, server-only, Chainguard base, binary `live_update` + restart wrapper (fallback: cached image rebuild) | Sub-5s iteration when wanted; opt-in, restores prod shape on `tilt down`. |
| Worker components | **Deploy both** `tmi-extractor` + `tmi-chunk-embed` CRs | Full prod shape; KEDA keeps them at zero until a job arrives. |
| Orchestration | **Rewrite `start-dev.py`** | Keep the Python orchestrator + `tmi_common` logging/preflight helpers. |
| Namespace | **`tmi-platform`** (alongside the workers) | One namespace, simplest wiring; components already live there. |

## Architecture

### Topology

| Where | Components |
|---|---|
| **kind cluster `tmi-platform`** | monolith **server**, **Redis**, NATS JetStream, KEDA, `TMIComponent` CRD + controller, `tmi-extractor`, `tmi-chunk-embed` |
| **Host Docker (external)** | **Postgres** (`manage-database.py`), published on host `:5432` |
| **Host processes (unchanged)** | OAuth callback stub, wstest, postman/newman, curl — all target `http://localhost:8080` |

`start-dev` runs the existing platform-base steps (Calico → NATS → KEDA → CRD →
controller, reusing `e2e-platform-up`) and layers a **dev overlay**.

`kind-cluster.yml` gains `extraPortMappings`: containerPort `30080` → hostPort
`8080`. This is additive and harmless to the e2e flow that shares this file.

### New manifests — `deployments/k8s/dev/` (namespace `tmi-platform`)

- **`postgres-externalname.yml`** — a `Service` of type `ExternalName` named
  `postgres`, `externalName: host.docker.internal`. The in-cluster server
  reaches the host Postgres container at `postgres:5432`. On Docker Desktop
  (macOS) `host.docker.internal` resolves out of the box; on Linux the kind
  nodes need a `host-gateway` host entry (documented). **Fallback** if DNS
  chaining proves unreliable: a `Service` (no selector) + `Endpoints` object
  whose address is the host-gateway IP, resolved by `start-dev.py` at apply
  time. There is **no in-cluster Postgres Deployment and no PVC** — persistence
  lives in the host container's volume.
- **`redis.yml`** — Deployment + ClusterIP Service `redis:6379` (in-cluster
  cache; `emptyDir`, no persistence needed). Image: reuse `Dockerfile.redis` /
  Chainguard redis.
- **`server.yml`** — Deployment + **NodePort** Service (nodePort `30080`,
  targetPort `8080`). Runs `tmi-server:dev`. An init-container blocks until the
  external Postgres accepts connections; GORM `AutoMigrate` then builds the
  schema on boot. Readiness/liveness probe on `GET /` (the root endpoint — there
  is no `/health`).

No dev-specific NetworkPolicy is required: Calico is allow-all except where the
controller's deny-by-default policies *select component pods*. The dev
server/redis pods are unselected, so they communicate freely (including to the
external Postgres and to NATS).

### Config & secrets

`config-development.yaml` remains the single baseline (not forked). The server
Deployment overrides only host/URL values via `TMI_*` env, which the config
loader already honors:

- `TMI_DATABASE_POSTGRES_HOST=postgres`  (ExternalName → host Postgres)
- `TMI_DATABASE_REDIS_HOST=redis`        (in-cluster Service)
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
The runtime stage stays on `cgr.dev/chainguard/static` — identical to prod.

## Tilt fast inner loop (optional)

`make tilt-up` assumes `make start-dev` has already brought up the cluster +
infra + workers, and **takes over only `deploy/tmi-server`** for fast iteration.
Everything else (Redis, NATS, workers, the external Postgres) is untouched.

Mechanism:
1. A `local_resource` compiles the binary **on the host**:
   `go build -tags=dev -o bin/tmiserver ./cmd/server` (~1–3 s, incremental).
2. A `Dockerfile.server-devloop` builds a one-COPY image on
   `cgr.dev/chainguard/static` (the same runtime base as the prod stage-2
   image) — closest possible to prod, fully layer-cached after the first build.
3. Tilt `live_update` `sync()`s just the rebuilt binary into the running
   container and restarts the process via Tilt's `restart_process` extension
   (a statically-linked restart wrapper, viable on the distroless static base).
   **Fallback** if the static-base wrapper proves unworkable: Tilt rebuilds the
   trivial one-COPY image (cached except the binary layer) + `kind load` +
   `rollout restart` — still fast, still on the Chainguard base.
4. The server Deployment is patched to the `tmi-server:devloop` image while Tilt
   is up.

`make tilt-down` runs `tilt down` and **re-applies the canonical `server.yml`**,
restoring `deploy/tmi-server` to the prod static image — the env returns to
exactly what `start-dev` produced.

Preflight: `make tilt-up` checks that `tilt` is installed and the cluster is up,
failing fast with an actionable message otherwise. Tilt is never required by any
other target.

## Make targets / orchestration

`start-dev.py` is rewritten to drive the sequence (keeping the `tmi_common`
logging + verbosity helpers and adding a preflight that checks for `kind`,
`kubectl`, and `docker`):

**`make start-dev`**
1. Preflight: `kind`, `kubectl`, `docker` present.
2. Start the host Postgres container (`manage-database.py start`) and wait for
   it to accept connections.
3. If the cluster is down, create it + the platform base (reuse the
   `e2e-platform-up` steps: Calico, NATS, KEDA, CRD).
4. Build & `kind load`: `tmi-server:dev`, `tmi-extractor:dev`,
   `tmi-chunk-embed:dev` (reuse the worker image build from `test-e2e-workers`).
5. Deploy the controller.
6. Apply the dev overlay (`deployments/k8s/dev/` — Redis, the Postgres
   ExternalName Service, the server), the component CRs
   (`deployments/k8s/platform/components/`), and the `tmi-embedding` secret.
7. Wait for the `tmi-server` rollout; print `http://localhost:8080`.

**`make restart-dev`** (default inner loop)
- `docker build tmi-server:dev` → `kind load` → `kubectl rollout restart
  deploy/tmi-server` → wait for rollout. Expected ~30–90 s per server change.

**`make tilt-up` / `make tilt-down`** (optional fast loop) — see above.

**`make stop-dev`**
- `kind delete cluster --name tmi-platform` and stop the host Postgres container
  (`manage-database.py stop`).

The `--no-workers` flag is repointed to "do not apply the component CRs" (the
workers are scale-to-zero now, so the distinction is mostly about whether the
CRs exist). `stop-all` and the per-service stop targets reconcile so the dev
teardown is `kind delete` + Postgres stop.

## Data flow

```
host: curl / tmi-ux / oauth-stub / postman / wstest
        │  http://localhost:8080  (kind extraPortMapping → NodePort 30080)
        ▼
   tmi-server (pod) ──► postgres (ExternalName svc) ──► host.docker.internal:5432
        │           │                                   (host Docker container)
        │           └─► redis (in-cluster svc)          GORM AutoMigrate on boot
        │  TMI_NATS_URL
        ▼
   NATS JetStream (svc) ──► KEDA scales ──► tmi-extractor / tmi-chunk-embed (0→N)
        ▲                                         │
        └──────────── jobs.result.* ◄────────────┘
```

## Error handling / edge cases

- **Postgres not ready at server boot** → server init-container blocks until the
  external Postgres accepts connections; the server pod does not start early.
- **`host.docker.internal` unresolvable from pods** (Linux, or DNS chaining
  failure) → fall back to the Service+Endpoints-with-host-gateway-IP form
  resolved by `start-dev.py`.
- **Cluster already up** → `start-dev` is idempotent: it detects the existing
  cluster and re-applies manifests / re-rolls images rather than recreating.
- **Missing prerequisites** → preflight fails fast naming which of
  `kind`/`kubectl`/`docker` (or `tilt`, for `tilt-up`) is missing.
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
- `make tilt-up` picks up a server source change in seconds without a manual
  rebuild; `make tilt-down` restores the prod-shaped server (image back to
  `tmi-server:dev` / static).
- `make stop-dev` deletes the cluster and stops the host Postgres cleanly.
- `make test-integration` and `make test-e2e-acceptance` still pass.

## Documentation

Update the **GitHub Wiki** contributor onramp:
- New prerequisites: `kind`, `kubectl`, a container runtime; **optional** `tilt`.
- Resource expectations (Calico + KEDA + NATS + workers + Redis + app).
- New commands: `make start-dev` / `make restart-dev` / `make tilt-up` /
  `make tilt-down` / `make stop-dev`.
- Explicit **breaking-change callout**: the host-process dev path is retired.
- Note the external-Postgres model and `host.docker.internal` (with the Linux
  host-gateway caveat).

Per project rules, the `docs/` directory is not modified.

## Risks

- **`host.docker.internal` DNS chaining** through CoreDNS can be
  platform-specific; the Endpoints-with-IP fallback de-risks it.
- **Tilt static-base restart wrapper**: if the wrapper cannot run on
  `chainguard/static`, the cached-image-rebuild fallback keeps the loop working
  (slower than binary sync, still on the Chainguard base).
- **Inner-loop latency** on the plain path: ~30–90 s per server change. Tilt is
  the answer for contributors who want faster.
- **chunk-embed #443 gap** persists in dev until that issue lands.

## Closing #347

#442 is the last open Plan 4 item for #347. When this lands, #347 can close.
Because the commit lands on `dev/1.4.0` (not `main`), the issues must be closed
explicitly (comment + `gh issue close`), not via a commit trailer.
