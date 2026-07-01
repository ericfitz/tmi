# Design: k3s dev deployment target (`CLUSTER=k3s`)

**Date:** 2026-07-01
**Status:** Approved (brainstorming), pending implementation plan
**Scope:** 3 of 3 — independent of the pre-commit-hook and CI-scanners specs. This is the
largest of the three and warrants its own implementation plan + PR.

## Summary

Add `CLUSTER=k3s` as a selectable dev deployment target that deploys to the existing
remote `k3s-rp` cluster, with an in-cluster image registry, single-node in-cluster
Postgres and Redis, and `kubectl port-forward` preserving the `localhost:8080` contract.
**kind remains the default and keeps working unchanged.**

Mirrors the existing `DB=oracle` pattern: a `CLUSTER` selector (default `kind`) plumbed
through the Python dev tooling, plus a kustomize overlay describing the k3s topology.

## Facts driving the design

- The `k3s-rp` context already exists locally; its API server is `https://rp2:6443` — a
  **remote LAN** cluster (Raspberry-Pi-class, arm64). The Mac dev host is also arm64.
- kind currently provides two things k3s must replace:
  - a local registry mirror (`localhost:5000`), and
  - `extraPortMappings` exposing NodePort 30080 as `localhost:8080` (a kind-only
    feature).
- In the kind topology, Redis runs **in-cluster** (`deployments/k8s/dev/redis.yml`) and
  Postgres runs as a **container on the Mac**, reached via `host.docker.internal`
  (`IN_CLUSTER_DB_HOST` in `scripts/lib/deploy.py`). `host.docker.internal` does not
  resolve from `rp2`.

## Decisions (from brainstorming)

- **Selectable, not a replacement.** `CLUSTER=k3s` is opt-in; `CLUSTER=kind` is the
  default.
- **Fully self-contained cluster:** in-cluster registry + single-node in-cluster
  Postgres + single-node in-cluster Redis. No cross-LAN dependency on the Mac at runtime.
- **`localhost:8080` preserved** via `kubectl port-forward`; NodePort `rp2:30080`
  documented as the high-throughput / CATS escape hatch.
- **Migrations** run as an **in-cluster Job**; Mac-side `dbtool`/`psql` reach Postgres
  via `kubectl port-forward svc/postgres 5432:5432`.

## Components

1. **Cluster lifecycle** (`devenv.py`, `scripts/lib/cluster.py`, `scripts/lib/deploy.py`):
   - `CLUSTER=kind` → existing kind create/delete flow.
   - `CLUSTER=k3s` → **use** the existing `k3s-rp` context
     (`kubectl config use-context k3s-rp`); never create or delete the cluster (we don't
     own it). `dev-up`/`dev-down`/`dev-status`/`dev-nuke` branch on `CLUSTER`.
   - `dev-nuke` under k3s tears down only the TMI namespace(s) + PVCs, not the cluster.

2. **In-cluster image registry:**
   - Deploy a registry (Deployment + PVC + Service, NodePort `30500`) into k3s-rp.
   - Build tooling (`scripts/build-app-containers.py`) tags/pushes images to
     `rp2:30500/tmi/...`; the k3s overlay manifests reference the same.
   - **One-time manual node config:** add `rp2:30500` as an insecure (plain-HTTP)
     registry to `/etc/rancher/k3s/registries.yaml` on each node and restart k3s. This
     requires SSH/root on the Pi nodes and cannot be driven from the Mac; it will be
     documented (optionally with a helper script that SSHes to the nodes).

3. **In-cluster Postgres (single node):**
   - New `postgres.yml` for the k3s overlay: StatefulSet + PVC + Service, credentials via
     a Secret. Server reaches it by service DNS (`postgres:5432`).
   - Schema applied by an in-cluster **migration Job**.
   - Mac `dbtool`/`psql`/`make migrate` reach it via `kubectl port-forward`.

4. **In-cluster Redis (single node):** reuse the existing `redis.yml` (already
   in-cluster) — moves to k3s for free.

5. **Server exposure:**
   - `make dev-up CLUSTER=k3s` starts a background
     `kubectl port-forward svc/tmi-server 8080:8080` so `localhost:8080` behaves exactly
     as under kind (OAuth stub callbacks, integration tests, curl unchanged).
   - NodePort `rp2:30080` documented for CATS / high-throughput runs (the port-forward
     userspace proxy throttles under that load — the #463 problem).

6. **Architecture / image builds:** build arm64 images (`buildx --platform linux/arm64`).
   `CGO_ENABLED=0` static binaries + Chainguard multi-arch bases make this clean; both
   Mac and Pi nodes are arm64.

## Verification

- `make dev-up` (no args) still brings up kind and serves `localhost:8080` — **no
  regression to the default path.**
- `make dev-up CLUSTER=k3s`:
  - pushes arm64 images to the in-cluster registry and the nodes pull them,
  - brings up in-cluster Postgres + Redis, runs migrations,
  - serves the API at `localhost:8080` via port-forward,
  - `make dev-status CLUSTER=k3s` reports healthy,
  - `make dev-down CLUSTER=k3s` / `dev-nuke CLUSTER=k3s` tear down TMI resources but
    leave the `k3s-rp` cluster intact.
- `curl http://localhost:8080/` returns the running version.

## Open implementation details (not blockers)

- Exact StatefulSet vs Deployment for the single-node Postgres (PVC either way).
- Whether the registries.yaml node step is documented-only or scripted via SSH.
- Storage class / PVC sizing appropriate to the Pi nodes.

## Out of scope

- Replacing kind (kind stays the default).
- Multi-node HA for in-cluster Postgres/Redis (single node is intentional for dev).
- Migrating CI or production deployment targets — this only affects local dev.
