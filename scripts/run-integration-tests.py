# /// script
# requires-python = ">=3.11"
# ///
"""Run TMI integration tests with formatted output.

Drives the existing PG (and optionally OCI) integration test entry points:
captures `go test -v` output to a temp file, parses it, prints failed-test
verbose output and a SUMMARY block, and exits with a non-zero code if any
package, test, or subtest failed.

Replaces the bash wrappers' `tee | grep` exit-code path which silently
masked failures (tee always exits 0 without `set -o pipefail`).

Targets:
  pg   — run integration + workflow tests against the dev PostgreSQL DB
  oci  — run integration tests against Oracle ADB (requires oci-env.sh)

Both targets require `make start-dev` (or `start-dev-oci`) to be running.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
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


def clear_redis_rate_limits() -> None:
    """Best-effort: drop auth/IP rate-limit keys from the dev Redis."""
    if not shutil.which("docker"):
        return
    for pattern in ("auth:ratelimit:*", "ip:ratelimit:*"):
        try:
            scan = subprocess.run(
                ["docker", "exec", "tmi-redis", "redis-cli", "--scan", "--pattern", pattern],
                capture_output=True, text=True, check=False,
            )
            keys = [k for k in scan.stdout.splitlines() if k.strip()]
            if not keys:
                continue
            subprocess.run(
                ["docker", "exec", "-i", "tmi-redis", "redis-cli", "DEL", *keys],
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


def run_pg(project_root: Path, log_path: str) -> int:
    server_url = "http://localhost:8080"
    if not server_is_running(f"{server_url}/"):
        log_error(f"TMI server is not running on {server_url}")
        log_info("Start the server first with: make start-dev")
        return 2

    log_info("Server is ready")

    oauth_running = ensure_oauth_stub(project_root)
    if not oauth_running:
        log_warn("OAuth stub not available — workflow tests will be skipped")

    base_env = {
        **os.environ,
        "LOGGING_IS_TEST": "true",
        "TEST_DB_HOST": "localhost",
        "TEST_DB_PORT": "5432",
        "TEST_DB_USER": "tmi_dev",
        "TEST_DB_PASSWORD": "dev123",
        "TEST_DB_NAME": "tmi_dev",
        "TEST_REDIS_HOST": "localhost",
        "TEST_REDIS_PORT": "6379",
        "TEST_SERVER_URL": server_url,
    }

    log_info("Running api/ integration tests")
    api_cmd = [
        "go", "test", "-v", "-timeout=10m", "-tags=test",
        "./api/...", "-run", "Integration",
    ]
    api_exit = run_go_test(api_cmd, project_root, base_env, log_path)

    workflow_exit = 0
    if oauth_running:
        clear_redis_rate_limits()
        log_info("Running workflow integration tests")
        wf_env = {**base_env, "INTEGRATION_TESTS": "true", "TMI_SERVER_URL": server_url}
        wf_cmd = ["go", "test", "-v", "-timeout=15m", "-p", "1", "./workflows/..."]
        # The workflows package is a separate module under test/integration.
        workflow_exit = run_go_test(
            wf_cmd, project_root / "test" / "integration", wf_env, log_path,
        )

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
        log_info("Start the server first with: make start-dev-oci")
        return 2

    log_info("Server is ready")
    ensure_oauth_stub(project_root)

    # Source oci-env.sh and run go test in the resulting environment. This
    # mirrors the original bash wrapper's behavior — the env file sets
    # DYLD_LIBRARY_PATH, TNS_ADMIN, ORACLE_PASSWORD, etc.
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
    return result.returncode


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
