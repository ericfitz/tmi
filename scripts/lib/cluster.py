"""kind cluster + local dev-registry lifecycle.

Pure helpers (local_image_ref, is_local_kube_context) are unit-tested in
scripts/lib/tests/test_cluster.py. Shell wrappers delegate to tmi_common.run_cmd
and are exercised against a live cluster by scripts/devenv.py.
"""
from __future__ import annotations

from pathlib import Path

from tmi_common import (
    check_tool, container_exists, container_is_running,
    log_info, log_success, run_cmd,
)

CLUSTER_NAME = "tmi-dev"
REGISTRY_CONTAINER = "tmi-dev-registry"
REGISTRY_IMAGE = "registry:2"
REGISTRY_PORT = 5000
LOCAL_REGISTRY = "localhost:5000"
_PROJECT_ROOT = Path(__file__).resolve().parents[2]
KIND_CONFIG = str(_PROJECT_ROOT / "deployments/k8s/dev/kind-cluster.yml")

# Remote k3s dev target (CLUSTER=k3s). We do not own this cluster: we select
# its context but never create/delete it. Images go to an in-cluster registry
# exposed at rp2:30500 (NodePort 30500).
K3S_CONTEXT = "k3s-rp"
K3S_REGISTRY = "rp2:30500"

# Docker Desktop dev target (CLUSTER=docker-desktop, the default). DD owns the
# cluster lifecycle (kind provisioner); we only select its context and never
# create/delete it. Images are imported straight into the node's containerd
# (no registry): docker save <img> | docker exec -i DD_NODE ctr -n k8s.io images import -.
DD_CONTEXT = "docker-desktop"
DD_NODE = "desktop-control-plane"


def registry_for(cluster: str = "kind") -> str | None:
    """Return the dev image-registry hostname, or None for docker-desktop (no
    registry — images are imported into the node's containerd)."""
    if cluster == "docker-desktop":
        return None
    return K3S_REGISTRY if cluster == "k3s" else LOCAL_REGISTRY


def expected_context(cluster: str = "kind") -> str:
    """Return the kube-context that must be active for the given cluster target."""
    if cluster == "docker-desktop":
        return DD_CONTEXT
    return K3S_CONTEXT if cluster == "k3s" else f"kind-{CLUSTER_NAME}"

# Contexts we consider safe to deploy a dev environment into without --yes.
_LOCAL_CONTEXT_PREFIXES = ("kind-", "k3d-")
_LOCAL_CONTEXT_EXACT = {"k3s", "default", "rancher-desktop", "docker-desktop", "minikube"}


def local_image_ref(name: str, tag: str = "dev", *, cluster: str = "kind") -> str:
    """Return the dev image reference for the cluster: registry-qualified for
    kind/k3s, or a bare name:tag for docker-desktop (imported, not pulled)."""
    reg = registry_for(cluster)
    return f"{name}:{tag}" if reg is None else f"{reg}/{name}:{tag}"


def is_local_kube_context(name: str) -> bool:
    """True if the kubectl context name looks like a local dev cluster."""
    if not name:
        return False
    if name in _LOCAL_CONTEXT_EXACT:
        return True
    return any(name.startswith(p) for p in _LOCAL_CONTEXT_PREFIXES)


def is_registry_running() -> bool:
    """True if the local dev registry container is currently running."""
    return container_is_running(REGISTRY_CONTAINER)


def ensure_registry() -> None:
    """Start the local registry container if it is not already running."""
    name = REGISTRY_CONTAINER
    if container_is_running(name):
        log_info(f"Registry container already running: {name}")
        return
    if container_exists(name):
        log_info(f"Starting existing registry container: {name}")
        run_cmd(["docker", "start", name])
        log_success(f"Registry container started: {name}")
        return
    log_info(f"Creating registry container: {name}")
    run_cmd([
        "docker", "run", "-d",
        "--restart=always",
        "-p", f"127.0.0.1:{REGISTRY_PORT}:{REGISTRY_PORT}",
        "--name", name,
        REGISTRY_IMAGE,
    ])
    log_success(f"Registry container created and started: {name}")


def cluster_exists() -> bool:
    """Return True if the kind cluster named CLUSTER_NAME already exists."""
    result = run_cmd(
        ["kind", "get", "clusters"],
        capture=True,
        check=False,
    )
    return CLUSTER_NAME in result.stdout.splitlines()


def _kind_node_containers(*, running_only: bool) -> list[str]:
    """Return the kind node container names for this cluster (control-plane + workers).

    Discovered by kind's own container label rather than by guessing node names,
    so it stays correct if the cluster topology in kind-cluster.yml changes.
    """
    cmd = ["docker", "ps"]
    if not running_only:
        cmd.append("-a")
    cmd += [
        "--filter", f"label=io.x-k8s.kind.cluster={CLUSTER_NAME}",
        "--format", "{{.Names}}",
    ]
    result = run_cmd(cmd, capture=True, check=False)
    return result.stdout.split()


def start_stopped_nodes() -> None:
    """Start any kind node containers that exist but are stopped (idempotent).

    `kind create cluster` is a no-op once the cluster exists, but it will not
    restart node containers that were halted by `devenv.py cluster down` (or a host
    reboot). Bring them back so `up` reliably yields a running cluster.
    """
    running = set(_kind_node_containers(running_only=True))
    stopped = [n for n in _kind_node_containers(running_only=False) if n not in running]
    if stopped:
        log_info(f"Starting stopped kind node container(s): {', '.join(stopped)}")
        run_cmd(["docker", "start", *stopped], check=False)


def connect_registry_to_kind() -> None:
    """Attach the registry container to the kind Docker network (idempotent)."""
    run_cmd(
        ["docker", "network", "connect", "kind", REGISTRY_CONTAINER],
        check=False,
    )


def up(cluster: str = "kind") -> None:
    """Bring up the dev cluster: create kind + local registry, or (k3s) select the
    remote context without creating anything (the in-cluster registry and workloads
    are applied by deploy)."""
    if cluster == "k3s":
        check_tool("kubectl")
        log_info(f"Using existing k3s context '{K3S_CONTEXT}' (no cluster create)")
        run_cmd(["kubectl", "config", "use-context", K3S_CONTEXT])
        log_success(f"kube context set to '{K3S_CONTEXT}'")
        return

    if cluster == "docker-desktop":
        check_tool("kubectl")
        log_info(f"Using Docker Desktop Kubernetes context '{DD_CONTEXT}' (no cluster create)")
        run_cmd(["kubectl", "config", "use-context", DD_CONTEXT])
        log_success(f"kube context set to '{DD_CONTEXT}'")
        return

    for tool in ("docker", "kind", "kubectl"):
        check_tool(tool)

    ensure_registry()

    if cluster_exists():
        log_info(f"kind cluster '{CLUSTER_NAME}' already exists — skipping create")
        start_stopped_nodes()
    else:
        log_info(f"Creating kind cluster '{CLUSTER_NAME}' with config: {KIND_CONFIG}")
        run_cmd(["kind", "create", "cluster", "--config", KIND_CONFIG])
        log_success(f"kind cluster '{CLUSTER_NAME}' created")

    # `kind create` sets the kubectl context, but the skip-create path does not,
    # so an active context from another cluster (e.g. k3s-rp) would linger and fail
    # deploy's context guard. Always select the kind context — idempotent.
    run_cmd(["kubectl", "config", "use-context", f"kind-{CLUSTER_NAME}"])

    connect_registry_to_kind()
    log_success(f"kind cluster '{CLUSTER_NAME}' ready; context kind-{CLUSTER_NAME}")


def down(cluster: str = "kind") -> None:
    """Delete the kind cluster entirely; no-op for the remote k3s cluster (not ours
    to delete — namespace teardown is handled by deploy)."""
    if cluster == "k3s":
        log_info("cluster down is a no-op for k3s (remote cluster is not ours to delete)")
        return
    if cluster == "docker-desktop":
        log_info("cluster down is a no-op for docker-desktop (Docker Desktop owns the cluster)")
        return
    check_tool("kind")
    run_cmd(["kind", "delete", "cluster", "--name", CLUSTER_NAME])
    log_success(f"kind cluster '{CLUSTER_NAME}' deleted")
