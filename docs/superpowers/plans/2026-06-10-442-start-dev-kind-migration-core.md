# start-dev Kubernetes Migration — Core (Postgres) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make start-dev` deploy the TMI server + platform into the current `kubectl` context (kind on laptop, k3s on desktop), with an external config-defined Postgres, dynamic config delivery, a local registry, an in-cluster controller + workers, and `restart-dev`/`stop-dev` lifecycle.

**Architecture:** Cluster-agnostic deploy into the active context. Images flow through a local `registry:2` at `localhost:5000`. A new Python module `scripts/lib/devenv.py` holds the pure logic (context guard, image refs, hashing, configmap rendering) and thin `kubectl`/`docker` wrappers; `scripts/dev-cluster.py` owns kind lifecycle + registry; `scripts/start-dev.py` is rewritten to build/push/apply/port-forward. The DB is never modeled in Kubernetes — the pod dials the configured endpoint directly. The controller runs in-cluster (new `Dockerfile.controller` + RBAC + Deployment); `GetConfigOrDie()` needs no Go change.

**Tech Stack:** Python 3.11 + uv (orchestration), kind, kubectl, kustomize (built into kubectl), Docker, `registry:2`, Chainguard static images, controller-runtime, KEDA, NATS JetStream.

**Spec:** `docs/superpowers/specs/2026-06-10-442-start-dev-kind-migration-design.md`

---

## File Structure

**Create:**
- `scripts/lib/devenv.py` — pure helpers (`local_image_ref`, `is_local_kube_context`, `content_hash`, `render_configmap_yaml`) + shell wrappers (`ensure_local_registry`, `current_kube_context`, `kubectl`, `docker_build_push`, `wait_rollout`).
- `scripts/lib/tests/__init__.py`, `scripts/lib/tests/test_devenv.py` — stdlib `unittest` for the pure helpers.
- `scripts/dev-cluster.py` — `up`/`down` for a local kind cluster wired to the registry.
- `deployments/k8s/dev/kind-cluster.yml` — dev kind config (default CNI + registry mirror).
- `deployments/k8s/dev/redis.yml` — Redis Deployment + Service.
- `Dockerfile.controller` — controller image (Chainguard static).
- `deployments/k8s/dev/controller.yml` — controller SA + ClusterRole + Binding + Deployment.
- `deployments/k8s/dev/server.yml` — server Deployment + Service.
- `deployments/k8s/dev/kustomization.yaml` — overlay: redis + controller + server + component CR image patches.
- `deployments/k8s/dev/patches/extractor-image.yaml`, `deployments/k8s/dev/patches/chunkembed-image.yaml` — JSON6902 image patches.

**Modify:**
- `Dockerfile.server` — add `BUILD_TAGS` build-arg (default empty; dev passes `dev`).
- `scripts/start-dev.py` — rewrite for the cluster flow (start / `--restart` / `--stop`).
- `Makefile` — rewire `start-dev`, add `restart-dev` (rewire), `stop-dev`, `dev-cluster-up`, `dev-cluster-down`, `test-dev-scripts`.

---

## Task 1: Pure helper module + tests (`devenv.py`)

**Files:**
- Create: `scripts/lib/devenv.py`
- Create: `scripts/lib/tests/__init__.py`
- Test: `scripts/lib/tests/test_devenv.py`

- [ ] **Step 1: Write the failing tests**

Create `scripts/lib/tests/__init__.py` (empty) and `scripts/lib/tests/test_devenv.py`:

```python
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import devenv  # noqa: E402


class TestLocalImageRef(unittest.TestCase):
    def test_default_registry_and_tag(self):
        self.assertEqual(devenv.local_image_ref("tmi-server"), "localhost:5000/tmi-server:dev")

    def test_explicit_tag(self):
        self.assertEqual(devenv.local_image_ref("tmi-extractor", tag="x"), "localhost:5000/tmi-extractor:x")


class TestIsLocalKubeContext(unittest.TestCase):
    def test_kind_prefix_is_local(self):
        self.assertTrue(devenv.is_local_kube_context("kind-tmi-platform"))

    def test_k3d_prefix_is_local(self):
        self.assertTrue(devenv.is_local_kube_context("k3d-dev"))

    def test_known_exact_names_local(self):
        for name in ("k3s", "default", "rancher-desktop", "docker-desktop", "minikube"):
            self.assertTrue(devenv.is_local_kube_context(name), name)

    def test_prod_like_context_not_local(self):
        self.assertFalse(devenv.is_local_kube_context("gke_prod-proj_us-east1_tmi"))

    def test_empty_not_local(self):
        self.assertFalse(devenv.is_local_kube_context(""))


class TestContentHash(unittest.TestCase):
    def test_stable_and_short(self):
        h1 = devenv.content_hash("abc")
        h2 = devenv.content_hash("abc")
        self.assertEqual(h1, h2)
        self.assertEqual(len(h1), 12)

    def test_differs_on_change(self):
        self.assertNotEqual(devenv.content_hash("abc"), devenv.content_hash("abd"))


class TestRenderConfigmapYaml(unittest.TestCase):
    def test_contains_name_namespace_and_file_key(self):
        out = devenv.render_configmap_yaml(
            name="tmi-server-config", namespace="tmi-platform",
            file_key="config.yml", content="server:\n  port: 8080\n",
        )
        self.assertIn("name: tmi-server-config", out)
        self.assertIn("namespace: tmi-platform", out)
        self.assertIn("config.yml: |", out)
        self.assertIn("port: 8080", out)

    def test_indents_multiline_content_under_key(self):
        out = devenv.render_configmap_yaml(
            name="c", namespace="n", file_key="f.yml", content="a: 1\nb: 2\n",
        )
        # Each content line indented 4 spaces under the "f.yml: |" block scalar.
        self.assertIn("\n    a: 1", out)
        self.assertIn("\n    b: 2", out)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `uv run python -m unittest discover -s scripts/lib/tests -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'devenv'`.

- [ ] **Step 3: Write the minimal implementation**

Create `scripts/lib/devenv.py`:

```python
"""Pure helpers + thin shell wrappers for the cluster-based dev environment.

Pure functions are unit-tested in scripts/lib/tests/test_devenv.py.  Shell
wrappers delegate to tmi_common.run_cmd and are exercised by start-dev.py /
dev-cluster.py against a real cluster.
"""

from __future__ import annotations

import hashlib
import subprocess

from tmi_common import run_cmd

LOCAL_REGISTRY = "localhost:5000"
REGISTRY_CONTAINER = "tmi-dev-registry"
PLATFORM_NAMESPACE = "tmi-platform"

# Contexts we consider safe to deploy a dev environment into without --yes.
_LOCAL_CONTEXT_PREFIXES = ("kind-", "k3d-")
_LOCAL_CONTEXT_EXACT = {"k3s", "default", "rancher-desktop", "docker-desktop", "minikube"}


def local_image_ref(name: str, tag: str = "dev", registry: str = LOCAL_REGISTRY) -> str:
    """Return the fully-qualified local-registry image reference."""
    return f"{registry}/{name}:{tag}"


def is_local_kube_context(name: str) -> bool:
    """True if the kubectl context name looks like a local dev cluster."""
    if not name:
        return False
    if name in _LOCAL_CONTEXT_EXACT:
        return True
    return any(name.startswith(p) for p in _LOCAL_CONTEXT_PREFIXES)


def content_hash(text: str) -> str:
    """Stable 12-char hex digest of text (for config-change annotations)."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:12]


def render_configmap_yaml(*, name: str, namespace: str, file_key: str, content: str) -> str:
    """Render a ConfigMap manifest embedding `content` under `file_key`.

    Uses a block scalar with 4-space indentation; annotates the content hash.
    """
    indented = "\n".join("    " + line for line in content.splitlines())
    return (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        f"  name: {name}\n"
        f"  namespace: {namespace}\n"
        "  annotations:\n"
        f"    tmi.dev/config-hash: \"{content_hash(content)}\"\n"
        "data:\n"
        f"  {file_key}: |\n"
        f"{indented}\n"
    )


# --- shell wrappers (not unit-tested; exercised against a live cluster) ---

def current_kube_context() -> str:
    """Return the active kubectl context name (empty string if none)."""
    try:
        out = subprocess.run(
            ["kubectl", "config", "current-context"],
            capture_output=True, text=True, check=True,
        )
        return out.stdout.strip()
    except subprocess.CalledProcessError:
        return ""


def kubectl(args: list[str], *, check: bool = True, input_text: str | None = None):
    """Run kubectl with the given args."""
    return run_cmd(["kubectl", *args], check=check, input_text=input_text)
```

Note: `run_cmd` must accept `input_text`. If it does not yet, add an `input_text: str | None = None` kwarg that is forwarded to `subprocess.run(..., input=input_text, text=True)` in `scripts/lib/tmi_common.py`. Check its current signature (line ~175) first; if `input` is already supported under another name, use that and adjust the wrapper.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `uv run python -m unittest discover -s scripts/lib/tests -v`
Expected: PASS (all tests).

- [ ] **Step 5: Add a make target and commit**

Add to `Makefile` (near the other test targets):

```makefile
test-dev-scripts:  ## Run unit tests for the dev-environment Python helpers
	@uv run python -m unittest discover -s scripts/lib/tests -v
```

Add `test-dev-scripts` to the relevant `.PHONY` line.

```bash
git add scripts/lib/devenv.py scripts/lib/tests/__init__.py scripts/lib/tests/test_devenv.py scripts/lib/tmi_common.py Makefile
git commit -m "feat(dev): pure helpers for cluster-based dev env + unit tests"
```

---

## Task 2: `BUILD_TAGS` arg on `Dockerfile.server`

**Files:**
- Modify: `Dockerfile.server` (the `go build` invocation)

- [ ] **Step 1: Add the build-arg and thread it into `go build`**

In `Dockerfile.server`, add near the top of the builder stage (after `FROM ... AS builder`):

```dockerfile
# Optional Go build tags (e.g. "dev" to enable login_hint + the test OAuth provider)
ARG BUILD_TAGS=""
```

Change the `go build` line to include the tags. Locate the existing
`CGO_ENABLED=0 GOOS=linux go build \` and add `-tags "${BUILD_TAGS}"` as the
first flag after `build`:

```dockerfile
    CGO_ENABLED=0 GOOS=linux go build \
    -tags "${BUILD_TAGS}" \
    -ldflags "-s -w -X github.com/ericfitz/tmi/api.VersionMajor=${MAJOR} ...(unchanged)..." \
    -trimpath \
    -buildmode=exe \
    -o tmiserver \
    ./cmd/server
```

(Keep the rest of the `-ldflags` value exactly as it is.)

- [ ] **Step 2: Verify the prod build is unchanged (empty tags) and dev build works**

Run:
```bash
docker build -f Dockerfile.server -t tmi-server:plain-check . --target builder >/dev/null && echo OK-plain
docker build -f Dockerfile.server --build-arg BUILD_TAGS=dev -t tmi-server:dev-check . --target builder >/dev/null && echo OK-dev
```
Expected: both print `OK-...`. (If `--target builder` is unavailable because the stage isn't named, build the full image instead.)

- [ ] **Step 3: Commit**

```bash
git add Dockerfile.server
git commit -m "build(dev): add BUILD_TAGS arg to Dockerfile.server for -tags=dev"
```

---

## Task 3: Controller image (`Dockerfile.controller`)

**Files:**
- Create: `Dockerfile.controller`

- [ ] **Step 1: Write the Dockerfile**

Create `Dockerfile.controller` (mirrors `Dockerfile.server`'s two-stage shape; builds the controller, CGO-free):

```dockerfile
# Multi-stage Chainguard build for the TMIComponent controller.
FROM cgr.dev/chainguard/go:latest AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY .docker-deps/tmi-client/ /tmi-client/
RUN sed -i 's|=> ../tmi-clients/go-client-generated/[^ ]*|=> /tmi-client|' go.mod
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildmode=exe \
    -o component-controller ./cmd/component-controller

FROM cgr.dev/chainguard/static:latest
COPY --from=builder /app/component-controller /component-controller
USER nonroot:nonroot
ENTRYPOINT ["/component-controller"]
```

- [ ] **Step 2: Verify it builds**

Run: `docker build -f Dockerfile.controller -t tmi-component-controller:build-check .`
Expected: build succeeds, ends with `naming to ... tmi-component-controller:build-check`.

Note: the `.docker-deps/tmi-client/` staging is created by the existing container-build plumbing. If the build fails on the `COPY .docker-deps/...` line, run the existing prerequisite first (the same step `make build-server-container` performs to stage the client) and retry. Inspect `scripts/build-app-containers.py` for the exact staging command and reuse it.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile.controller
git commit -m "build(dev): add Dockerfile.controller (Chainguard static, CGO-free)"
```

---

## Task 4: Controller RBAC + Deployment manifest

**Files:**
- Create: `deployments/k8s/dev/controller.yml`

- [ ] **Step 1: Write the manifest**

Create `deployments/k8s/dev/controller.yml`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tmi-controller
  namespace: tmi-platform
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tmi-controller
rules:
  - apiGroups: ["tmi.dev"]
    resources: ["tmicomponents"]
    verbs: ["get", "list", "watch", "update", "patch"]
  - apiGroups: ["tmi.dev"]
    resources: ["tmicomponents/status"]
    verbs: ["get", "update", "patch"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["networkpolicies"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["keda.sh"]
    resources: ["scaledobjects"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tmi-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: tmi-controller
subjects:
  - kind: ServiceAccount
    name: tmi-controller
    namespace: tmi-platform
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-component-controller
  namespace: tmi-platform
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tmi-component-controller
  template:
    metadata:
      labels:
        app: tmi-component-controller
    spec:
      serviceAccountName: tmi-controller
      containers:
        - name: controller
          image: localhost:5000/tmi-component-controller:dev
          securityContext:
            runAsNonRoot: true
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { cpu: 500m, memory: 256Mi }
```

- [ ] **Step 2: Validate against the API server (dry-run)**

Run (requires a reachable cluster with the CRD + KEDA installed — e.g. after `make e2e-platform-up`, or defer to Task 9's end-to-end run):
```bash
kubectl apply --dry-run=server -f deployments/k8s/dev/controller.yml
```
Expected: each object reports `... (server dry run)` with no error. If KEDA/CRD aren't installed yet, use `--dry-run=client` for a schema-only check here and rely on Task 9 for the live check.

- [ ] **Step 3: Commit**

```bash
git add deployments/k8s/dev/controller.yml
git commit -m "feat(dev): in-cluster controller RBAC + Deployment manifest"
```

---

## Task 5: Redis manifest

**Files:**
- Create: `deployments/k8s/dev/redis.yml`

- [ ] **Step 1: Write the manifest**

Create `deployments/k8s/dev/redis.yml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: tmi-platform
spec:
  replicas: 1
  selector:
    matchLabels: { app: redis }
  template:
    metadata:
      labels: { app: redis }
    spec:
      containers:
        - name: redis
          image: cgr.dev/chainguard/redis:latest
          args: ["--save", "", "--appendonly", "no"]
          ports:
            - containerPort: 6379
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { cpu: 500m, memory: 256Mi }
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: tmi-platform
spec:
  selector: { app: redis }
  ports:
    - port: 6379
      targetPort: 6379
```

Verify the Chainguard redis image entrypoint accepts those args; if it differs, drop `args` (defaults are fine for a dev cache). Cross-check `Dockerfile.redis` for the exact image/run shape the project already uses and prefer that.

- [ ] **Step 2: Validate**

Run: `kubectl apply --dry-run=client -f deployments/k8s/dev/redis.yml`
Expected: `deployment.apps/redis configured (dry run)` and `service/redis configured (dry run)`.

- [ ] **Step 3: Commit**

```bash
git add deployments/k8s/dev/redis.yml
git commit -m "feat(dev): in-cluster Redis manifest for the dev environment"
```

---

## Task 6: Server manifest

**Files:**
- Create: `deployments/k8s/dev/server.yml`

- [ ] **Step 1: Write the manifest**

Create `deployments/k8s/dev/server.yml`. The pod mounts the config ConfigMap (created imperatively by start-dev) at `/etc/tmi` and runs with `--config`. Redis/NATS addresses are injected as env (cluster-topology values we own); DB connectivity comes from the mounted config.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tmi-server
  namespace: tmi-platform
spec:
  replicas: 1
  selector:
    matchLabels: { app: tmi-server }
  template:
    metadata:
      labels: { app: tmi-server }
    spec:
      containers:
        - name: server
          image: localhost:5000/tmi-server:dev
          args: ["--config=/etc/tmi/config.yml"]
          ports:
            - containerPort: 8080
          env:
            - { name: TMI_SERVER_INTERFACE, value: "0.0.0.0" }
            - { name: TMI_SERVER_PORT, value: "8080" }
            - { name: TMI_DATABASE_REDIS_HOST, value: "redis" }
            - { name: TMI_NATS_URL, value: "nats://nats.tmi-platform.svc:4222" }
          volumeMounts:
            - name: config
              mountPath: /etc/tmi
              readOnly: true
          readinessProbe:
            httpGet: { path: /, port: 8080 }
            initialDelaySeconds: 3
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /, port: 8080 }
            initialDelaySeconds: 15
            periodSeconds: 20
          resources:
            requests: { cpu: 100m, memory: 128Mi }
            limits: { cpu: 1000m, memory: 512Mi }
      volumes:
        - name: config
          configMap:
            name: tmi-server-config
---
apiVersion: v1
kind: Service
metadata:
  name: tmi-server
  namespace: tmi-platform
spec:
  selector: { app: tmi-server }
  ports:
    - port: 8080
      targetPort: 8080
```

Confirm the config key: the ConfigMap is created with key `config.yml` (Task 8), mounted at `/etc/tmi/config.yml`, matching `--config`. Confirm the server treats `TMI_*` env as overrides over the file (it does — `manage-server.py` relies on the same loader). Confirm the readiness path `/` returns 200 once booted (the root endpoint returns version JSON).

- [ ] **Step 2: Validate**

Run: `kubectl apply --dry-run=client -f deployments/k8s/dev/server.yml`
Expected: `deployment.apps/tmi-server configured (dry run)` and `service/tmi-server configured (dry run)`.

- [ ] **Step 3: Commit**

```bash
git add deployments/k8s/dev/server.yml
git commit -m "feat(dev): in-cluster server Deployment + Service manifest"
```

---

## Task 7: Component-CR image-patch overlay (kustomize)

**Files:**
- Create: `deployments/k8s/dev/patches/extractor-image.yaml`
- Create: `deployments/k8s/dev/patches/chunkembed-image.yaml`
- Create: `deployments/k8s/dev/kustomization.yaml`

- [ ] **Step 1: Write the JSON6902 patches**

`deployments/k8s/dev/patches/extractor-image.yaml`:

```yaml
- op: replace
  path: /spec/image
  value: localhost:5000/tmi-extractor:dev
```

`deployments/k8s/dev/patches/chunkembed-image.yaml`:

```yaml
- op: replace
  path: /spec/image
  value: localhost:5000/tmi-chunk-embed:dev
```

- [ ] **Step 2: Write the kustomization**

`deployments/k8s/dev/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: tmi-platform
resources:
  - controller.yml
  - redis.yml
  - server.yml
  - ../platform/components/tmi-extractor.yml
  - ../platform/components/tmi-chunk-embed.yml
patches:
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

- [ ] **Step 3: Verify kustomize renders with patched images**

Run: `kubectl kustomize deployments/k8s/dev | grep -nE "image:" | sort -u`
Expected output includes:
```
image: cgr.dev/chainguard/redis:latest
image: localhost:5000/tmi-chunk-embed:dev
image: localhost:5000/tmi-component-controller:dev
image: localhost:5000/tmi-extractor:dev
image: localhost:5000/tmi-server:dev
```
And `kubectl kustomize deployments/k8s/dev | grep -c "kind: TMIComponent"` prints `2`.

- [ ] **Step 4: Commit**

```bash
git add deployments/k8s/dev/kustomization.yaml deployments/k8s/dev/patches/
git commit -m "feat(dev): kustomize overlay with component-CR image patches"
```

---

## Task 8: `dev-cluster.py` (kind + registry lifecycle)

**Files:**
- Create: `deployments/k8s/dev/kind-cluster.yml`
- Create: `scripts/dev-cluster.py`
- Modify: `Makefile` (add `dev-cluster-up`, `dev-cluster-down`)

- [ ] **Step 1: Write the dev kind config**

Create `deployments/k8s/dev/kind-cluster.yml` (default CNI — NetworkPolicy enforcement not needed in dev — plus a containerd mirror so `localhost:5000` resolves inside the cluster):

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: tmi-dev
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5000"]
      endpoint = ["http://tmi-dev-registry:5000"]
nodes:
  - role: control-plane
  - role: worker
```

- [ ] **Step 2: Write `dev-cluster.py`**

Create `scripts/dev-cluster.py`:

```python
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Create/delete a local kind cluster wired to the local dev registry."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import run_cmd, log_info, log_success, check_tool  # noqa: E402
import devenv  # noqa: E402

CLUSTER_NAME = "tmi-dev"
KIND_CONFIG = "deployments/k8s/dev/kind-cluster.yml"


def ensure_registry() -> None:
    """Start the local registry container if it is not already running."""
    existing = run_cmd(
        ["docker", "ps", "-aq", "-f", f"name=^{devenv.REGISTRY_CONTAINER}$"],
        check=False, capture=True,
    )
    if existing and existing.stdout.strip():
        run_cmd(["docker", "start", devenv.REGISTRY_CONTAINER], check=False)
    else:
        run_cmd([
            "docker", "run", "-d", "--restart=always", "-p", "127.0.0.1:5000:5000",
            "--name", devenv.REGISTRY_CONTAINER, "registry:2",
        ])
    log_success(f"local registry running on {devenv.LOCAL_REGISTRY}")


def connect_registry_to_kind() -> None:
    """Attach the registry container to the kind network so nodes can pull."""
    run_cmd(["docker", "network", "connect", "kind", devenv.REGISTRY_CONTAINER], check=False)


def up() -> None:
    for tool in ("docker", "kind", "kubectl"):
        check_tool(tool)
    ensure_registry()
    existing = run_cmd(["kind", "get", "clusters"], check=False, capture=True)
    if existing and CLUSTER_NAME in (existing.stdout or ""):
        log_info(f"kind cluster '{CLUSTER_NAME}' already exists")
    else:
        run_cmd(["kind", "create", "cluster", "--config", KIND_CONFIG])
    connect_registry_to_kind()
    log_success(f"kind cluster '{CLUSTER_NAME}' ready; kubectl context kind-{CLUSTER_NAME}")


def down() -> None:
    check_tool("kind")
    run_cmd(["kind", "delete", "cluster", "--name", CLUSTER_NAME])
    log_success(f"kind cluster '{CLUSTER_NAME}' deleted")


def main() -> None:
    action = sys.argv[1] if len(sys.argv) > 1 else "up"
    {"up": up, "down": down}[action]()


if __name__ == "__main__":
    main()
```

Note: this uses `run_cmd(..., capture=True)` and `check_tool`. Verify `run_cmd`'s signature supports a capture kwarg (it returns a `CompletedProcess` per Task 1's `current_kube_context` usage — confirm and align the kwarg name; adjust if it is e.g. `capture_output`). `check_tool` exists at `scripts/lib/tmi_common.py:574`.

- [ ] **Step 3: Add make targets**

Add to `Makefile`:

```makefile
dev-cluster-up:  ## Create a local kind cluster wired to the dev registry (laptop path)
	@uv run scripts/dev-cluster.py up

dev-cluster-down:  ## Delete the local kind dev cluster
	@uv run scripts/dev-cluster.py down
```

Add both to the appropriate `.PHONY` line.

- [ ] **Step 4: Smoke-test the cluster lifecycle**

Run:
```bash
make dev-cluster-up
kubectl config current-context        # expect: kind-tmi-dev
docker ps --filter name=tmi-dev-registry --format '{{.Names}}'   # expect: tmi-dev-registry
```
Expected: context is `kind-tmi-dev`; registry container listed. Leave the cluster up for Task 9.

- [ ] **Step 5: Commit**

```bash
git add deployments/k8s/dev/kind-cluster.yml scripts/dev-cluster.py Makefile
git commit -m "feat(dev): kind dev-cluster lifecycle + local registry wiring"
```

---

## Task 9: `start-dev.py` rewrite (build, push, deliver config, apply, port-forward)

**Files:**
- Modify: `scripts/start-dev.py` (full rewrite)
- Modify: `Makefile` (`start-dev`, `restart-dev`, `stop-dev`)

- [ ] **Step 1: Rewrite `start-dev.py`**

Replace `scripts/start-dev.py` with:

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Deploy the TMI dev environment into the current kubectl context.

Cluster-agnostic: deploys into whatever cluster kubectl currently targets.
The database is external and defined entirely by config-development.yml; the
server pod dials it directly.  Redis, NATS, KEDA, the controller, and the two
worker components run in-cluster.  Images are pushed to a local registry that
the cluster pulls from.

Actions: (default) start | --restart | --stop
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args, apply_verbosity, check_tool, get_project_root,
    log_error, log_info, log_success, run_cmd,
)
import devenv  # noqa: E402

NS = devenv.PLATFORM_NAMESPACE
DEV_DIR = "deployments/k8s/dev"
PLATFORM_DIR = "deployments/k8s/platform"
CONFIG_FILE = "config-development.yml"
CONFIGMAP_NAME = "tmi-server-config"
SERVER_IMAGES = ["tmi-server", "tmi-component-controller", "tmi-extractor", "tmi-chunk-embed"]
PORT_FORWARD_PID = "/tmp/tmi-dev-portforward.pid"


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Deploy the TMI dev environment.")
    add_verbosity_args(p)
    g = p.add_mutually_exclusive_group()
    g.add_argument("--restart", action="store_true", help="Rebuild+push the server, redeliver config, roll the server")
    g.add_argument("--stop", action="store_true", help="Tear down everything start-dev deployed")
    p.add_argument("--yes", action="store_true", help="Skip the local-context safety check")
    p.add_argument("--no-workers", action="store_true", help="Do not apply the component CRs")
    return p.parse_args()


def guard_context(skip: bool) -> str:
    ctx = devenv.current_kube_context()
    log_info(f"kubectl context: {ctx or '(none)'}  namespace: {NS}")
    if not ctx:
        log_error("No kubectl context is set. Run 'make dev-cluster-up' or select your k3s context.")
        sys.exit(1)
    if not skip and not devenv.is_local_kube_context(ctx):
        log_error(f"Context '{ctx}' is not a recognized local dev cluster. Re-run with --yes to override.")
        sys.exit(1)
    return ctx


def preflight() -> None:
    for tool in ("docker", "kubectl"):
        check_tool(tool)
    r = run_cmd(["kubectl", "cluster-info"], check=False)
    if r.returncode != 0:
        log_error("No reachable cluster. Run 'make dev-cluster-up' (kind) or start your k3s cluster.")
        sys.exit(1)


def build_and_push() -> None:
    root = get_project_root()
    builds = {
        "tmi-server": ("Dockerfile.server", {"BUILD_TAGS": "dev"}),
        "tmi-component-controller": ("Dockerfile.controller", {}),
        "tmi-extractor": ("Dockerfile.extractor", {}),
        "tmi-chunk-embed": ("Dockerfile.chunkembed", {}),
    }
    for name, (dockerfile, args) in builds.items():
        ref = devenv.local_image_ref(name)
        cmd = ["docker", "build", "-f", dockerfile, "-t", ref]
        for k, v in args.items():
            cmd += ["--build-arg", f"{k}={v}"]
        cmd += ["."]
        run_cmd(cmd, cwd=str(root))
        run_cmd(["docker", "push", ref])
    log_success("images built and pushed to " + devenv.LOCAL_REGISTRY)


def apply_platform_base() -> None:
    devenv.kubectl(["apply", "-f", f"{PLATFORM_DIR}/nats.yml"])
    devenv.kubectl(["apply", "--server-side", "-f", f"{PLATFORM_DIR}/keda.yml"])
    devenv.kubectl(["apply", "-f", "config/crd/bases/tmi.dev_tmicomponents.yaml"])


def deliver_config() -> None:
    content = (get_project_root() / CONFIG_FILE).read_text()
    manifest = devenv.render_configmap_yaml(
        name=CONFIGMAP_NAME, namespace=NS, file_key="config.yml", content=content,
    )
    devenv.kubectl(["apply", "-f", "-"], input_text=manifest)
    log_success(f"config delivered as ConfigMap/{CONFIGMAP_NAME}")


def ensure_namespace() -> None:
    # Idempotent namespace create via apply.
    devenv.kubectl(["apply", "-f", "-"], input_text=f"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: {NS}\n")


def create_embedding_secret() -> None:
    import os
    key = os.environ.get("TMI_EMBEDDING_API_KEY", "sk-e2e-placeholder")
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-embedding",
         "-n", NS, f"--from-literal=api-key={key}", "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    devenv.kubectl(["apply", "-f", "-"], input_text=rendered)


def apply_overlay(no_workers: bool) -> None:
    if no_workers:
        # Apply only the non-component resources by rendering and filtering is complex;
        # instead apply the individual core manifests and skip the kustomize component refs.
        for f in ("controller.yml", "redis.yml", "server.yml"):
            devenv.kubectl(["apply", "-f", f"{DEV_DIR}/{f}"])
    else:
        devenv.kubectl(["apply", "-k", DEV_DIR])


def wait_and_forward() -> None:
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-component-controller", "--timeout=120s"])
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", "--timeout=180s"])
    start_port_forward()
    log_success("dev environment ready at http://localhost:8080")


def start_port_forward() -> None:
    stop_port_forward()
    import subprocess
    proc = subprocess.Popen(
        ["kubectl", "-n", NS, "port-forward", "svc/tmi-server", "8080:8080"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    Path(PORT_FORWARD_PID).write_text(str(proc.pid))


def stop_port_forward() -> None:
    p = Path(PORT_FORWARD_PID)
    if p.exists():
        try:
            import os, signal
            os.kill(int(p.read_text().strip()), signal.SIGTERM)
        except (ProcessLookupError, ValueError):
            pass
        p.unlink(missing_ok=True)


def do_start(args) -> None:
    preflight()
    guard_context(args.yes)
    build_and_push()
    ensure_namespace()
    apply_platform_base()
    deliver_config()
    create_embedding_secret()
    apply_overlay(args.no_workers)
    wait_and_forward()


def do_restart(args) -> None:
    preflight()
    guard_context(args.yes)
    build_and_push()
    deliver_config()
    devenv.kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-server"])
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", "--timeout=180s"])
    start_port_forward()
    log_success("server restarted; http://localhost:8080")


def do_stop(args) -> None:
    stop_port_forward()
    devenv.kubectl(["delete", "-k", DEV_DIR, "--ignore-not-found"], check=False)
    devenv.kubectl(["-n", NS, "delete", "configmap", CONFIGMAP_NAME, "--ignore-not-found"], check=False)
    devenv.kubectl(["-n", NS, "delete", "secret", "tmi-embedding", "--ignore-not-found"], check=False)
    run_cmd(["docker", "stop", devenv.REGISTRY_CONTAINER], check=False)
    log_success("dev environment torn down (cluster left intact)")


def main() -> None:
    args = parse_args()
    apply_verbosity(args)
    if args.stop:
        do_stop(args)
    elif args.restart:
        do_restart(args)
    else:
        do_start(args)


if __name__ == "__main__":
    main()
```

Reconcile kwargs with `tmi_common.run_cmd`: this rewrite assumes `run_cmd` supports `check=`, `cwd=`, `capture=` (returning `CompletedProcess` with `.stdout`/`.returncode`), and `input_text=`. Before running, open `scripts/lib/tmi_common.py:175` and align: rename kwargs to the real ones, or extend `run_cmd` minimally to support them. Keep changes to `run_cmd` backward-compatible (the other `manage-*.py` scripts call it).

- [ ] **Step 2: Rewire the make targets**

In `Makefile`, replace the body of `start-dev` and `restart-dev`, and add `stop-dev`:

```makefile
start-dev:  ## Deploy the TMI dev environment into the current kubectl context
	@uv run scripts/start-dev.py

restart-dev:  ## Rebuild+push the server, redeliver config, roll the server pod
	@uv run scripts/start-dev.py --restart

stop-dev:  ## Tear down everything start-dev deployed (leaves a dedicated cluster intact)
	@uv run scripts/start-dev.py --stop
```

Add `stop-dev` to the appropriate `.PHONY` line. Remove the now-dead `start-dev-oci`/process-mode wiring only if it is unused elsewhere; otherwise leave it (Oracle is Plan 2).

- [ ] **Step 3: End-to-end run against the kind dev cluster**

Pre-req: `make dev-cluster-up` (Task 8) has run; a local Postgres is reachable and `config-development.yml`'s `database.postgres.host` is set to a pod-reachable value (`host.docker.internal` on Docker Desktop). Start the host Postgres if needed: `make start-database`.

Run:
```bash
make start-dev
sleep 2
curl -s http://localhost:8080/ | head -c 200; echo
kubectl -n tmi-platform get pods
```
Expected: the root endpoint returns version JSON; `tmi-server` and `tmi-component-controller` pods are `Running`; the two worker Deployments exist at 0 replicas (KEDA scale-to-zero).

- [ ] **Step 4: Verify config swap + restart**

Run:
```bash
make restart-dev
curl -s http://localhost:8080/ | head -c 80; echo
```
Expected: server rolls and serves again. (Editing `config-development.yml` then `make restart-dev` re-delivers the ConfigMap — verify by changing a non-DB value like log level and confirming the new pod picked it up via `kubectl -n tmi-platform get cm tmi-server-config -o jsonpath='{.metadata.annotations.tmi\.dev/config-hash}'` changing.)

- [ ] **Step 5: Verify teardown**

Run:
```bash
make stop-dev
kubectl -n tmi-platform get deploy 2>/dev/null | grep -c tmi-server   # expect 0
```
Expected: dev workloads removed; the kind cluster still exists (`kind get clusters` lists `tmi-dev`).

- [ ] **Step 6: Commit**

```bash
git add scripts/start-dev.py scripts/lib/tmi_common.py Makefile
git commit -m "feat(dev): rewrite start-dev to deploy into the current kubectl context

start/restart/stop the cluster-based dev environment: build+push images to the
local registry, deliver config-development.yml as a ConfigMap, apply the dev
overlay, and manage a kubectl port-forward to localhost:8080. Refs #442, #347"
```

---

## Task 10: Acceptance — OAuth + async extraction round-trip

**Files:** none (verification task)

- [ ] **Step 1: OAuth login end-to-end**

With `make start-dev` up and `make start-oauth-stub` running:
```bash
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid":"alice"}'
# wait for completion, then:
curl -s "http://localhost:8079/creds?userid=alice" | python3 -c 'import sys,json;print(bool(json.load(sys.stdin).get("access_token")))'
```
Expected: prints `True` — a JWT was issued by the in-cluster server.

- [ ] **Step 2: Async extraction round-trip through tmi-extractor**

Trigger an extraction via the documented content path (a document submission that enqueues `jobs.extract.*`). Then:
```bash
kubectl -n tmi-platform get pods -l tmi.dev/component=tmi-extractor
```
Expected: KEDA scales `tmi-extractor` from 0→≥1 while the job is in flight, the job result updates the document `access_status`, and the pod scales back to 0 afterward. (chunk-embed may not complete due to #443 — that is expected and out of scope.)

- [ ] **Step 3: Confirm scope boundary intact**

Run: `make test-integration` and `make test-e2e-acceptance`
Expected: both still pass (dev migration did not disturb test provisioning).

- [ ] **Step 4: Commit any doc/fixup deltas surfaced during acceptance**

```bash
git add -A && git commit -m "test(dev): acceptance fixups for the cluster-based dev environment" || echo "nothing to commit"
```

---

## Self-Review Notes (for the implementer)

- **`run_cmd` kwargs** are the single most likely source of friction: Tasks 1, 8, 9 assume `check`, `cwd`, `capture`, `input_text`. Reconcile with the real signature at `scripts/lib/tmi_common.py:175` in Task 1 and keep it backward-compatible for the other `manage-*.py` callers.
- **`--no-workers`** uses per-file apply to skip the component CRs; everything else uses `kubectl apply -k`. Keep both paths applying the same controller/redis/server manifests.
- **k3s path**: this plan stands up the kind path end-to-end. For k3s, the same `make start-dev` works once `kubectl` targets the k3s context and `/etc/rancher/k3s/registries.yaml` mirrors `localhost:5000`; that one-time host setup is documented in the wiki (separate docs task), not automated here.
- **Oracle (`DB=oracle`) and Tilt are intentionally absent** — Plans 2 and 3.
```
