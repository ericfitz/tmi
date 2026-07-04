"""TMI dev image build/push + in-cluster deploy + teardown.

Pure helpers are unit-tested in scripts/lib/tests/test_deploy.py; orchestration
functions (start/restart/teardown) are exercised against a live cluster by
scripts/devenv.py. Depends on lib/cluster.py for registry + image refs.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path

import cluster
from tmi_common import (
    check_tool, container_exists, container_is_running, get_project_root,
    log_error, log_info, log_success, run_cmd,
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

NS = "tmi-platform"
DEV_DIR = "deployments/k8s/dev"
PLATFORM_DIR = "deployments/k8s/platform"
CONFIG_FILE = "config-development.yml"
CONFIGMAP_NAME = "tmi-server-config"

# The server is reached on the host at localhost:HOST_PORT. The kind cluster
# publishes the tmi-server Service's NodePort (NODE_PORT) directly on the host
# via extraPortMappings (deployments/k8s/dev/kind-cluster.yml), so there is NO
# `kubectl port-forward` for the server. The old userspace port-forward proxy
# collapsed under high connection rates (CATS fuzzing -> 99% CONNECTION_ERROR,
# issue #463); Docker's published port + kube-proxy DNAT has no such bottleneck.
# KEEP NODE_PORT IN SYNC with server.yml/server-oracle.yml (.spec.ports[].nodePort)
# and kind-cluster.yml (extraPortMappings[].containerPort).
HOST_PORT = 8080
NODE_PORT = 30080
SERVER_URL = f"http://localhost:{HOST_PORT}"

# Legacy server port-forward pidfile. The server no longer uses a port-forward
# (it is reached via the NodePort above), but stop_port_forward() still cleans
# up this file so an upgrade from a port-forward-based checkout kills any stale
# forwarder left running on :8080 that would otherwise shadow the NodePort.
PORT_FORWARD_PID = "/tmp/tmi-dev-portforward.pid"
# Redis is an in-cluster ClusterIP service; the server reaches it as redis:6379.
# Integration tests that seed Redis directly (e.g. the step-up legacy refresh
# token round-trip) connect to TEST_REDIS_HOST:TEST_REDIS_PORT, defaulting to
# localhost:6379 — so forward the in-cluster Redis to the host as well. Redis is
# low-throughput from the host (test setup only), so a port-forward is fine here.
REDIS_PORT_FORWARD_PID = "/tmp/tmi-dev-redis-portforward.pid"


# ---------------------------------------------------------------------------
# Pure helpers
# ---------------------------------------------------------------------------

def image_builds_for(db: str) -> list[tuple[str, str, dict]]:
    """Return the (name, dockerfile, build_args) tuples for the chosen DB flavor.

    The controller and the two workers are identical across DB flavors; only the
    server image differs (static Postgres image vs. Oracle CGO image).
    """
    if db == "oracle":
        server = ("tmi-server-oracle", "Dockerfile.server-oracle", {"EXTRA_TAGS": "dev"})
    else:
        server = ("tmi-server", "Dockerfile.server", {"BUILD_TAGS": "dev"})
    return [
        server,
        ("tmi-component-controller", "Dockerfile.controller", {}),
        ("tmi-extractor",            "Dockerfile.extractor",  {}),
        ("tmi-chunk-embed",          "Dockerfile.chunkembed", {}),
    ]


def overlay_dir_for(db: str, cluster_target: str = "kind") -> str:
    """Return the kustomize overlay directory path for the chosen cluster + DB flavor.

    CLUSTER=k3s uses its own overlay (in-cluster registry image refs, full stack);
    Oracle-on-k3s is out of scope, so k3s implies the postgres overlay.
    """
    if cluster_target == "k3s":
        return f"{DEV_DIR}/k3s"
    if cluster_target == "docker-desktop":
        return f"{DEV_DIR}/docker-desktop"
    return f"{DEV_DIR}/oracle" if db == "oracle" else DEV_DIR


def _no_workers_files(db: str) -> tuple[str, ...]:
    """Return the per-file manifest list for --no-workers mode.

    controller.yml and redis.yml are shared across DB flavors; only the server
    manifest filename differs (server.yml for postgres, server-oracle.yml for oracle).
    All files physically live in DEV_DIR.
    """
    server_file = "server-oracle.yml" if db == "oracle" else "server.yml"
    return ("controller.yml", "redis.yml", server_file)


def content_hash(text: str) -> str:
    """Stable 12-char hex digest of text (for config-change annotations)."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:12]


# The host the in-cluster server uses to reach the host-published Postgres.
# config-development.yml carries `localhost` (correct for host-side tools like
# bin/tmi-dbtool, which connect from the Mac host). The server pod's localhost is
# the pod itself, so when we deliver that file as a ConfigMap we rewrite ONLY the
# database-URL authority to this host. Docker Desktop maps host.docker.internal to
# the host, reaching the 127.0.0.1:5432-published Postgres container.
IN_CLUSTER_DB_HOST = "host.docker.internal"


def in_cluster_db_host(cluster_target: str = "kind") -> str:
    """Host the in-cluster server uses to reach Postgres for the given cluster.

    kind: Postgres is a Mac container, reached via host.docker.internal.
    k3s:  Postgres runs in-cluster as the `postgres` Service (postgres.yml).
    """
    return "postgres" if cluster_target in ("k3s", "docker-desktop") else IN_CLUSTER_DB_HOST


# Match the host (and optional :port) inside a postgres:// URL authority, after
# the credentials '@'. Deliberately narrow: it touches ONLY a postgres URL's host,
# never other localhost references in the config (redis host, OAuth callbacks, ...).
_DB_URL_HOST_RE = re.compile(r"(postgres://[^\"'\s]*@)(localhost|127\.0\.0\.1)(?=[:/])")


def rewrite_db_host_for_incluster(config_text: str, *, db_host: str = IN_CLUSTER_DB_HOST) -> str:
    """Rewrite a postgres:// URL's localhost/127.0.0.1 host to db_host.

    Used when delivering config-development.yml to the in-cluster server so the
    pod can reach the host-published Postgres. Leaves every other host reference
    (redis, OAuth callback allowlist, etc.) untouched, and is a no-op when the
    URL already points somewhere else (e.g. an oracle:// URL, or an explicit host).
    """
    return _DB_URL_HOST_RE.sub(rf"\1{db_host}", config_text)


def render_configmap_yaml(*, name: str, namespace: str, file_key: str, content: str) -> str:
    """Render a ConfigMap manifest embedding `content` under `file_key`.

    Uses a block scalar with 4-space indentation; annotates the content hash.
    """
    # name/namespace/file_key are dev-internal identifiers, not user input — not escaped.
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


# ---------------------------------------------------------------------------
# Shell wrappers (not unit-tested; exercised against a live cluster)
# ---------------------------------------------------------------------------

def current_kube_context() -> str:
    """Return the active kubectl context name (empty string if none)."""
    try:
        out = subprocess.run(
            ["kubectl", "config", "current-context"],
            capture_output=True, text=True, check=True,
        )
        return out.stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def kubectl(args: list[str], *, check: bool = True, input_text: str | None = None):
    """Run kubectl with the given args."""
    return run_cmd(["kubectl", *args], check=check, input_text=input_text)


# ---------------------------------------------------------------------------
# Preflight + context guard
# ---------------------------------------------------------------------------

def _preflight() -> None:
    for tool in ("docker", "kubectl"):
        check_tool(tool)
    if run_cmd(["kubectl", "cluster-info"], check=False).returncode != 0:
        log_error("No reachable cluster. Run 'make dev-cluster-up' (kind) or start your k3s cluster.")
        sys.exit(1)


def _guard_context(skip: bool, cluster_target: str = "kind") -> str:
    ctx = current_kube_context()
    log_info(f"kubectl context: {ctx or '(none)'}  namespace: {NS}")
    if not ctx:
        log_error("No kubectl context set. Run 'make dev-cluster-up'")
        sys.exit(1)
    expected = cluster.expected_context(cluster_target)
    if not skip and ctx != expected and not cluster.is_local_kube_context(ctx):
        log_error(f"Context '{ctx}' is not the expected '{expected}' for CLUSTER={cluster_target}, "
                  f"nor a recognized local dev cluster. Re-run with --yes to override.")
        sys.exit(1)
    return ctx


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

def build_and_push(db: str, cluster_target: str = "kind") -> None:
    """Build all images and push them to the target cluster's registry.

    kind -> localhost:5000; k3s -> the in-cluster registry at rp2:30500. The Mac
    and the k3s nodes are both arm64, so a plain host-arch `docker build` already
    produces arm64 images — no buildx/--platform is needed; only the registry the
    ref points at differs.

    All four Dockerfiles require the tmi-client staged in .docker-deps/.
    Stage once before the first build and clean up in a try/finally block
    so the staging dir is always removed even if a build fails — but only
    when this run created it (pre-existing dirs are left untouched).
    """
    project_root = get_project_root()

    # All four images need the client — stage once (no-op if pre-existing).
    created = stage_tmi_client()
    try:
        for name, dockerfile, build_args_map in image_builds_for(db):
            ref = cluster.local_image_ref(name, cluster=cluster_target)
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
    kubectl(["apply", "-f", str(project_root / PLATFORM_DIR / "nats.yml")])
    kubectl(["apply", "--server-side", "-f", str(project_root / PLATFORM_DIR / "keda.yml")])
    kubectl(["apply", "-f", str(project_root / "config/crd/bases/tmi.dev_tmicomponents.yaml")])


def ensure_namespace() -> None:
    kubectl(
        ["apply", "-f", "-"],
        input_text=f"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: {NS}\n",
    )


def ensure_k3s_registry() -> None:
    """Apply the in-cluster registry and wait for it (k3s prerequisite before push).

    The Mac builds images and pushes them to this registry (rp2:30500), and the
    nodes pull from it, so it must be Running before build_and_push."""
    project_root = get_project_root()
    kubectl(["apply", "-f", str(project_root / DEV_DIR / "k3s" / "registry.yml")])
    kubectl(["-n", NS, "rollout", "status", "deploy/registry", "--timeout=180s"])
    log_success("In-cluster registry ready (rp2:30500)")


def apply_k3s_postgres() -> None:
    """Apply the in-cluster Postgres and wait for it (k3s prerequisite before the
    server, mirroring how the kind path brings the host DB up first)."""
    project_root = get_project_root()
    kubectl(["apply", "-f", str(project_root / DEV_DIR / "k3s" / "postgres.yml")])
    kubectl(["-n", NS, "rollout", "status", "statefulset/postgres", "--timeout=180s"])
    log_success("In-cluster Postgres ready (svc/postgres:5432)")


def deliver_config(cluster_target: str = "kind") -> None:
    content = (get_project_root() / CONFIG_FILE).read_text()
    # The on-disk config points the DB at localhost (for host-side tools); rewrite
    # it to the host the in-cluster server uses: host.docker.internal (kind) or the
    # in-cluster `postgres` Service (k3s).
    content = rewrite_db_host_for_incluster(content, db_host=in_cluster_db_host(cluster_target))
    manifest = render_configmap_yaml(
        name=CONFIGMAP_NAME, namespace=NS, file_key="config.yml", content=content,
    )
    kubectl(["apply", "-f", "-"], input_text=manifest)
    log_success(f"Config delivered as ConfigMap/{CONFIGMAP_NAME}")


def create_embedding_secret() -> None:
    key = os.environ.get("TMI_EMBEDDING_API_KEY", "sk-e2e-placeholder")
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-embedding", "-n", NS,
         f"--from-literal=api-key={key}", "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    kubectl(["apply", "-f", "-"], input_text=rendered)


def create_oracle_wallet_secret() -> None:
    """Create the tmi-oracle-wallet Secret from the developer's wallet zip.

    Path comes from TMI_ORACLE_WALLET_ZIP (a path to the OCI ADB wallet .zip).
    The Oracle image entrypoint reads /wallet/wallet.zip and extracts it.
    (There is no existing wallet-*zip* env var to reuse: scripts/oci-env.sh's
    TNS_ADMIN points to an extracted wallet *directory* for host-process tests,
    not a zip, so this introduces TMI_ORACLE_WALLET_ZIP for the k8s path.)
    """
    wallet = os.environ.get("TMI_ORACLE_WALLET_ZIP", "")
    if not wallet or not Path(wallet).is_file():
        log_error("DB=oracle requires TMI_ORACLE_WALLET_ZIP to point at your ADB wallet .zip")
        sys.exit(1)
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-oracle-wallet", "-n", NS,
         f"--from-file=wallet.zip={wallet}", "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    kubectl(["apply", "-f", "-"], input_text=rendered)
    log_success("oracle wallet delivered as Secret/tmi-oracle-wallet")


def create_oracle_db_secret() -> None:
    """Create the tmi-oracle-db Secret carrying the ADB connection settings.

    In-cluster the server pod cannot see the host's scripts/oci-env.sh, so the
    Oracle DB URL and password (which live there for the host-process path) must
    be delivered as a Secret and surfaced to the pod as env vars. server-oracle.yml
    pulls them in via secretKeyRef. The server reads TMI_DATABASE_URL (12-factor
    override of database.url — see internal/config/config.go) and ORACLE_PASSWORD
    (the ADB user password — see auth/db/gorm.go); config-development.yml carries a
    postgres URL, so without this the oracle pod would dial postgres and crash.
    """
    url = os.environ.get("TMI_DATABASE_URL", "")
    password = os.environ.get("ORACLE_PASSWORD", "")
    if not url.startswith("oracle://"):
        log_error("DB=oracle requires TMI_DATABASE_URL=oracle://... (run: source scripts/oci-env.sh)")
        sys.exit(1)
    if not password:
        log_error("DB=oracle requires ORACLE_PASSWORD to be set (run: source scripts/oci-env.sh)")
        sys.exit(1)
    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-oracle-db", "-n", NS,
         f"--from-literal=database-url={url}",
         f"--from-literal=oracle-password={password}",
         "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    kubectl(["apply", "-f", "-"], input_text=rendered)
    log_success("oracle DB connection delivered as Secret/tmi-oracle-db")


def apply_overlay(no_workers: bool, db: str, cluster_target: str = "kind") -> None:
    """Apply the dev overlay.

    When --no-workers: apply the three core manifests individually to avoid
    kustomize referencing the component CR files (which include TMIComponent
    resources from ../platform/components/).

    Otherwise: render the full kustomize overlay with --load-restrictor
    LoadRestrictionsNone (needed because the overlay references files outside
    its own directory tree, i.e. ../platform/components/; the oracle overlay at
    deployments/k8s/dev/oracle references ../../platform/components/ so the flag
    is equally required for both flavors).
    """
    project_root = get_project_root()
    if no_workers:
        for f in _no_workers_files(db):
            kubectl(["apply", "-f", str(project_root / DEV_DIR / f)])
    else:
        rendered = run_cmd(
            ["kubectl", "kustomize", "--load-restrictor", "LoadRestrictionsNone",
             str(project_root / overlay_dir_for(db, cluster_target))],
            capture=True,
        ).stdout
        kubectl(["apply", "-f", "-"], input_text=rendered)


def server_rollout_timeout(db: str) -> str:
    """Rollout-status timeout for the tmi-server Deployment, DB-aware.

    On the FIRST boot against a remote Oracle ADB, GORM AutoMigrate issues
    hundreds of per-object introspection round-trips and can take 10-20 min
    (#480) before the server's HTTP listener (and thus its startupProbe) comes
    up. A fixed 180s wait timed out on that first boot and the rollout failed
    (#479). Oracle therefore gets a long budget; Postgres migrates locally in
    seconds and keeps the short one. Later boots take the schema-fingerprint
    fast path regardless, so the long budget is only ever consumed once.
    """
    return "1200s" if db == "oracle" else "180s"


def wait_and_forward(db: str = "postgres", cluster_target: str = "kind") -> None:
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-component-controller", "--timeout=120s"])
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", f"--timeout={server_rollout_timeout(db)}"])
    start_redis_port_forward()
    # k3s has no extraPortMappings, so preserve localhost:8080 with a server
    # port-forward. Start it AFTER the redis forward, whose stop_port_forward()
    # clears both pidfiles, so this one survives.
    if cluster_target == "k3s":
        start_server_port_forward()
    wait_for_server()
    log_success(f"Dev environment ready at {SERVER_URL}")


def wait_for_server(*, attempts: int = 30, delay_s: float = 1.0) -> None:
    """Poll the server NodePort until it answers (or give up after attempts).

    The Deployment rollout completing means the pod is Ready, but kube-proxy
    programming the NodePort DNAT and the kind extraPortMapping becoming live on
    the host can lag a beat. Poll localhost:8080 so callers (and CATS) don't race
    a not-yet-reachable NodePort.
    """
    for i in range(1, attempts + 1):
        reachable, code = server_http_status()
        if reachable:
            log_info(f"Server reachable at {SERVER_URL} (HTTP {code})")
            return
        if i < attempts:
            time.sleep(delay_s)
    log_error(
        f"Server not reachable at {SERVER_URL} after {attempts} attempts. "
        f"If you upgraded an existing cluster, recreate it so the NodePort "
        f"mapping takes effect: 'make dev-cluster-down && make dev-cluster-up' "
        f"(or 'make dev-nuke')."
    )


def start_redis_port_forward() -> None:
    """Forward the in-cluster Redis to localhost:6379 for host integration tests.

    The server itself is NOT forwarded — it is reached via the NodePort published
    on localhost:8080 by the kind extraPortMapping. Only Redis needs a host
    forward, and only for test setup (low throughput), so a port-forward is fine.
    """
    stop_port_forward()
    redis_proc = subprocess.Popen(
        ["kubectl", "-n", NS, "port-forward", "svc/redis", "6379:6379"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    Path(REDIS_PORT_FORWARD_PID).write_text(str(redis_proc.pid))
    log_info(f"Port-forward started (PID {redis_proc.pid}): localhost:6379 -> svc/redis:6379")


def start_server_port_forward() -> None:
    """Forward the in-cluster server to localhost:8080 (k3s only).

    kind publishes the server NodePort on localhost:8080 via extraPortMappings, so
    no forward is needed there. A remote k3s cluster has no such mapping, so we
    preserve the localhost:8080 contract with a port-forward. (For CATS/high-
    throughput, hit the NodePort at rp2:30080 directly — the userspace forward
    throttles under load, the #463 problem.) Stops only a prior SERVER forward so
    it does not disturb the redis forward started just before it."""
    _stop_port_forward_pidfile(PORT_FORWARD_PID)
    srv_proc = subprocess.Popen(
        ["kubectl", "-n", NS, "port-forward", "svc/tmi-server", f"{HOST_PORT}:{HOST_PORT}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    Path(PORT_FORWARD_PID).write_text(str(srv_proc.pid))
    log_info(f"Port-forward started (PID {srv_proc.pid}): localhost:{HOST_PORT} -> svc/tmi-server:{HOST_PORT}")


def _stop_port_forward_pidfile(pid_path: str) -> None:
    p = Path(pid_path)
    if p.exists():
        try:
            pid = int(p.read_text().strip())
            os.kill(pid, signal.SIGTERM)
            log_info(f"Stopped port-forward (PID {pid})")
        except (ProcessLookupError, ValueError):
            pass
        p.unlink(missing_ok=True)


def stop_port_forward() -> None:
    _stop_port_forward_pidfile(PORT_FORWARD_PID)
    _stop_port_forward_pidfile(REDIS_PORT_FORWARD_PID)


# ---------------------------------------------------------------------------
# New helpers (consumed by devstatus / devenv nuke)
# ---------------------------------------------------------------------------

def tail_server_logs() -> None:
    """Stream the tmi-server pod logs (Ctrl-C to stop)."""
    kubectl(["-n", NS, "logs", "-f", "deploy/tmi-server", "--tail=200"], check=False)


def remove_local_images(db: str, cluster_target: str = "kind") -> None:
    """Remove the locally-built dev images (used by `devenv.py nuke`)."""
    for name, _df, _args in image_builds_for(db):
        run_cmd(["docker", "rmi", "-f", cluster.local_image_ref(name, cluster=cluster_target)],
                check=False)


def server_http_status() -> tuple[bool, str]:
    """Return (reachable, http_code) for the server NodePort at localhost:8080."""
    r = subprocess.run(
        ["curl", "-s", "--connect-timeout", "2", "--max-time", "5",
         "-o", "/dev/null", "-w", "%{http_code}", SERVER_URL],
        capture_output=True, text=True,
    )
    code = r.stdout.strip() or "000"
    return (code in ("200", "429"), code)


# ---------------------------------------------------------------------------
# Orchestration entry points
# ---------------------------------------------------------------------------

def start(*, db: str, cluster_target: str = "kind", no_workers: bool = False,
          skip_context_guard: bool = False) -> None:
    """Build images, deploy all components, wait for readiness, and start port-forwards."""
    _preflight()
    _guard_context(skip_context_guard, cluster_target)
    if cluster_target == "k3s":
        ensure_k3s_registry()          # in-cluster registry must be up before push
    else:
        cluster.ensure_registry()
        cluster.connect_registry_to_kind()
    build_and_push(db, cluster_target)
    ensure_namespace()
    apply_platform_base()
    if cluster_target == "k3s":
        apply_k3s_postgres()           # in-cluster DB up before the server (AutoMigrate)
    deliver_config(cluster_target)
    create_embedding_secret()
    if db == "oracle":
        create_oracle_wallet_secret()
        create_oracle_db_secret()
    apply_overlay(no_workers, db, cluster_target)
    # `kubectl apply` of an unchanged Deployment spec does not roll a new pod, so
    # a freshly-built :dev image (same tag) would not be picked up, and a pod
    # stuck in CrashLoopBackOff from a prior transient outage (e.g. the host DB
    # being down on an earlier start) would never be reset — leaving the rollout
    # wait below to time out on a pod that will not recover within its window.
    # Force a fresh rollout so `start` always runs the just-built images on new,
    # backoff-cleared pods. imagePullPolicy:Always ensures the new image is pulled.
    kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-component-controller"])
    kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-server"])
    wait_and_forward(db, cluster_target)


def restart(*, db: str, cluster_target: str = "kind", no_workers: bool = False,
            skip_context_guard: bool = False) -> None:
    """Rebuild the server image, re-deliver config, and roll the server deployment."""
    _preflight()
    _guard_context(skip_context_guard, cluster_target)
    if cluster_target == "k3s":
        ensure_k3s_registry()
    else:
        cluster.ensure_registry()
        cluster.connect_registry_to_kind()
    build_and_push(db, cluster_target)
    deliver_config(cluster_target)
    if db == "oracle":
        create_oracle_wallet_secret()
        create_oracle_db_secret()
    apply_overlay(no_workers, db, cluster_target)
    kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-server"])
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", f"--timeout={server_rollout_timeout(db)}"])
    start_redis_port_forward()
    if cluster_target == "k3s":
        start_server_port_forward()
    wait_for_server()
    log_success(f"Server restarted; {SERVER_URL}")


def teardown(*, db: str = "postgres") -> None:
    """Tear down everything that start() deployed.

    Removes (tolerating absence for all):
    - port-forward process
    - server Deployment + Service
    - redis Deployment + Service
    - controller Deployment + RBAC (ServiceAccount, ClusterRole, ClusterRoleBinding)
    - TMIComponent CRs (tmi-extractor, tmi-chunk-embed)
    - ConfigMap tmi-server-config
    - Secret tmi-embedding
    - Secret tmi-oracle-wallet (defensive; no-op if never created)
    - Secret tmi-oracle-db (defensive; no-op if never created)
    - local registry container (docker stop)
    """
    stop_port_forward()

    # TMIComponent CRs (worker component definitions)
    kubectl(
        ["-n", NS, "delete", "tmicomponents.tmi.dev", "tmi-extractor", "tmi-chunk-embed",
         "--ignore-not-found"],
        check=False,
    )

    # Server and Redis Deployments + Services
    kubectl(
        ["-n", NS, "delete", "deploy,svc", "tmi-server", "redis", "--ignore-not-found"],
        check=False,
    )

    # Controller Deployment
    kubectl(
        ["-n", NS, "delete", "deploy", "tmi-component-controller", "--ignore-not-found"],
        check=False,
    )

    # Controller RBAC
    kubectl(
        ["delete", "clusterrolebinding,clusterrole", "tmi-controller", "--ignore-not-found"],
        check=False,
    )
    kubectl(
        ["-n", NS, "delete", "serviceaccount", "tmi-controller", "--ignore-not-found"],
        check=False,
    )

    # ConfigMap and Secrets
    kubectl(
        ["-n", NS, "delete", "configmap", CONFIGMAP_NAME, "--ignore-not-found"],
        check=False,
    )
    kubectl(
        ["-n", NS, "delete", "secret", "tmi-embedding", "--ignore-not-found"],
        check=False,
    )
    kubectl(
        ["-n", NS, "delete", "secret", "tmi-oracle-wallet", "--ignore-not-found"],
        check=False,
    )
    kubectl(
        ["-n", NS, "delete", "secret", "tmi-oracle-db", "--ignore-not-found"],
        check=False,
    )

    # Stop the local registry container
    run_cmd(["docker", "stop", cluster.REGISTRY_CONTAINER], check=False)

    log_success("Dev environment torn down (cluster left intact)")


def teardown_k3s_namespace() -> None:
    """Hard reset for k3s: delete the entire tmi-platform namespace (all workloads,
    the in-cluster registry, and the Postgres PVC/data). Never touches the k3s
    cluster itself — we do not own it."""
    stop_port_forward()
    kubectl(["delete", "namespace", NS, "--ignore-not-found", "--wait=true"])
    log_success(f"Namespace {NS} deleted (k3s hard reset)")
