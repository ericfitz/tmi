# Design: Rationalized Dev Environment

**Date:** 2026-06-17
**Status:** Approved (brainstorm) — pending implementation plan
**Branch context:** dev/1.4.0

## Problem

TMI recently migrated dev from running the server as a local `bin/tmiserver`
process to always running it inside a `kind` (Kubernetes-in-Docker) cluster.
The migration was incomplete. The scripts and make targets that manage the dev
environment now span two eras and contradict each other:

- **Canonical (kind) path:** `dev-cluster.py` (cluster + local registry) +
  `start-dev.py` (builds images, pushes to local registry, applies manifests;
  deploys server, Redis, NATS, KEDA, controller, and both workers in-cluster;
  database is external). This works.
- **Orphaned (local-process) path:** `manage-server.py` (runs `bin/tmiserver`
  on the host), `manage-workers.py` (runs workers as host processes),
  `start-dev-oci.sh` (runs `./bin/tmiserver` locally against Oracle). None of
  these are part of the canonical flow anymore.
- **Broken status:** `status.py` scans `lsof :8080` / `ps` / `.server.pid` for a
  local server process, so it reports "stopped" after `make start-dev` has
  deployed the server into kind.
- **Stale cleanup:** `clean.py` calls `manage-server.py stop` and
  `manage-workers.py stop` (local-process teardown).

The surface is also inconsistent: verbs are `up`/`down`/`stop` (cluster) vs
`start`/`stop` (services) vs `start-dev`/`stop-dev`/`restart-dev` (flags) vs
`clean-*`; there are three overlapping ways to get a server running
(`start-dev`, `start-server`, `start-dev-oci`).

## Goals

1. Make it trivial to get dev **up**, **down**, or into a **known state**.
2. One consistent verb set with a single prefix.
3. **Scripts do all the work**; make targets are thin, consistently named
   wrappers around a script (or a script with parameters). No real logic in the
   Makefile.
4. Remove everything stale from the local-server era.
5. `dev-status` must reflect the real (kind) topology.

## Non-goals

- Reworking the **test** infrastructure (`--test` containers driven by
  `manage-database.py` / `manage-redis.py` / `manage-nats.py` via
  `start-test-*`). It stays a separate concern owned by the test framework.
- Changing the OAuth stub's lifecycle ownership — it remains a separate,
  on-demand dev tool.
- Changing the in-cluster manifests / kustomize overlays themselves (beyond
  verifying the Oracle overlay works).

## Decisions (from brainstorm)

- **Surface:** one orchestrator with a few verbs; granular per-service control
  remains but is secondary.
- **Local-process paths:** delete `manage-server.py`, `manage-workers.py`, and
  `start-dev-oci.sh` entirely. kind is the only dev path.
- **Scope:** the orchestrator owns only the kind stack (cluster + external db +
  in-cluster services + deploy). The OAuth stub and the test infrastructure are
  both **separate** concerns, not managed by `dev-up`.
- **Known-state:** two explicit levels — `dev-reset` (soft: redeploy, keep DB
  data) and `dev-nuke` (hard: wipe everything incl. DB data + images, rebuild).
- **Script structure:** a single `scripts/devenv.py` exposes all verbs;
  cluster/db/deploy logic lives in `scripts/lib/` modules it imports.

## Design

### 1. Command surface (the lifecycle ladder)

A single orchestrator, `scripts/devenv.py`, exposes a consistent verb set. Make
targets are 1:1 thin wrappers. The model is a ladder from cheapest →
most destructive:

| Make target | `devenv.py` verb | Behavior |
|---|---|---|
| `make dev-up` | `up` | Idempotent bring-up: create kind cluster + local registry if missing, start db container, build + push images, apply manifests, wait for rollout. `DB=postgres\|oracle`. |
| `make dev-restart` | `restart` | **App only** — rebuild server image, push, `kubectl rollout restart`, wait. Cluster + db untouched. Fast loop after a code change. |
| `make dev-reset` | `reset` | **Soft known-state** — delete & redeploy the in-cluster stack with freshly built images; keep cluster and **keep DB data**. "App is wedged but my data is fine." |
| `make dev-down` | `down` | Turn everything off: delete cluster + registry, stop db container. **DB data volume preserved** (reversible pause). |
| `make dev-nuke` | `nuke` | **Hard known-state** — destroy everything (cluster, registry, **db data**, built images, logs/generated files), then rebuild from scratch and bring up clean. |
| `make dev-status` | `status` | kind-aware dashboard (see §4). |
| `make dev-logs` | `logs` | `kubectl logs` tail of the server pod (convenience). |

Advanced / granular wrappers, same `dev-` prefix, also just call `devenv.py`:

- `make dev-cluster-up` / `make dev-cluster-down` → `devenv.py cluster up|down`
- `make dev-db-up` / `make dev-db-down` → `devenv.py db up|down`
- `make dev-deploy` → `devenv.py deploy` (apply manifests / rollout without
  recreating cluster or db)

The ladder, ordered by cost/destructiveness:
`restart` (app rollout) < `reset` (redeploy stack, keep data) <
`down` (off, keep data) < `nuke` (wipe + rebuild).

### 2. Script architecture

```
scripts/devenv.py          NEW — the only script make calls for dev lifecycle.
                           argparse subcommands:
                           up down restart reset nuke status deploy logs cluster db

scripts/lib/cluster.py     kind cluster + local registry lifecycle.
                           Absorbs scripts/dev-cluster.py and the registry /
                           kubectl-context helpers currently in lib/devenv.py
                           (which is renamed away to free the `devenv` name for
                           the top-level orchestrator).

scripts/lib/deploy.py      image build/push, kubectl apply / kustomize, rollout
                           wait, oracle overlay selection.
                           Absorbs scripts/start-dev.py and the manifest /
                           ConfigMap rendering helpers from lib/devenv.py.

scripts/lib/database.py    postgres container lifecycle + migrate. SINGLE source
                           of db-container logic, shared by devenv.py (dev) and
                           the test path.
```

Existing shared helpers (`scripts/lib/tmi_common.py`, its `run_cmd`, etc.) are
reused unchanged. Pure functions get unit tests under `scripts/lib/tests/`
(following the existing `test_devenv.py` pattern).

**Delete / retire:**

- `scripts/dev-cluster.py` (→ `lib/cluster.py`)
- `scripts/start-dev.py` (→ `lib/deploy.py` + `devenv.py`)
- `scripts/start-dev-oci.sh` (→ `dev-up DB=oracle`)
- `scripts/manage-server.py` (local server — gone)
- `scripts/manage-workers.py` (local workers — gone; workers run in-cluster as
  `TMIComponent` CRs)
- `scripts/lib/_server_state.py` (local-server PID/state helper — gone if only
  used by the deleted local paths; verify before removing)
- `scripts/status.py` (→ `devenv.py status`)

**Keep (test infra + dev tool; de-entangled from dev lifecycle):**

- `scripts/manage-database.py` — refactored to call `lib/database.py` so there
  is one db-container implementation; retains its `--test` mode for `start-test-*`.
- `scripts/manage-redis.py`, `scripts/manage-nats.py` — used only by the test
  path now (Redis/NATS run in-cluster for dev). Left as-is.
- `scripts/manage-oauth-stub.py` — separate on-demand dev tool, unchanged.

### 3. Oracle

`start-dev-oci.sh` is deleted. Oracle dev becomes `make dev-up DB=oracle`,
fully in-cluster via the existing `deployments/k8s/dev/server-oracle.yml`
overlay (already supported by today's `start-dev.py --db oracle`).

**Plan checkpoint (capability-preserving):** verify the in-cluster Oracle path
actually builds, deploys, and reaches the database **before** deleting
`start-dev-oci.sh`, so no Oracle dev capability is lost in the transition.

### 4. `dev-status`

Replaces the broken local-process scan in `status.py`. No `lsof` / `ps` /
`.server.pid`. Reports:

- kind cluster present? (`kind get clusters`)
- local registry container up?
- db container up + reachable + schema migrated?
- `kubectl get deploy,pods -n tmi-platform` — server, Redis, NATS, KEDA,
  controller, both worker components — with ready/desired counts
- server reachable on `:8080` (port-forward or ingress, whichever the deploy
  path uses)
- oauth-stub status (informational; delegates to `manage-oauth-stub.py status`)

One glance shows exactly where the environment is.

### 5. Naming, cleanup, and back-compat

- All dev-lifecycle targets use the `dev-` prefix and the verb set above.
- `DB=postgres|oracle` parameter selects the flavor (keeps the existing
  convention used by `start-dev`).
- `clean.py` loses its `manage-server.py stop` / `manage-workers.py stop` calls.
  The `clean-logs` / `clean-files` / `clean-containers` family remains as
  general cleanup; `dev-nuke` calls into it for the wipe step.
- `stop-all` and `clean-everything` are rewired onto the new verbs.
- **Back-compat:** `start-dev`, `stop-dev`, `restart-dev` (and `start-dev-oci`)
  are kept for one release as deprecated aliases that print a one-line
  "renamed to `dev-up` / `dev-down` / `dev-restart`" notice and forward to the
  new target. Removable next release. Cheap insurance for muscle memory and any
  wiki/CI references.

## Risks & mitigations

- **Oracle regression** when deleting `start-dev-oci.sh`: mitigated by the §3
  checkpoint (prove `dev-up DB=oracle` works first).
- **Hidden consumers** of the deleted scripts/targets (wiki, CI, other make
  targets, `clean.py`): grep the repo for every deleted script and target name
  and rewire or remove each reference as part of the change.
- **Shared db logic divergence**: mitigated by making `lib/database.py` the
  single implementation that both `devenv.py` and `manage-database.py --test`
  call.
- **`lib/devenv.py` name collision** with the new top-level `scripts/devenv.py`:
  the helper module is split into `lib/cluster.py` + `lib/deploy.py`, freeing
  the `devenv` name for the orchestrator and clarifying each module's role.

## Out of scope / follow-ups

- Migrating `manage-redis.py` / `manage-nats.py` into `lib/` for symmetry (only
  test infra uses them; not blocking).
- Any wiki documentation updates for the new commands (tracked separately;
  per project convention docs live in the GitHub Wiki, not `docs/`).
