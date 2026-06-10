# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Deploy the TMI dev environment into the current kubectl context.

Cluster-agnostic: deploys into whatever cluster kubectl currently targets.
The database is external and defined entirely by config-development.yml; the
server pod dials it directly.  Redis, NATS, KEDA, the controller, and the two
worker components run in-cluster.  Images are pushed to a local registry that
the cluster pulls from.  Actions: (default) start | --restart | --stop
"""

import argparse
import os
import shutil
import signal
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args, apply_verbosity, check_tool, container_exists,
    container_is_running, get_project_root, log_error, log_info, log_success,
    run_cmd,
)
import devenv  # noqa: E402

NS = devenv.PLATFORM_NAMESPACE
DEV_DIR = "deployments/k8s/dev"
PLATFORM_DIR = "deployments/k8s/platform"
CONFIG_FILE = "config-development.yml"
CONFIGMAP_NAME = "tmi-server-config"
PORT_FORWARD_PID = "/tmp/tmi-dev-portforward.pid"

# (name, dockerfile, build_args)
# All four Dockerfiles have COPY .docker-deps/tmi-client/, so all need staging.
IMAGE_BUILDS = [
    ("tmi-server",               "Dockerfile.server",    {"BUILD_TAGS": "dev"}),
    ("tmi-component-controller", "Dockerfile.controller", {}),
    ("tmi-extractor",            "Dockerfile.extractor",  {}),
    ("tmi-chunk-embed",          "Dockerfile.chunkembed", {}),
]


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Deploy the TMI dev environment.")
    add_verbosity_args(p)
    g = p.add_mutually_exclusive_group()
    g.add_argument("--restart", action="store_true")
    g.add_argument("--stop", action="store_true")
    p.add_argument("--yes", action="store_true", help="Skip the local-context safety check")
    p.add_argument("--no-workers", action="store_true", help="Do not apply the component CRs")
    return p.parse_args()


def guard_context(skip: bool) -> str:
    ctx = devenv.current_kube_context()
    log_info(f"kubectl context: {ctx or '(none)'}  namespace: {NS}")
    if not ctx:
        log_error("No kubectl context set. Run 'make dev-cluster-up' or select your k3s context.")
        sys.exit(1)
    if not skip and not devenv.is_local_kube_context(ctx):
        log_error(f"Context '{ctx}' is not a recognized local dev cluster. Re-run with --yes to override.")
        sys.exit(1)
    return ctx


def preflight() -> None:
    for tool in ("docker", "kubectl"):
        check_tool(tool)
    if run_cmd(["kubectl", "cluster-info"], check=False).returncode != 0:
        log_error("No reachable cluster. Run 'make dev-cluster-up' (kind) or start your k3s cluster.")
        sys.exit(1)


# ---------------------------------------------------------------------------
# Registry helpers
# ---------------------------------------------------------------------------

_REGISTRY_IMAGE = "registry:2"
_REGISTRY_PORT = 5000


def ensure_registry() -> None:
    """Ensure the local registry container is running.

    Starts the existing container if it was stopped, or creates it fresh if it
    doesn't exist yet.  Also (re)connects it to the kind network so the cluster
    can pull from it.
    """
    name = devenv.REGISTRY_CONTAINER
    if container_is_running(name):
        log_info(f"Registry container already running: {name}")
    elif container_exists(name):
        log_info(f"Starting existing registry container: {name}")
        run_cmd(["docker", "start", name])
        log_success(f"Registry container started: {name}")
    else:
        log_info(f"Creating registry container: {name}")
        run_cmd([
            "docker", "run", "-d",
            "--restart=always",
            "-p", f"127.0.0.1:{_REGISTRY_PORT}:{_REGISTRY_PORT}",
            "--name", name,
            _REGISTRY_IMAGE,
        ])
        log_success(f"Registry container created: {name}")

    # (Re)connect the registry to the kind network so cluster nodes can pull from it.
    run_cmd(["docker", "network", "connect", "kind", name], check=False)


# ---------------------------------------------------------------------------
# tmi-client staging
# ---------------------------------------------------------------------------

def _resolve_client_path(project_root: Path) -> str:
    """Resolve the tmi-clients root directory.

    Checks (in order):
    1. TMI_CLIENT_PATH environment variable
    2. .local-projects.json entry for tmi-clients
    3. Default sibling directory ../tmi-clients
    """
    env_path = os.environ.get("TMI_CLIENT_PATH", "")
    if env_path:
        return env_path

    import json
    local_projects_file = project_root / ".local-projects.json"
    if local_projects_file.exists():
        try:
            data = json.loads(local_projects_file.read_text())
            for proj in data.get("projects", []):
                if proj.get("name") == "tmi-clients":
                    return proj["path"]
        except (json.JSONDecodeError, KeyError):
            pass

    return str(project_root.parent / "tmi-clients")


def _resolve_client_version(project_root: Path) -> str:
    """Derive the tmi-clients version directory from go.mod's replace directive."""
    env_version = os.environ.get("TMI_CLIENT_VERSION", "")
    if env_version:
        return env_version

    go_mod = (project_root / "go.mod").read_text()
    import re
    # e.g. "=> ../tmi-clients/go-client-generated/v1_4_0"
    m = re.search(r"tmi-clients/go-client-generated/(\S+)", go_mod)
    if m:
        return m.group(1)

    log_error(
        "Cannot derive tmi-client version from go.mod replace directive. "
        "Set TMI_CLIENT_VERSION (e.g. 'v1_4_0')."
    )
    sys.exit(1)


def stage_tmi_client() -> bool:
    """Copy the tmi-client Go module into .docker-deps/tmi-client/ if not already present.

    If .docker-deps/tmi-client/ already exists the developer is assumed to have
    intentionally staged it; this function leaves it untouched and returns False
    so the caller knows NOT to clean it up later.

    Returns True if this call created .docker-deps/tmi-client/ (caller must
    call unstage_tmi_client() to clean up), False if it was pre-existing (leave
    it alone).
    """
    project_root = get_project_root()
    dest = project_root / ".docker-deps" / "tmi-client"

    if dest.exists():
        log_info(f"Pre-existing .docker-deps/tmi-client/ found — using as-is: {dest}")
        return False

    client_root = _resolve_client_path(project_root)
    client_version = _resolve_client_version(project_root)

    src = Path(client_root) / "go-client-generated" / client_version
    if not src.is_dir():
        log_error(
            f"TMI client source not found: {src}\n"
            f"  TMI_CLIENT_PATH={client_root}\n"
            f"  TMI_CLIENT_VERSION={client_version}\n"
            "Ensure the tmi-clients repo is checked out and the version directory exists."
        )
        sys.exit(1)

    # Create only the .docker-deps/ parent if needed; never touch other contents.
    dest.parent.mkdir(parents=True, exist_ok=True)

    log_info(f"Staging tmi-client: {src} -> {dest}")
    shutil.copytree(src, dest)
    return True


def unstage_tmi_client(created: bool) -> None:
    """Remove .docker-deps/tmi-client/ — but only if this run created it.

    Never removes the parent .docker-deps/ directory or any other content
    inside it; that would destroy a developer's intentionally staged files.
    """
    if not created:
        return
    project_root = get_project_root()
    dest = project_root / ".docker-deps" / "tmi-client"
    if dest.exists():
        shutil.rmtree(dest)
        log_info("Cleaned up staged tmi-client (.docker-deps/tmi-client/)")


# ---------------------------------------------------------------------------
# Image build + push
# ---------------------------------------------------------------------------

def build_and_push() -> None:
    """Build all images and push them to the local registry.

    All four Dockerfiles require the tmi-client staged in .docker-deps/.
    Stage once before the first build and clean up in a try/finally block
    so the staging dir is always removed even if a build fails — but only
    when this run created it (pre-existing dirs are left untouched).
    """
    project_root = get_project_root()

    # All four images need the client — stage once (no-op if pre-existing).
    created = stage_tmi_client()
    try:
        for name, dockerfile, build_args_map in IMAGE_BUILDS:
            ref = devenv.local_image_ref(name)
            log_info(f"Building {name}  ({dockerfile}) -> {ref}")

            cmd = ["docker", "build", "-f", str(project_root / dockerfile)]
            for k, v in build_args_map.items():
                cmd += ["--build-arg", f"{k}={v}"]
            cmd += ["-t", ref, str(project_root)]

            run_cmd(cmd)

            log_info(f"Pushing {ref}")
            run_cmd(["docker", "push", ref])

        log_success("All images built and pushed to local registry")
    finally:
        unstage_tmi_client(created)


# ---------------------------------------------------------------------------
# Kubernetes helpers
# ---------------------------------------------------------------------------

def apply_platform_base() -> None:
    project_root = get_project_root()
    devenv.kubectl(["apply", "-f", str(project_root / PLATFORM_DIR / "nats.yml")])
    devenv.kubectl(["apply", "--server-side", "-f", str(project_root / PLATFORM_DIR / "keda.yml")])
    devenv.kubectl(["apply", "-f", str(project_root / "config/crd/bases/tmi.dev_tmicomponents.yaml")])


def ensure_namespace() -> None:
    devenv.kubectl(
        ["apply", "-f", "-"],
        input_text=f"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: {NS}\n",
    )


def deliver_config() -> None:
    content = (get_project_root() / CONFIG_FILE).read_text()
    manifest = devenv.render_configmap_yaml(
        name=CONFIGMAP_NAME, namespace=NS, file_key="config.yml", content=content,
    )
    devenv.kubectl(["apply", "-f", "-"], input_text=manifest)
    log_success(f"Config delivered as ConfigMap/{CONFIGMAP_NAME}")


def create_embedding_secret() -> None:
    key = os.environ.get("TMI_EMBEDDING_API_KEY", "sk-e2e-placeholder")
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-embedding", "-n", NS,
         f"--from-literal=api-key={key}", "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    devenv.kubectl(["apply", "-f", "-"], input_text=rendered)


def apply_overlay(no_workers: bool) -> None:
    """Apply the dev overlay.

    When --no-workers: apply the three core manifests individually to avoid
    kustomize referencing the component CR files (which include TMIComponent
    resources from ../platform/components/).

    Otherwise: render the full kustomize overlay with --load-restrictor
    LoadRestrictionsNone (needed because the overlay references files outside
    its own directory tree, i.e. ../platform/components/).
    """
    project_root = get_project_root()
    if no_workers:
        for f in ("controller.yml", "redis.yml", "server.yml"):
            devenv.kubectl(["apply", "-f", str(project_root / DEV_DIR / f)])
    else:
        rendered = run_cmd(
            ["kubectl", "kustomize", "--load-restrictor", "LoadRestrictionsNone",
             str(project_root / DEV_DIR)],
            capture=True,
        ).stdout
        devenv.kubectl(["apply", "-f", "-"], input_text=rendered)


def wait_and_forward() -> None:
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-component-controller", "--timeout=120s"])
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", "--timeout=180s"])
    start_port_forward()
    log_success("Dev environment ready at http://localhost:8080")


def start_port_forward() -> None:
    stop_port_forward()
    proc = subprocess.Popen(
        ["kubectl", "-n", NS, "port-forward", "svc/tmi-server", "8080:8080"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    Path(PORT_FORWARD_PID).write_text(str(proc.pid))
    log_info(f"Port-forward started (PID {proc.pid}): localhost:8080 -> svc/tmi-server:8080")


def stop_port_forward() -> None:
    p = Path(PORT_FORWARD_PID)
    if p.exists():
        try:
            pid = int(p.read_text().strip())
            os.kill(pid, signal.SIGTERM)
            log_info(f"Stopped port-forward (PID {pid})")
        except (ProcessLookupError, ValueError):
            pass
        p.unlink(missing_ok=True)


# ---------------------------------------------------------------------------
# Actions
# ---------------------------------------------------------------------------

def do_start(args) -> None:
    preflight()
    guard_context(args.yes)
    ensure_registry()
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
    ensure_registry()
    build_and_push()
    deliver_config()
    apply_overlay(args.no_workers)
    devenv.kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-server"])
    devenv.kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", "--timeout=180s"])
    start_port_forward()
    log_success("Server restarted; http://localhost:8080")


def do_stop(args) -> None:
    """Tear down everything that do_start deployed.

    Removes (tolerating absence for all):
    - port-forward process
    - server Deployment + Service
    - redis Deployment + Service
    - controller Deployment + RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)
    - TMIComponent CRs (tmi-extractor, tmi-chunk-embed)
    - ConfigMap tmi-server-config
    - Secret tmi-embedding
    - local registry container (docker stop)
    """
    stop_port_forward()

    # TMIComponent CRs (worker component definitions)
    devenv.kubectl(
        ["-n", NS, "delete", "tmicomponents.tmi.dev", "tmi-extractor", "tmi-chunk-embed",
         "--ignore-not-found"],
        check=False,
    )

    # Server and Redis Deployments + Services
    devenv.kubectl(
        ["-n", NS, "delete", "deploy,svc", "tmi-server", "redis", "--ignore-not-found"],
        check=False,
    )

    # Controller Deployment
    devenv.kubectl(
        ["-n", NS, "delete", "deploy", "tmi-component-controller", "--ignore-not-found"],
        check=False,
    )

    # Controller RBAC
    devenv.kubectl(
        ["delete", "clusterrolebinding,clusterrole", "tmi-controller", "--ignore-not-found"],
        check=False,
    )
    devenv.kubectl(
        ["-n", NS, "delete", "serviceaccount", "tmi-controller", "--ignore-not-found"],
        check=False,
    )

    # ConfigMap and Secret
    devenv.kubectl(
        ["-n", NS, "delete", "configmap", CONFIGMAP_NAME, "--ignore-not-found"],
        check=False,
    )
    devenv.kubectl(
        ["-n", NS, "delete", "secret", "tmi-embedding", "--ignore-not-found"],
        check=False,
    )

    # Stop the local registry container
    run_cmd(["docker", "stop", devenv.REGISTRY_CONTAINER], check=False)

    log_success("Dev environment torn down (cluster left intact)")


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
