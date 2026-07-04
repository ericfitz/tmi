# Docker Desktop Kubernetes Dev Environment

This directory contains the kustomize overlay for running TMI's local dev
environment on **Docker Desktop's built-in Kubernetes cluster**.  It is the
default cluster target — `make dev-up` (no `CLUSTER=` override) uses this
overlay.

---

## One-Time Setup

### 1. Enable Kubernetes in Docker Desktop

1. Open Docker Desktop → **Settings** (gear icon) → **Kubernetes**.
2. Check **Enable Kubernetes**.
3. Leave the provisioner set to **kind** (the default).  Docker Desktop
   provisions the cluster using kind internally; the resulting context is
   named `docker-desktop`.
4. Click **Apply & Restart** and wait for the Kubernetes status indicator to
   turn green (may take a minute or two).

### 2. Verify the cluster is ready

```bash
kubectl --context docker-desktop get nodes
```

Expected output shows a single node named `desktop-control-plane` in `Ready`
state:

```
NAME                     STATUS   ROLES           AGE   VERSION
desktop-control-plane    Ready    control-plane   …     v1.X.Y
```

If you see a different node name or a non-`Ready` status, go back to Docker
Desktop and confirm Kubernetes is fully started.

---

## How Images Are Loaded

There is **no local registry and no image mirror** in this topology.  Images
are imported directly into the cluster node's containerd image store via:

```bash
docker save <image> | docker exec -i <node-container> ctr -n k8s.io images import -
```

The `make dev-up` orchestration (`scripts/devenv.py` + `scripts/lib/cluster.py`)
runs this import automatically after building each image.  You do not need to
push to any registry.

---

## Endpoints After `make dev-up`

| Service     | Address              | How                            |
|-------------|----------------------|--------------------------------|
| TMI server  | `http://localhost:8080` | `kubectl port-forward` managed by devenv |
| Redis       | `localhost:6379`     | `kubectl port-forward` managed by devenv |

Run `make dev-status` to see live port-forward and pod status.

---

## Teardown

```bash
make dev-down      # stop pods + port-forwards; keep DB data
make dev-nuke      # destroy everything including DB data
```

---

## Why e2e-platform Tests Stay on Standalone kind

The automated end-to-end platform tests (`e2e-platform/`) are **not** run
against Docker Desktop Kubernetes.  They require:

- A **swappable CNI** — specifically Calico — to test and enforce
  `NetworkPolicy` rules.  Docker Desktop ships with its own fixed CNI that
  does not support runtime replacement.
- **Ephemeral clusters** — each test run spins up a fresh kind cluster,
  applies Calico, runs the suite, then destroys the cluster.  Docker Desktop
  does not support programmatic cluster creation and deletion.

For day-to-day feature development the Docker Desktop topology is faster and
requires no extra tooling.  For changes that touch networking, policy
enforcement, or multi-cluster behavior, use `CLUSTER=kind` and a standalone
kind installation.
