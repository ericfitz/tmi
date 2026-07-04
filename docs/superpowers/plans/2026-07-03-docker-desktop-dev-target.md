# Docker Desktop Dev Target Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the standalone-kind local dev deployment path with Docker Desktop's kind-provisioned Kubernetes (`CLUSTER=docker-desktop`, the new default), delivering images registry-free via `ctr import` and reusing the k3s port-forward/in-cluster-Postgres machinery.

**Architecture:** `docker-desktop` is treated like k3s — a cluster whose lifecycle we do NOT own (select context, never create/delete). The one new mechanism is registry-free image delivery: build locally, then `docker save <img> | docker exec -i desktop-control-plane ctr -n k8s.io images import -`. Endpoint is a port-forward (DD LoadBalancer is not localhost here). DB is in-cluster Postgres. Tasks 1–5 add docker-desktop and make it the default while kind still works; Task 6 retires the dev kind machinery.

**Tech Stack:** Python dev tooling (`scripts/devenv.py`, `scripts/lib/{cluster,deploy}.py`), kustomize overlays under `deployments/k8s/dev/`, Docker Desktop Kubernetes (kind provisioner), containerd `ctr`, GORM AutoMigrate.

## Global Constraints

- Tests run ONLY via make: `make test-dev-scripts` (dev-scripts unit tests). Never run `python`/`pytest`/`go test` directly.
- docker-desktop node container name: `desktop-control-plane`. Image import target namespace: `k8s.io`.
- docker-desktop images are referenced by BARE name+tag (`tmi-server:dev`), no registry prefix; `imagePullPolicy` must resolve to `IfNotPresent` (never `Always`) so the imported image is used without a registry pull.
- docker-desktop endpoint: port-forward `localhost:8080` → `svc/tmi-server`, `localhost:6379` → `svc/redis`. No LoadBalancer, no NodePort mapping, no extraPortMappings.
- docker-desktop DB: in-cluster Postgres, DB-URL host rewritten to the `postgres` Service. Postgres uses the cluster DEFAULT storageclass (`hostpath`) — NOT `longhorn` (that is k3s-only).
- docker-desktop redis: chainguard redis unchanged (DD kernel is 4KB pages; the k3s `redis:7-alpine` remap does NOT apply).
- Do NOT touch `deployments/k8s/platform/kind-cluster.yml`, `e2e-platform-*` Makefile targets, or `test/e2e/platform/` — e2e stays on standalone kind.
- Conventional commits; `feat(dev):`/`refactor(dev):`/`docs(dev):` scope.

---

## File Structure

- `scripts/lib/cluster.py` — add docker-desktop to `registry_for`/`local_image_ref`/`expected_context`/`up`/`down`; add `DD_NODE`, `DD_CONTEXT` constants. (Task 6 removes the kind machinery.)
- `scripts/lib/deploy.py` — add docker-desktop branches to `build_and_push` (import), `in_cluster_db_host`, `overlay_dir_for`; generalize `apply_k3s_postgres`→`apply_incluster_postgres` and `teardown_k3s_namespace`→`teardown_namespace`; extend port-forward gating; add `save_import_cmds` pure builder + `import_image_to_node`.
- `scripts/devenv.py` — add `docker-desktop` to `--cluster` choices; add cmd_nuke docker-desktop branch; (Task 5) change default; (Task 6) drop kind + Mac-Postgres path.
- `deployments/k8s/dev/docker-desktop/kustomization.yaml` — new overlay (bare image names, server pull-policy patch, worker image patches).
- `deployments/k8s/dev/docker-desktop/postgres.yml` — new in-cluster Postgres (default storageclass).
- `deployments/k8s/dev/docker-desktop/patches/{server-pullpolicy,extractor-image,chunkembed-image}.yaml` — new patches.
- `deployments/k8s/dev/docker-desktop/README.md` — new setup doc (enable DD k8s w/ kind provisioner; import mechanism; e2e-on-kind note).
- `Makefile` — `CLUSTER ?= docker-desktop`; help text.
- Tests: `scripts/lib/tests/test_cluster.py`, `test_deploy.py`, `test_devenv_cli.py`.

---

### Task 1: docker-desktop cluster identity + selector plumbing

Adds the docker-desktop target to the pure helpers and the `--cluster` parser, and its no-create lifecycle. No image/deploy behavior yet.

**Files:**
- Modify: `scripts/lib/cluster.py` (constants ~16-42, `registry_for`, `local_image_ref`, `expected_context`, `up`, `down`)
- Modify: `scripts/lib/deploy.py` (`in_cluster_db_host`, `overlay_dir_for`)
- Modify: `scripts/devenv.py` (`--cluster` choices, both parser branches ~172, ~180)
- Test: `scripts/lib/tests/test_cluster.py`, `test_deploy.py`, `test_devenv_cli.py`

**Interfaces:**
- Produces: `cluster.registry_for("docker-desktop") -> None`; `cluster.local_image_ref(name, cluster="docker-desktop") -> f"{name}:dev"` (bare); `cluster.expected_context("docker-desktop") -> "docker-desktop"`; `deploy.in_cluster_db_host("docker-desktop") -> "postgres"`; `deploy.overlay_dir_for(db, "docker-desktop") -> "deployments/k8s/dev/docker-desktop"`; `cluster.DD_CONTEXT="docker-desktop"`, `cluster.DD_NODE="desktop-control-plane"`.

- [ ] **Step 1: Write failing tests**

In `scripts/lib/tests/test_cluster.py`, add:
```python
class TestDockerDesktopIdentity(unittest.TestCase):
    def test_registry_for_docker_desktop_is_none(self):
        self.assertIsNone(cluster.registry_for("docker-desktop"))

    def test_local_image_ref_docker_desktop_is_bare(self):
        # No registry prefix — the image is imported straight into the node's containerd.
        self.assertEqual(cluster.local_image_ref("tmi-server", cluster="docker-desktop"), "tmi-server:dev")

    def test_expected_context_docker_desktop(self):
        self.assertEqual(cluster.expected_context("docker-desktop"), "docker-desktop")

    def test_constants(self):
        self.assertEqual(cluster.DD_CONTEXT, "docker-desktop")
        self.assertEqual(cluster.DD_NODE, "desktop-control-plane")
```
In `scripts/lib/tests/test_deploy.py`, extend `TestOverlayDirFor` and `TestInClusterDbHost`:
```python
    def test_overlay_dir_docker_desktop(self):
        self.assertTrue(deploy.overlay_dir_for("postgres", "docker-desktop").endswith("/docker-desktop"))

    def test_docker_desktop_uses_postgres_service(self):
        self.assertEqual(deploy.in_cluster_db_host("docker-desktop"), "postgres")
```
In `scripts/lib/tests/test_devenv_cli.py`, add (match the existing parser-test pattern in that file):
```python
    def test_cluster_accepts_docker_desktop(self):
        args = build_parser().parse_args(["--cluster", "docker-desktop", "up"])
        self.assertEqual(args.cluster, "docker-desktop")
```
(If the existing tests call the parser differently, mirror that file's existing `--cluster k3s` test exactly, substituting `docker-desktop`.)

- [ ] **Step 2: Run tests, verify they fail**

Run: `make test-dev-scripts`
Expected: FAIL — `AttributeError: module 'cluster' has no attribute 'DD_CONTEXT'` and assertion errors on registry_for/overlay_dir.

- [ ] **Step 3: Implement in cluster.py**

Add constants after the K3S ones (~line 28):
```python
# Docker Desktop dev target (CLUSTER=docker-desktop, the default). DD owns the
# cluster lifecycle (kind provisioner); we only select its context and never
# create/delete it. Images are imported straight into the node's containerd
# (no registry): docker save <img> | docker exec -i DD_NODE ctr -n k8s.io images import -.
DD_CONTEXT = "docker-desktop"
DD_NODE = "desktop-control-plane"
```
Change `registry_for` and `local_image_ref`:
```python
def registry_for(cluster: str = "kind") -> str | None:
    """Return the dev image-registry hostname, or None for docker-desktop (no
    registry — images are imported into the node's containerd)."""
    if cluster == "docker-desktop":
        return None
    return K3S_REGISTRY if cluster == "k3s" else LOCAL_REGISTRY


def local_image_ref(name: str, tag: str = "dev", *, cluster: str = "kind") -> str:
    """Return the dev image reference for the cluster: registry-qualified for
    kind/k3s, or a bare name:tag for docker-desktop (imported, not pulled)."""
    reg = registry_for(cluster)
    return f"{name}:{tag}" if reg is None else f"{reg}/{name}:{tag}"
```
Change `expected_context`:
```python
def expected_context(cluster: str = "kind") -> str:
    """Return the kube-context that must be active for the given cluster target."""
    if cluster == "docker-desktop":
        return DD_CONTEXT
    return K3S_CONTEXT if cluster == "k3s" else f"kind-{CLUSTER_NAME}"
```
Extend `up` (add BEFORE the `for tool in ("docker","kind","kubectl")` kind block, after the k3s block):
```python
    if cluster == "docker-desktop":
        check_tool("kubectl")
        log_info(f"Using Docker Desktop Kubernetes context '{DD_CONTEXT}' (no cluster create)")
        run_cmd(["kubectl", "config", "use-context", DD_CONTEXT])
        log_success(f"kube context set to '{DD_CONTEXT}'")
        return
```
Extend `down` (after the k3s no-op block):
```python
    if cluster == "docker-desktop":
        log_info("cluster down is a no-op for docker-desktop (Docker Desktop owns the cluster)")
        return
```

- [ ] **Step 4: Implement in deploy.py**

`in_cluster_db_host` — return `postgres` for docker-desktop too:
```python
    return "postgres" if cluster_target in ("k3s", "docker-desktop") else IN_CLUSTER_DB_HOST
```
`overlay_dir_for` — add docker-desktop:
```python
    if cluster_target == "k3s":
        return f"{DEV_DIR}/k3s"
    if cluster_target == "docker-desktop":
        return f"{DEV_DIR}/docker-desktop"
    return f"{DEV_DIR}/oracle" if db == "oracle" else DEV_DIR
```

- [ ] **Step 5: Implement in devenv.py**

At BOTH `--cluster` argument definitions (~172, ~180), change `choices=["kind", "k3s"]` to `choices=["kind", "k3s", "docker-desktop"]`. Leave the defaults unchanged for now (Task 5 changes them). Update the module docstring line 19: `--cluster kind|k3s|docker-desktop`.

- [ ] **Step 6: Run tests, verify pass**

Run: `make test-dev-scripts`
Expected: PASS (all prior tests + the new ones).

- [ ] **Step 7: Commit**
```bash
git add scripts/lib/cluster.py scripts/lib/deploy.py scripts/devenv.py \
        scripts/lib/tests/test_cluster.py scripts/lib/tests/test_deploy.py scripts/lib/tests/test_devenv_cli.py
git commit -m "feat(dev): add docker-desktop cluster identity + selector plumbing"
```

---

### Task 2: Registry-free image build + import

Build images locally and import them into the DD node's containerd, no registry.

**Files:**
- Modify: `scripts/lib/deploy.py` (`build_and_push`, add `save_import_cmds`, `import_image_to_node`)
- Test: `scripts/lib/tests/test_deploy.py`

**Interfaces:**
- Consumes: `cluster.local_image_ref(name, cluster="docker-desktop")` (bare ref), `cluster.DD_NODE`.
- Produces: `deploy.save_import_cmds(ref, node) -> tuple[list[str], list[str]]` (the `docker save` and `docker exec … ctr import` argv pair); `deploy.import_image_to_node(ref, node)` (runs the pipe); `build_and_push(db, "docker-desktop")` builds + imports (no push).

- [ ] **Step 1: Write failing test** (pure command builder)

In `test_deploy.py`:
```python
class TestSaveImportCmds(unittest.TestCase):
    def test_builds_docker_save_and_ctr_import_pair(self):
        save, imp = deploy.save_import_cmds("tmi-server:dev", "desktop-control-plane")
        self.assertEqual(save, ["docker", "save", "tmi-server:dev"])
        self.assertEqual(
            imp,
            ["docker", "exec", "-i", "desktop-control-plane",
             "ctr", "-n", "k8s.io", "images", "import", "-"],
        )
```

- [ ] **Step 2: Run test, verify it fails**

Run: `make test-dev-scripts`
Expected: FAIL — `module 'deploy' has no attribute 'save_import_cmds'`.

- [ ] **Step 3: Implement the builder + importer** in `deploy.py` (near `build_and_push`):
```python
def save_import_cmds(ref: str, node: str) -> tuple[list[str], list[str]]:
    """Return the (docker save, docker exec ctr import) argv pair that streams a
    locally-built image straight into a cluster node's containerd (k8s.io ns).
    This is exactly what `kind load docker-image` does under the hood — used for
    docker-desktop, whose cluster our standalone kind CLI cannot address."""
    return (
        ["docker", "save", ref],
        ["docker", "exec", "-i", node, "ctr", "-n", "k8s.io", "images", "import", "-"],
    )


def import_image_to_node(ref: str, node: str) -> None:
    """Stream `ref` from the host Docker into `node`'s containerd via a pipe."""
    save_cmd, import_cmd = save_import_cmds(ref, node)
    log_info(f"Importing {ref} -> {node} containerd (k8s.io)")
    saver = subprocess.Popen(save_cmd, stdout=subprocess.PIPE)
    try:
        importer = subprocess.Popen(import_cmd, stdin=saver.stdout)
        saver.stdout.close()  # allow saver to receive SIGPIPE if importer exits
        importer.communicate()
        if importer.returncode != 0:
            log_error(f"ctr import failed for {ref} (exit {importer.returncode})")
            sys.exit(1)
    finally:
        saver.wait()
    if saver.returncode != 0:
        log_error(f"docker save failed for {ref} (exit {saver.returncode})")
        sys.exit(1)
```

- [ ] **Step 4: Branch `build_and_push` for docker-desktop.** Replace the push line inside the build loop so it imports for docker-desktop and pushes otherwise:
```python
            run_cmd(cmd)

            if cluster_target == "docker-desktop":
                import_image_to_node(ref, cluster.DD_NODE)
            else:
                log_info(f"Pushing {ref}")
                run_cmd(["docker", "push", ref])

        if cluster_target == "docker-desktop":
            log_success("All images built and imported into the docker-desktop node")
        else:
            log_success("All images built and pushed to local registry")
```
Update the docstring's first paragraph to mention docker-desktop uses `ctr import` (no registry).

- [ ] **Step 5: Run tests, verify pass**

Run: `make test-dev-scripts`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add scripts/lib/deploy.py scripts/lib/tests/test_deploy.py
git commit -m "feat(dev): registry-free image import for docker-desktop (docker save | ctr import)"
```

---

### Task 3: docker-desktop overlay + in-cluster Postgres manifest

**Files:**
- Create: `deployments/k8s/dev/docker-desktop/kustomization.yaml`
- Create: `deployments/k8s/dev/docker-desktop/postgres.yml`
- Create: `deployments/k8s/dev/docker-desktop/patches/server-pullpolicy.yaml`
- Create: `deployments/k8s/dev/docker-desktop/patches/extractor-image.yaml`
- Create: `deployments/k8s/dev/docker-desktop/patches/chunkembed-image.yaml`

**Interfaces:**
- Produces: overlay dir consumed by `deploy.overlay_dir_for(db, "docker-desktop")`; `postgres.yml` applied by `deploy.apply_incluster_postgres` (Task 4).

- [ ] **Step 1: Create `postgres.yml`** — copy `deployments/k8s/dev/k3s/postgres.yml` verbatim EXCEPT the `volumeClaimTemplates` storageClassName: remove the `storageClassName: longhorn` line so the PVC uses DD's default (`hostpath`). Keep everything else (Secret `tmi-postgres` creds `tmi_dev/dev123/tmi_dev`, Service `postgres`, StatefulSet, PGDATA/locale env, probes, 8Gi request). Update the header comment to say "Docker Desktop dev target; uses the cluster default storageclass (hostpath)."

- [ ] **Step 2: Create the worker image patches** (bare names, IfNotPresent applies by default on `:dev`):

`patches/extractor-image.yaml`:
```yaml
- op: replace
  path: /spec/image
  value: tmi-extractor:dev
```
`patches/chunkembed-image.yaml`:
```yaml
- op: replace
  path: /spec/image
  value: tmi-chunk-embed:dev
```

- [ ] **Step 3: Create the server pull-policy patch** `patches/server-pullpolicy.yaml`. server.yml sets `imagePullPolicy: Always`, which would try to pull the bare `tmi-server:dev` from a nonexistent registry. Force IfNotPresent:
```yaml
- op: replace
  path: /spec/template/spec/containers/0/imagePullPolicy
  value: IfNotPresent
```

- [ ] **Step 4: Create `kustomization.yaml`**:
```yaml
# docker-desktop (CLUSTER=docker-desktop, the default) dev overlay. Images are
# imported into the node's containerd by bare name (no registry), so the images
# transformer strips the localhost:5000/ prefix and the server's imagePullPolicy
# is forced to IfNotPresent. Postgres (postgres.yml) is applied as a prerequisite
# by deploy.py, not here. Redis stays chainguard (DD kernel is 4KB pages).
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tmi-platform
resources:
  - ../controller.yml
  - ../redis.yml
  - ../server.yml
  - ../../platform/components/tmi-extractor.yml
  - ../../platform/components/tmi-chunk-embed.yml
images:
  - name: localhost:5000/tmi-server
    newName: tmi-server
  - name: localhost:5000/tmi-component-controller
    newName: tmi-component-controller
patches:
  - path: patches/server-pullpolicy.yaml
    target:
      kind: Deployment
      name: tmi-server
  - path: patches/extractor-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-extractor
  - path: patches/chunkembed-image.yaml
    target:
      group: tmi.dev
      version: v1alpha1
      kind: TMIComponent
      name: tmi-chunk-embed
```

- [ ] **Step 5: Verify the overlay renders correctly** (manual render check — no pytest, matching how the k3s overlay is validated):

Run: `kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/docker-desktop | rg -n "image:|imagePullPolicy"`
Expected: server → `image: tmi-server:dev` with `imagePullPolicy: IfNotPresent`; controller → `tmi-component-controller:dev`; redis → `cgr.dev/chainguard/redis:latest`; extractor → `tmi-extractor:dev`; chunk-embed → `tmi-chunk-embed:dev`. No `localhost:5000` anywhere:
Run: `kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/docker-desktop | rg "localhost:5000" && echo LEAK || echo OK`
Expected: `OK`.

- [ ] **Step 6: Commit**
```bash
git add deployments/k8s/dev/docker-desktop/
git commit -m "feat(dev): docker-desktop overlay + in-cluster Postgres (default storageclass)"
```

---

### Task 4: deploy.start / restart / nuke orchestration for docker-desktop

Wire the full bring-up, reusing the k3s in-cluster-Postgres + port-forward flow (generalized), and add the docker-desktop nuke path.

**Files:**
- Modify: `scripts/lib/deploy.py` (`apply_k3s_postgres`→`apply_incluster_postgres`, `teardown_k3s_namespace`→`teardown_namespace`, `start`, `restart`, `wait_and_forward`, port-forward gating)
- Modify: `scripts/devenv.py` (`cmd_nuke` docker-desktop branch)
- Test: `scripts/lib/tests/test_deploy.py`

**Interfaces:**
- Consumes: Task 1–3 outputs (`overlay_dir_for`, `in_cluster_db_host`, `build_and_push` import path, the overlay + postgres.yml).
- Produces: `make dev-up CLUSTER=docker-desktop` brings up the full stack at `localhost:8080`.

- [ ] **Step 1: Generalize the in-cluster Postgres + namespace helpers.**

Rename `apply_k3s_postgres` → `apply_incluster_postgres(cluster_target)` so it applies the right overlay's `postgres.yml`:
```python
def apply_incluster_postgres(cluster_target: str) -> None:
    """Apply the in-cluster Postgres for a cluster that hosts its own DB (k3s,
    docker-desktop) and wait for it — a prerequisite before the server, mirroring
    how the kind path brings the host DB up first."""
    project_root = get_project_root()
    subdir = "k3s" if cluster_target == "k3s" else "docker-desktop"
    kubectl(["apply", "-f", str(project_root / DEV_DIR / subdir / "postgres.yml")])
    kubectl(["-n", NS, "rollout", "status", "statefulset/postgres", "--timeout=180s"])
    log_success("In-cluster Postgres ready (svc/postgres:5432)")
```
Update the k3s `start()` call site from `apply_k3s_postgres()` to `apply_incluster_postgres("k3s")`.

Rename `teardown_k3s_namespace` → `teardown_namespace` (body unchanged — it deletes the `NS` namespace). Update its docstring to "Hard reset for a cluster we don't own (k3s, docker-desktop)…". Update the k3s call site in `devenv.py` `cmd_nuke`.

- [ ] **Step 2: Extend port-forward gating.** In `wait_and_forward` and `restart`, change `if cluster_target == "k3s":` (guarding `start_server_port_forward()`) to:
```python
    if cluster_target in ("k3s", "docker-desktop"):
        start_server_port_forward()
```

- [ ] **Step 3: Add the docker-desktop branch to `start()`.** Mirror the k3s branch but with NO registry and the in-cluster Postgres from the DD overlay:
```python
    if cluster_target == "k3s":
        ensure_k3s_registry()
    elif cluster_target != "docker-desktop":
        cluster.ensure_registry()
        cluster.connect_registry_to_kind()
    build_and_push(db, cluster_target)
    ensure_namespace()
    apply_platform_base()
    if cluster_target in ("k3s", "docker-desktop"):
        apply_incluster_postgres(cluster_target)
    deliver_config(cluster_target)
```
(That replaces the current k3s-vs-else registry block and the `if cluster_target == "k3s": apply_k3s_postgres()` line. docker-desktop skips registry setup entirely; `build_and_push` imports the images.)

- [ ] **Step 4: Add the docker-desktop branch to `restart()`** — same registry guard shape:
```python
    if cluster_target == "k3s":
        ensure_k3s_registry()
    elif cluster_target != "docker-desktop":
        cluster.ensure_registry()
        cluster.connect_registry_to_kind()
    build_and_push(db, cluster_target)
```

- [ ] **Step 5: Add the docker-desktop nuke branch in `devenv.py` `cmd_nuke`.** Change the k3s guard to cover both no-own-cluster targets:
```python
    if args.cluster in ("k3s", "docker-desktop"):
        cluster.up(cluster=args.cluster)      # ensure the right context is active
        deploy.teardown_namespace()
        deploy.remove_local_images(args.db, cluster_target=args.cluster)
        _clean_logs_and_files()
        deploy.start(db=args.db, cluster_target=args.cluster,
                     no_workers=args.no_workers, skip_context_guard=args.yes)
        log_success(f"dev-nuke complete (fresh {args.cluster} environment up)")
        return
```

- [ ] **Step 6: Add a unit test for the generalized port-forward gating** in `test_deploy.py` (source-level, extending the existing `test_server_port_forward_is_k3s_only` idea to include docker-desktop):
```python
    def test_server_port_forward_gated_for_docker_desktop_too(self):
        src = (Path(deploy.__file__)).read_text()
        # Both no-own-cluster targets gate the server port-forward together.
        self.assertIn('if cluster_target in ("k3s", "docker-desktop"):', src)
```
(If the existing `test_server_port_forward_is_k3s_only` test now conflicts — it asserted an exact `if cluster_target == "k3s":\n  start_server_port_forward()` string — update that test's regex to accept the tuple form `cluster_target in ("k3s", "docker-desktop")` guarding `start_server_port_forward()`, keeping its intent: the server port-forward exists only for no-own-cluster targets and is always gated.)

- [ ] **Step 7: Run unit tests**

Run: `make test-dev-scripts`
Expected: PASS.

- [ ] **Step 8: Live bring-up (the real gate).** Prereq: Docker Desktop → Settings → Kubernetes → Enable, provisioner = kind (context `docker-desktop` reachable).

Run: `make dev-up CLUSTER=docker-desktop`
Expected: images built + imported; NATS/KEDA applied; in-cluster Postgres Ready; overlay applied; server + controller roll out; port-forwards start; final line `Dev environment ready at http://localhost:8080`.
Run: `curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8080/`
Expected: `200`.
Run: `kubectl --context docker-desktop -n tmi-platform get pods`
Expected: `postgres-0`, `redis`, `nats-0`, `tmi-server`, `tmi-component-controller`, `tmi-extractor`, `tmi-chunk-embed` all `Running`/`Ready`.

- [ ] **Step 9: Commit**
```bash
git add scripts/lib/deploy.py scripts/devenv.py scripts/lib/tests/test_deploy.py
git commit -m "feat(dev): wire deploy.start/restart/nuke for the docker-desktop target"
```

---

### Task 5: Make docker-desktop the default + setup doc

**Files:**
- Modify: `Makefile` (line 26 `CLUSTER ?= kind`; help text lines 323, 326)
- Modify: `scripts/devenv.py` (subparser `--cluster` default ~180; docstring)
- Create: `deployments/k8s/dev/docker-desktop/README.md`

- [ ] **Step 1: Change the Makefile default.** Line 26: `CLUSTER ?= docker-desktop`. Update the two help/comment lines (323, 326) `CLUSTER=kind|k3s` → `CLUSTER=docker-desktop|k3s|kind`.

- [ ] **Step 2: Change the devenv default.** At the subparser `--cluster` (the one with `default="kind"`, ~line 180): `default="docker-desktop"`. Leave the top-level parser's `default=argparse.SUPPRESS` as-is.

- [ ] **Step 3: Write `deployments/k8s/dev/docker-desktop/README.md`** documenting: the one-time prereq (Docker Desktop → Settings → Kubernetes → Enable, provisioner = **kind**; verify `kubectl --context docker-desktop get nodes` shows `desktop-control-plane`); that images are imported into the node's containerd via `docker save | docker exec … ctr import` (no registry, no mirror); the endpoint is a port-forward on `localhost:8080`; and a note that **e2e-platform stays on standalone kind** because it needs a swappable NetworkPolicy-enforcing CNI (Calico) + ephemeral clusters that DD can't provide.

- [ ] **Step 4: Verify the default works.**

Run: `make dev-status` (should target docker-desktop context now) and `make dev-up` (no CLUSTER arg)
Expected: brings up docker-desktop; `curl localhost:8080` → 200. (If a stack is already up from Task 4, `make dev-reset` is the idempotent re-check.)

- [ ] **Step 5: Commit**
```bash
git add Makefile scripts/devenv.py deployments/k8s/dev/docker-desktop/README.md
git commit -m "feat(dev): default CLUSTER to docker-desktop; document setup"
```

---

### Task 6: Retire the standalone-kind dev machinery

Remove the dev kind path now that docker-desktop is the default. Keep the `kind` CLI + `deployments/k8s/platform/kind-cluster.yml` + `e2e-platform-*` (e2e only).

**Files:**
- Delete: `deployments/k8s/dev/kind-cluster.yml`
- Modify: `scripts/lib/cluster.py` (remove kind lifecycle + registry helpers + kind constants)
- Modify: `scripts/lib/deploy.py` (remove kind push path, kind constants/comments referencing extraPortMappings)
- Modify: `scripts/devenv.py` (drop `kind` from `--cluster` choices; drop the Mac-Postgres `_uses_host_db` path)
- Modify: tests referencing kind
- Modify: `scripts/lib/devstatus.py` if it references removed symbols

- [ ] **Step 1: Find every usage of the kind-only symbols** so nothing dangles:

Run: `rg -n "ensure_registry|connect_registry_to_kind|cluster_exists|start_stopped_nodes|_kind_node_containers|LOCAL_REGISTRY|REGISTRY_CONTAINER|KIND_CONFIG|is_registry_running|kind-cluster.yml|_uses_host_db|database\.(up|down|destroy)" scripts/`
Record the call sites; each must be removed or updated below. Expected users: `cluster.py` (definitions), `deploy.py` (`start`/`restart` registry block, `remove_local_images`, `build_and_push`), `devenv.py` (`_uses_host_db`, cmd_*), possibly `devstatus.py`.

- [ ] **Step 2: Remove the Mac-Postgres path in devenv.py.** With kind gone, no dev target uses the host Postgres container (k3s + docker-desktop are in-cluster). Delete `_uses_host_db` and every `if _uses_host_db(args): database.up/down/destroy(...)` block in `cmd_up`/`cmd_down`/`cmd_reset`/`cmd_nuke`. Remove the now-unused `database` import if nothing else uses it (verify with the Step-1 grep). `cmd_db` (the `db up/down` verb) — if it only managed the host container, remove it and its parser subcommand; if it is still referenced, leave it but have it log that it is a no-op for in-cluster DBs.

- [ ] **Step 3: Drop `kind` from the `--cluster` choices** (both parser definitions): `choices=["k3s", "docker-desktop"]`. Update the docstring line 19.

- [ ] **Step 4: Remove kind lifecycle from cluster.py.** Delete `ensure_registry`, `is_registry_running`, `cluster_exists`, `_kind_node_containers`, `start_stopped_nodes`, `connect_registry_to_kind`, and the entire kind branch of `up()` (the `for tool …`, `ensure_registry()`, `cluster_exists()`/create, `connect_registry_to_kind()` block) and the kind delete branch of `down()`. Remove now-unused constants: `REGISTRY_CONTAINER`, `REGISTRY_IMAGE`, `REGISTRY_PORT`, `LOCAL_REGISTRY`, `KIND_CONFIG`, and `CLUSTER_NAME` if unused after (check `expected_context` — it referenced `kind-{CLUSTER_NAME}`; since kind is no longer a valid target, simplify `expected_context` to only k3s + docker-desktop). Simplify `registry_for`/`local_image_ref` to drop the `LOCAL_REGISTRY` fallback (only `k3s` → registry, `docker-desktop` → None; make k3s explicit and raise/assert on unknown). Keep `is_local_kube_context` (still used by the guard).

  After edits, `up()` handles only `k3s` and `docker-desktop` (both: check_tool kubectl + use-context + return). `down()` is a no-op for both.

- [ ] **Step 5: Remove the kind push path in deploy.py.** In `start()`/`restart()`, the registry block reduces to:
```python
    if cluster_target == "k3s":
        ensure_k3s_registry()
    # docker-desktop: no registry — build_and_push imports the images.
    build_and_push(db, cluster_target)
```
In `build_and_push`, remove the kind branch: only `docker-desktop` (import) and `k3s` (push). In `remove_local_images`, drop kind handling. Update the `HOST_PORT`/`NODE_PORT` constant comment block (lines ~35-45) — it describes kind extraPortMappings; rewrite it to say the server is reached via a port-forward on both remaining targets (k3s/docker-desktop). `NODE_PORT` is still used by `server.yml`/`server-oracle.yml` (k3s CATS at `rp2:30080`) — keep the constant, drop the kind-extraPortMapping sentence.

- [ ] **Step 6: Delete the dev kind config.**

Run: `git rm deployments/k8s/dev/kind-cluster.yml`

- [ ] **Step 7: Update/remove kind tests.** In `test_deploy.py`: delete `test_kind_cluster_maps_host_to_nodeport` (the file it reads is gone). Keep `test_server_service_is_nodeport`/`test_server_oracle_service_is_nodeport` (the Service is still NodePort for k3s). In `test_cluster.py`: remove tests asserting kind create/registry behavior or `local_image_ref(..., cluster="kind")`/`expected_context("kind")` (kind is no longer a target); keep the docker-desktop + k3s + `is_local_kube_context` tests. In `test_devenv_cli.py`: remove any `--cluster kind` acceptance test; the parser now rejects `kind`. In `test_database.py`: if it only tested the host-Postgres helper that is now unused, leave the helper+tests in place only if the helper is still imported anywhere; otherwise delete both.

- [ ] **Step 8: Check devstatus.py** (make dev-status). If it references any removed symbol (e.g. `is_registry_running`, `REGISTRY_CONTAINER`, kind node discovery), update it to the k3s/docker-desktop reality (no local registry container; context-based status).

- [ ] **Step 9: Run unit tests**

Run: `make test-dev-scripts`
Expected: PASS (kind tests removed; docker-desktop + k3s green).

- [ ] **Step 10: Live non-regression** — verify BOTH remaining targets still work end to end.

Run: `make dev-up CLUSTER=docker-desktop` → `curl localhost:8080` → 200; `make dev-down`.
Run: `make dev-up CLUSTER=k3s` → `curl localhost:8080` → 200; `make dev-down`.

- [ ] **Step 11: Commit**
```bash
git add -A
git commit -m "refactor(dev): retire the standalone-kind dev path (docker-desktop is the default)"
```

---

## Notes for the implementer

- The docker-desktop node name (`desktop-control-plane`) is the DD kind-provisioner default. If a future DD version names it differently, `build_and_push`/`import_image_to_node` fail fast on `docker exec`; discover the real name with `kubectl --context docker-desktop get nodes` and update `cluster.DD_NODE`.
- `dev-nuke` for docker-desktop deletes ONLY the `tmi-platform` namespace — never the DD cluster or other namespaces the developer runs.
- e2e-platform is deliberately untouched. Do not "helpfully" migrate it — its Calico/NetworkPolicy + ephemeral-cluster requirements are incompatible with DD-managed Kubernetes (see the design spec).
