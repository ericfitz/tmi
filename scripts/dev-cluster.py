# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Create/delete a local kind cluster wired to the local dev registry."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    check_tool,
    container_exists,
    container_is_running,
    log_info,
    log_success,
    run_cmd,
)
import devenv  # noqa: E402

CLUSTER_NAME = "tmi-dev"
_PROJECT_ROOT = Path(__file__).resolve().parent.parent
KIND_CONFIG = str(_PROJECT_ROOT / "deployments/k8s/dev/kind-cluster.yml")

REGISTRY_IMAGE = "registry:2"
REGISTRY_PORT = 5000


def ensure_registry() -> None:
    """Start the local registry container if it is not already running."""
    name = devenv.REGISTRY_CONTAINER
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
        ["docker", "network", "connect", "kind", devenv.REGISTRY_CONTAINER],
        check=False,
    )


def up() -> None:
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
    if container_is_running(devenv.REGISTRY_CONTAINER):
        targets.append(devenv.REGISTRY_CONTAINER)
    if not targets:
        log_info("No running dev-cluster containers to stop")
        return
    log_info(f"Stopping dev-cluster containers: {', '.join(targets)}")
    run_cmd(["docker", "stop", *targets], check=False)
    log_success("Dev-cluster containers stopped (cluster state preserved)")


def down() -> None:
    check_tool("kind")
    run_cmd(["kind", "delete", "cluster", "--name", CLUSTER_NAME])
    log_success(f"kind cluster '{CLUSTER_NAME}' deleted")


def main() -> None:
    if len(sys.argv) < 2 or sys.argv[1] not in ("up", "down", "stop"):
        print(f"Usage: {sys.argv[0]} up|down|stop", file=sys.stderr)
        sys.exit(1)
    action = sys.argv[1]
    {"up": up, "down": down, "stop": stop}[action]()


if __name__ == "__main__":
    main()
