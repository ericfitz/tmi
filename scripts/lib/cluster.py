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
    restart node containers that were halted by `dev-cluster.py stop` (or a host
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


def up() -> None:
    """Bring up the kind cluster and local dev registry."""
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

    connect_registry_to_kind()
    log_success(f"kind cluster '{CLUSTER_NAME}' ready; context kind-{CLUSTER_NAME}")


def stop() -> None:
    """Stop the kind node containers and the dev registry without deleting them.

    Unlike `down` (which `kind delete cluster`s and discards all cluster state),
    `stop` just halts the Docker containers so the whole dev footprint comes to
    rest while staying revivable via `dev-cluster.py up` / `make start-dev`.
    Used by `make stop-all`.
    """
    check_tool("docker")
    targets = _kind_node_containers(running_only=True)
    if container_is_running(REGISTRY_CONTAINER):
        targets.append(REGISTRY_CONTAINER)
    if not targets:
        log_info("No running dev-cluster containers to stop")
        return
    log_info(f"Stopping dev-cluster containers: {', '.join(targets)}")
    run_cmd(["docker", "stop", *targets], check=False)
    log_success("Dev-cluster containers stopped (cluster state preserved)")


def down() -> None:
    """Delete the kind cluster entirely."""
    check_tool("kind")
    run_cmd(["kind", "delete", "cluster", "--name", CLUSTER_NAME])
    log_success(f"kind cluster '{CLUSTER_NAME}' deleted")
