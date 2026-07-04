# Docker Desktop Dev Target — Design

**Goal:** Replace the standalone-kind local dev deployment path with Docker Desktop's
built-in Kubernetes (kind provisioner, context `docker-desktop`), so the dev tooling
stops managing its own cluster + local registry container + containerd mirror. Fewer
moving parts; reuse the DD cluster the developer already runs.

**Status:** Design approved (brainstorm). Implementation sequenced AFTER the k3s dev
target (PR #516) lands on `main`, since this extends the `CLUSTER` selector introduced
there.

## Context / Motivation

- The developer already runs Docker Desktop Kubernetes and wants dev to reuse it.
- Current standalone-kind dev path carries several moving parts our scripts manage:
  a self-created 2-node kind cluster (`tmi-dev`), a `tmi-dev-registry` container on
  `localhost:5000`, a containerd registry-mirror injected via `kind-cluster.yml`, and
  `extraPortMappings` for the NodePort→`localhost:8080` contract.
- Motivation (user-selected): fewer moving parts, lower resource usage, reuse the
  existing DD cluster.

## Decisions (locked)

1. **Replace, not add.** `docker-desktop` becomes the default local target; the dev
   standalone-kind machinery is retired. `CLUSTER` options become
   `docker-desktop` (default) | `k3s`. Makefile default: `CLUSTER ?= docker-desktop`.
2. **In-cluster Postgres** (reuse the k3s pattern), not the Mac `tmi-postgresql`
   container.
3. **e2e-platform stays on standalone kind.** It requires a swappable,
   NetworkPolicy-enforcing CNI (`disableDefaultCNI: true` + Calico) and ephemeral
   per-run clusters — neither of which DD-managed Kubernetes can provide. This change
   does NOT touch `e2e-platform-up/down`, `deployments/k8s/platform/kind-cluster.yml`,
   or `test/e2e/platform/`. The `kind` CLI remains a project dependency for e2e only.

## Verified environment facts (probes, 2026-07-03)

- DD Kubernetes is enabled with the **kind provisioner**: single node
  `desktop-control-plane`, containerd, arm64 linuxkit kernel **6.12.x (4KB pages)**,
  k8s v1.36.1. (kubeadm provisioner would name the node `docker-desktop`.)
- The standalone `kind` CLI **cannot see** the DD-managed cluster
  (`kind get clusters` → none) — so `kind load` and our mirror approach are out.
- **Registry-free image import works**: `docker save <img> |
  docker exec -i desktop-control-plane ctr -n k8s.io images import -` lands the image
  in the node's containerd; `crictl images` then sees it. Verified roundtrip with a
  throwaway busybox image.
- **LoadBalancer is NOT localhost** here: a `type=LoadBalancer` service gets
  EXTERNAL-IP `172.18.0.2` (kind network), not `localhost`. So the endpoint must be a
  port-forward, as with k3s.
- **4KB pages** on DD's kernel → the chainguard redis jemalloc problem that forced
  `redis:7-alpine` on the Pi (16KB) does NOT apply; DD uses chainguard redis unchanged.
- The component controller (`internal/platform/controller/render_deployment.go`) sets
  `Image: c.Spec.Image` but never sets `ImagePullPolicy`; worker images are `:dev`
  tagged, so k8s defaults to `IfNotPresent` → imported images are used without a pull.

## Architecture

Treat `docker-desktop` like k3s: a cluster whose lifecycle we do NOT own. We manage
only the `tmi-platform` namespace + workloads. ~80% of the k3s `cluster_target`
machinery is reused; the one new piece is registry-free image delivery.

### Cluster lifecycle
- `cluster.up(docker-desktop)`: verify DD k8s reachable + `kubectl config use-context
  docker-desktop`. No create.
- `cluster.down(docker-desktop)`: no-op (never delete DD's cluster).
- `dev-nuke` (docker-desktop): namespace-scoped hard reset (delete `tmi-platform`),
  like k3s.
- Prereq (documented): DD → Settings → Kubernetes → Enable, provisioner = kind.

### Image delivery (registry-free)
- Build the 4 TMI images locally (`docker build`, arm64-native, **no push, no
  buildx**): tmi-server, tmi-component-controller, tmi-extractor, tmi-chunk-embed,
  all `:dev`.
- Import each into the node: `docker save <name>:dev | docker exec -i
  desktop-control-plane ctr -n k8s.io images import -`.
- No registry container, no mirror. Public images (postgres, redis, nats, keda) are
  pulled normally from their upstreams.

### Endpoint
- Port-forward `localhost:8080` → `svc/tmi-server` and `localhost:6379` →
  `svc/redis`, reusing the k3s `start_server_port_forward` / `start_redis_port_forward`
  (both gated on cluster_target). No LoadBalancer, no NodePort, no extraPortMappings.

### Database
- In-cluster Postgres: `deployments/k8s/dev/docker-desktop/postgres.yml` — chainguard
  postgres, DD's default storageclass (`hostpath`; k3s uses `longhorn`, so this is a
  DD-specific manifest), creds matching `config-development.yml`. DB-URL host rewritten
  to the `postgres` Service via the k3s `in_cluster_db_host` (extended to
  docker-desktop) + `rewrite_db_host_for_incluster`.

### Redis
- In-cluster **chainguard** redis, unchanged (4KB pages). No alpine remap.

### Overlay (`deployments/k8s/dev/docker-desktop/`)
- `kustomization.yaml`: base workloads (controller, redis, server, extractor,
  chunk-embed).
- images-transformer: strip `localhost:5000/` → bare names (`tmi-server`,
  `tmi-component-controller`); worker TMIComponent images patched to bare
  `tmi-extractor:dev` / `tmi-chunk-embed:dev`.
- imagePullPolicy patch: server `Always → IfNotPresent` (server.yml hardcodes Always;
  controller.yml and workers already default to IfNotPresent on `:dev`).
- Server/redis Services can stay as-is (base type); port-forward works regardless.
- Postgres applied as a prerequisite (not in the overlay), like k3s.
- Platform base (NATS + KEDA + CRD) applied same as k3s.

## What gets retired (dev standalone-kind)

- `deployments/k8s/dev/kind-cluster.yml` (dev only — NOT the platform one).
- `tmi-dev-registry` container + `ensure_registry` / `connect_registry_to_kind`.
- containerd registry mirror + `extraPortMappings`.
- `cluster.py` kind create / `start_stopped_nodes` / `cluster_exists` paths and the
  kind branch of `up`/`down`.
- Makefile `CLUSTER ?= kind` → `docker-desktop`; help text and `dev-cluster-up/down`
  descriptions updated.
- The Mac `tmi-postgresql` container from the dev path (in-cluster now). (The
  `database.py` helper may remain for other uses; the dev-up path no longer calls it.)

Kept: the `kind` CLI, `deployments/k8s/platform/kind-cluster.yml`, `e2e-platform-*`
targets, and all `test/e2e/platform/` usage.

## deploy.py / devenv.py / cluster.py changes

Add a `docker-desktop` branch alongside `kind`/`k3s`:
- `cluster.up/down`, `expected_context` (`docker-desktop`), `registry_for` (n/a — no
  registry; image build path skips push for docker-desktop).
- `deploy.start`: for docker-desktop → build (no push) + `ctr import`; apply postgres
  prereq; apply platform base; deliver_config with host=`postgres`; apply
  docker-desktop overlay; port-forward server + redis.
- `overlay_dir_for(db, "docker-desktop")` → the new overlay dir.
- `in_cluster_db_host("docker-desktop")` → `postgres`.
- A new image-delivery helper `import_images_to_node(...)` (the `docker save | ctr
  import` builder) — unit-testable command construction.

## Testing

- Unit (`make test-dev-scripts`): new-branch coverage for `overlay_dir_for`,
  `in_cluster_db_host`, `expected_context`, the `--cluster docker-desktop` parser
  value, the `ctr import` command builder, and port-forward gating.
- Live gate: `make dev-up` (now docker-desktop) → full stack, `curl localhost:8080` →
  HTTP 200. And `make dev-up CLUSTER=k3s` non-regression.

## Risks / open items

- `docker exec desktop-control-plane` worked in probing though `docker ps` did not list
  the node (likely a docker-context nuance). Implementation must confirm the exact
  invocation is stable across DD restarts; fall back to `kind`-style node discovery if
  the container name differs by DD version.
- imagePullPolicy for the server must be patched to IfNotPresent or the pod will try to
  pull `tmi-server:dev` from a nonexistent registry.
- DD cluster is shared/persistent — `dev-nuke` scopes to the `tmi-platform` namespace
  and must never touch other namespaces the developer runs.

## Sequencing

1. Land k3s (PR #516) on `main`.
2. Branch `feature/docker-desktop-dev-target` off `main`.
3. Implement per the plan; verify live; PR.
