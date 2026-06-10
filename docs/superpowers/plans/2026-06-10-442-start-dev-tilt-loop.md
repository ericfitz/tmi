# start-dev Tilt Fast Inner Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an **optional** `make tilt-up` that gives a sub-5-second server-only inner loop on top of the Plan 1 cluster (compile binary on host → sync into the running container → restart process), and `make tilt-down` that restores the prod-shaped server. Tilt is never a hard prerequisite for any other target.

**Architecture:** Tilt assumes `make start-dev` already brought up the cluster + infra + workers, and **takes over only `deploy/tmi-server`**. A `Tiltfile` compiles the dev binary on the host (`go build -tags=dev`), builds a one-COPY image on `cgr.dev/chainguard/static` (the same runtime base as the prod image) via the `restart_process` extension, and `live_update`-syncs just the binary with an in-place process restart. Everything else (Redis, NATS, KEDA, controller, workers, external DB) is untouched. `tilt down` re-applies the canonical `server.yml` to restore the prod-shaped server.

**Tech Stack:** Tilt (optional dev dependency), Tilt `restart_process` extension, Docker, Chainguard static base, the Plan 1 kind cluster + local registry.

**Spec:** `docs/superpowers/specs/2026-06-10-442-start-dev-kind-migration-design.md` § "Tilt fast inner loop (optional)".

**Depends on:** Plan 1 (core) — merged to `dev/1.4.0`. Reuses `deployments/k8s/dev/server.yml`, the `localhost:5000` registry, and the `tmi-server-config` ConfigMap created by `start-dev`. Postgres path only (the Oracle CGO image is out of scope for the fast loop).

---

## File Structure

**Create:**
- `Dockerfile.server-devloop` — one-COPY image: prod static base + the host-built dev binary.
- `Tiltfile` — compile-on-host + live-update binary sync for `deploy/tmi-server` only.

**Modify:**
- `Makefile` — add `tilt-up` / `tilt-down` (with preflight).
- `.gitignore` — ensure `bin/tmiserver` (the host-built artifact) is ignored if not already.

---

## Task 1: `Dockerfile.server-devloop`

**Files:** Create `Dockerfile.server-devloop`.

- [ ] **Step 1: Write the Dockerfile**

It is intentionally trivial: the binary is compiled on the host by Tilt (`local_resource`), so this image is a single COPY onto the same runtime base the prod image uses (`cgr.dev/chainguard/static`). Tilt's `restart_process` extension overrides the entrypoint with a restart wrapper.

```dockerfile
# Tilt fast-loop image. The dev binary is compiled on the HOST (see Tiltfile);
# this is a one-COPY image on the same runtime base as the prod server image, so
# the fast-loop pod stays close to prod shape. Tilt live-update syncs the binary
# and restarts the process in place (via the restart_process extension).
FROM cgr.dev/chainguard/static:latest
COPY bin/tmiserver /tmiserver
ENTRYPOINT ["/tmiserver"]
```

- [ ] **Step 2: Verify it builds (needs a host-built binary first)**
```bash
go build -tags=dev -o bin/tmiserver ./cmd/server
docker build -f Dockerfile.server-devloop -t tmi-server:devloop-check .
```
Expected: the binary builds (no staging needed — it's a host build, not the multi-stage Dockerfile), then the image builds in <2s (single COPY). If `go build -tags=dev` fails, report it — the dev tag must compile (it does in Plan 1's image build).

- [ ] **Step 3: Commit**
```bash
git add Dockerfile.server-devloop
git commit -m "build(dev): Dockerfile.server-devloop for the Tilt fast loop"
```

---

## Task 2: `Tiltfile`

**Files:** Create `Tiltfile`. Possibly modify `.gitignore`.

- [ ] **Step 1: Write the Tiltfile**

```python
# Tilt fast inner loop for the TMI server ONLY.
#
# Prereq: `make start-dev` has already deployed the full dev environment
# (cluster + infra + workers + a prod-shaped tmi-server). `tilt up` takes over
# deploy/tmi-server, swapping it for a live-updatable image: edits to the Go
# sources recompile the binary on the host and sync it into the running
# container, restarting the process in place (~seconds, no image rebuild/roll).
#
# `tilt down` removes Tilt's tmi-server; `make tilt-down` then re-applies the
# canonical server.yml to restore the prod-shaped server.

load('ext://restart_process', 'docker_build_with_restart')

# Push the devloop image to the same local registry the cluster pulls from.
default_registry('localhost:5000')

# 1) Compile the dev binary on the host (fast, incremental). Watching the Go
#    source trees triggers a recompile on save.
local_resource(
    'server-compile',
    cmd='go build -tags=dev -o bin/tmiserver ./cmd/server',
    deps=['cmd/server', 'api', 'auth', 'internal', 'pkg'],
)

# 2) One-COPY image on the prod static base; live-update syncs just the binary
#    and the restart_process wrapper re-execs it in place.
docker_build_with_restart(
    'localhost:5000/tmi-server',
    '.',
    dockerfile='Dockerfile.server-devloop',
    entrypoint=['/tmiserver', '--config=/etc/tmi/config.yml'],
    only=['./bin/tmiserver'],
    live_update=[sync('./bin/tmiserver', '/tmiserver')],
)

# 3) Deploy ONLY the server (the rest of the env is owned by start-dev). Tilt
#    matches the image ref in server.yml and substitutes the freshly built one.
k8s_yaml('deployments/k8s/dev/server.yml')
k8s_resource('tmi-server', port_forwards='8080:8080', resource_deps=['server-compile'])
```

Notes for the implementer:
- The image name `localhost:5000/tmi-server` (no tag) must match the ref in `deployments/k8s/dev/server.yml` (`localhost:5000/tmi-server:dev`) so Tilt injects the built image. Confirm Tilt matches on the untagged name; if it requires an exact match, set the Tiltfile name to `localhost:5000/tmi-server:dev`.
- `server.yml` mounts the `tmi-server-config` ConfigMap and uses the `app: tmi-server` selector — both already exist in the cluster from `start-dev`, so Tilt's tmi-server slots in cleanly.
- If `docker_build_with_restart`'s wrapper cannot run on `cgr.dev/chainguard/static` (no shell), fall back per the spec: drop `docker_build_with_restart` for a plain `docker_build(...)` with the same `live_update` minus the restart, and add `k8s_resource(..., trigger_mode=TRIGGER_MODE_AUTO)` so a changed binary triggers an image rebuild + rolling update (slower than in-place restart, still on the Chainguard base). Document whichever path works.

- [ ] **Step 2: Ensure the host artifact is gitignored**

Confirm `bin/` or `bin/tmiserver` is in `.gitignore` (Plan 1 / the repo likely already ignores `bin/`). If not, add `bin/tmiserver`.

- [ ] **Step 3: Static validation**
```bash
command -v tilt >/dev/null && tilt alpha tiltfile-result >/dev/null 2>&1 && echo "Tiltfile parses" || echo "tilt not installed — static review only"
```
If `tilt` is installed, `tilt alpha tiltfile-result` should parse without error. If not installed, visually confirm the Tiltfile is well-formed (this is verified live in Task 4).

- [ ] **Step 4: Commit**
```bash
git add Tiltfile .gitignore
git commit -m "feat(dev): Tiltfile for the optional server-only fast loop"
```

---

## Task 3: `tilt-up` / `tilt-down` make targets

**Files:** Modify `Makefile`.

- [ ] **Step 1: Add the targets (with preflight)**
```makefile
tilt-up:  ## Optional fast server-only loop (requires tilt + a running start-dev cluster)
	@command -v tilt >/dev/null 2>&1 || { echo "tilt not installed — see https://docs.tilt.dev/install.html"; exit 1; }
	@kubectl cluster-info >/dev/null 2>&1 || { echo "no reachable cluster — run 'make start-dev' first"; exit 1; }
	@tilt up --stream=true

tilt-down:  ## Stop Tilt and restore the prod-shaped server
	@command -v tilt >/dev/null 2>&1 && tilt down || true
	@kubectl apply -f deployments/k8s/dev/server.yml
	@kubectl -n tmi-platform rollout status deploy/tmi-server --timeout=120s
	@echo "prod-shaped server restored"
```
Add `tilt-up tilt-down` to the appropriate `.PHONY` line.

- [ ] **Step 2: Verify the targets exist and preflight fires when tilt is absent**
```bash
grep -nE "tilt-up|tilt-down" Makefile
make tilt-up   # with tilt absent, expect the "tilt not installed" message + non-zero exit
```
Expected: clear preflight error if tilt isn't installed (proves the guard); no partial side effects.

- [ ] **Step 3: Commit**
```bash
git add Makefile
git commit -m "build(dev): tilt-up/tilt-down targets (optional fast loop)"
```

---

## Task 4: Acceptance (Tilt loop)

**Files:** none (verification). Requires `tilt` installed; if it cannot be installed in this environment, mark the live steps DEFERRED and note that the Tiltfile/Dockerfile/targets are in place for a developer with tilt.

- [ ] **Step 1: Bring up the base env + Tilt**
```bash
# install tilt if feasible (e.g. brew install tilt-dev/tap/tilt) — else DEFERRED
make start-dev          # prod-shaped server up (Plan 1)
make tilt-up            # Tilt takes over deploy/tmi-server
```
Expected: Tilt builds the devloop image, deploys it, and the server is reachable at http://localhost:8080 (`curl -s http://localhost:8080/ | head -c 80`).

- [ ] **Step 2: Verify the fast loop**

Make a trivial, observable server source change (e.g. a log line or a string in the root handler), save, and confirm Tilt recompiles on the host and the change appears in seconds WITHOUT a full image rebuild/`kind load`/rollout. Capture the Tilt update timing (the `server-compile` local_resource fires, then the binary syncs).
Expected: end-to-end update is markedly faster than `make restart-dev` (~seconds vs tens of seconds). Revert the trivial change afterward.

- [ ] **Step 3: Verify restore**
```bash
make tilt-down
kubectl -n tmi-platform get deploy tmi-server -o jsonpath='{.spec.template.spec.containers[0].image}'; echo
curl -s http://localhost:8080/ | head -c 80; echo
```
Expected: the server image is back to `localhost:5000/tmi-server:dev` (prod-shaped), and the server still serves. (Re-establish the port-forward via `make start-dev` if `tilt down` removed Tilt's forward.)

- [ ] **Step 4: Commit any fixups**
```bash
git add -A && git commit -m "test(dev): tilt fast-loop acceptance fixups" || echo "nothing to commit"
```

---

## Self-Review Notes (for the implementer)

- **Tilt is optional** — every target must preflight for `tilt` and fail with an install hint; nothing else may depend on tilt.
- **Image-name matching** between the Tiltfile `docker_build_with_restart` name and the ref in `server.yml` is the most likely snag — confirm Tilt substitutes the built image into the deployed Deployment.
- **restart wrapper on distroless static** is the known risk: if `docker_build_with_restart` won't run on `cgr.dev/chainguard/static`, use the documented cached-image-rebuild fallback (still Chainguard base) and note it.
- **`tilt down` must restore prod shape** — `make tilt-down` re-applies `server.yml`; verify the image reverts.
- If `tilt` cannot be installed in the execution environment, the live acceptance (Task 4) is DEFERRED but the artifacts (Dockerfile.server-devloop, Tiltfile, targets) should still be created and statically reviewed.
