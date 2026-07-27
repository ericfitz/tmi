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

Also runs two preflight checks the legacy script had (directly or
indirectly) that the plugin's own preflight doesn't cover: the CATS refData
file must already exist (`make cats-seed` writes it; see `_check_ref_data`),
and the active kubectl context must look like a local dev cluster before
`kubectl exec` is used to reach Redis (see `_check_kube_context`).

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

from cluster import is_local_kube_context  # noqa: E402
from tmi_common import get_project_root, log_error, log_info, log_success  # noqa: E402

REDIS_NAMESPACE = "tmi-platform"
REDIS_DOCKER_CONTAINER = "tmi-redis"
REDIS_DEPLOYMENT = "redis"

# Where dbtool writes the CATS refData file (test/seeds/cats-seed-data.json's
# output.reference_yaml). Duplicated from .local/cats/config.yaml's
# `cats.ref_data` rather than read from it, since this script has no
# dependency on the plugin's config module and the path is a fixed repo
# convention, not something a developer is expected to change per-run.
REF_DATA_RELPATH = Path("test") / "results" / "cats" / "cats-test-data.yml"


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
    parser.add_argument(
        "--allow-any-context",
        action="store_true",
        default=False,
        help="Skip the local-kube-context safety check (see _check_kube_context)",
    )
    return parser.parse_args()


# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------


def _check_ref_data() -> None:
    """Fail fast if the CATS refData file dbtool writes during seeding is missing.

    The portable plugin's own preflight (catslib.runner.checks) verifies spec,
    cats binary, rules and server health -- not refData. Without this check, a
    missing or stale refData file reaches CATS as `--refData=<nonexistent>`, and
    the run silently degrades into a campaign of 404s that reads as real
    findings rather than failing loudly. This matters most on any --skip-seed
    path (e.g. `make cats-fuzz-oci`), since those skip the plugin's own `seed`
    hook entirely and rely on refData already being present from a prior seed.
    Mirrors the guard the legacy run-cats-fuzz.py had at lines 452-456.
    """
    ref_data = get_project_root() / REF_DATA_RELPATH
    if not ref_data.exists():
        log_error(f"CATS refData file not found: {ref_data}")
        log_error("Run 'make cats-seed' first to create test data.")
        sys.exit(1)


def _current_kube_context() -> str | None:
    """Return `kubectl config current-context`, or None if it can't be determined."""
    try:
        proc = subprocess.run(
            ["kubectl", "config", "current-context"],
            capture_output=True, text=True, check=False,
        )
    except FileNotFoundError:
        return None
    if proc.returncode != 0:
        return None
    return proc.stdout.strip() or None


def _check_kube_context(allow_any_context: bool) -> None:
    """Log the active kubectl context and refuse to touch Redis outside a
    recognized local dev cluster, unless overridden.

    This script clears Redis rate-limit keys via `kubectl exec`, which acts on
    whatever context is currently active -- not necessarily "the machine this
    script runs on". A developer whose context happens to be pointed at a
    shared/remote cluster (this repo's own dev session hit exactly this: the
    active context was an AWS EKS cluster) would otherwise silently mutate
    that cluster's Redis as a side effect of running CATS locally.

    Uses `cluster.is_local_kube_context` -- the same predicate scripts/devenv.py
    uses to decide whether a `make dev-up` needs `--yes` -- rather than a second,
    independently-maintained list. That predicate exact-matches "k3s" (a generic
    local context name) but NOT this repo's own K3S_CONTEXT = "k3s-rp" (a real,
    remote home-lab cluster devenv.py itself requires --yes to touch); a
    substring match here previously auto-approved it, which was actually looser
    than the rest of the project's own safety bar for that exact cluster.
    """
    context = _current_kube_context()
    log_info(f"Active kubectl context: {context or '(unknown)'}")
    if allow_any_context or context is None:
        return
    if is_local_kube_context(context):
        return
    log_error(
        f"Active kubectl context {context!r} does not look like a local dev cluster "
        "(cluster.is_local_kube_context returned False). Refusing to run `kubectl exec` "
        "against it -- this would clear Redis rate-limit keys on whatever cluster that "
        "context points at. Switch to your local dev cluster's context, or pass "
        "--allow-any-context to override."
    )
    sys.exit(1)


# ---------------------------------------------------------------------------
# Redis target detection
# ---------------------------------------------------------------------------


def _detect_redis_target(*, allow_any_context: bool) -> list[str] | None:
    """Return an exec command prefix for reaching Redis, or None if unreachable.

    Prefers the legacy `docker exec tmi-redis` container if it is actually
    running, otherwise falls back to `kubectl exec` against the in-cluster
    Redis Deployment that `make dev-up` deploys. The kubectl fallback acts on
    whatever context happens to be active, so `_check_kube_context` runs right
    before committing to that path -- not unconditionally -- since a Docker
    container being available makes the active kube context irrelevant.
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
            _check_kube_context(allow_any_context)
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

    Auth is passed via the REDISCLI_AUTH env var rather than `-a`: `-a` puts
    the password on redis-cli's own argv, which is visible to `ps` inside the
    container for the life of that call (this is also why `-a` normally
    prints a "using a password on the command line" warning -- REDISCLI_AUTH
    doesn't trigger it, so --no-auth-warning is no longer needed either).
    """
    quoted = " ".join(shlex.quote(a) for a in cli_args)
    script = (
        'if [ -n "$REDIS_PASSWORD" ]; then '
        f'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli {quoted}; '
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

    _check_ref_data()

    log_info("Preparing CATS test environment: clearing rate-limit keys...")

    target = _detect_redis_target(allow_any_context=args.allow_any_context)
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
