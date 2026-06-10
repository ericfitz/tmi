# Design: Migrate `make start-dev` to a local Kubernetes cluster

**Issue:** #442 (remainder of #347 Plan 4) · **Milestone:** 1.4.0 · **Date:** 2026-06-10

## Summary

Rework `make start-dev` so the local development environment runs the TMI app
and platform on a **local Kubernetes cluster** instead of host Go processes. The
dev workflow is **cluster-agnostic**: `start-dev` deploys into whatever cluster
the current `kubectl` context targets — a local **kind** cluster on the laptop
(macOS / Docker Desktop) or a **dedicated k3s** cluster on the desktop. The
cluster hosts the monolith server, Redis, NATS JetStream, KEDA, the
`TMIComponent` controller, and the two worker components.

**Postgres stays a host Docker container** (its current `manage-database.py`
shape) reached from the cluster as an external service — because in production
the database is always a managed provider offering outside the app cluster, so
an in-cluster Postgres would be the wrong shape. Container images reach the
cluster through a **local registry** (`localhost:5000`) that both kind and k3s
pull from. The pre-existing dev path (server + workers as host Go processes) is
retired.

An **opt-in Tilt fast loop** (`make tilt-up`) gives a sub-5-second server-only
inner loop on top of the same cluster, without compromising the prod-shaped
default that `make start-dev` produces.

This is a **breaking change to the contributor workflow**. The project has a
single active contributor today, so no coordination is required; all future
contributors experience only the cluster-based workflow.

## Goals

- `make start-dev` deploys a fully working dev environment into the current
  `kubectl` context: an in-cluster server + Redis + NATS + KEDA + controller +
  workers, plus a host Postgres container wired in as an external service.
- The same path works on kind (laptop) and k3s (desktop) with no code changes —
  only the active `kubectl` context differs.
- The server is reachable at a stable `http://localhost:8080` on the host
  (via a managed `kubectl port-forward`), so curl, tmi-ux, the OAuth callback
  stub, postman/newman, and wstest all keep working unchanged.
- The async-extraction pipeline is exercised end-to-end: an extraction job
  round-trips through `tmi-extractor`, which KEDA scales from zero.
- `make restart-dev` rebuilds the server image and rolls the pod with no new
  *required* tooling (plain make path).
- An **optional** `make tilt-up` provides a fast server-only loop, restoring the
  prod-shaped server on `make tilt-down`.
- Contributor docs (GitHub Wiki) updated with the new onramp and a
  breaking-change callout.

## Non-goals / scope boundaries

- **`start-dev` does not own cluster lifecycle.** It deploys into an existing,
  reachable cluster. Creating a local kind cluster is an *optional* helper
  (`make dev-cluster-up`); a dedicated k3s cluster is user-managed.
- **Worker/integration test targets are not migrated.** `make test-workers`
  keeps its own NATS container; `make e2e-platform-up` / `make
  test-e2e-acceptance` keep their own kind+Calico cluster. This issue migrates
  the **dev workflow only**.
- **Postgres provisioning is now shared, intentionally.** Both `make start-dev`
  and `make test-integration` use the host Postgres container via
  `manage-database.py`. This is alignment, not scope creep.
- The standalone-container `manage-*.py` scripts are **not deleted**. The dev
  start path retires only the *host-process server/worker* orchestration;
  `manage-database.py` (Postgres) is retained and now also serves dev.
- Tilt is **optional** — never a hard prerequisite.
- **NetworkPolicy enforcement is not a dev requirement.** kind's kindnet and
  k3s's default Flannel do not enforce NetworkPolicy; the controller still
  *renders* the policies, but their enforcement is validated only by the
  e2e/kind+Calico acceptance suite. Dev does not rely on it.
- Heroku and cloud (OCI/AWS/Azure/GCP) build targets are unchanged.
- Closing the in-cluster chunk-embed embedding-reachability gap is out of scope;
  tracked in **#443**.

## Key decisions (locked during brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Cluster relationship | **Cluster-agnostic** — deploy into the current `kubectl` context | Supports kind (laptop) and dedicated k3s (desktop) from one path. |
| Cluster lifecycle | `start-dev` does **not** create/delete clusters; optional `make dev-cluster-up`/`dev-cluster-down` for the kind path only | A dedicated k3s cluster is persistent infrastructure. |
| Image delivery | **Local registry** at `localhost:5000`; both backends pull from it | One portable mechanism; no `kind load` vs `ctr import` branching. |
| Server placement | **In-cluster** (Deployment) | Prod shape; the platform spine is in-cluster. |
| Postgres placement | **External host Docker container** (`manage-database.py`), wired via a manually-managed `Service` + `Endpoints` to the host IP | Prod DB is always an external managed service. Free persistence via the host volume. Portable across kind/k3s. |
| Redis placement | **In-cluster** (Deployment/Service) | Treated as a cluster-local cache for dev. |
| NATS / KEDA / controller / workers | **In-cluster** | This *is* the prod component-platform shape. |
| Host port exposure | **Managed `kubectl port-forward`** → `localhost:8080` | Uniform across kind/k3s; matches the existing NATS port-forward pattern. |
| Default inner loop | **Plain make**: rebuild image → push to registry → `rollout restart` | Zero required dev dependencies. |
| Optional fast loop | **Tilt**, server-only, Chainguard base, registry-backed `live_update` (fallback: cached image rebuild) | Sub-5s iteration when wanted; opt-in, restores prod shape on `tilt down`. |
| Worker components | **Deploy both** `tmi-extractor` + `tmi-chunk-embed` CRs | Full prod shape; KEDA keeps them at zero until a job arrives. |
| Orchestration | **Rewrite `start-dev.py`** | Keep the Python orchestrator + `tmi_common` logging/preflight helpers. |
| Namespace | **`tmi-platform`** (alongside the workers) | One namespace, simplest wiring; components already live there. |

## Architecture

### Topology

| Where | Components |
|---|---|
| **current `kubectl` context** (kind or k3s) | monolith **server**, **Redis**, NATS JetStream, KEDA, `TMIComponent` CRD + controller, `tmi-extractor`, `tmi-chunk-embed` |
| **Host Docker (external, same machine)** | **Postgres** (`manage-database.py`) on host `:5432`; **local registry** `registry:2` on `localhost:5000` |
| **Host processes (unchanged)** | OAuth callback stub, wstest, postman/newman, curl — all target `http://localhost:8080` |

### Cluster-agnostic deployment & safety

`start-dev` operates on the **current `kubectl` context**. Because that could in
principle point anywhere, it includes a **prod-protection guard**: it prints the
target context + namespace and refuses to proceed unless the context matches a
known-local allowlist (e.g. `kind-*`, the k3s/`default` context, Docker/Rancher
Desktop) or the operator passes an explicit `--context <name>` / `--yes`.

Optional helpers (kind path only):
- **`make dev-cluster-up`** — creates a local kind cluster wired to the local
  registry (the well-known kind-with-registry pattern), with the default CNI
  (NetworkPolicy enforcement not needed in dev), and points `kubectl` at it.
- **`make dev-cluster-down`** — deletes that kind cluster.

On the desktop, the user runs their dedicated k3s cluster and points `kubectl`
at it; k3s is configured once (via `/etc/rancher/k3s/registries.yaml`) to mirror
`localhost:5000`. Since k3s is on the same machine, `localhost:5000` is directly
reachable by its containerd.

### Local registry

A `registry:2` container on `localhost:5000`, started by `start-dev` if absent.
`start-dev` builds the server + worker images, tags them
`localhost:5000/tmi-server:dev` / `…/tmi-extractor:dev` / `…/tmi-chunk-embed:dev`,
and `docker push`es. All dev manifests reference the `localhost:5000/...` image
names so both backends resolve identically.

### New manifests — `deployments/k8s/dev/` (namespace `tmi-platform`)

Applied via a dev **kustomize overlay**. The overlay references the canonical
platform component CRs and patches their `spec.image` to the registry names
(JSON6902 patches — kustomize's image transformer does not rewrite CRD fields),
so the e2e component CRs are left untouched.

- **`postgres-endpoints.yml`** — a `Service` named `postgres` (no selector) plus
  a manually-managed `Endpoints`/`EndpointSlice` whose address is the
  host-reachable IP, resolved by `start-dev.py` at apply time:
  - **kind** (Docker Desktop): `host.docker.internal` (or the Docker network
    gateway IP).
  - **k3s, same machine**: the node `InternalIP` (the host's IP); the host
    Postgres listens on `0.0.0.0:5432`, reachable from pods at that IP.

  The server config stays `TMI_DATABASE_POSTGRES_HOST=postgres`; only the
  Endpoints object knows the concrete host address. There is **no in-cluster
  Postgres Deployment and no PVC** — persistence lives in the host container's
  volume.
- **`redis.yml`** — Deployment + ClusterIP Service `redis:6379` (in-cluster
  cache; `emptyDir`, no persistence needed).
- **`server.yml`** — Deployment + ClusterIP Service `tmi-server:8080`. Runs
  `localhost:5000/tmi-server:dev`. An init-container blocks until the external
  Postgres accepts connections; GORM `AutoMigrate` then builds the schema on
  boot. Readiness/liveness probe on `GET /` (no `/health` exists). Exposed to
  the host via a managed `kubectl port-forward svc/tmi-server 8080:8080`.

### Config & secrets

`config-development.yaml` remains the single baseline (not forked). The server
Deployment overrides only host/URL values via `TMI_*` env, which the config
loader already honors:

- `TMI_DATABASE_POSTGRES_HOST=postgres`  (Endpoints → host Postgres)
- `TMI_DATABASE_REDIS_HOST=redis`        (in-cluster Service)
- `TMI_NATS_URL=nats://nats.tmi-platform.svc:4222`
- `TMI_SERVER_INTERFACE=0.0.0.0`

The OAuth provider URLs reference `http://localhost:8080/...`; because the
port-forward exposes the in-cluster server there, they resolve correctly from
the host-driven OAuth flow (browser/stub).

The `tmi-embedding` Secret (`api-key`) is created from the `TMI_EMBEDDING_API_KEY`
environment variable (defaulting to the `sk-e2e-placeholder` sentinel), exactly
as `make test-e2e-workers` does today. With the placeholder, chunk-embed cannot
reach a real embedding API — consistent with the #443 gap; a developer
exercising real chunk-embed sets `TMI_EMBEDDING_API_KEY` before `start-dev`.

### Server image — `tmi-server:dev`

Add a `BUILD_TAGS` build-arg to `Dockerfile.server` so the dev image is built
with `-tags=dev` (enables `login_hint` and the test OAuth provider). The image
reuses the existing tmi-client staging the prod container build performs; the
runtime stage stays on `cgr.dev/chainguard/static` — identical to prod.

## Tilt fast inner loop (optional)

`make tilt-up` assumes `make start-dev` has already populated the current
context, and **takes over only `deploy/tmi-server`**. Everything else (Redis,
NATS, workers, external Postgres) is untouched.

Mechanism:
1. A `local_resource` compiles the binary **on the host**:
   `go build -tags=dev -o bin/tmiserver ./cmd/server` (~1–3 s, incremental).
2. `default_registry('localhost:5000')` + a `Dockerfile.server-devloop` build a
   one-COPY image on `cgr.dev/chainguard/static` (same runtime base as prod) —
   fully layer-cached after the first build.
3. Tilt `live_update` `sync()`s just the rebuilt binary into the running
   container and restarts the process via Tilt's `restart_process` extension
   (a statically-linked restart wrapper, viable on the distroless static base).
   **Fallback** if the static-base wrapper proves unworkable: Tilt rebuilds the
   trivial one-COPY image (cached except the binary layer), pushes, and rolls —
   still fast, still on the Chainguard base.
4. The server Deployment is patched to the devloop image while Tilt is up.

`make tilt-down` runs `tilt down` and **re-applies the canonical `server.yml`**,
restoring the prod-shaped server. Preflight checks `tilt` is installed and a
cluster is reachable.

## Make targets / orchestration

`start-dev.py` is rewritten to drive the sequence (keeping the `tmi_common`
logging/verbosity helpers, adding preflight + the prod-protection guard):

**`make start-dev`**
1. Preflight: `kubectl`, `docker` present; a cluster is reachable
   (`kubectl cluster-info`); context passes the prod-protection guard.
2. Start the local registry container if absent.
3. Start the host Postgres container (`manage-database.py start`) and wait for
   it to accept connections.
4. Build, tag (`localhost:5000/...`), and push: `tmi-server:dev`,
   `tmi-extractor:dev`, `tmi-chunk-embed:dev`.
5. Apply the platform base into the context (NATS, KEDA, CRD, controller).
6. Resolve the host IP for Postgres; apply the dev overlay (Redis, the Postgres
   Service+Endpoints, the server), the component CRs (image-patched to the
   registry), and the `tmi-embedding` secret.
7. Wait for the `tmi-server` rollout; start the managed `kubectl port-forward`;
   print `http://localhost:8080`.

**`make restart-dev`** (default inner loop)
- `docker build` → push to `localhost:5000` → `kubectl rollout restart
  deploy/tmi-server` → wait. Expected ~20–60 s per server change.

**`make tilt-up` / `make tilt-down`** (optional fast loop) — see above.

**`make stop-dev`** (stops *everything it deployed*, like `stop-all`)
- Delete the TMI dev resources from the context (server, Redis, component CRs,
  the Postgres Service/Endpoints) — i.e. tear down the in-cluster workloads.
- Kill the managed port-forward(s).
- Stop the host Postgres container (`manage-database.py stop`).
- Stop the local registry container.
- Stop the OAuth stub if running.
- **Does not** delete a dedicated cluster. Deleting an *ephemeral kind* cluster
  is the separate `make dev-cluster-down`.

The `--no-workers` flag is repointed to "do not apply the component CRs."

## Data flow

```
host: curl / tmi-ux / oauth-stub / postman / wstest
        │  http://localhost:8080  (managed kubectl port-forward → svc/tmi-server)
        ▼
   tmi-server (pod) ──► postgres (svc + manual Endpoints) ──► <host-ip>:5432
        │           │                                          (host Docker container)
        │           └─► redis (in-cluster svc)                 GORM AutoMigrate on boot
        │  TMI_NATS_URL
        ▼
   NATS JetStream (svc) ──► KEDA scales ──► tmi-extractor / tmi-chunk-embed (0→N)
        ▲                                         │   (images: localhost:5000/...)
        └──────────── jobs.result.* ◄────────────┘
```

## Error handling / edge cases

- **Wrong/prod context** → prod-protection guard refuses with the offending
  context name; `--context`/`--yes` to override.
- **No cluster reachable** → preflight fails fast (`kubectl cluster-info`),
  pointing at `make dev-cluster-up` (kind) or "start your k3s cluster".
- **Postgres not ready at server boot** → server init-container blocks until the
  external Postgres accepts connections.
- **Host IP resolution differs per backend** → `start-dev.py` resolves it
  per-backend and writes the Endpoints; a `--postgres-host <ip>` override is
  available if auto-detection is wrong.
- **Registry not running / image not pushed** → start-dev starts the registry
  and pushes before applying manifests; a pull failure surfaces as a clear
  `ImagePullBackOff` with the registry URL in the message.
- **chunk-embed embedding unreachable (#443)** → surfaces only when a real
  chunk-embed job runs; extraction through `tmi-extractor` is unaffected.

## Testing / acceptance criteria

- On **kind** (laptop) and on **k3s** (desktop): `make start-dev` →
  `curl http://localhost:8080/` returns the server version.
- A full OAuth login via the stub (tmi provider, `login_hint=alice`) completes
  end-to-end and yields a usable JWT.
- An async extraction job round-trips through `tmi-extractor` (KEDA scales from
  zero; result consumer updates `access_status`).
- `make restart-dev` picks up a server source change (visible at `/`).
- `make tilt-up` picks up a server source change in seconds with no manual
  rebuild; `make tilt-down` restores the prod-shaped server.
- `make stop-dev` removes all TMI workloads, stops host Postgres + registry +
  stub, and kills port-forwards — leaving any dedicated cluster intact.
- `make test-integration` and `make test-e2e-acceptance` still pass.

## Documentation

Update the **GitHub Wiki** contributor onramp:
- Prereqs: `kubectl`, `docker`; **and one of** a local kind cluster
  (`make dev-cluster-up`) or a dedicated k3s cluster with `localhost:5000`
  mirroring configured. **Optional** `tilt`.
- The local-registry setup and the k3s `registries.yaml` mirror snippet.
- New commands: `dev-cluster-up`/`down`, `start-dev`, `restart-dev`,
  `tilt-up`/`down`, `stop-dev`.
- Explicit **breaking-change callout**: the host-process dev path is retired.
- The external-Postgres model and per-backend host-IP resolution (with the
  `--postgres-host` override).

Per project rules, the `docs/` directory is not modified.

## Risks

- **Per-backend host-IP resolution** is the trickiest portable piece; the
  `--postgres-host` override and a clear preflight check de-risk it.
- **Local-registry trust config** differs per backend (kind registry ConfigMap
  vs k3s `registries.yaml`); documented one-time setup for each.
- **Tilt static-base restart wrapper**: cached-image-rebuild fallback keeps the
  loop working if the wrapper can't run on `chainguard/static`.
- **port-forward longevity**: a dropped forward needs re-establishing; the
  orchestrator monitors and restarts it.
- **chunk-embed #443 gap** persists in dev until that issue lands.

## Closing #347

#442 is the last open Plan 4 item for #347. When this lands, #347 can close.
Because the commit lands on `dev/1.4.0` (not `main`), both issues must be closed
explicitly (comment + `gh issue close`), not via a commit trailer.
