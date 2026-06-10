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

**The database is external and developer-owned via config — exactly as in
production.** The server pod dials whatever `database.*` connection the current
`config-development.yml` specifies: a local host Postgres container, a cloud
Postgres, or an OCI ADB. There is no Kubernetes Service abstraction for the DB;
the pod connects directly to the configured endpoint, just as prod connects to
RDS/ADB. Config is delivered into the cluster **dynamically on every deploy**, so
swapping DB targets is an edit-and-redeploy.

Container images reach the cluster through a **local registry**
(`localhost:5000`) that both kind and k3s pull from. The pre-existing dev path
(server + workers as host Go processes) is retired.

An **opt-in Tilt fast loop** (`make tilt-up`) gives a sub-5-second server-only
inner loop on top of the same cluster, without compromising the prod-shaped
default that `make start-dev` produces.

This is a **breaking change to the contributor workflow**. The project has a
single active contributor today, so no coordination is required; all future
contributors experience only the cluster-based workflow.

## Goals

- `make start-dev` deploys a fully working dev environment into the current
  `kubectl` context: an in-cluster server + Redis + NATS + KEDA + controller +
  workers, talking to an external DB defined entirely by the developer's config.
- The same path works on kind (laptop) and k3s (desktop) with no code changes —
  only the active `kubectl` context differs.
- **DB target is dynamic**: local Postgres, cloud Postgres, or OCI ADB, selected
  by editing config (+ `DB=oracle` for the Oracle image variant) and
  redeploying. Config travels with each new server build.
- The server is reachable at a stable `http://localhost:8080` on the host
  (via a managed `kubectl port-forward`), so curl, tmi-ux, the OAuth callback
  stub, postman/newman, and wstest all keep working unchanged.
- The async-extraction pipeline round-trips through `tmi-extractor` (KEDA scales
  it from zero).
- `make restart-dev` rebuilds the server image, re-delivers the current config,
  and rolls the pod with no new *required* tooling.
- An **optional** `make tilt-up` provides a fast server-only loop, restoring the
  prod-shaped server on `make tilt-down`.
- Contributor docs (GitHub Wiki) updated with the new onramp and a
  breaking-change callout.

## Non-goals / scope boundaries

- **`start-dev` does not own cluster lifecycle.** It deploys into an existing,
  reachable cluster. Creating a local kind cluster is an *optional* helper
  (`make dev-cluster-up`); a dedicated k3s cluster is user-managed.
- **No Kubernetes plumbing for the database.** DB connectivity is config only.
  We do not create Services, Endpoints, or host-IP auto-resolution for the DB.
- **Worker/integration test targets are not migrated.** `make test-workers`
  keeps its own NATS container; `make e2e-platform-up` / `make
  test-e2e-acceptance` keep their own kind+Calico cluster.
- The standalone-container `manage-*.py` scripts are **not deleted**;
  `manage-database.py` (a convenient local Postgres) is retained as one option a
  developer may point their config at.
- Tilt is **optional** — never a hard prerequisite.
- **NetworkPolicy enforcement is not a dev requirement** (kindnet / k3s Flannel
  don't enforce it); enforcement is validated only by the e2e/kind+Calico suite.
- Heroku and cloud build targets are unchanged.
- Closing the chunk-embed embedding-reachability gap is out of scope (**#443**).

## Key decisions (locked during brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Cluster relationship | **Cluster-agnostic** — deploy into the current `kubectl` context | Supports kind (laptop) and dedicated k3s (desktop) from one path. |
| Cluster lifecycle | `start-dev` does **not** create/delete clusters; optional `dev-cluster-up`/`down` for kind only | A dedicated k3s cluster is persistent infrastructure. |
| Image delivery | **Local registry** at `localhost:5000`; both backends pull from it | One portable mechanism; no `kind load` vs `ctr import` branching. |
| Server placement | **In-cluster** (Deployment) | Prod shape; the platform spine is in-cluster. |
| **Database** | **External, config-defined**; pod dials the configured endpoint directly (no Service/Endpoints) | Identical to prod; the developer owns DB connectivity. |
| Config delivery | **Dynamic ConfigMap**, regenerated from the host's current `config-development.yml` on every deploy | DB target is dynamic; config is deploy-time, not build-time. |
| DB target / image | `DB=postgres` (default, static image) or `DB=oracle` (Oracle image variant + mounted wallet) | Postgres driver is pure-Go; Oracle needs CGO + Instant Client + wallet. |
| Redis placement | **In-cluster** (Deployment/Service); address injected by `start-dev` | Cluster-local cache; topology value we own, not developer config. |
| NATS / KEDA / controller / workers | **In-cluster** | This *is* the prod component-platform shape. |
| Host port exposure | **Managed `kubectl port-forward`** → `localhost:8080` | Uniform across kind/k3s. |
| Default inner loop | **Plain make**: rebuild image → push → re-deliver config → `rollout restart` | Zero required dev dependencies. |
| Optional fast loop | **Tilt**, server-only, Chainguard base, registry-backed `live_update` | Sub-5s iteration; opt-in; restores prod shape on `tilt down`. |
| Worker components | **Deploy both** `tmi-extractor` + `tmi-chunk-embed` CRs | Full prod shape; KEDA scale-to-zero. |
| Orchestration | **Rewrite `start-dev.py`** | Keep the `tmi_common` logging/preflight helpers. |
| Namespace | **`tmi-platform`** | One namespace; components already live there. |

## Architecture

### Topology

| Where | Components |
|---|---|
| **current `kubectl` context** (kind or k3s) | monolith **server**, **Redis**, NATS JetStream, KEDA, `TMIComponent` CRD + controller, `tmi-extractor`, `tmi-chunk-embed` |
| **External, config-defined** | the **database** — local host Postgres (`manage-database.py`), cloud Postgres, or OCI ADB — dialed directly by the server pod |
| **Host Docker** | **local registry** `registry:2` on `localhost:5000` |
| **Host processes (unchanged)** | OAuth callback stub, wstest, postman/newman, curl → `http://localhost:8080` |

### Database connectivity — config only

The server reads `database.*` from its config and connects directly to that
endpoint. This is the prod model verbatim; the dev environment adds no DB
plumbing. Consequences:

- **Local host Postgres**: the developer sets `database.postgres.host` to a
  pod-reachable address — `host.docker.internal` on kind/Docker Desktop, the
  host/node IP on k3s (same machine). `localhost` will *not* work from inside a
  pod; this is the one documented gotcha, and it is the developer's config
  responsibility (as in prod).
- **Cloud Postgres**: set the cloud endpoint. Works with the default static
  image (pure-Go driver).
- **OCI ADB**: set the ADB connect descriptor *and* use `DB=oracle` (below).

### Dynamic config delivery

On every `start-dev` / `restart-dev`, the orchestrator reads the host's current
`config-development.yml` and writes it into a **ConfigMap**, mounted into the
server pod at the `--config` path. A content hash is stamped as a pod annotation
so the rollout always picks up changes. The **image carries code only**; config
is deploy-time. Swapping DB targets = edit `config-development.yml` → redeploy.

Split of responsibility:
- **`config-development.yml` (developer-owned, gitignored)** carries the
  external/secret bits: database connection, OAuth providers, JWT secret,
  embedding endpoint/key.
- **`start-dev` injects only cluster-topology values it owns** as env overrides:
  `TMI_DATABASE_REDIS_HOST=redis`, `TMI_NATS_URL=nats://nats.tmi-platform.svc:4222`.
  (Want external Redis instead? Drop the override and configure it like the DB.)

For local dev the whole config-as-ConfigMap is acceptable (gitignored, low
sensitivity). Optional hardening: split genuinely sensitive keys into a Secret.

### Server image — `tmi-server:dev` (Postgres) / `tmi-server-oracle:dev` (Oracle)

- **`DB=postgres` (default)**: build `Dockerfile.server` with a `BUILD_TAGS=dev`
  arg (enables `login_hint` + the test OAuth provider). Runtime stage stays on
  `cgr.dev/chainguard/static` — identical to prod; pure-Go Postgres driver.
- **`DB=oracle`**: build `Dockerfile.server-oracle` (`-tags=dev,oracle`, CGO +
  Oracle Instant Client). Additionally:
  - **Wallet Secret**: `start-dev` creates a `tmi-oracle-wallet` Secret from the
    developer's local wallet directory (`kubectl create secret generic
    tmi-oracle-wallet --from-file=<wallet-dir>`), mounts it read-only at
    `/etc/tmi/wallet`, and sets `TNS_ADMIN=/etc/tmi/wallet` in the pod. The ADB
    mTLS connection requires the wallet; without it the driver cannot connect.
  - The wallet path is supplied via env/config (reuse the existing
    `oci-env.sh` / Oracle dev convention).

Both images are tagged `localhost:5000/...` and pushed to the local registry.

### New manifests — `deployments/k8s/dev/` (namespace `tmi-platform`)

Applied via a dev **kustomize overlay** that references the canonical platform
component CRs and JSON6902-patches their `spec.image` to the registry names
(kustomize's image transformer does not rewrite CRD fields), leaving the e2e CRs
untouched.

- **`redis.yml`** — Deployment + ClusterIP Service `redis:6379` (`emptyDir`).
- **`server.yml`** — Deployment + ClusterIP Service `tmi-server:8080`. Runs the
  selected registry image; mounts the config ConfigMap (and, for Oracle, the
  wallet Secret). Readiness/liveness probe on `GET /` (no `/health`). Exposed to
  the host via a managed `kubectl port-forward svc/tmi-server 8080:8080`.
  - **No DB init-container that assumes a local Postgres** — the DB may be
    remote. The server's own startup/retry handles "DB not yet reachable";
    GORM `AutoMigrate` builds the schema on first successful connect.
- The `tmi-embedding` Secret is created from `TMI_EMBEDDING_API_KEY` (default
  `sk-e2e-placeholder`), as `make test-e2e-workers` does today.

No dev-specific NetworkPolicy is required (Calico-style policies the controller
renders select only component pods; server/redis are unselected).

## Tilt fast inner loop (optional)

`make tilt-up` assumes `start-dev` has populated the current context and **takes
over only `deploy/tmi-server`**. It compiles the binary on the host
(`go build -tags=dev ...`, ~1–3 s), builds a one-COPY image on
`cgr.dev/chainguard/static` via `default_registry('localhost:5000')`, and
`live_update`-syncs just the binary with the `restart_process` extension
(fallback: cached one-COPY image rebuild + push + roll). The config ConfigMap is
unchanged by Tilt. `make tilt-down` restores the canonical `server.yml`. Tilt is
Postgres-path only; the Oracle CGO image is out of the fast-loop scope.

## Make targets / orchestration

`start-dev.py` is rewritten (keeping `tmi_common` helpers; adding preflight +
the prod-protection guard). It accepts `DB=postgres|oracle` (default `postgres`).

**`make start-dev [DB=oracle]`**
1. Preflight: `kubectl`, `docker`; a cluster is reachable; context passes the
   **prod-protection guard** (refuses non-local contexts unless `--context` /
   `--yes`).
2. Start the local registry container if absent.
3. Build, tag (`localhost:5000/...`), and push the server image for the chosen
   `DB` (static or Oracle) + the two worker images.
4. Apply the platform base into the context (NATS, KEDA, CRD, controller).
5. Generate the config ConfigMap from the current `config-development.yml`; for
   `DB=oracle`, create the `tmi-oracle-wallet` Secret. Apply the dev overlay
   (Redis, server), the image-patched component CRs, and the `tmi-embedding`
   secret.
6. Wait for the `tmi-server` rollout; start the managed `kubectl port-forward`;
   print `http://localhost:8080`.

**`make restart-dev`** — rebuild + push the server image, regenerate the config
ConfigMap, `rollout restart deploy/tmi-server`, wait. ~20–60 s per change.

**`make tilt-up` / `make tilt-down`** — optional fast loop (Postgres path).

**`make stop-dev`** (stops *everything it deployed*, like `stop-all`)
- Delete the TMI dev resources from the context (server, Redis, component CRs,
  config ConfigMap, wallet/embedding Secrets).
- Kill the managed port-forward(s).
- Stop the host Postgres container if `manage-database.py` started one.
- Stop the local registry container; stop the OAuth stub if running.
- **Does not** delete a dedicated cluster. Deleting an ephemeral kind cluster is
  the separate `make dev-cluster-down`.

The `--no-workers` flag is repointed to "do not apply the component CRs."

## Data flow

```
host: curl / tmi-ux / oauth-stub / postman / wstest
        │  http://localhost:8080  (managed kubectl port-forward → svc/tmi-server)
        ▼
   tmi-server (pod)                       config: ConfigMap (current config-development.yml)
        │  database.* from config ─────► external DB (host PG / cloud PG / OCI ADB)
        │                                 (Oracle: wallet Secret @ TNS_ADMIN)
        │  redis (in-cluster svc) ◄─ TMI_DATABASE_REDIS_HOST=redis (injected)
        │  TMI_NATS_URL (injected)
        ▼
   NATS JetStream (svc) ──► KEDA scales ──► tmi-extractor / tmi-chunk-embed (0→N)
        ▲                                         │   (images: localhost:5000/...)
        └──────────── jobs.result.* ◄────────────┘
```

## Error handling / edge cases

- **Wrong/prod context** → prod-protection guard refuses with the context name;
  `--context`/`--yes` to override.
- **No cluster reachable** → preflight fails fast, pointing at `dev-cluster-up`
  (kind) or "start your k3s cluster".
- **DB unreachable / misconfigured** → the server's normal connect-retry surfaces
  a clear error; this is config the developer owns (same as prod). `localhost`
  in a pod is the documented first thing to check.
- **Oracle wallet missing / wrong dir** → `DB=oracle` fails fast if the wallet
  path is unset or empty, before deploying.
- **Registry not running / image not pushed** → start-dev starts the registry
  and pushes first; pull failures surface as `ImagePullBackOff` with the
  registry URL.
- **chunk-embed embedding unreachable (#443)** → only when a real chunk-embed
  job runs; `tmi-extractor` is unaffected.

## Testing / acceptance criteria

- On **kind** (laptop) and **k3s** (desktop), with a local Postgres configured:
  `make start-dev` → `curl http://localhost:8080/` returns the server version.
- Re-point config at a cloud Postgres, `make restart-dev`, confirm the server
  connects to the new target (proves dynamic config delivery).
- `make start-dev DB=oracle` against OCI ADB (wallet present) connects and
  AutoMigrates (validated where ADB creds exist; otherwise the wallet-missing
  fast-fail path is unit-checked).
- Full OAuth login via the stub (tmi provider, `login_hint=alice`) yields a JWT.
- An async extraction job round-trips through `tmi-extractor`.
- `make tilt-up` picks up a server change in seconds; `make tilt-down` restores
  the prod-shaped server.
- `make stop-dev` removes all deployed workloads + host deps, leaving a
  dedicated cluster intact.
- `make test-integration` and `make test-e2e-acceptance` still pass.

## Documentation

Update the **GitHub Wiki** contributor onramp:
- Prereqs: `kubectl`, `docker`; one of a local kind cluster
  (`make dev-cluster-up`) or a dedicated k3s with `localhost:5000` mirroring.
  **Optional** `tilt`.
- **DB config is the developer's responsibility** (like prod): how to point at
  local Postgres (the `localhost` → `host.docker.internal`/node-IP gotcha),
  cloud Postgres, or OCI ADB (`DB=oracle` + wallet).
- The local-registry setup and the k3s `registries.yaml` mirror snippet.
- New commands: `dev-cluster-up`/`down`, `start-dev [DB=oracle]`, `restart-dev`,
  `tilt-up`/`down`, `stop-dev`.
- Explicit **breaking-change callout**: the host-process dev path is retired.

Per project rules, the `docs/` directory is not modified.

## Risks

- **Local-registry trust config** differs per backend (kind registry ConfigMap
  vs k3s `registries.yaml`); documented one-time setup for each.
- **Oracle dev image** is heavier (CGO + Instant Client) and slower to build;
  it is opt-in via `DB=oracle` and excluded from the Tilt fast loop.
- **Tilt static-base restart wrapper**: cached-image-rebuild fallback if the
  wrapper can't run on `chainguard/static`.
- **port-forward longevity**: a dropped forward needs re-establishing; the
  orchestrator monitors and restarts it.
- **chunk-embed #443 gap** persists in dev until that issue lands.

## Closing #347

#442 is the last open Plan 4 item for #347. When this lands, #347 can close.
Because the commit lands on `dev/1.4.0` (not `main`), both issues must be closed
explicitly (comment + `gh issue close`), not via a commit trailer.
