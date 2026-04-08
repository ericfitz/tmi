#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""CATS API Fuzzing Script with OAuth Integration.

Automates OAuth authentication and runs CATS fuzzing against the TMI API.
Replaces the former run-cats-fuzz.sh script.

Usage:
    uv run scripts/run-cats-fuzz.py
    uv run scripts/run-cats-fuzz.py --user alice --path /addons
    uv run scripts/run-cats-fuzz.py --oci --skip-seed
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

# ---------------------------------------------------------------------------
# Import shared helpers from scripts/lib/tmi_common.py
# ---------------------------------------------------------------------------
sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    ensure_oauth_stub,
    get_project_root,
    log_error,
    log_info,
    log_success,
    log_warn,
    run_cmd,
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

OAUTH_STUB_PORT = 8079
OAUTH_STUB_URL = f"http://localhost:{OAUTH_STUB_PORT}"
OPENAPI_SPEC = "api-schema/tmi-openapi.json"
HTTP_METHODS = "POST,PUT,GET,DELETE,PATCH"
DEFAULT_MAX_REQUESTS_PER_MINUTE = 3000


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run CATS API fuzzing with OAuth authentication against TMI.",
    )
    parser.add_argument(
        "--user",
        metavar="USER",
        default="charlie",
        help="OAuth user login hint (default: charlie)",
    )
    parser.add_argument(
        "--server",
        metavar="URL",
        default="http://localhost:8080",
        help="TMI server URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "--path",
        metavar="PATH",
        default="",
        help="Restrict fuzzing to a specific endpoint path (e.g. /addons)",
    )
    parser.add_argument(
        "--rate",
        metavar="N",
        type=int,
        default=DEFAULT_MAX_REQUESTS_PER_MINUTE,
        help=f"Max requests per minute (default: {DEFAULT_MAX_REQUESTS_PER_MINUTE})",
    )
    parser.add_argument(
        "--blackbox",
        action="store_true",
        default=False,
        help="Ignore all error codes other than 500",
    )
    add_config_arg(parser)
    parser.add_argument(
        "--oci",
        action="store_true",
        default=False,
        help="Use OCI Autonomous Database configuration",
    )
    parser.add_argument(
        "--provider",
        metavar="IDP",
        default="tmi",
        help="OAuth identity provider (default: tmi)",
    )
    parser.add_argument(
        "--skip-seed",
        action="store_true",
        default=False,
        help="Skip database seeding (assumes data already exists)",
    )
    parser.add_argument(
        "--skip-parse",
        action="store_true",
        default=False,
        help="Skip automatic result parsing after CATS completes",
    )
    add_verbosity_args(parser)
    return parser.parse_args()


# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------

PROJECT_ROOT = get_project_root()


def check_prerequisites(server: str) -> None:
    """Verify all prerequisites before running CATS."""
    log_info("Checking prerequisites...")

    spec_path = PROJECT_ROOT / OPENAPI_SPEC
    if not spec_path.exists():
        log_error(f"OpenAPI spec not found at {spec_path}")
        sys.exit(1)

    check_tool(
        "cats",
        install_instructions=(
            "See: https://github.com/Endava/cats\n"
            "On macOS with Homebrew: brew install cats"
        ),
    )

    # Check TMI server is running
    try:
        urllib.request.urlopen(f"{server}/", timeout=5)  # noqa: S310
    except (urllib.error.URLError, OSError):
        log_error(f"TMI server is not running at {server}")
        log_error("Start the server first with 'make start-dev' or 'make start-dev-oci'")
        sys.exit(1)

    log_success("Prerequisites check completed")


# ---------------------------------------------------------------------------
# Seed
# ---------------------------------------------------------------------------


def run_seed(args: argparse.Namespace) -> None:
    """Run cats-seed via the Python script."""
    cmd = ["uv", "run", str(PROJECT_ROOT / "scripts" / "run-cats-seed.py")]
    if args.oci:
        cmd.append("--oci")
    cmd += [
        "--user", args.user,
        "--provider", args.provider,
        "--server", args.server,
        f"--config={args.config}",
    ]
    log_info(f"Seeding database: {' '.join(cmd)}")
    run_cmd(cmd, cwd=str(PROJECT_ROOT))


# ---------------------------------------------------------------------------
# OAuth authentication
# ---------------------------------------------------------------------------


def authenticate_user(user: str, server: str) -> str:
    """Authenticate via the OAuth stub's automated flow and return an access token."""
    log_info(f"Authenticating user: {user}")

    # Start the automated OAuth flow
    start_url = f"{OAUTH_STUB_URL}/flows/start"
    flow_body = json.dumps({
        "userid": user,
        "idp": "tmi",
        "tmi_server": server,
    }).encode()

    req = urllib.request.Request(
        start_url,
        data=flow_body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:  # noqa: S310
            start_data = json.loads(resp.read())
    except (urllib.error.URLError, OSError) as exc:
        log_error(f"Failed to start OAuth flow: {exc}")
        sys.exit(1)

    flow_id = start_data.get("flow_id")
    if not flow_id:
        log_error(f"No flow_id in response: {start_data}")
        sys.exit(1)

    log_info(f"Flow started with ID: {flow_id}")

    # Poll for completion
    poll_url = f"{OAUTH_STUB_URL}/flows/{flow_id}"
    max_attempts = 10
    poll_data: dict = {}

    for attempt in range(1, max_attempts + 1):
        log_info(f"Polling flow status (attempt {attempt}/{max_attempts})...")
        try:
            with urllib.request.urlopen(poll_url, timeout=10) as resp:  # noqa: S310
                poll_data = json.loads(resp.read())
        except (urllib.error.URLError, OSError) as exc:
            log_error(f"Failed to poll flow status: {exc}")
            sys.exit(1)

        if poll_data.get("tokens_ready") is True:
            log_info("Flow completed successfully")
            break

        status = poll_data.get("status", "")
        if status in ("error", "failed"):
            log_error(f"Flow failed: {poll_data.get('error', 'Unknown error')}")
            sys.exit(1)

        time.sleep(2)
    else:
        log_error(f"Flow did not complete within {max_attempts} attempts")
        log_error(f"Last status: {poll_data.get('status')}")
        sys.exit(1)

    token: str | None = (poll_data.get("tokens") or {}).get("access_token")
    if not token:
        log_error(f"No access token in flow response: {poll_data}")
        sys.exit(1)

    log_success(f"Authentication successful for user: {user}")
    return str(token)


# ---------------------------------------------------------------------------
# Environment preparation
# ---------------------------------------------------------------------------


def prepare_test_environment() -> None:
    """Clear old reports and rate-limit keys."""
    log_info("Preparing test environment...")

    report_dir = PROJECT_ROOT / "test" / "outputs" / "cats" / "report"
    if report_dir.exists():
        shutil.rmtree(report_dir, ignore_errors=True)
    report_dir.mkdir(parents=True, exist_ok=True)

    # Clear ALL rate limit keys from Redis
    log_info("Clearing all rate limit keys from Redis...")
    _redis_del_pattern("*ratelimit*")

    log_success("Test environment prepared")


def disable_rate_limits(user: str) -> None:
    """Clear Redis rate-limit entries for the test user and localhost."""
    log_info(f"Disabling rate limits for CATS test user: {user}...")

    _redis_del_pattern("ip:ratelimit:*:127.0.0.1")
    _redis_del_pattern("ip:ratelimit:*:::1")
    _redis_del_pattern(f"auth:ratelimit:*:{user}*")

    log_success(f"Rate limits disabled for user: {user}")


def _redis_del_pattern(pattern: str) -> None:
    """Scan and delete Redis keys matching *pattern*."""
    try:
        scan = subprocess.run(
            ["docker", "exec", "tmi-redis", "redis-cli", "--scan", "--pattern", pattern],
            capture_output=True, text=True, check=False,
        )
        keys = scan.stdout.strip()
        if keys:
            subprocess.run(
                ["docker", "exec", "-i", "tmi-redis", "redis-cli", "DEL", *keys.split()],
                capture_output=True, check=False,
            )
    except FileNotFoundError:
        log_warn("docker not found; skipping Redis cleanup")


# ---------------------------------------------------------------------------
# CATS execution
# ---------------------------------------------------------------------------


def run_cats_fuzz(
    token: str,
    server: str,
    user: str,
    *,
    path: str = "",
    rate: int = DEFAULT_MAX_REQUESTS_PER_MINUTE,
    blackbox: bool = False,
) -> int:
    """Build and execute the CATS command. Returns the process exit code."""
    log_info("Running CATS fuzzing...")
    log_info(f"Server: {server}")
    log_info(f"OpenAPI Spec: {OPENAPI_SPEC}")
    log_info(f"Rate limit: {rate} requests/minute")
    log_info("Skipping UUID format fields to avoid false positives with malformed UUIDs")
    log_info("Skipping 'offset' field - extreme values return empty results (200), not errors")

    disable_rate_limits(user)

    cats_cmd: list[str] = [
        "cats",
        f"--contract={PROJECT_ROOT / OPENAPI_SPEC}",
        f"--server={server}",
        f"--maxRequestsPerMinute={rate}",
    ]

    if blackbox:
        cats_cmd.append("-b")

    cats_cmd += [
        "-H", f"Authorization=Bearer {token}",
        f"-X={HTTP_METHODS}",
        "--skipFieldFormat=uuid",
        "--skipField=offset",
        "--printExecutionStatistics",
        f"--refData={PROJECT_ROOT / 'test' / 'outputs' / 'cats' / 'cats-test-data.yml'}",
        f"--output={PROJECT_ROOT / 'test' / 'outputs' / 'cats' / 'report'}",
        # Skip BypassAuthentication fuzzer on public endpoints marked in OpenAPI spec
        # Public endpoints (OAuth, OIDC, SAML) are marked with x-public-endpoint: true
        # per RFCs 8414, 7517, 6749, and SAML 2.0 specifications
        "--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication",
        # Skip CheckSecurityHeaders fuzzer on cacheable discovery endpoints
        # Discovery endpoints (OIDC, OAuth metadata, JWKS, provider lists) intentionally use
        # Cache-Control: public, max-age=3600 instead of no-store per RFC 8414/7517
        "--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders",
        # Skip CheckDeletedResourcesNotAvailable on /me - users can't delete themselves
        "--skipFuzzersForExtension=x-skip-deleted-resource-check=true:CheckDeletedResourcesNotAvailable",
        # Skip InsecureDirectObjectReferences on /oauth2/revoke - accepting different client_ids is valid
        "--skipFuzzersForExtension=x-skip-idor-check=true:InsecureDirectObjectReferences",
        # Skip fuzzers that produce false positives due to valid API behavior:
        # - DuplicateHeaders: TMI ignores duplicate/unknown headers (valid per HTTP spec)
        # - LargeNumberOfRandomAlphanumericHeaders: TMI ignores extra headers (valid behavior)
        # - EnumCaseVariantFields: TMI uses case-sensitive enum validation (stricter is valid)
        #
        # Additional fuzzers skipped due to 100% false positive rate with 0 real issues found:
        # - BidirectionalOverrideFields: Unicode BiDi override chars in JSON API don't cause issues
        # - ResponseHeadersMatchContractHeaders: Flags missing optional headers as errors
        # - PrefixNumbersWithZeroFields: API correctly rejects invalid JSON numbers (leading zeros)
        # - ZalgoTextInFields: Exotic Unicode in JSON API correctly handled
        # - HangulFillerFields: Korean filler chars in JSON API correctly handled
        # - AbugidasInStringFields: Indic script chars in JSON API correctly handled
        # - FullwidthBracketsFields: CJK brackets in JSON API correctly handled
        # - ZeroWidthCharsInValuesFields: Zero-width chars in values correctly handled
        (
            "--skipFuzzers=DuplicateHeaders,LargeNumberOfRandomAlphanumericHeaders,"
            "EnumCaseVariantFields,BidirectionalOverrideFields,"
            "ResponseHeadersMatchContractHeaders,PrefixNumbersWithZeroFields,"
            "ZalgoTextInFields,HangulFillerFields,AbugidasInStringFields,"
            "FullwidthBracketsFields,ZeroWidthCharsInValuesFields"
        ),
    ]

    if path:
        log_info(f"Restricting to endpoint path: {path}")
        cats_cmd.append(f"--paths={path}")

    # Log command with token redacted
    redacted = " ".join(cats_cmd).replace(token, "[REDACTED]")
    log_info(f"Executing: {redacted}")

    env = {**os.environ, "TMI_ACCESS_TOKEN": token}
    result = subprocess.run(cats_cmd, env=env, check=False)
    return result.returncode


# ---------------------------------------------------------------------------
# Auto-parse results
# ---------------------------------------------------------------------------


def auto_parse_results() -> None:
    """Import parse_cats_results and run the parser on the report directory."""
    log_info("Auto-parsing CATS results into SQLite database...")

    # Import the parser module from scripts/
    scripts_dir = str(Path(__file__).resolve().parent)
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)

    from parse_cats_results import CATSResultsParser  # noqa: E402

    db_path = PROJECT_ROOT / "test" / "outputs" / "cats" / "cats-results.db"
    report_dir = PROJECT_ROOT / "test" / "outputs" / "cats" / "report"

    # Remove old database files
    for suffix in ("", "-shm", "-wal"):
        p = Path(f"{db_path}{suffix}")
        if p.exists():
            p.unlink()

    parser = CATSResultsParser(str(db_path))
    parser.create_schema()
    parser.process_directory(report_dir, batch_size=100)

    log_success(f"CATS results parsed to {db_path}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    log_info("Starting CATS fuzzing with OAuth integration")
    log_info(f"User: {args.user}")
    log_info(f"Server: {args.server}")
    if args.path:
        log_info(f"Path filter: {args.path}")

    check_prerequisites(args.server)
    prepare_test_environment()
    ensure_oauth_stub()

    # Seed unless skipped
    if not args.skip_seed:
        run_seed(args)

    # Verify reference files exist
    ref_file = PROJECT_ROOT / "test" / "outputs" / "cats" / "cats-test-data.json"
    if not ref_file.exists():
        log_error(f"Test data reference file not found: {ref_file}")
        log_error("Run 'make cats-seed' first to create test data")
        sys.exit(1)

    token = authenticate_user(args.user, args.server)

    exit_code = run_cats_fuzz(
        token,
        args.server,
        args.user,
        path=args.path,
        rate=args.rate,
        blackbox=args.blackbox,
    )

    if not args.skip_parse:
        auto_parse_results()

    log_success("CATS fuzzing completed!")
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
