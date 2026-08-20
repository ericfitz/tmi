"""TMI dev image build/push + in-cluster deploy + teardown.

Pure helpers are unit-tested in scripts/lib/tests/test_deploy.py; orchestration
functions (start/restart/teardown) are exercised against a live cluster by
scripts/devenv.py. Depends on lib/cluster.py for registry + image refs.
"""
from __future__ import annotations

import hashlib
import os
import re
import shlex
import signal
import subprocess
import sys
import time
from pathlib import Path

import cluster
import portfwd
from tmi_common import (
    check_tool, container_exists, container_is_running, get_project_root,
    log_error, log_info, log_success, log_warn, run_cmd, wait_for_port,
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

NS = "tmi-platform"
DEV_DIR = "deployments/k8s/dev"
PLATFORM_DIR = "deployments/k8s/platform"
CONFIG_FILE = "config-development.yml"
CONFIGMAP_NAME = "tmi-server-config"
# Gitignored, machine-local OAuth/SAML provider config for a dev cluster, in
# kubectl --from-env-file format. Delivered as Secret/tmi-oauth-providers and
# consumed by server.yml via envFrom. Lives under .local/ per the repo's
# machine-local config convention. See create_oauth_providers_secret().
OAUTH_PROVIDERS_ENV_FILE = ".local/oauth-providers.env"
OAUTH_PROVIDERS_EXAMPLE_FILE = f"{DEV_DIR}/oauth-providers.env.example"
ENV_KEY_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")

# The server is reached on the host at localhost:HOST_PORT via a kubectl
# port-forward (start_server_port_forward). Neither docker-desktop nor k3s
# publishes the NodePort directly on the host (only the former kind cluster did
# that via extraPortMappings). The port-forward replaces the removed kind path;
# for high-throughput testing (CATS) against k3s, hit the NodePort at
# rp2:30080 directly — the userspace forward throttles under load (#463).
# KEEP NODE_PORT IN SYNC with server.yml/server-oracle.yml (.spec.ports[].nodePort).
HOST_PORT = 8080
NODE_PORT = 30080
SERVER_URL = f"http://localhost:{HOST_PORT}"

# Server port-forward pidfile. The server is reached on the host at localhost:8080
# via the server port-forward (k3s and docker-desktop), which writes this pidfile
# so stop_port_forward() can tear it down. Also cleans up any stale forwarder
# left running on :8080 from a prior session.
PORT_FORWARD_PID = "/tmp/tmi-dev-portforward.pid"
# Redis is an in-cluster ClusterIP service; the server reaches it as redis:6379.
# Integration tests that seed Redis directly (e.g. the step-up legacy refresh
# token round-trip) connect to TEST_REDIS_HOST:TEST_REDIS_PORT, defaulting to
# localhost:6379 — so forward the in-cluster Redis to the host as well. Redis is
# low-throughput from the host (test setup only), so a port-forward is fine here.
REDIS_PORT_FORWARD_PID = "/tmp/tmi-dev-redis-portforward.pid"
# Postgres is an in-cluster StatefulSet reachable only as a ClusterIP Service.
# Seeding (scripts/run-dbtool.py, which the cats plugin also calls as its `seed`
# hook) opens a DIRECT database connection using config-development.yml's
# localhost:5432, so it needs this forward on top of the server one.
#
# Unlike the redis and server forwards, this one is NOT started by dev-up: 5432
# collides with a locally installed PostgreSQL on many machines, and a developer
# who never runs CATS should not have to care. It is established on demand by
# ensure_port_forward("postgres"). It is tracked by a pidfile like the others so
# stop_port_forward() tears it down deliberately -- a hand-started forward is
# instead matched by that function's legacy reaper (its "-n tmi-platform
# port-forward svc/" pattern) and killed as an orphan on the next dev-up /
# dev-restart / dev-down, which is exactly why seeding could not rely on one.
POSTGRES_PORT_FORWARD_PID = "/tmp/tmi-dev-postgres-portforward.pid"
POSTGRES_PORT = 5432

# Public base images the docker-desktop node would otherwise pull from cgr.dev on
# first bring-up. Docker Desktop Kubernetes' containerd pulls these independently
# of the host Docker daemon, and that first pull occasionally fails with a
# transient EOF from cgr.dev (#517), leaving postgres/redis in ErrImagePull. We
# instead `docker pull` them on the host and import them into the node's
# containerd alongside the tmi-* images, then pin imagePullPolicy: IfNotPresent on
# the postgres/redis manifests so the imported copy is used and no cgr.dev pull is
# attempted. KEEP THESE IN SYNC with the image refs in
# deployments/k8s/dev/docker-desktop/postgres.yml and deployments/k8s/dev/redis.yml.
DD_POSTGRES_IMAGE = "cgr.dev/chainguard/postgres:latest"
DD_REDIS_IMAGE = "cgr.dev/chainguard/redis:latest"
DD_BASE_IMAGES = (DD_POSTGRES_IMAGE, DD_REDIS_IMAGE)


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


def overlay_dir_for(db: str, cluster_target: str = "docker-desktop") -> str:
    """Return the kustomize overlay directory path for the chosen cluster + DB flavor.

    CLUSTER=k3s uses its own overlay (in-cluster registry image refs, full stack);
    CLUSTER=docker-desktop uses its own overlay (image import, no registry).
    For docker-desktop the DB flavor further selects: oracle gets the dedicated
    docker-desktop-oracle overlay (external ADB, no in-cluster Postgres); postgres
    gets the standard docker-desktop overlay (in-cluster Postgres).
    """
    if cluster_target == "k3s":
        return f"{DEV_DIR}/k3s"
    if cluster_target == "docker-desktop":
        return f"{DEV_DIR}/docker-desktop-oracle" if db == "oracle" else f"{DEV_DIR}/docker-desktop"
    raise ValueError(f"unknown cluster target: {cluster_target!r}")


def dd_base_images_for(db: str) -> tuple[str, ...]:
    """Public base images to pre-import into the docker-desktop node for the given
    DB flavor (#517). Redis is always deployed; in-cluster Postgres is deployed
    only for the postgres flavor (oracle uses an external ADB and brings up no
    Postgres pod), so skip the Postgres base image there."""
    return (DD_REDIS_IMAGE,) if db == "oracle" else DD_BASE_IMAGES


def content_hash(text: str) -> str:
    """Stable 12-char hex digest of text (for config-change annotations)."""
    return hashlib.sha256(text.encode("utf-8")).hexdigest()[:12]


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
        try:
            importer = subprocess.Popen(import_cmd, stdin=saver.stdout)
        except BaseException:
            # The importer never started, so nobody will drain saver's stdout.
            # Kill saver before the finally's wait() so it can't block forever
            # writing into a pipe with no reader, then re-raise.
            saver.kill()
            raise
        finally:
            # Release the parent's copy of the pipe's read end unconditionally,
            # whether or not the importer Popen raised. This both lets saver
            # receive SIGPIPE if the importer exits and guarantees saver.wait()
            # below cannot deadlock on a pipe the parent is still holding open.
            saver.stdout.close()
        importer.communicate()
        if importer.returncode != 0:
            log_error(f"ctr import failed for {ref} (exit {importer.returncode})")
            sys.exit(1)
    finally:
        saver.wait()
    if saver.returncode != 0:
        log_error(f"docker save failed for {ref} (exit {saver.returncode})")
        sys.exit(1)


# Fallback host for reaching a host-published Postgres from inside a cluster
# (kept for historical reference; k3s and docker-desktop use in-cluster Postgres
# and therefore use the `postgres` service name instead).
IN_CLUSTER_DB_HOST = "host.docker.internal"


def in_cluster_db_host(cluster_target: str = "docker-desktop") -> str:
    """Host the in-cluster server uses to reach Postgres for the given cluster.

    k3s and docker-desktop: Postgres runs in-cluster as the `postgres` Service.
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
        log_error("No reachable cluster. Run 'make dev-cluster-up' to set the cluster context.")
        sys.exit(1)


def _guard_context(skip: bool, cluster_target: str = "docker-desktop") -> str:
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
# Image build + push
# ---------------------------------------------------------------------------

def build_and_push(db: str, cluster_target: str = "docker-desktop") -> None:
    """Build all images and deliver them to the target cluster.

    k3s -> push to the in-cluster registry at rp2:30500; docker-desktop ->
    import straight into the DD node's containerd via `docker save | ctr import`
    (no registry, no push). The Mac and the k3s nodes are both arm64, so a plain
    host-arch `docker build` already produces arm64 images — no buildx/--platform
    is needed; only the delivery step differs.
    """
    project_root = get_project_root()

    for name, dockerfile, build_args_map in image_builds_for(db):
        ref = cluster.local_image_ref(name, cluster=cluster_target)
        log_info(f"Building {name}  ({dockerfile}) -> {ref}")

        cmd = ["docker", "build", "-f", str(project_root / dockerfile)]
        for k, v in build_args_map.items():
            cmd += ["--build-arg", f"{k}={v}"]
        cmd += ["-t", ref, str(project_root)]

        run_cmd(cmd)

        if cluster_target == "docker-desktop":
            import_image_to_node(ref, cluster.DD_NODE)
        else:
            log_info(f"Pushing {ref}")
            run_cmd(["docker", "push", ref])

    if cluster_target == "docker-desktop":
        # Import the public postgres/redis base images too, so first bring-up
        # doesn't depend on the DD node's containerd reaching cgr.dev (#517).
        # Pull on the host first (respects the host Docker's proxy/cache), then
        # stream into the node's containerd exactly like the tmi-* images.
        for base in dd_base_images_for(db):
            log_info(f"Pulling base image {base}")
            run_cmd(["docker", "pull", base])
            import_image_to_node(base, cluster.DD_NODE)
        log_success("All images built and imported into the docker-desktop node")
    else:
        log_success("All images built and pushed to local registry")


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


def apply_incluster_postgres(cluster_target: str) -> None:
    """Apply the in-cluster Postgres (k3s and docker-desktop) and wait for it —
    a prerequisite before the server starts AutoMigrate."""
    project_root = get_project_root()
    subdir = "k3s" if cluster_target == "k3s" else "docker-desktop"
    kubectl(["apply", "-f", str(project_root / DEV_DIR / subdir / "postgres.yml")])
    kubectl(["-n", NS, "rollout", "status", "statefulset/postgres", "--timeout=180s"])
    log_success("In-cluster Postgres ready (svc/postgres:5432)")


def deliver_config(cluster_target: str = "docker-desktop") -> None:
    content = (get_project_root() / CONFIG_FILE).read_text()
    # The on-disk config points the DB at localhost (for host-side tools); rewrite
    # it to the in-cluster `postgres` Service (k3s and docker-desktop).
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


def create_oauth_providers_secret() -> None:
    """Create/refresh Secret/tmi-oauth-providers from .local/oauth-providers.env.

    This is the dev-cluster counterpart to the AWS overlay's Secret of the same
    name: it carries the real OAuth/SAML provider config plus the callback and
    base URLs, which server.yml pulls in wholesale via `envFrom`.

    Why a file-backed Secret rather than the config file or the database: since
    the #415 three-category split, providers are *operational* settings, so the
    mounted config.yml (bootstrap-only) cannot carry them, and the only other
    home is the database — which `make dev-nuke` destroys along with the
    namespace and the Postgres PVC. Re-creating the Secret from a gitignored
    local file on every start()/restart() makes the config self-healing: after a
    nuke the providers come back instead of leaving the server advertising only
    the built-in "tmi" provider (#791).

    Absent file is not an error — a developer who has never set up real IdPs
    gets the "tmi"-only stack that dev has always had, and server.yml marks the
    secretRef optional so the pod still starts.

    Secret safety: `--from-env-file` makes kubectl read the values off disk, so
    they never reach argv, the environment, or any log. Nothing in this function
    prints a value; validation errors name only the offending key or line.
    """
    path = get_project_root() / OAUTH_PROVIDERS_ENV_FILE
    if not path.is_file():
        log_warn(
            f"{OAUTH_PROVIDERS_ENV_FILE} not found — deploying without real OAuth "
            "providers (only the built-in \"tmi\" provider will be advertised). "
            f"To set them up: cp {OAUTH_PROVIDERS_EXAMPLE_FILE} "
            f"{OAUTH_PROVIDERS_ENV_FILE} and fill it in."
        )
        return

    bad = _invalid_env_file_keys(path)
    if bad:
        log_error(
            f"{OAUTH_PROVIDERS_ENV_FILE} is not a valid env file. kubectl "
            "--from-env-file needs bare KEY=VALUE lines (no `export`, no quotes "
            f"around the whole assignment). Offending line(s): {', '.join(bad)}"
        )
        sys.exit(1)

    rendered = run_cmd(
        ["kubectl", "create", "secret", "generic", "tmi-oauth-providers", "-n", NS,
         "--from-env-file", str(path), "--dry-run=client", "-o", "yaml"],
        capture=True,
    ).stdout
    kubectl(["apply", "-f", "-"], input_text=rendered)
    log_success("OAuth/SAML provider config delivered as Secret/tmi-oauth-providers")


def _invalid_env_file_keys(path: Path) -> list[str]:
    """Return a description of each line of `path` that kubectl --from-env-file
    would reject, identified by line number and key only — never by value."""
    bad: list[str] = []
    for lineno, raw in enumerate(path.read_text().splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            bad.append(f"line {lineno} (no '=')")
            continue
        key = line.split("=", 1)[0]
        if not ENV_KEY_RE.match(key):
            bad.append(f"line {lineno} (key {key!r})")
    return bad


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


def apply_overlay(db: str, cluster_target: str = "docker-desktop") -> None:
    """Apply the dev overlay.

    Render the full kustomize overlay with --load-restrictor LoadRestrictionsNone
    (needed because the overlay references files outside its own directory tree,
    e.g. ../../platform/components/).
    """
    project_root = get_project_root()
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


def wait_and_forward(db: str = "postgres", cluster_target: str = "docker-desktop") -> None:
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-component-controller", "--timeout=120s"])
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", f"--timeout={server_rollout_timeout(db)}"])
    start_redis_port_forward()
    # k3s and docker-desktop have no extraPortMappings, so preserve localhost:8080
    # with a server port-forward. Order no longer matters here: each starter now
    # stops only its own pidfile, so these two (and the on-demand postgres
    # forward) cannot clobber each other.
    if cluster_target in ("k3s", "docker-desktop"):
        start_server_port_forward()
    wait_for_server()
    log_success(f"Dev environment ready at {SERVER_URL}")


def wait_for_server(*, attempts: int = 30, delay_s: float = 1.0) -> None:
    """Poll the server until it answers (or give up after attempts).

    The Deployment rollout completing means the pod is Ready, but the kubectl
    port-forward establishing the localhost:8080 path can lag a beat. Poll
    localhost:8080 so callers (and CATS) don't race a not-yet-reachable server.
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
        f"If the port-forward is not running, try 'make dev-nuke' for a clean restart."
    )


def _preflight_port(port: int) -> None:
    """Reclaim `port` from a squatting kubectl port-forward before spawning a
    fresh supervisor, or refuse to proceed if something else holds it.

    #580: an orphaned supervisor (lost/stale pidfile, reparented to init after
    an orchestrator crash, or a forward started before this module existed)
    keeps re-forwarding forever and silently squats on the port. That is the
    likely mechanism behind a 2026-07-27 incident where "local" verification
    on localhost:8080 silently reached an AWS EKS cluster instead of the local
    k3s dev environment. Rather than let a fresh `kubectl port-forward` race a
    squatter for the bind, actively reclaim the port from anything positively
    identified as one of our own forwards (see portfwd.is_kubectl_forward),
    and hard-fail if an unrelated process is holding it instead of silently
    binding somewhere else or proceeding against the wrong target.
    """
    listeners = portfwd.port_listeners(port)
    if not listeners:
        return
    for pid, command in listeners:
        if not portfwd.is_kubectl_forward(command):
            continue
        try:
            os.killpg(os.getpgid(pid), signal.SIGTERM)
            log_info(f"Reclaimed port {port} from squatting port-forward (PID {pid})")
        except ProcessLookupError:
            pass
    time.sleep(0.5)
    for pid, command in portfwd.port_listeners(port):
        if not portfwd.is_kubectl_forward(command):
            hint = ""
            if port == POSTGRES_PORT:
                hint = (
                    f"\nA locally installed PostgreSQL commonly listens on {POSTGRES_PORT}. "
                    "Stop it (or free the port) and retry — seeding talks to the in-cluster "
                    "database, so silently using the local one would seed the wrong database."
                )
            log_error(
                f"Port {port} is held by an unrelated process (PID {pid}): {command}\n"
                "Refusing to start a port-forward that would not reach the dev cluster."
                f"{hint}"
            )
            sys.exit(1)


def _spawn_supervised_forward(argv: list[str], pid_path: str, human_desc: str,
                               *, port: int, context: str) -> None:
    """Start a self-healing kubectl port-forward and record its supervisor PID.

    A bare `kubectl port-forward` exits whenever its backing pod rolls or
    restarts (a `dev-restart`, a crash, an eviction, or a manual scale) — after
    which localhost silently loses its path to the service until the next
    `dev-up`. Wrapping it in a re-launching shell loop keeps the localhost
    binding alive for the life of the dev environment, so `dev-up` yields an
    environment that STAYS immediately usable rather than only being usable the
    instant it finishes. The loop runs in its own session (start_new_session) so
    its pidfile can tear down the whole group (supervisor shell + the current
    kubectl child) and so it outlives this one-shot orchestrator process.

    #580: before spawning, reap any orphaned supervisor tied to this exact
    pidfile (marker-scoped, so this never touches a different forward's
    orphans) that pidfile-based stop couldn't reach, then preflight the port
    so a squatter can never be raced or silently left in place. The marker is
    embedded as a trailing `sh -c` comment — inert to the shell, but visible
    to `pgrep -f` — so a future run (or a different caller, e.g. #578) can
    find and reap this exact supervisor even if its pidfile is lost.
    """
    _stop_port_forward_pidfile(pid_path)
    reaped = portfwd.reap_supervisors(portfwd.marker_for(pid_path))
    if reaped:
        log_info(f"Reaped {len(reaped)} orphaned port-forward supervisor(s) for {Path(pid_path).name}")
    _preflight_port(port)
    inner = " ".join(shlex.quote(a) for a in argv)
    marker = portfwd.marker_for(pid_path)
    proc = subprocess.Popen(
        ["sh", "-c", f"while true; do {inner} >/dev/null 2>&1; sleep 1; done # {marker}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    portfwd.write_pidfile(pid_path, proc.pid, context=context, port=port)
    log_info(f"Port-forward started (PID {proc.pid}): {human_desc}")


def start_redis_port_forward() -> None:
    """Forward the in-cluster Redis to localhost:6379 for host integration tests.

    Redis is low-throughput from the host (test setup only), so a port-forward
    is fine here. The kube context is pinned via --context (rather than left
    to resolve from the ambient kubeconfig) because the supervisor re-execs
    kubectl on every pod roll — an unpinned kubectl would silently retarget
    to whatever context is CURRENT at each respawn, so switching context (say,
    to a remote EKS cluster) after this forward starts would re-point "local"
    Redis at the wrong cluster without any visible error (#580).

    Stops only its OWN forward, via _spawn_supervised_forward's per-pidfile
    stop. This used to call the blanket stop_port_forward(), which cleared
    every pidfile — forcing the server forward to be started afterwards to
    survive, an ordering constraint that would silently extend to any third
    forward, and destroying an on-demand postgres forward (see
    POSTGRES_PORT_FORWARD_PID) on every dev-up/dev-restart. Squatters are
    still handled: _spawn_supervised_forward reaps marker-scoped orphans and
    _preflight_port reclaims the port from any other kubectl forward.
    """
    ctx = current_kube_context()
    _spawn_supervised_forward(
        ["kubectl", "--context", ctx, "-n", NS, "port-forward", "svc/redis", "6379:6379"],
        REDIS_PORT_FORWARD_PID,
        "localhost:6379 -> svc/redis:6379",
        port=6379,
        context=ctx,
    )


def start_server_port_forward() -> None:
    """Forward the in-cluster server to localhost:8080 (k3s and docker-desktop).

    Neither target publishes the server NodePort directly on the host, so we
    preserve the localhost:8080 contract with a port-forward. (For CATS/high-
    throughput against k3s, hit the NodePort at rp2:30080 directly — the
    userspace forward throttles under load, the #463 problem.) Stops only a
    prior SERVER forward so it does not disturb the redis forward started
    just before it. The forward is supervised so it survives server pod rolls
    (see _spawn_supervised_forward). The kube context is pinned via --context
    for the same reason as start_redis_port_forward: an unpinned kubectl
    resolves the CURRENT context on every respawn, so a context switch after
    this forward starts (e.g. to EKS) would silently retarget "local" traffic
    at the wrong cluster — the likely mechanism behind the #580 incident."""
    ctx = current_kube_context()
    _spawn_supervised_forward(
        ["kubectl", "--context", ctx, "-n", NS, "port-forward", "svc/tmi-server", f"{HOST_PORT}:{HOST_PORT}"],
        PORT_FORWARD_PID,
        f"localhost:{HOST_PORT} -> svc/tmi-server:{HOST_PORT}",
        port=HOST_PORT,
        context=ctx,
    )


def start_postgres_port_forward() -> None:
    """Forward the in-cluster Postgres to localhost:5432 for host-side seeding.

    Seeding is low-volume (a few hundred inserts), so the userspace forward's
    throughput ceiling -- the #463/#578 problem that bans a forward for the
    fuzzing campaign itself -- does not apply. The campaign still hits the
    NodePort directly; this forward only ever serves seeding and prep.

    The kube context is pinned via --context for the same reason as the redis
    and server forwards: the supervisor re-execs kubectl on every pod roll, and
    an unpinned kubectl would resolve whatever context is CURRENT at each
    respawn -- so switching context mid-run would silently re-point "localhost"
    Postgres at another cluster (#580).
    """
    ctx = current_kube_context()
    _spawn_supervised_forward(
        ["kubectl", "--context", ctx, "-n", NS, "port-forward", "svc/postgres",
         f"{POSTGRES_PORT}:{POSTGRES_PORT}"],
        POSTGRES_PORT_FORWARD_PID,
        f"localhost:{POSTGRES_PORT} -> svc/postgres:{POSTGRES_PORT}",
        port=POSTGRES_PORT,
        context=ctx,
    )


# Named dev port-forwards that ensure_port_forward() knows how to establish.
# The starter functions own their kubectl argv; this table only maps a name to
# the pidfile/port used for the health check and to the starter itself.
_FORWARDS: dict[str, tuple[str, int]] = {
    "server": (PORT_FORWARD_PID, HOST_PORT),
    "redis": (REDIS_PORT_FORWARD_PID, 6379),
    "postgres": (POSTGRES_PORT_FORWARD_PID, POSTGRES_PORT),
}


def _forward_is_healthy(name: str) -> bool:
    """True when the named forward is already up, on-target, and listening.

    All three conditions matter. A live supervisor whose recorded context is
    not the current one is NOT healthy: it is forwarding localhost to a
    different cluster, which is the #580 failure mode, so it must be replaced
    rather than reused.
    """
    pid_path, port = _FORWARDS[name]
    record = portfwd.read_pidfile(pid_path)
    if record is None:
        return False
    try:
        os.killpg(os.getpgid(record.pid), 0)
    except (ProcessLookupError, PermissionError, OSError):
        return False
    if record.context and record.context != current_kube_context():
        return False
    return bool(portfwd.port_listeners(port))


def ensure_port_forward(name: str) -> None:
    """Ensure the named dev port-forward is up, starting it only if needed.

    The routine that needs a forward should call this rather than relying on a
    developer having started one by hand -- a hand-started forward does not
    survive the next dev-up/dev-restart/dev-down (see POSTGRES_PORT_FORWARD_PID)
    and nothing detects its absence until a confusing downstream failure.

    Modelled on tmi_common.ensure_oauth_stub: probe, return quietly if already
    healthy, otherwise start and let the underlying helpers hard-fail loudly.
    Idempotent by design, so callers can invoke it unconditionally without
    churning forwards that dev-up already established.

    Hard-fails (via _preflight_port) if an unrelated process holds the port,
    rather than binding elsewhere or proceeding against the wrong target.
    """
    if name not in _FORWARDS:
        log_error(f"Unknown port-forward '{name}' (known: {', '.join(sorted(_FORWARDS))})")
        sys.exit(1)
    if _forward_is_healthy(name):
        log_info(f"Port-forward already running: {name}")
        return
    log_info(f"Port-forward for {name} not running, starting it...")
    if name == "server":
        start_server_port_forward()
    elif name == "redis":
        start_redis_port_forward()
    else:
        start_postgres_port_forward()
    # The supervisor is spawned asynchronously, so the port is not bound the
    # instant the starter returns -- measured ~3s for postgres. Without this
    # wait the contract would be "started", not "usable", and an immediate
    # connect would race it. ensure_oauth_stub does the same for the same
    # reason. Callers may then connect as soon as this returns.
    wait_for_port(_FORWARDS[name][1], timeout=30, label=f"{name} port-forward")


def _stop_port_forward_pidfile(pid_path: str) -> None:
    record = portfwd.read_pidfile(pid_path)
    if record is not None:
        try:
            # Supervised forwards are session leaders (pgid == pid): signal the
            # whole group so the kubectl child dies with the supervisor shell.
            # A legacy bare forward (pgid != pid) gets a single-process signal
            # so we never tear down an unrelated process group.
            if os.getpgid(record.pid) == record.pid:
                os.killpg(record.pid, signal.SIGTERM)
            else:
                os.kill(record.pid, signal.SIGTERM)
            log_info(f"Stopped port-forward (PID {record.pid})")
        except ProcessLookupError:
            pass
    Path(pid_path).unlink(missing_ok=True)
    # #580: the pidfile alone cannot reach an orphan that was reparented to
    # init after losing its pidfile entirely (stale/lost file, crash before
    # write). Reap anything still carrying this pidfile's marker so it can't
    # keep squatting on the port after this "stop".
    reaped = portfwd.reap_supervisors(portfwd.marker_for(pid_path))
    if reaped:
        log_info(f"Reaped {len(reaped)} orphaned port-forward supervisor(s) for {Path(pid_path).name}")


def stop_port_forward() -> None:
    _stop_port_forward_pidfile(PORT_FORWARD_PID)
    _stop_port_forward_pidfile(REDIS_PORT_FORWARD_PID)
    # Tear the on-demand postgres forward down deliberately here, by pidfile,
    # rather than leaving it to the legacy reaper below — whose pattern does
    # match it, but which would report it as an "orphan" and, being a blanket
    # sweep, gives no way to distinguish a tracked forward from a stray one.
    _stop_port_forward_pidfile(POSTGRES_PORT_FORWARD_PID)
    # Legacy tier (#580): reap any kubectl port-forward supervisor for a
    # tmi-platform Service that predates marker-based pidfile tracking
    # entirely — e.g. a forward started before this module existed, or one
    # whose pidfile was lost/removed out-of-band. The pattern is deliberately
    # narrow: it requires the literal substring "-n tmi-platform port-forward
    # svc/" (namespace flag immediately followed by the port-forward verb and
    # a Service target) to appear in the command line, in that exact order —
    # a combination that only ever appears in our own dev port-forward
    # supervisors. reap_supervisors additionally requires the "while true"
    # supervisor-loop shape (or the kubectl-forward shape) and pgid isolation
    # from our own group before signaling anything (see portfwd.py).
    reaped = portfwd.reap_supervisors("-n tmi-platform port-forward svc/")
    if reaped:
        log_info(f"Reaped {len(reaped)} orphaned legacy port-forward supervisor(s)")


# ---------------------------------------------------------------------------
# New helpers (consumed by devstatus / devenv nuke)
# ---------------------------------------------------------------------------

def tail_server_logs() -> None:
    """Stream the tmi-server pod logs (Ctrl-C to stop)."""
    kubectl(["-n", NS, "logs", "-f", "deploy/tmi-server", "--tail=200"], check=False)


def remove_local_images(db: str, cluster_target: str = "docker-desktop") -> None:
    """Remove the locally-built dev images (used by `devenv.py nuke`)."""
    for name, _df, _args in image_builds_for(db):
        run_cmd(["docker", "rmi", "-f", cluster.local_image_ref(name, cluster=cluster_target)],
                check=False)


def server_http_status() -> tuple[bool, str]:
    """Return (reachable, http_code) for the server at localhost:8080 (via port-forward)."""
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

def start(*, db: str, cluster_target: str = "docker-desktop",
          skip_context_guard: bool = False) -> None:
    """Build images, deploy all components, wait for readiness, and start port-forwards."""
    _preflight()
    _guard_context(skip_context_guard, cluster_target)
    if db == "oracle" and cluster_target == "k3s":
        log_error("DB=oracle is not supported on CLUSTER=k3s. Use CLUSTER=docker-desktop for local Oracle dev.")
        sys.exit(1)
    if cluster_target == "k3s":
        ensure_k3s_registry()          # in-cluster registry must be up before push
    # docker-desktop: no registry — build_and_push imports the images directly.
    build_and_push(db, cluster_target)
    ensure_namespace()
    apply_platform_base()
    if cluster_target in ("k3s", "docker-desktop") and db != "oracle":
        apply_incluster_postgres(cluster_target)  # in-cluster DB up before the server (AutoMigrate)
    deliver_config(cluster_target)
    create_embedding_secret()
    create_oauth_providers_secret()
    if db == "oracle":
        create_oracle_wallet_secret()
        create_oracle_db_secret()
    apply_overlay(db, cluster_target)
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


def restart(*, db: str, cluster_target: str = "docker-desktop",
            skip_context_guard: bool = False) -> None:
    """Rebuild the server image, re-deliver config, and roll the server deployment."""
    _preflight()
    _guard_context(skip_context_guard, cluster_target)
    if cluster_target == "k3s":
        ensure_k3s_registry()
    # docker-desktop: no registry — build_and_push imports the images directly.
    build_and_push(db, cluster_target)
    deliver_config(cluster_target)
    # Re-delivered on every restart for the same reason config.yml is: the local
    # file is the source of truth, so editing it and running dev-restart applies
    # the change (#791).
    create_oauth_providers_secret()
    if db == "oracle":
        create_oracle_wallet_secret()
        create_oracle_db_secret()
    apply_overlay(db, cluster_target)
    kubectl(["-n", NS, "rollout", "restart", "deploy/tmi-server"])
    kubectl(["-n", NS, "rollout", "status", "deploy/tmi-server", f"--timeout={server_rollout_timeout(db)}"])
    start_redis_port_forward()
    if cluster_target in ("k3s", "docker-desktop"):
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

    Deliberately NOT removed: Secret/tmi-oauth-providers. Deleting it would make
    every dev-down/dev-reset drop the cluster's OAuth provider config, which is
    the exact failure this secret exists to prevent (#791). start() re-creates it
    from .local/oauth-providers.env anyway, so leaving it in place merely keeps
    the environment working between a down and the next up.
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

    log_success("Dev environment torn down (cluster left intact)")


def teardown_namespace() -> None:
    """Hard reset for a cluster we don't own (k3s, docker-desktop): delete the entire
    tmi-platform namespace (all workloads, the in-cluster registry, and the Postgres
    PVC/data). Never touches the cluster itself."""
    stop_port_forward()
    kubectl(["delete", "namespace", NS, "--ignore-not-found", "--wait=true"])
    log_success(f"Namespace {NS} deleted (hard reset)")
