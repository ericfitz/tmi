# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Run TMI integration tests with formatted output.

Drives the existing PG (and optionally OCI) integration test entry points:
captures `go test -v` output to a temp file, parses it, prints failed-test
verbose output and a SUMMARY block, and exits with a non-zero code if any
package, test, or subtest failed.

Replaces the bash wrappers' `tee | grep` exit-code path which silently
masked failures (tee always exits 0 without `set -o pipefail`).

Targets:
  pg   — run integration + workflow tests against the ISOLATED test PostgreSQL
         container (tmi-postgresql-test @ config-test.yml's port, db tmi_test),
         never the dev DB (#477). This target is self-contained: it brings up
         and migrates the isolated test container, points the api/ suite's
         direct DB connection (TEST_DB_*) at it, and launches a dedicated
         tmiserver bound to it (TMI_DATABASE_URL) for the workflow tests. It
         does NOT require `make dev-up` and never touches the dev container.
  oci  — run integration tests against Oracle ADB (requires oci-env.sh)
         Server started with config-test.yml + TMI_DATABASE_URL=oracle://...
         (TMI_DATABASE_URL set by scripts/oci-env.sh via ORACLE_CONNECT_STRING)
         Requires `make dev-up DB=oracle` to be running.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import database  # noqa: E402
from tmi_common import (
    add_verbosity_args,
    apply_verbosity,
    config_get,
    get_project_root,
    load_config,
    log_error,
    log_info,
    log_success,
    log_warn,
)
from tmi_test_runner import extract_failed_test_output, parse_output, print_results


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run TMI integration tests with formatted output."
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "--target",
        choices=["pg", "oci"],
        default="pg",
        help="Backend target: pg (PostgreSQL, default) or oci (Oracle ADB)",
    )
    return parser.parse_args()


def server_is_running(url: str) -> bool:
    try:
        with urllib.request.urlopen(url, timeout=2) as resp:
            return resp.status < 500
    except Exception:
        return False


def ensure_oauth_stub(project_root: Path) -> bool:
    """Best-effort: try to start the OAuth stub via scripts/oauth-stub-lib.sh.

    Returns True if the stub is reachable on http://localhost:8079/, False
    otherwise. Workflow tests are skipped when the stub is not running.
    """
    stub_url = "http://localhost:8079/"
    if server_is_running(stub_url):
        return True

    # Source the helper and call ensure_oauth_stub from a subshell.
    helper = project_root / "scripts" / "oauth-stub-lib.sh"
    if not helper.exists():
        return False
    try:
        subprocess.run(
            ["bash", "-c", f"source '{helper}' && ensure_oauth_stub"],
            cwd=str(project_root),
            check=False,
            capture_output=True,
        )
    except OSError:
        return False
    return server_is_running(stub_url)


def clear_redis_rate_limits(redis_db: str = "0") -> None:
    """Best-effort: drop auth/IP rate-limit keys from the test Redis logical DB.

    Targets the test logical DB (``-n redis_db``) so it never touches dev's
    keyspace (DB 0). dev and test share the Redis container but are isolated by
    logical DB index (#477).
    """
    if not shutil.which("docker"):
        return
    for pattern in ("auth:ratelimit:*", "ip:ratelimit:*"):
        try:
            scan = subprocess.run(
                ["docker", "exec", "tmi-redis", "redis-cli", "-n", redis_db,
                 "--scan", "--pattern", pattern],
                capture_output=True, text=True, check=False,
            )
            keys = [k for k in scan.stdout.splitlines() if k.strip()]
            if not keys:
                continue
            subprocess.run(
                ["docker", "exec", "-i", "tmi-redis", "redis-cli", "-n", redis_db,
                 "DEL", *keys],
                check=False, capture_output=True,
            )
        except OSError:
            return


def run_go_test(cmd: list[str], cwd: Path, env: dict, log_path: str) -> int:
    """Run go test, append all output to log_path, return its exit code."""
    with open(log_path, "a") as fh:
        result = subprocess.run(
            cmd,
            stdout=fh,
            stderr=subprocess.STDOUT,
            check=False,
            cwd=str(cwd),
            env=env,
        )
    return result.returncode


def wait_for_server(url: str, timeout: int = 60) -> bool:
    """Poll url until it answers (status < 500) or timeout elapses."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if server_is_running(url):
            return True
        time.sleep(1)
    return False


def ensure_redis(project_root: Path) -> None:
    """Best-effort: ensure a Redis is listening on localhost:6379.

    The isolated test server and the integration suite use Redis; config-test.yml
    selects logical DB 1 for test isolation. Postgres is the only store #477
    isolates by container, so Redis intentionally reuses the local instance.
    """
    scripts_dir = project_root / "scripts"
    try:
        subprocess.run(
            ["uv", "run", str(scripts_dir / "manage-redis.py"), "start"],
            cwd=str(project_root), check=False, capture_output=True,
        )
    except OSError:
        log_warn("Could not start Redis; tests needing Redis may fail")


TEST_SERVER_CONTAINER = "tmi-server-test"
TEST_SERVER_IMAGE = "tmi/tmi-server:latest"
TEST_SERVER_HOST_PORT = "8081"


def build_server_image(project_root: Path) -> bool:
    """Build the dev-tagged test server image (tmi/tmi-server:latest).

    The `dev` build tag compiles in login_hint + the built-in tmi OAuth provider
    the workflow tests rely on. Returns True on success.
    """
    log_info("Building the test server container image (BUILD_TAGS=dev)...")
    try:
        result = subprocess.run(
            ["uv", "run", "scripts/build-app-containers.py",
             "--target", "local", "--component", "server", "--build-tags", "dev"],
            cwd=str(project_root), check=False,
        )
    except OSError as exc:
        log_error(f"Failed to invoke image build: {exc}")
        return False
    return result.returncode == 0


def stop_test_server_container() -> None:
    """Remove the test server container (best-effort)."""
    if not shutil.which("docker"):
        return
    subprocess.run(
        ["docker", "rm", "-f", TEST_SERVER_CONTAINER],
        check=False, capture_output=True,
    )


def start_test_server_container(
    project_root: Path, config_path: Path, container_db_url: str,
    redis_host: str, redis_port: str, host_port: str,
    *, disable_rate_limiting: bool = True,
    force_auth_flow_rate_limiting: bool = False,
) -> str | None:
    """Start the test server in a Docker container, mirroring the dev pod topology.

    The container reaches all host-side dependencies (test DB, Redis, OAuth stub,
    webhook receiver) via host.docker.internal, and the host reaches the server
    via the published port. Because host→server traffic is NAT'd through the
    Docker bridge, the server sees a NON-loopback client IP — so IP/auth
    rate-limiting and the webhook SSRF path behave exactly as in dev-up. The
    workflow tests depend on this; a host-run loopback server cannot satisfy
    them (#477).

    Rate limiting is disabled by default (like the dev server): during testing,
    rate limits are a liability that flakes unrelated tests. Set the runner env
    TMI_TEST_ENABLE_RATE_LIMITING=true to keep it on (e.g. when explicitly
    exercising the rate-limit workflow tests).

    The auth-flow limiter additionally no-ops in build_mode=test (which the
    container runs for the built-in tmi provider), so force_auth_flow_rate_limiting
    sets the server-side override that enforces it anyway — required for the
    auth-flow multi-scope workflow test to actually run instead of skip.

    Returns the container name, or None on failure.
    """
    stop_test_server_container()
    cmd = [
        "docker", "run", "-d", "--name", TEST_SERVER_CONTAINER,
        "--add-host", "host.docker.internal:host-gateway",
        "-p", f"{host_port}:8080",
        "-v", f"{config_path}:/etc/tmi/config.yml:ro",
        "-e", f"TMI_DATABASE_URL={container_db_url}",
        "-e", f"TMI_REDIS_HOST={redis_host}",
        "-e", f"TMI_REDIS_PORT={redis_port}",
        "-e", "TMI_SERVER_INTERFACE=0.0.0.0",
        "-e", "TMI_SERVER_PORT=8080",
        "-e", "LOGGING_IS_TEST=true",
        # The server builds self-referential OAuth callback URLs from this. The
        # container listens on 8080 internally but the host reaches it on the
        # published port, so advertise the host-reachable URL or the OAuth
        # callback the tests follow would point at an unreachable port.
        "-e", f"TMI_OAUTH_CALLBACK_URL=http://localhost:{host_port}/oauth2/callback",
        # Dev/test toggles mirrored from deployments/k8s/dev/server.yml: the tmi
        # OAuth provider, first-user admin auto-promote, http webhook targets, and
        # the SSRF allowlist for the host-run webhook receiver (reached, like the
        # dev pod, via host.docker.internal).
        "-e", "OAUTH_PROVIDERS_TMI_ENABLED=true",
        "-e", "TMI_AUTH_AUTO_PROMOTE_FIRST_USER=true",
        "-e", "TMI_WEBHOOK_ALLOW_HTTP_TARGETS=true",
        "-e", "TMI_SSRF_WEBHOOK_ALLOWLIST=host.docker.internal",
        # The OAuth flow drives the callback stub at localhost:8079; allowlist it
        # here (tracked runner) rather than in the gitignored config-test.yml, so
        # the harness is reproducible. Comma-separated; overrides the file value.
        "-e", "TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST="
        "http://localhost:8079/,http://localhost:8079/*,http://localhost:4200/*",
    ]
    if disable_rate_limiting:
        cmd += ["-e", "TMI_DISABLE_RATE_LIMITING=true"]
    if force_auth_flow_rate_limiting:
        cmd += ["-e", "TMI_TEST_FORCE_AUTH_FLOW_RATE_LIMITING=true"]
    cmd += [TEST_SERVER_IMAGE, "--config=/etc/tmi/config.yml"]
    try:
        result = subprocess.run(
            cmd, cwd=str(project_root), check=False, capture_output=True, text=True,
        )
    except OSError as exc:
        log_error(f"Failed to start test server container: {exc}")
        return None
    if result.returncode != 0:
        log_error(f"docker run failed: {result.stderr.strip()}")
        return None
    return TEST_SERVER_CONTAINER


def dump_test_server_logs(server_log: str) -> None:
    """Capture the test server container logs to a file for debugging."""
    if not shutil.which("docker"):
        return
    try:
        with open(server_log, "w") as fh:
            subprocess.run(
                ["docker", "logs", TEST_SERVER_CONTAINER],
                check=False, stdout=fh, stderr=subprocess.STDOUT,
            )
    except OSError:
        pass


def run_pg(project_root: Path, log_path: str) -> int:
    # Bring up the ISOLATED test database container (tmi-postgresql-test on the
    # config-test.yml port, db tmi_test) and migrate it. This never touches the
    # dev container, and does not require `make dev-up` (#477).
    test_cfg = project_root / "config-test.yml"
    profile = database.test_profile(str(test_cfg))

    log_info(
        f"Starting isolated test DB container '{profile.container}' on port {profile.port}"
    )
    database.up(profile)
    database.wait(profile, timeout=120)
    log_info("Running migrations against the isolated test DB")
    database.migrate(profile)

    ensure_redis(project_root)

    db_host = "localhost"
    db_port = str(profile.port)
    db_user = profile.user
    db_password = profile.password
    db_name = profile.database
    db_url = (
        f"postgres://{db_user}:{db_password}@{db_host}:{db_port}/{db_name}?sslmode=disable"
    )

    # Redis is shared with dev by container but isolated by logical DB index:
    # config-test.yml selects DB 1 for tests (dev uses DB 0). Keep config-test.yml
    # authoritative and propagate the same index to the test helpers via
    # TEST_REDIS_DB so direct test connections never collide with dev (#477).
    raw_test_cfg = load_config(test_cfg)
    redis_host = str(config_get(raw_test_cfg, "database.redis.host") or "localhost")
    redis_port = str(config_get(raw_test_cfg, "database.redis.port") or "6379")
    redis_db = str(config_get(raw_test_cfg, "database.redis.db") or "0")

    base_env = {
        **os.environ,
        "TMI_DATABASE_URL": db_url,
        "LOGGING_IS_TEST": "true",
        "TEST_DB_HOST": db_host,
        "TEST_DB_PORT": db_port,
        "TEST_DB_USER": db_user,
        "TEST_DB_PASSWORD": db_password,
        "TEST_DB_NAME": db_name,
        # framework.NewDevDatabase() reads TEST_DEV_DB_PORT (default 5432) for
        # the "same DB the server uses" cleanup connection. The server now uses
        # the isolated test DB, so point this at the test port too (#477).
        "TEST_DEV_DB_PORT": db_port,
        "TEST_REDIS_HOST": redis_host,
        "TEST_REDIS_PORT": redis_port,
        "TEST_REDIS_DB": redis_db,
        # The webhook receiver binds on the host; advertise it as
        # host.docker.internal so the containerized test server reaches it the
        # same way the dev pod does (and the SSRF deny-list, which globs literal
        # hostnames, doesn't block it) (#477).
        "TEST_WEBHOOK_ADVERTISE_HOST": "host.docker.internal",
    }

    # Optional: restrict the workflow run to tests matching a regex (e.g. to
    # iterate on one workflow). When set, the api/ suite is skipped so the run
    # is fast and focused.
    workflow_run = os.environ.get("TMI_TEST_WORKFLOW_RUN", "").strip()

    # The api/ integration suite connects directly to TEST_DB_* (and spins up
    # in-process httptest servers where needed), so it is self-contained — no
    # external server required.
    api_exit = 0
    if workflow_run:
        log_info(f"TMI_TEST_WORKFLOW_RUN={workflow_run} set — skipping api/ suite")
    else:
        log_info("Running api/ integration tests against the isolated test DB")
        api_cmd = [
            "go", "test", "-v", "-timeout=10m", "-tags=test",
            "./api/...", "-run", "Integration",
        ]
        api_exit = run_go_test(api_cmd, project_root, base_env, log_path)

    # The workflow tests drive a live HTTP server. Run a dedicated server
    # CONTAINER bound to the isolated test DB (never the dev-up server),
    # mirroring the dev pod topology so non-loopback-dependent behaviour
    # (rate-limiting, webhook SSRF) works (#477).
    workflow_exit = 0
    server_log = str((project_root / "logs" / "tmi-test-server.log"))
    (project_root / "logs").mkdir(exist_ok=True)
    oauth_running = ensure_oauth_stub(project_root)
    if not oauth_running:
        log_warn("OAuth stub not available — workflow tests will be skipped")
    elif not build_server_image(project_root):
        log_warn("test server image build failed — workflow tests will be skipped")
    else:
        server_url = f"http://localhost:{TEST_SERVER_HOST_PORT}"
        # The container reaches the host-published test DB and dev Redis via
        # host.docker.internal (its own localhost is the container, not the host).
        container_db_url = (
            f"postgres://{db_user}:{db_password}@host.docker.internal:{db_port}"
            f"/{db_name}?sslmode=disable"
        )
        # Rate limiting is a testing liability (flakes unrelated tests), so the
        # test server disables it by default — matching the dev server. Opt back
        # in with TMI_TEST_ENABLE_RATE_LIMITING=true to run the rate-limit
        # workflow tests (which otherwise skip via IsRateLimitingActive).
        enable_rl = os.environ.get("TMI_TEST_ENABLE_RATE_LIMITING", "").lower() == "true"
        container = start_test_server_container(
            project_root, test_cfg, container_db_url,
            "host.docker.internal", redis_port, TEST_SERVER_HOST_PORT,
            disable_rate_limiting=not enable_rl,
            force_auth_flow_rate_limiting=enable_rl,
        )
        try:
            if container is None or not wait_for_server(f"{server_url}/", timeout=90):
                dump_test_server_logs(server_log)
                log_warn(
                    f"Test server did not become ready (see {server_log}) — "
                    "skipping workflow tests"
                )
            else:
                log_info(f"Test server ready on {server_url}")
                clear_redis_rate_limits(redis_db)
                log_info("Running workflow integration tests against the isolated test server")
                wf_env = {
                    **base_env,
                    "INTEGRATION_TESTS": "true",
                    "TMI_SERVER_URL": server_url,
                    "TEST_SERVER_URL": server_url,
                }
                wf_cmd = ["go", "test", "-v", "-timeout=15m", "-p", "1", "./workflows/..."]
                if workflow_run:
                    wf_cmd += ["-run", workflow_run]
                # The workflows package is a separate module under test/integration.
                workflow_exit = run_go_test(
                    wf_cmd, project_root / "test" / "integration", wf_env, log_path,
                )
        finally:
            dump_test_server_logs(server_log)
            stop_test_server_container()

    return api_exit if api_exit != 0 else workflow_exit


def run_oci(project_root: Path, log_path: str) -> int:
    oci_env_file = project_root / "scripts" / "oci-env.sh"
    if not oci_env_file.exists():
        log_error(f"OCI environment file not found: {oci_env_file}")
        log_info("Set up with: cp scripts/oci-env.sh.example scripts/oci-env.sh")
        return 2

    server_url = "http://localhost:8080"
    if not server_is_running(f"{server_url}/"):
        log_error(f"TMI server is not running on {server_url}")
        log_info("Start the server first with: make dev-up DB=oracle")
        return 2

    log_info("Server is ready")
    ensure_oauth_stub(project_root)

    # Source oci-env.sh and run go test in the resulting environment. This
    # mirrors the original bash wrapper's behavior — the env file sets
    # DYLD_LIBRARY_PATH, TNS_ADMIN, ORACLE_PASSWORD, etc.
    # config-test.yml is the bootstrap config; TMI_DATABASE_URL selects the
    # Oracle ADB backend (oci-env.sh must export TMI_DATABASE_URL or set
    # ORACLE_CONNECT_STRING which the server startup script translates).
    log_info("Running api/ integration tests against OCI ADB")
    bash_cmd = (
        f"source '{oci_env_file}' && "
        "LOGGING_IS_TEST=true "
        f"TEST_SERVER_URL='{server_url}' "
        "TEST_REDIS_HOST=localhost "
        "TEST_REDIS_PORT=6379 "
        "go test -v -timeout=10m ./api/... -run Integration"
    )
    with open(log_path, "a") as fh:
        result = subprocess.run(
            ["bash", "-c", bash_cmd],
            stdout=fh,
            stderr=subprocess.STDOUT,
            check=False,
            cwd=str(project_root),
        )
    http_exit = result.returncode

    # Oracle driver-level store tests (#441) open a direct gorm-oracle connection
    # rather than talking to the running server over HTTP, so they need the
    # gorm-oracle driver compiled in — the `oracle` build tag, CGO, and the
    # Oracle Instant Client. The HTTP suite above is intentionally CGO-free, so
    # run these in a dedicated invocation (matched by name suffix
    # "OracleIntegration") instead of widening the whole suite's build tags.
    log_info("Running oracle-tagged driver-level store tests against OCI ADB")
    oracle_cmd = (
        f"source '{oci_env_file}' && "
        "LOGGING_IS_TEST=true "
        "CGO_ENABLED=1 "
        "go test -v -timeout=10m -tags oracle ./api/... -run OracleIntegration"
    )
    with open(log_path, "a") as fh:
        oracle_result = subprocess.run(
            ["bash", "-c", oracle_cmd],
            stdout=fh,
            stderr=subprocess.STDOUT,
            check=False,
            cwd=str(project_root),
        )

    return http_exit if http_exit != 0 else oracle_result.returncode


def main() -> int:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()

    fd, log_path = tempfile.mkstemp(prefix=f"tmi-test-integration-{args.target}-", dir="/tmp")
    os.close(fd)

    log_info(f"Integration tests target={args.target}; raw log={log_path}")

    if args.target == "pg":
        exit_code = run_pg(project_root, log_path)
    else:
        exit_code = run_oci(project_root, log_path)

    stats = parse_output(log_path)
    failed_output: list[str] = []
    if stats["failed"] > 0:
        failed_output = extract_failed_test_output(log_path)

    print_results(stats, failed_output, log_path, label="Integration tests")

    # Exit non-zero if either go test reported failure OR the parsed log
    # observed any FAIL line. Either signal alone must surface as failure —
    # don't trust just one (go test exit code can be 0 if nothing ran, and
    # FAIL counts can be 0 if go test crashed before any subtest).
    if exit_code != 0 or stats["failed"] > 0 or stats["pkg_fail"] > 0:
        log_error(
            f"Integration tests failed "
            f"(go test exit={exit_code}, failed_tests={stats['failed']}, failed_pkgs={stats['pkg_fail']})"
        )
        return exit_code if exit_code != 0 else 1

    log_success("All integration tests passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
