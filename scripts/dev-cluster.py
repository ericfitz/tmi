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
    else:
        log_info(f"Creating kind cluster '{CLUSTER_NAME}' with config: {KIND_CONFIG}")
        run_cmd(["kind", "create", "cluster", "--config", KIND_CONFIG])
        log_success(f"kind cluster '{CLUSTER_NAME}' created")

    connect_registry_to_kind()
    log_success(f"kind cluster '{CLUSTER_NAME}' ready; context kind-{CLUSTER_NAME}")


def down() -> None:
    check_tool("kind")
    run_cmd(["kind", "delete", "cluster", "--name", CLUSTER_NAME])
    log_success(f"kind cluster '{CLUSTER_NAME}' deleted")


def main() -> None:
    if len(sys.argv) < 2 or sys.argv[1] not in ("up", "down"):
        print(f"Usage: {sys.argv[0]} up|down", file=sys.stderr)
        sys.exit(1)
    action = sys.argv[1]
    {"up": up, "down": down}[action]()


if __name__ == "__main__":
    main()
