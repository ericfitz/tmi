#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""CATS `pre_run` hook: clear Redis rate-limit keys before a fuzzing run.

Extracted from disable_rate_limits()/_redis_del_pattern() in the former
run-cats-fuzz.py so the cats plugin can invoke it as an opaque `pre_run`
hook command. Does NOT touch report directories — the plugin owns run
directories now.

Redis access: the legacy script shelled out to `docker exec tmi-redis`,
which assumed Redis ran as a plain Docker container. `make dev-up` now
deploys Redis as an in-cluster Kubernetes Deployment (namespace
`tmi-platform`), so this script prefers `kubectl exec` against that
Deployment and falls back to the legacy `docker exec tmi-redis` form if
that container happens to exist instead. Some environments (e.g. the AWS
overlay) run Redis with `--requirepass`, sourced into the container's own
`REDIS_PASSWORD` env var; others (local dev) run it unauthenticated. Both
are handled by asking the container's own shell whether that env var is
set, rather than assuming one way or the other.

Usage:
    uv run scripts/cats-prep.py
    uv run scripts/cats-prep.py --user alice
"""

import argparse
import shlex
import subprocess
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Import shared helpers from scripts/lib/tmi_common.py
# ---------------------------------------------------------------------------
sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from tmi_common import log_error, log_info, log_success  # noqa: E402

REDIS_NAMESPACE = "tmi-platform"
REDIS_DOCKER_CONTAINER = "tmi-redis"
REDIS_DEPLOYMENT = "redis"


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Clear CATS rate-limit keys from Redis (cats plugin `pre_run` hook).",
    )
    parser.add_argument(
        "--user",
        metavar="USER",
        default="charlie",
        help="OAuth user login hint whose rate-limit keys should be cleared (default: charlie)",
    )
    return parser.parse_args()


# ---------------------------------------------------------------------------
# Redis target detection
# ---------------------------------------------------------------------------


def _detect_redis_target() -> list[str] | None:
    """Return an exec command prefix for reaching Redis, or None if unreachable.

    Prefers the legacy `docker exec tmi-redis` container if it is actually
    running, otherwise falls back to `kubectl exec` against the in-cluster
    Redis Deployment that `make dev-up` deploys.
    """
    try:
        docker_check = subprocess.run(
            ["docker", "ps", "--filter", f"name=^{REDIS_DOCKER_CONTAINER}$", "--format", "{{.Names}}"],
            capture_output=True, text=True, check=False,
        )
        if docker_check.returncode == 0 and REDIS_DOCKER_CONTAINER in docker_check.stdout.split():
            log_info(f"Found Docker container {REDIS_DOCKER_CONTAINER}; using docker exec")
            return ["docker", "exec", REDIS_DOCKER_CONTAINER]
    except FileNotFoundError:
        pass  # docker not installed; fall through to kubectl

    try:
        kubectl_check = subprocess.run(
            [
                "kubectl", "get", "deployment", REDIS_DEPLOYMENT,
                "-n", REDIS_NAMESPACE,
                "-o", "jsonpath={.status.readyReplicas}",
            ],
            capture_output=True, text=True, check=False,
        )
        if kubectl_check.returncode == 0 and kubectl_check.stdout.strip() not in ("", "0"):
            log_info(
                f"Found deployment/{REDIS_DEPLOYMENT} in namespace {REDIS_NAMESPACE}; "
                "using kubectl exec"
            )
            return ["kubectl", "exec", "-n", REDIS_NAMESPACE, f"deploy/{REDIS_DEPLOYMENT}", "--"]
    except FileNotFoundError:
        pass  # kubectl not installed

    return None


def _redis_cli_shell(cli_args: list[str]) -> list[str]:
    """Build a shell invocation that runs redis-cli with auth iff the
    container's own REDIS_PASSWORD env var is set.

    This lets one code path handle both authenticated Redis (e.g. the AWS
    overlay, which sets --requirepass from a Secret-sourced REDIS_PASSWORD
    env var) and unauthenticated Redis (local dev) without this script ever
    needing to know or carry the password itself — it is resolved by the
    container's own shell, not interpolated by us.
    """
    quoted = " ".join(shlex.quote(a) for a in cli_args)
    script = (
        'if [ -n "$REDIS_PASSWORD" ]; then '
        f'redis-cli -a "$REDIS_PASSWORD" --no-auth-warning {quoted}; '
        f"else redis-cli {quoted}; fi"
    )
    return ["sh", "-c", script]


def _redis_del_pattern(target: list[str], pattern: str) -> int:
    """Scan and delete Redis keys matching *pattern* via the given exec target.

    Returns the number of keys deleted. Raises RuntimeError if redis-cli
    itself fails (connection refused, auth error, etc.) so the caller can
    report an actionable failure rather than silently reporting zero.
    """
    scan = subprocess.run(
        target + _redis_cli_shell(["--scan", "--pattern", pattern]),
        capture_output=True, text=True, check=False,
    )
    if scan.returncode != 0:
        detail = scan.stderr.strip() or scan.stdout.strip() or f"exit code {scan.returncode}"
        raise RuntimeError(f"redis-cli scan failed for pattern {pattern!r}: {detail}")

    keys = scan.stdout.strip()
    if not keys:
        return 0

    key_list = keys.split()
    dele = subprocess.run(
        target + _redis_cli_shell(["DEL", *key_list]),
        capture_output=True, text=True, check=False,
    )
    if dele.returncode != 0:
        detail = dele.stderr.strip() or dele.stdout.strip() or f"exit code {dele.returncode}"
        raise RuntimeError(f"redis-cli DEL failed for pattern {pattern!r}: {detail}")

    return len(key_list)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> None:
    args = parse_args()

    log_info("Preparing CATS test environment: clearing rate-limit keys...")

    target = _detect_redis_target()
    if target is None:
        log_error(
            f"Could not reach Redis via docker (container {REDIS_DOCKER_CONTAINER!r}) "
            f"or kubectl (deployment/{REDIS_DEPLOYMENT} in namespace {REDIS_NAMESPACE}). "
            "CATS requests would be rate-limited without this step; ensure the dev "
            "environment is running (make dev-up) and kubectl/docker are on PATH."
        )
        sys.exit(1)

    patterns = [
        "ip:ratelimit:*:127.0.0.1",
        "ip:ratelimit:*:::1",
        f"auth:ratelimit:*:{args.user}*",
        "*ratelimit*",
    ]

    total_cleared = 0
    try:
        for pattern in patterns:
            cleared = _redis_del_pattern(target, pattern)
            if cleared:
                log_info(f"Cleared {cleared} key(s) matching {pattern!r}")
            total_cleared += cleared
    except RuntimeError as exc:
        log_error(str(exc))
        sys.exit(1)

    if total_cleared:
        log_success(f"Rate-limit cleanup complete: {total_cleared} key(s) cleared")
    else:
        log_success("Rate-limit cleanup complete: no matching keys were present")


if __name__ == "__main__":
    main()
