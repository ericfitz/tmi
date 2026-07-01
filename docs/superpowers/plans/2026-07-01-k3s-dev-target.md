# k3s Dev Target (`CLUSTER=k3s`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `CLUSTER=k3s` as a selectable dev-deployment target that deploys TMI to the existing remote `k3s-rp` cluster with an in-cluster registry, single-node in-cluster Postgres and Redis, and a `kubectl port-forward` that preserves the `localhost:8080` contract — while keeping `kind` the default, unchanged.

**Architecture:** Mirror the existing `DB=oracle` selector with a new orthogonal `CLUSTER` selector (`kind` default) threaded Makefile → `scripts/devenv.py` → `scripts/lib/cluster.py` / `scripts/lib/deploy.py`. `CLUSTER=k3s` switches the kube-context to `k3s-rp` (never creating/deleting it), points image push/refs at an in-cluster registry (`rp2:30500`), selects a new `deployments/k8s/dev/k3s/` kustomize overlay that adds in-cluster Postgres, rewrites the DB URL host to the in-cluster `postgres` Service, and exposes the server via `kubectl port-forward svc/tmi-server 8080:8080`.

**Tech Stack:** k3s, kubectl, kustomize, Docker buildx, Python dev tooling (`uv`), Chainguard images, GORM AutoMigrate (schema-on-startup).

## Global Constraints

- **`kind` stays the default and must not regress.** `make dev-up` (no args) behaves exactly as today.
- `CLUSTER ?= kind`; only values `kind` and `k3s` are supported. `CLUSTER` is orthogonal to `DB` (all four `CLUSTER`×`DB` combos should be selectable, though this plan validates `CLUSTER=k3s DB=postgres`).
- We do **not** own the `k3s-rp` cluster: never `create`/`delete` it. `dev-nuke CLUSTER=k3s` removes only the `tmi-platform` namespace + PVCs.
- Remote cluster API server: `https://rp2:6443`; nodes and Mac dev host are both **arm64**.
- The in-cluster registry is `rp2:30500` (NodePort 30500), plain-HTTP (insecure). Requires one-time Mac Docker `insecure-registries` config and one-time k3s node `registries.yaml` config.
- Schema is created by the server's **GORM `AutoMigrate` at startup** (same as kind) — no separate migration Job. *(This deviates from the spec's "in-cluster migration Job" note; it is a deliberate simplification to match the existing kind path. Confirm before implementing.)*
- Redis stays in-cluster (`deployments/k8s/dev/redis.yml`), reused unchanged.
- Use Make targets; Python is invoked via `uv run scripts/devenv.py`.
- Never use Go's std `log`; use `slogging` — N/A here (no Go changes expected).

## Pre-flight environment facts to confirm (run once, before Task 1)

- [ ] `kubectl config get-contexts` shows `k3s-rp`; `kubectl --context k3s-rp get nodes` returns Ready nodes.
- [ ] `rp2` resolves from the Mac: `ping -c1 rp2` (or `getent hosts rp2`). If not, add it to `/etc/hosts` or use the node IP consistently in place of `rp2` throughout.
- [ ] Default StorageClass exists on k3s: `kubectl --context k3s-rp get storageclass` shows `local-path (default)`. PVCs in this plan omit `storageClassName` and rely on it.
- [ ] Node arch is arm64: `kubectl --context k3s-rp get nodes -o wide` (or `kubectl ... get node -o jsonpath='{.items[*].status.nodeInfo.architecture}'`) → `arm64`.

---

### Task 1: Thread the `CLUSTER` selector (Makefile + devenv.py), default `kind`

**Files:**
- Modify: `Makefile` (add `CLUSTER ?= kind`; pass `--cluster $(CLUSTER)` to every `devenv.py` dev invocation, next to the existing `--db $(DB)`)
- Modify: `scripts/devenv.py` (`_add_global_options()` ~line 127-169; `cmd_up`/`cmd_down`/`cmd_status`/`cmd_nuke`/`cmd_reset` ~line 40+; `cmd_cluster` ~line 106)

**Interfaces:**
- Consumes: nothing.
- Produces: `args.cluster` (`"kind"|"k3s"`, default `"kind"`) available to all dev commands and passed into `deploy.start(...)` / cluster lifecycle as a `cluster=` kwarg. Later tasks branch on it.

- [ ] **Step 1: Add the CLUSTER default and plumb it in the Makefile**

Near the top of `Makefile` (by `DB ?= postgres`, ~line 22), add:

```makefile
# Default kube cluster target for dev environment (kind|k3s)
CLUSTER ?= kind
```

Then, for every dev target that invokes `devenv.py` with `--db $(DB)`, add `--cluster $(CLUSTER)`. Example (apply the same edit to `dev-up`, `dev-down`, `dev-status`, `dev-reset`, `dev-nuke`, and the cluster up/down targets):

```makefile
# before:  @uv run scripts/devenv.py up --db $(DB)
# after:
	@uv run scripts/devenv.py up --db $(DB) --cluster $(CLUSTER)
```

- [ ] **Step 2: Add the `--cluster` global option in devenv.py**

In `scripts/devenv.py`, inside `_add_global_options()` (where `--db` is defined), add:

```python
    parser.add_argument(
        "--cluster",
        choices=["kind", "k3s"],
        default="kind",
        help="Kube cluster target: 'kind' (local, default) or 'k3s' (remote k3s-rp).",
    )
```

- [ ] **Step 3: Pass `args.cluster` through the command dispatchers**

In each `cmd_*` that calls into `deploy`/`cluster` (e.g. `cmd_up` calling `deploy.start(db=args.db, ...)`), thread the new kwarg, e.g.:

```python
    deploy.start(db=args.db, cluster=args.cluster, workers=..., ...)
```

Do the same for `cmd_down`, `cmd_status`, `cmd_reset`, `cmd_nuke`, and `cmd_cluster`. (Later tasks add the `cluster=` parameter to those functions; for now, adding it here with the functions still ignoring it is fine because default is `"kind"`.)

- [ ] **Step 4: Verify no regression to the default path**

Run: `make dev-status`
Expected: behaves exactly as before (reports kind `tmi-dev` status); no error about unknown `--cluster` arg.

Run: `uv run scripts/devenv.py status --cluster k3s` (should parse cleanly even though later tasks implement the behavior)
Expected: argument accepted (no argparse error). Behavior may still be kind-oriented until later tasks land.

- [ ] **Step 5: Commit**

```bash
git add Makefile scripts/devenv.py
git commit -m "feat(dev): thread CLUSTER selector (default kind) through dev tooling"
```

---

### Task 2: Cluster-aware lifecycle & context guard in `cluster.py`

**Files:**
- Modify: `scripts/lib/cluster.py` (constants ~line 16-31; `up()` ~119-136; `down()` ~138-142; `local_image_ref()` ~29-31; context-guard helper used by `deploy._guard_context()` ~188)

**Interfaces:**
- Consumes: `cluster` string from Task 1.
- Produces:
  - `K3S_CONTEXT = "k3s-rp"`, `K3S_REGISTRY = "rp2:30500"`.
  - `registry_for(cluster) -> str` → `"localhost:5000"` (kind) or `"rp2:30500"` (k3s).
  - `local_image_ref(name, tag="dev", *, cluster="kind")` → registry-correct image ref.
  - `up(cluster="kind")` / `down(cluster="kind")` that no-op cluster creation/deletion for k3s and switch context instead.
  - context guard accepts `k3s-rp` as a valid target when `cluster == "k3s"`.

- [ ] **Step 1: Add k3s constants and a registry selector**

In `scripts/lib/cluster.py`, near the existing registry constants:

```python
K3S_CONTEXT = "k3s-rp"
K3S_REGISTRY = "rp2:30500"   # in-cluster registry exposed via NodePort 30500


def registry_for(cluster: str) -> str:
    """Return the image registry hostname for the given cluster target."""
    return K3S_REGISTRY if cluster == "k3s" else LOCAL_REGISTRY
```

- [ ] **Step 2: Make `local_image_ref` cluster-aware**

Update the signature (currently `local_image_ref(name, tag="dev", registry=LOCAL_REGISTRY)` at ~line 29-31):

```python
def local_image_ref(name: str, tag: str = "dev", *, cluster: str = "kind") -> str:
    """Fully-qualified dev image ref for the given cluster's registry."""
    return f"{registry_for(cluster)}/{name}:{tag}"
```

Update existing callers that pass `registry=` to pass `cluster=` instead (search: `local_image_ref(`).

- [ ] **Step 3: Branch `up()`/`down()` on cluster**

At the top of `up()` (~line 119) and `down()` (~line 138):

```python
def up(cluster: str = "kind") -> None:
    if cluster == "k3s":
        # We do not own k3s-rp: just select its context. No create, no local registry.
        run(["kubectl", "config", "use-context", K3S_CONTEXT])
        return
    # ... existing kind path unchanged ...


def down(cluster: str = "kind") -> None:
    if cluster == "k3s":
        # Never delete a cluster we don't own; namespace teardown is handled by deploy.
        return
    # ... existing kind path unchanged ...
```

- [ ] **Step 4: Accept `k3s-rp` in the context guard**

`deploy._guard_context()` (deploy.py ~line 188) currently asserts the active kube-context is the local kind context. Make the allowed context depend on `cluster`. In `cluster.py`, expose:

```python
def expected_context(cluster: str) -> str:
    """The kube-context that must be active for the given cluster target."""
    return K3S_CONTEXT if cluster == "k3s" else f"kind-{CLUSTER_NAME}"
```

(Task 3/5 wire `deploy._guard_context(cluster)` to use this; for now just add the helper.)

- [ ] **Step 5: Verify kind path unchanged**

Run: `python3 -c "import sys; sys.path.insert(0,'scripts'); from lib import cluster; print(cluster.registry_for('kind'), cluster.registry_for('k3s')); print(cluster.local_image_ref('tmi-server', cluster='kind')); print(cluster.local_image_ref('tmi-server', cluster='k3s')); print(cluster.expected_context('kind'), cluster.expected_context('k3s'))"`
Expected:
```
localhost:5000 rp2:30500
localhost:5000/tmi-server:dev
rp2:30500/tmi-server:dev
kind-tmi-dev k3s-rp
```

- [ ] **Step 6: Commit**

```bash
git add scripts/lib/cluster.py scripts/lib/deploy.py
git commit -m "feat(dev): cluster-aware registry, image refs, and context guard"
```

---

### Task 3: In-cluster registry + one-time insecure-registry config + cluster-aware build/push

**Files:**
- Create: `deployments/k8s/dev/k3s/registry.yml` (registry Deployment + PVC + Service NodePort 30500)
- Modify: `scripts/lib/deploy.py` (`build_and_push(db)` ~300-330 → add `cluster` param; `ensure_registry` usage; push target)
- Create: `scripts/lib/k3s-node-setup.md` (documented one-time node config; optional helper)

**Interfaces:**
- Consumes: `registry_for(cluster)` (Task 2).
- Produces: a running `registry` Service reachable at `rp2:30500`; `build_and_push(db, cluster="kind")` that tags/pushes to the correct registry and builds arm64 for k3s.

- [ ] **Step 1: Write the in-cluster registry manifest**

Create `deployments/k8s/dev/k3s/registry.yml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tmi-platform
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: registry-data
  namespace: tmi-platform
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 10Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry
  namespace: tmi-platform
spec:
  replicas: 1
  selector:
    matchLabels: { app: registry }
  template:
    metadata:
      labels: { app: registry }
    spec:
      containers:
        - name: registry
          image: registry:2
          ports:
            - containerPort: 5000
          volumeMounts:
            - name: data
              mountPath: /var/lib/registry
          resources:
            requests: { cpu: 50m, memory: 128Mi }
            limits: { cpu: "1", memory: 512Mi }
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: registry-data
---
apiVersion: v1
kind: Service
metadata:
  name: registry
  namespace: tmi-platform
spec:
  type: NodePort
  selector: { app: registry }
  ports:
    - port: 5000
      targetPort: 5000
      nodePort: 30500
```

- [ ] **Step 2: Deploy the registry to k3s-rp**

Run: `kubectl --context k3s-rp apply -f deployments/k8s/dev/k3s/registry.yml`
Then: `kubectl --context k3s-rp -n tmi-platform rollout status deploy/registry --timeout=120s`
Expected: rollout succeeds; `kubectl --context k3s-rp -n tmi-platform get svc registry` shows NodePort 30500.

- [ ] **Step 3: One-time Mac Docker insecure-registry config**

Add `rp2:30500` to the Docker daemon's insecure registries so `docker push` works over plain HTTP. In Docker Desktop → Settings → Docker Engine, add:

```json
{ "insecure-registries": ["rp2:30500"] }
```

Apply & restart Docker. Verify: `docker info --format '{{.RegistryConfig.InsecureRegistryCIDRs}} {{range .RegistryConfig.IndexConfigs}}{{.Name}} {{end}}'` (or `docker info | grep -A3 "Insecure Registries"`) lists `rp2:30500`.

- [ ] **Step 4: One-time k3s node registries.yaml config (documented)**

Create `deployments/k8s/dev/k3s/README-node-setup.md` documenting the manual, per-node step (needs SSH/root on each Pi node — cannot be driven from the Mac):

```
On EACH k3s node, create/merge /etc/rancher/k3s/registries.yaml:

    mirrors:
      "rp2:30500":
        endpoint:
          - "http://rp2:30500"

Then restart k3s so containerd picks it up:
    sudo systemctl restart k3s        # on server nodes
    sudo systemctl restart k3s-agent  # on agent nodes

Verify containerd can pull:
    sudo k3s crictl pull rp2:30500/tmi-server:dev   # after first push
```

*(Optional helper: a small script that SSHes to a provided node list and installs the file. Keep it out of the default `dev-up` path — it needs credentials we don't manage.)*

- [ ] **Step 5: Make `build_and_push` cluster-aware (arm64 for k3s)**

In `deploy.py`, add a `cluster` parameter to `build_and_push` (~line 300) and use `cluster.registry_for(cluster)` for tags/push. For k3s, build explicitly for arm64 via buildx (Mac is arm64, so `docker build` already yields arm64, but be explicit and push in one shot):

```python
def build_and_push(db: str, cluster: str = "kind") -> None:
    registry = cluster_lib.registry_for(cluster)
    # ... stage tmi-client deps (unchanged) ...
    for name, dockerfile, build_args in image_builds_for(db):
        ref = f"{registry}/{name}:dev"
        if cluster == "k3s":
            # arm64 nodes; buildx --push tags+pushes to the in-cluster registry.
            cmd = ["docker", "buildx", "build", "--platform", "linux/arm64",
                   "-f", dockerfile]
            for k, v in build_args.items():
                cmd += ["--build-arg", f"{k}={v}"]
            cmd += ["-t", ref, "--push", str(PROJECT_ROOT)]
            run(cmd)
        else:
            # ... existing kind docker build + docker push to localhost:5000 ...
```

- [ ] **Step 6: Verify a build+push to the in-cluster registry**

Run: `uv run scripts/devenv.py --help` (sanity: tooling imports cleanly), then trigger the k3s build path once Task 4/5 wiring exists. Standalone check now:
`docker buildx build --platform linux/arm64 -f Dockerfile.server --build-arg BUILD_TAGS=dev -t rp2:30500/tmi-server:dev --push .`
Then: `curl -s http://rp2:30500/v2/tmi-server/tags/list`
Expected: `{"name":"tmi-server","tags":["dev"]}`

- [ ] **Step 7: Commit**

```bash
git add deployments/k8s/dev/k3s/registry.yml deployments/k8s/dev/k3s/README-node-setup.md scripts/lib/deploy.py
git commit -m "feat(dev): in-cluster k3s registry and cluster-aware image build/push"
```

---

### Task 4: k3s kustomize overlay with in-cluster Postgres + DB-host rewrite

**Files:**
- Create: `deployments/k8s/dev/k3s/postgres.yml` (StatefulSet + PVC + Service + Secret)
- Create: `deployments/k8s/dev/k3s/kustomization.yaml` (overlay: reuse server/redis/controller, remap images to `rp2:30500`, add postgres)
- Modify: `scripts/lib/deploy.py` (`overlay_dir_for` ~83-85 to be cluster-aware; DB-host rewrite ~117-125 & 354; `_no_workers_files` ~88-96)

**Interfaces:**
- Consumes: registry from Task 3; server manifest `deployments/k8s/dev/server.yml` (image `localhost:5000/tmi-server`).
- Produces: an overlay applied for `CLUSTER=k3s DB=postgres` that stands up in-cluster Postgres (`postgres:5432`) and points the server's DB URL at it.

- [ ] **Step 1: Write the in-cluster Postgres manifest**

Create `deployments/k8s/dev/k3s/postgres.yml`. Credentials MUST match `config-development.yml`'s `database.url` (user/password/db). Confirm those values first: `rg 'url:|postgres' config-development.yml`. This template assumes `tmi_dev` / `dev123` / `tmi_dev` — adjust to match:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tmi-postgres
  namespace: tmi-platform
type: Opaque
stringData:
  POSTGRES_USER: "tmi_dev"
  POSTGRES_PASSWORD: "dev123"
  POSTGRES_DB: "tmi_dev"
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: tmi-platform
spec:
  selector: { app: postgres }
  ports:
    - port: 5432
      targetPort: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: tmi-platform
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels: { app: postgres }
  template:
    metadata:
      labels: { app: postgres }
    spec:
      containers:
        - name: postgres
          image: cgr.dev/chainguard/postgres:latest
          envFrom:
            - secretRef: { name: tmi-postgres }
          ports:
            - containerPort: 5432
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          readinessProbe:
            exec: { command: ["pg_isready", "-U", "tmi_dev"] }
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests: { cpu: 100m, memory: 256Mi }
            limits: { cpu: "1", memory: 1Gi }
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 10Gi
```

- [ ] **Step 2: Write the k3s kustomize overlay**

Create `deployments/k8s/dev/k3s/kustomization.yaml`. Reuse the base resources, add postgres, and remap every `localhost:5000` image to `rp2:30500` via the images transformer:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tmi-platform
resources:
  - postgres.yml
  - ../controller.yml
  - ../redis.yml
  - ../server.yml
  - ../../platform/components/tmi-extractor.yml
  - ../../platform/components/tmi-chunk-embed.yml
images:
  - name: localhost:5000/tmi-server
    newName: rp2:30500/tmi-server
  - name: localhost:5000/tmi-component-controller
    newName: rp2:30500/tmi-component-controller
  - name: localhost:5000/tmi-extractor
    newName: rp2:30500/tmi-extractor
  - name: localhost:5000/tmi-chunk-embed
    newName: rp2:30500/tmi-chunk-embed
```

*(If the base applies image patches via `patches/extractor-image.yaml` that set `localhost:5000/...`, the `images:` transformer above still rewrites the final ref. Verify with the render in Step 5.)*

- [ ] **Step 3: Make `overlay_dir_for` cluster-aware**

`overlay_dir_for(db)` (deploy.py ~83-85) currently returns `dev/oracle` for oracle else `dev`. Extend to consider cluster:

```python
def overlay_dir_for(db: str, cluster: str = "kind") -> str:
    if cluster == "k3s":
        return "deployments/k8s/dev/k3s"     # postgres-in-cluster overlay
    if db == "oracle":
        return "deployments/k8s/dev/oracle"
    return "deployments/k8s/dev"
```

*(Oracle-on-k3s is out of scope for this plan; `CLUSTER=k3s` implies the postgres overlay.)*

- [ ] **Step 4: Rewrite the DB host to the in-cluster Service for k3s**

`rewrite_db_host_for_incluster(config_text)` (deploy.py ~117-125) rewrites `localhost` → `host.docker.internal`. Make the target host cluster-aware:

```python
def in_cluster_db_host(cluster: str) -> str:
    # kind: Postgres is a container on the Mac, reached via host.docker.internal.
    # k3s: Postgres runs in-cluster as the `postgres` Service.
    return "postgres" if cluster == "k3s" else "host.docker.internal"


def rewrite_db_host_for_incluster(config_text: str, cluster: str = "kind") -> str:
    host = in_cluster_db_host(cluster)
    # ... existing regex, substituting `host` for the hardcoded host.docker.internal ...
```

Update the caller in `deliver_config()` (~line 354) to pass `cluster`.

- [ ] **Step 5: Verify the overlay renders with correct images and DB host**

Run: `kubectl kustomize deployments/k8s/dev/k3s | rg 'image:|name: postgres|host'`
Expected: server/controller/extractor/chunk-embed images are `rp2:30500/...`; a `postgres` StatefulSet + Service are present.

Run (DB-host rewrite unit check):
`python3 -c "import sys; sys.path.insert(0,'scripts'); from lib import deploy; print(deploy.rewrite_db_host_for_incluster('url: postgres://tmi_dev:dev123@localhost:5432/tmi_dev', cluster='k3s'))"`
Expected: the URL host becomes `postgres` (`postgres://tmi_dev:dev123@postgres:5432/tmi_dev`).

- [ ] **Step 6: Commit**

```bash
git add deployments/k8s/dev/k3s/postgres.yml deployments/k8s/dev/k3s/kustomization.yaml scripts/lib/deploy.py
git commit -m "feat(dev): k3s overlay with in-cluster postgres and DB-host rewrite"
```

---

### Task 5: Server exposure via port-forward + wire dev-up/down/status/nuke for k3s

**Files:**
- Modify: `scripts/lib/deploy.py` (`start()` orchestration; add `start_server_port_forward()` mirroring `start_redis_port_forward()` ~495-509; `wait_for_server()` ~472-493; teardown/nuke path; `_guard_context()` ~188)

**Interfaces:**
- Consumes: overlay + registry + context from Tasks 2-4; `expected_context(cluster)` (Task 2).
- Produces: `make dev-up CLUSTER=k3s` brings up the stack and serves `http://localhost:8080` via port-forward; `dev-down`/`dev-nuke CLUSTER=k3s` tear down cleanly without touching the cluster.

- [ ] **Step 1: Guard the correct context per cluster**

Update `_guard_context()` (deploy.py ~188) to accept a `cluster` arg and assert the active context equals `cluster_lib.expected_context(cluster)` (kind-tmi-dev for kind, k3s-rp for k3s). Fail with a clear message if not.

- [ ] **Step 2: Add a server port-forward for k3s**

k3s has no kind `extraPortMappings`, so preserve `localhost:8080` with a background port-forward (mirror the existing `start_redis_port_forward()` at ~495-509):

```python
def start_server_port_forward() -> None:
    """Preserve the localhost:8080 contract on k3s via kubectl port-forward.

    NodePort rp2:30080 remains available for CATS/high-throughput runs where the
    userspace port-forward proxy throttles (see #463)."""
    _spawn_port_forward("svc/tmi-server", f"{HOST_PORT}:{HOST_PORT}")
```

Call it in `start()` only when `cluster == "k3s"` (after the server Deployment is Ready, before `wait_for_server()`). For kind, do nothing (the extraPortMappings path is unchanged). `wait_for_server()` already polls `http://localhost:8080`, so it works unchanged once the forward is up.

- [ ] **Step 3: Branch `start()` orchestration on cluster**

In `deploy.start(db, cluster="kind", ...)`:
- call `cluster_lib.up(cluster)` (switches context for k3s; creates kind otherwise),
- `_guard_context(cluster)`,
- `build_and_push(db, cluster)`,
- `ensure_namespace()` (unchanged; overlay also declares it),
- `deliver_config()` with `cluster` (DB-host rewrite),
- apply `overlay_dir_for(db, cluster)` via `kubectl apply -k`,
- for k3s: wait for `postgres` + `redis` rollouts, then `start_server_port_forward()`,
- `wait_for_server()`.

- [ ] **Step 4: Branch teardown / nuke on cluster**

- `dev-down CLUSTER=k3s`: kill the port-forward(s), `cluster_lib.down(cluster)` (no-op for k3s). Leave namespace running (parity with kind `dev-down` keeping data).
- `dev-nuke CLUSTER=k3s`: `kubectl --context k3s-rp delete namespace tmi-platform` (removes Deployments, StatefulSet, PVCs, registry). Do NOT delete the cluster.

Implement `dev-nuke` k3s branch:

```python
if cluster == "k3s":
    run(["kubectl", "--context", cluster_lib.K3S_CONTEXT,
         "delete", "namespace", "tmi-platform", "--ignore-not-found"])
    return
```

- [ ] **Step 5: Verify status reporting for k3s**

Run: `make dev-status CLUSTER=k3s`
Expected: reports the `k3s-rp` context and `tmi-platform` workloads (no attempt to query a kind cluster).

- [ ] **Step 6: Commit**

```bash
git add scripts/lib/deploy.py
git commit -m "feat(dev): k3s server port-forward, lifecycle, and nuke wiring"
```

---

### Task 6: End-to-end verification (k3s up) + kind non-regression

**Files:** none (validation).

**Interfaces:**
- Consumes: Tasks 1-5 and the one-time pre-flight node/Mac config.
- Produces: a confirmed working `CLUSTER=k3s` path and a confirmed-unchanged `kind` default.

- [ ] **Step 1: Bring up the k3s stack**

Run: `make dev-up CLUSTER=k3s`
Expected: images build (arm64) and push to `rp2:30500`; nodes pull them; `postgres`, `redis`, `tmi-server` (and workers) become Ready; a port-forward binds `localhost:8080`.

- [ ] **Step 2: Confirm the server is reachable and migrated**

Run: `curl -s http://localhost:8080/ | head -c 400`
Expected: the root endpoint returns the running version JSON (proves the server started and GORM AutoMigrate created the schema against in-cluster Postgres).

- [ ] **Step 3: Confirm DB tooling reaches in-cluster Postgres via port-forward**

Run: `kubectl --context k3s-rp -n tmi-platform port-forward svc/postgres 5432:5432 &` then `PGPASSWORD=dev123 psql -h localhost -U tmi_dev -d tmi_dev -c '\dt' | head`
Expected: TMI tables are listed. Kill the port-forward afterward.

- [ ] **Step 4: Confirm teardown leaves the cluster intact**

Run: `make dev-nuke CLUSTER=k3s`
Then: `kubectl --context k3s-rp get ns tmi-platform` → `NotFound`; `kubectl --context k3s-rp get nodes` → still Ready.
Expected: TMI namespace gone; cluster untouched.

- [ ] **Step 5: Confirm the kind default did not regress**

Run: `make dev-up` (no args), then `curl -s http://localhost:8080/ | head -c 200`, then `make dev-down`.
Expected: kind `tmi-dev` comes up and serves as before; no k3s involvement.

- [ ] **Step 6: Final commit (docs/status only, if any)**

```bash
git add -A
git commit -m "docs(dev): note CLUSTER=k3s usage" || echo "nothing to commit"
```

---

## Notes / follow-ups (not tasks)

- **Migration mechanism deviation:** this plan relies on the server's startup `AutoMigrate` (matching kind), not a discrete migration Job as the spec's open note suggested. Confirm this is acceptable; if a Job is required, add it before Task 5's `wait_for_server`.
- **Oracle-on-k3s** is out of scope (`CLUSTER=k3s` ⇒ postgres overlay). Supporting `CLUSTER=k3s DB=oracle` would need a `dev/k3s-oracle` overlay + secret wiring.
- **CATS / high-throughput:** the port-forward proxy throttles under load (#463). Document `rp2:30080` (NodePort) as the escape hatch for CATS runs against k3s.
- **User onboarding** for the one-time Mac `insecure-registries` and node `registries.yaml` steps belongs in the GitHub Wiki (per docs policy).
- **rp2 name resolution:** if `rp2` doesn't resolve on the Mac, every `rp2:30500`/`rp2:30080` reference must use the node IP or an `/etc/hosts` entry consistently.
