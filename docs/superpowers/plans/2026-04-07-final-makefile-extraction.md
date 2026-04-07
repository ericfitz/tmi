# Final Makefile Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract all remaining inline shell logic and human-facing output from the Makefile into Python scripts, leaving a thin pass-through Makefile with zero logging or conditionals.

**Architecture:** Each domain of inline logic becomes a standalone Python script (or extends an existing one) following the established pattern: inline uv TOML, argparse CLI, imports from `scripts/lib/tmi_common.py`. The Makefile targets become one-liner `@uv run scripts/...` calls.

**Tech Stack:** Python 3.11+, uv, argparse, subprocess, sqlite3, tmi_common shared library

---

## File Map

### New Files
- `scripts/manage-terraform.py` — All terraform operations
- `scripts/run-cats-fuzz.py` — CATS fuzzing with OAuth (replaces `run-cats-fuzz.sh`)
- `scripts/query-cats-results.py` — Query CATS SQLite results (replaces `query-cats-results.sh`)
- `scripts/run-api-tests.py` — Postman/Newman API testing
- `scripts/manage-oci-functions.py` — OCI Functions (fn CLI) operations
- `scripts/manage-arazzo.py` — Arazzo workflow generation

### Modified Files
- `scripts/lib/tmi_common.py` — Add `ensure_oauth_stub()`, `check_tool()`
- `scripts/clean.py` — Add `build`, `containers` scopes; wstest cleanup in existing scopes
- `scripts/run-coverage.py` — Add `--full` mode for infra orchestration
- `scripts/run-wstest.py` — Add `--monitor` flag, add argparse, build-on-demand
- `scripts/manage-database.py` — Add `check` subcommand
- `scripts/generate-sbom.py` — Add grype check (already has cyclonedx check)
- `Makefile` — Replace all inline logic with one-liner script calls; remove macros/defines

### Deleted Files
- `scripts/run-cats-fuzz.sh`
- `scripts/oauth-stub-lib.sh`
- `scripts/query-cats-results.sh`

---

## Task 1: Add shared helpers to `tmi_common.py`

**Files:**
- Modify: `scripts/lib/tmi_common.py`

- [ ] **Step 1: Add `check_tool()` helper**

Add after the `apply_verbosity` function at the end of the CLI helpers section:

```python
def check_tool(
    name: str,
    *,
    install_instructions: str | None = None,
) -> None:
    """Exit with an error if a CLI tool is not on PATH.

    Args:
        name: Tool binary name (e.g., "terraform", "newman").
        install_instructions: Optional multi-line install guidance.
    """
    import shutil

    if shutil.which(name) is not None:
        return
    log_error(f"{name} not found")
    if install_instructions:
        print("")
        log_info("Install using:")
        for line in install_instructions.strip().splitlines():
            print(f"  {line.strip()}")
    sys.exit(1)
```

- [ ] **Step 2: Add `ensure_oauth_stub()` helper**

Add at the end of the process management section:

```python
def ensure_oauth_stub(port: int = 8079) -> None:
    """Ensure the OAuth callback stub is running.

    Checks if the stub responds on the given port.  If not, starts it
    via manage-oauth-stub.py.  Exits on failure.
    """
    import urllib.request
    import urllib.error

    url = f"http://127.0.0.1:{port}/latest"
    try:
        urllib.request.urlopen(url, timeout=2)  # noqa: S310
        log_info("OAuth stub already running")
        return
    except (urllib.error.URLError, OSError):
        pass

    log_info("OAuth stub not running, starting it...")
    scripts_dir = Path(__file__).resolve().parent.parent
    result = run_cmd(
        ["uv", "run", str(scripts_dir / "manage-oauth-stub.py"), "start"],
        check=False,
    )
    if result.returncode != 0:
        log_error("Failed to start OAuth stub")
        sys.exit(1)

    # Wait for it to come up
    wait_for_port(port, timeout=15, label="OAuth stub")
```

- [ ] **Step 3: Verify imports work**

Run: `python3 -c "import sys; sys.path.insert(0, 'scripts/lib'); from tmi_common import check_tool, ensure_oauth_stub; print('OK')"`
Expected: `OK`

- [ ] **Step 4: Commit**

```bash
git add scripts/lib/tmi_common.py
git commit -m "refactor: add check_tool() and ensure_oauth_stub() to tmi_common"
```

---

## Task 2: Create `scripts/manage-terraform.py`

**Files:**
- Create: `scripts/manage-terraform.py`
- Modify: `Makefile` (tf-* targets)

- [ ] **Step 1: Create the script**

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Manage Terraform infrastructure operations.

Subcommands: init, plan, apply, validate, fmt, output, destroy
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    get_project_root,
    log_info,
    log_success,
    log_warn,
    run_cmd,
)

INSTALL_INSTRUCTIONS = "Homebrew: brew install terraform"


def get_tf_dir(environment: str) -> Path:
    """Return the Terraform environment directory."""
    return get_project_root() / "terraform" / "environments" / environment


def cmd_init(tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info(f"Initializing Terraform in {tf_dir}...")
    run_cmd(["terraform", "init"], cwd=tf_dir)
    log_success("Terraform initialized successfully")


def cmd_plan(tf_dir: Path, _args: argparse.Namespace) -> None:
    cmd_init(tf_dir, _args)
    log_info(f"Planning Terraform changes...")
    run_cmd(
        ["terraform", "plan", "-out=tfplan"],
        cwd=tf_dir,
        env={"GODEBUG": "x509negativeserial=1"},
    )
    log_success(f"Terraform plan saved to {tf_dir}/tfplan")


def cmd_apply(tf_dir: Path, args: argparse.Namespace) -> None:
    cmd_init(tf_dir, args)
    log_info("Applying Terraform changes...")
    cmd = ["terraform", "apply"]
    if args.from_plan:
        cmd.append("tfplan")
    elif args.auto_approve:
        cmd.append("-auto-approve")
    run_cmd(cmd, cwd=tf_dir, env={"GODEBUG": "x509negativeserial=1"})
    log_success("Terraform apply completed")


def cmd_validate(tf_dir: Path, _args: argparse.Namespace) -> None:
    cmd_init(tf_dir, _args)
    log_info("Validating Terraform configuration...")
    run_cmd(["terraform", "validate"], cwd=tf_dir)
    log_success("Terraform configuration is valid")


def cmd_fmt(_tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info("Formatting Terraform files...")
    run_cmd(
        ["terraform", "fmt", "-recursive", "terraform/"],
        cwd=get_project_root(),
    )
    log_success("Terraform files formatted")


def cmd_output(tf_dir: Path, _args: argparse.Namespace) -> None:
    log_info(f"Terraform outputs...")
    run_cmd(["terraform", "output"], cwd=tf_dir)


def cmd_destroy(tf_dir: Path, args: argparse.Namespace) -> None:
    log_warn("This will destroy all infrastructure!")
    cmd = ["terraform", "destroy"]
    if args.auto_approve:
        cmd.append("-auto-approve")
    run_cmd(cmd, cwd=tf_dir)


SUBCOMMANDS = {
    "init": (cmd_init, "Initialize Terraform"),
    "plan": (cmd_plan, "Plan changes"),
    "apply": (cmd_apply, "Apply changes"),
    "validate": (cmd_validate, "Validate configuration"),
    "fmt": (cmd_fmt, "Format files"),
    "output": (cmd_output, "Show outputs"),
    "destroy": (cmd_destroy, "Destroy infrastructure"),
}


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Manage Terraform infrastructure operations.",
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "subcommand",
        choices=list(SUBCOMMANDS.keys()),
        help="Terraform operation to perform",
    )
    parser.add_argument(
        "--environment",
        default="oci-public",
        help="Terraform environment (default: oci-public)",
    )
    parser.add_argument(
        "--auto-approve",
        action="store_true",
        default=False,
        help="Skip interactive approval for apply/destroy",
    )
    parser.add_argument(
        "--from-plan",
        action="store_true",
        default=False,
        help="Apply from saved tfplan file",
    )
    args = parser.parse_args()
    apply_verbosity(args)

    check_tool("terraform", install_instructions=INSTALL_INSTRUCTIONS)

    tf_dir = get_tf_dir(args.environment)
    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(tf_dir, args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile tf-* targets**

Replace the entire Terraform section (from `# TERRAFORM INFRASTRUCTURE MANAGEMENT` through `tf-destroy`) with:

```makefile
# ============================================================================
# TERRAFORM INFRASTRUCTURE MANAGEMENT
# ============================================================================

TF_ENV ?= oci-public

.PHONY: tf-init tf-plan tf-apply tf-apply-plan tf-validate tf-fmt tf-output tf-destroy

tf-init:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) init

tf-plan:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) plan

tf-apply:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) $(if $(AUTO_APPROVE),--auto-approve,) apply

tf-apply-plan:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) --from-plan apply

tf-validate:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) validate

tf-fmt:
	@uv run scripts/manage-terraform.py fmt

tf-output:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) output

tf-destroy:  ## Destroy Terraform infrastructure (DESTRUCTIVE!)
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) $(if $(AUTO_APPROVE),--auto-approve,) destroy
```

Also remove the `tf-check` target entirely.

- [ ] **Step 3: Test**

Run: `uv run scripts/manage-terraform.py --help`
Expected: Help text showing subcommands and flags

Run: `make tf-fmt` (safe — just formats files)
Expected: Success or "terraform not found" error from script

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-terraform.py Makefile
git commit -m "refactor: extract terraform targets to manage-terraform.py"
```

---

## Task 3: Create `scripts/run-cats-fuzz.py`

**Files:**
- Create: `scripts/run-cats-fuzz.py`
- Modify: `Makefile` (cats-fuzz, cats-fuzz-oci, parse-cats-results, analyze-cats-results targets)
- Delete: `scripts/run-cats-fuzz.sh`, `scripts/oauth-stub-lib.sh`

- [ ] **Step 1: Create the script**

This is a large script. Port all logic from `scripts/run-cats-fuzz.sh` to Python, following the `tmi_common` patterns. Key sections:

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Run CATS API fuzzing with OAuth authentication.

Automates: prerequisite checks, OAuth authentication, rate limit clearing,
CATS execution, and result parsing into SQLite.
"""

import argparse
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
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
# Configuration
# ---------------------------------------------------------------------------

DEFAULT_USER = "charlie"
DEFAULT_SERVER = "http://localhost:8080"
DEFAULT_PROVIDER = "tmi"
DEFAULT_MAX_REQUESTS_PER_MINUTE = 3000
OAUTH_STUB_PORT = 8079
OAUTH_STUB_URL = f"http://localhost:{OAUTH_STUB_PORT}"
OPENAPI_SPEC = "api-schema/tmi-openapi.json"
HTTP_METHODS = "POST,PUT,GET,DELETE,PATCH"

# Fuzzers to skip — see run-cats-fuzz.sh for full rationale comments
SKIP_FUZZERS = ",".join([
    "DuplicateHeaders",
    "LargeNumberOfRandomAlphanumericHeaders",
    "EnumCaseVariantFields",
    "BidirectionalOverrideFields",
    "ResponseHeadersMatchContractHeaders",
    "PrefixNumbersWithZeroFields",
    "ZalgoTextInFields",
    "HangulFillerFields",
    "AbugidasInStringFields",
    "FullwidthBracketsFields",
    "ZeroWidthCharsInValuesFields",
])


# ---------------------------------------------------------------------------
# OAuth authentication
# ---------------------------------------------------------------------------


def authenticate_user(user: str, server: str) -> str:
    """Authenticate via OAuth stub and return access token."""
    log_info(f"Authenticating user: {user}")

    flow_request = json.dumps({
        "userid": user,
        "idp": DEFAULT_PROVIDER,
        "tmi_server": server,
    }).encode()

    start_url = f"{OAUTH_STUB_URL}/flows/start"
    log_info(f"Starting automated OAuth flow via {start_url}")

    req = urllib.request.Request(
        start_url,
        data=flow_request,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:  # noqa: S310
            start_response = json.loads(resp.read())
    except (urllib.error.URLError, OSError) as exc:
        log_error(f"Failed to start OAuth flow: {exc}")
        sys.exit(1)

    flow_id = start_response.get("flow_id")
    if not flow_id:
        log_error(f"No flow_id in response: {start_response}")
        sys.exit(1)

    log_info(f"Flow started with ID: {flow_id}")

    # Poll for completion
    poll_url = f"{OAUTH_STUB_URL}/flows/{flow_id}"
    for attempt in range(1, 11):
        log_info(f"Polling flow status (attempt {attempt}/10)...")
        try:
            with urllib.request.urlopen(poll_url, timeout=10) as resp:  # noqa: S310
                poll_response = json.loads(resp.read())
        except (urllib.error.URLError, OSError) as exc:
            log_error(f"Failed to poll flow status: {exc}")
            sys.exit(1)

        if poll_response.get("tokens_ready"):
            log_info("Flow completed successfully")
            token = poll_response.get("tokens", {}).get("access_token")
            if not token:
                log_error(f"No access_token in response: {poll_response}")
                sys.exit(1)
            log_success(f"Authentication successful for user: {user}")
            return token

        status = poll_response.get("status", "")
        if status in ("error", "failed"):
            log_error(f"Flow failed: {poll_response.get('error', 'Unknown error')}")
            sys.exit(1)

        time.sleep(2)

    log_error(f"Flow did not complete within 10 attempts")
    sys.exit(1)


# ---------------------------------------------------------------------------
# Environment preparation
# ---------------------------------------------------------------------------


def prepare_test_environment(project_root: Path) -> None:
    """Clear old reports and Redis rate limit keys."""
    log_info("Preparing test environment...")

    report_dir = project_root / "test" / "outputs" / "cats" / "report"
    if report_dir.is_dir():
        import shutil
        for item in report_dir.iterdir():
            if item.is_file() or item.is_symlink():
                item.unlink()
            elif item.is_dir():
                shutil.rmtree(item)
    report_dir.mkdir(parents=True, exist_ok=True)

    log_info("Clearing all rate limit keys from Redis...")
    # Get keys matching pattern, then delete them
    scan_result = run_cmd(
        ["docker", "exec", "tmi-redis", "redis-cli", "--scan", "--pattern", "*ratelimit*"],
        check=False,
        capture=True,
    )
    if scan_result.returncode == 0 and scan_result.stdout.strip():
        keys = scan_result.stdout.strip().split("\n")
        if keys:
            run_cmd(
                ["docker", "exec", "tmi-redis", "redis-cli", "DEL", *keys],
                check=False,
            )

    log_success("Test environment prepared")


def disable_rate_limits(user: str) -> None:
    """Clear rate limit entries for the test user."""
    log_info(f"Disabling rate limits for CATS test user: {user}...")
    patterns = [
        f"ip:ratelimit:*:127.0.0.1",
        f"ip:ratelimit:*:::1",
        f"auth:ratelimit:*:{user}*",
    ]
    for pattern in patterns:
        scan = run_cmd(
            ["docker", "exec", "tmi-redis", "redis-cli", "--scan", "--pattern", pattern],
            check=False,
            capture=True,
        )
        if scan.returncode == 0 and scan.stdout.strip():
            keys = scan.stdout.strip().split("\n")
            if keys:
                run_cmd(
                    ["docker", "exec", "tmi-redis", "redis-cli", "DEL", *keys],
                    check=False,
                )
    log_success(f"Rate limits disabled for user: {user}")


# ---------------------------------------------------------------------------
# CATS execution
# ---------------------------------------------------------------------------


def run_cats(
    token: str,
    server: str,
    project_root: Path,
    *,
    path: str | None = None,
    user: str | None = None,
    max_requests_per_minute: int = DEFAULT_MAX_REQUESTS_PER_MINUTE,
    blackbox: bool = False,
) -> int:
    """Execute CATS fuzzing and return the exit code."""
    log_info("Running CATS fuzzing...")
    log_info(f"Server: {server}")
    log_info(f"Rate limit: {max_requests_per_minute} requests/minute")

    if user:
        disable_rate_limits(user)

    cats_cmd = [
        "cats",
        f"--contract={project_root / OPENAPI_SPEC}",
        f"--server={server}",
        f"--maxRequestsPerMinute={max_requests_per_minute}",
    ]

    if blackbox:
        cats_cmd.append("-b")

    cats_cmd.extend([
        "-H", f"Authorization=Bearer {token}",
        f"-X={HTTP_METHODS}",
        "--skipFieldFormat=uuid",
        "--skipField=offset",
        "--printExecutionStatistics",
        f"--refData={project_root / 'test' / 'outputs' / 'cats' / 'cats-test-data.yml'}",
        f"--output={project_root / 'test' / 'outputs' / 'cats' / 'report'}",
        "--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication",
        "--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders",
        "--skipFuzzersForExtension=x-skip-deleted-resource-check=true:CheckDeletedResourcesNotAvailable",
        "--skipFuzzersForExtension=x-skip-idor-check=true:InsecureDirectObjectReferences",
        f"--skipFuzzers={SKIP_FUZZERS}",
    ])

    if path:
        log_info(f"Restricting to endpoint path: {path}")
        cats_cmd.append(f"--paths={path}")

    # Log command with token redacted
    display = " ".join(str(c) for c in cats_cmd).replace(token, "[REDACTED]")
    log_info(f"Executing: {display}")

    import os
    env = {**os.environ, "TMI_ACCESS_TOKEN": token}
    result = subprocess.run(cats_cmd, env=env)
    return result.returncode


# ---------------------------------------------------------------------------
# Result parsing
# ---------------------------------------------------------------------------


def parse_results(project_root: Path) -> None:
    """Parse CATS JSON reports into SQLite database."""
    report_dir = project_root / "test" / "outputs" / "cats" / "report"
    db_path = project_root / "test" / "outputs" / "cats" / "cats-results.db"

    if not report_dir.is_dir() or not any(report_dir.iterdir()):
        log_warn("No CATS reports found to parse")
        return

    log_info("Parsing CATS results into SQLite database...")

    # Remove old database files
    for suffix in ("", "-shm", "-wal"):
        p = db_path.parent / f"{db_path.name}{suffix}"
        p.unlink(missing_ok=True)

    # Import and run the parser
    sys.path.insert(0, str(project_root / "scripts"))
    from parse_cats_results import CATSResultsParser  # type: ignore[import-untyped]

    parser = CATSResultsParser(str(db_path))
    parser.create_schema()
    parser.parse_directory(str(report_dir), batch_size=100)

    log_success(f"CATS results parsed to {db_path}")


# ---------------------------------------------------------------------------
# Seed
# ---------------------------------------------------------------------------


def run_seed(
    project_root: Path,
    *,
    config: str,
    user: str,
    provider: str,
    server: str,
    oci: bool = False,
) -> None:
    """Run cats-seed to populate database with test data."""
    scripts_dir = project_root / "scripts"
    cmd = [
        "uv", "run", str(scripts_dir / "run-cats-seed.py"),
        f"--config={config}",
        f"--user={user}",
        f"--provider={provider}",
        f"--server={server}",
    ]
    if oci:
        cmd.append("--oci")
    run_cmd(cmd, cwd=project_root)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run CATS API fuzzing with OAuth authentication.",
    )
    add_verbosity_args(parser)
    parser.add_argument("--user", default=DEFAULT_USER, help=f"OAuth user (default: {DEFAULT_USER})")
    parser.add_argument("--server", default=DEFAULT_SERVER, help=f"TMI server URL (default: {DEFAULT_SERVER})")
    parser.add_argument("--path", default=None, help="Restrict to specific endpoint path")
    parser.add_argument("--rate", type=int, default=DEFAULT_MAX_REQUESTS_PER_MINUTE, help="Max requests/minute")
    parser.add_argument("--blackbox", action="store_true", default=False, help="Ignore all errors except 500")
    parser.add_argument("--config", default="config-development.yml", help="Config file for cats-seed")
    parser.add_argument("--oci", action="store_true", default=False, help="Use OCI cats-seed variant")
    parser.add_argument("--provider", default=DEFAULT_PROVIDER, help=f"OAuth provider (default: {DEFAULT_PROVIDER})")
    parser.add_argument("--skip-seed", action="store_true", default=False, help="Skip cats-seed step")
    parser.add_argument("--skip-parse", action="store_true", default=False, help="Skip auto-parsing results")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()

    # Prerequisites
    check_tool("cats", install_instructions="Homebrew: brew install cats\nSee: https://github.com/Endava/cats")

    log_info(f"Server: {args.server}")
    result = run_cmd(
        ["curl", "-s", f"{args.server}/"],
        check=False,
        capture=True,
    )
    if result.returncode != 0:
        log_error(f"TMI server is not running at {args.server}")
        log_error("Start with: make start-dev")
        sys.exit(1)

    spec_path = project_root / OPENAPI_SPEC
    if not spec_path.exists():
        log_error(f"OpenAPI spec not found: {spec_path}")
        sys.exit(1)

    # Seed
    if not args.skip_seed:
        run_seed(
            project_root,
            config=args.config,
            user=args.user,
            provider=args.provider,
            server=args.server,
            oci=args.oci,
        )

    # Verify test data exists
    test_data = project_root / "test" / "outputs" / "cats" / "cats-test-data.json"
    if not test_data.exists():
        log_error(f"Test data not found: {test_data}")
        log_error("Run 'make cats-seed' first")
        sys.exit(1)

    prepare_test_environment(project_root)
    ensure_oauth_stub()

    token = authenticate_user(args.user, args.server)
    exit_code = run_cats(
        token,
        args.server,
        project_root,
        path=args.path,
        user=args.user,
        max_requests_per_minute=args.rate,
        blackbox=args.blackbox,
    )

    # Parse results
    if not args.skip_parse:
        parse_results(project_root)

    log_success("CATS fuzzing completed!")
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Rename `parse-cats-results.py` for importability**

The file uses hyphens in its name which prevents Python import. Create a symlink or rename. Since other scripts/docs reference it by the hyphenated name, the cleanest approach is to add a module alias. However, the simpler option: the `run-cats-fuzz.py` script already handles this by inserting the scripts dir into `sys.path` and using `from parse_cats_results import CATSResultsParser` — Python replaces hyphens when the file is on `sys.path` only if the file uses underscores. The file is named `parse-cats-results.py` (hyphens), so import won't work.

Fix: rename `parse-cats-results.py` to `parse_cats_results.py` and update the Makefile reference.

```bash
git mv scripts/parse-cats-results.py scripts/parse_cats_results.py
```

- [ ] **Step 3: Update Makefile cats targets**

Replace the entire CATS section with:

```makefile
# ============================================================================
# CATS FUZZING - API Security Testing
# ============================================================================

.PHONY: cats-seed cats-seed-oci cats-fuzz cats-fuzz-oci query-cats-results analyze-cats-results

CATS_CONFIG ?= config-development.yml
CATS_USER ?= charlie
CATS_PROVIDER ?= tmi
CATS_SERVER ?= http://localhost:8080

cats-seed:  ## Seed database for CATS fuzzing
	@uv run scripts/run-cats-seed.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)

cats-seed-oci:  ## Seed database for CATS fuzzing (Oracle ADB)
	@uv run scripts/run-cats-seed.py --oci --user=$(CATS_USER) --provider=$(CATS_PROVIDER)

cats-fuzz: cats-seed  ## Run CATS API fuzzing (auto-parses results)
	@uv run scripts/run-cats-fuzz.py --user $(CATS_USER) --server $(CATS_SERVER) --config $(CATS_CONFIG) --provider $(CATS_PROVIDER) --skip-seed $(if $(FUZZ_USER),--user $(FUZZ_USER),) $(if $(FUZZ_SERVER),--server $(FUZZ_SERVER),) $(if $(ENDPOINT),--path $(ENDPOINT),) $(if $(filter true,$(BLACKBOX)),--blackbox,)

cats-fuzz-oci: cats-seed-oci  ## Run CATS API fuzzing with OCI ADB (auto-parses results)
	@uv run scripts/run-cats-fuzz.py --oci --skip-seed $(if $(FUZZ_USER),--user $(FUZZ_USER),) $(if $(FUZZ_SERVER),--server $(FUZZ_SERVER),) $(if $(ENDPOINT),--path $(ENDPOINT),) $(if $(filter true,$(BLACKBOX)),--blackbox,)

query-cats-results:  ## Query parsed CATS results
	@uv run scripts/query-cats-results.py

analyze-cats-results: query-cats-results  ## Analyze CATS results
```

- [ ] **Step 4: Delete old shell scripts**

```bash
git rm scripts/run-cats-fuzz.sh scripts/oauth-stub-lib.sh
```

- [ ] **Step 5: Test**

Run: `uv run scripts/run-cats-fuzz.py --help`
Expected: Help text showing all flags

- [ ] **Step 6: Commit**

```bash
git add scripts/run-cats-fuzz.py scripts/parse_cats_results.py Makefile
git commit -m "refactor: replace CATS fuzzing shell script with Python"
```

---

## Task 4: Create `scripts/query-cats-results.py`

**Files:**
- Create: `scripts/query-cats-results.py`
- Delete: `scripts/query-cats-results.sh`

- [ ] **Step 1: Create the script**

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Query CATS fuzzing results from the SQLite database.

Runs summary queries against the parsed CATS results database and
prints formatted output with query examples.
"""

import argparse
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
)

DEFAULT_DB = "test/outputs/cats/cats-results.db"

QUERIES = [
    (
        "Summary (excluding OAuth false positives)",
        """
        SELECT
            rt.name AS result,
            COUNT(*) AS count,
            ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) AS percentage
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        WHERE t.is_false_positive = 0
        GROUP BY rt.name
        ORDER BY count DESC
        """,
    ),
    (
        "OAuth/Auth False Positives (expected 401/403 responses)",
        "SELECT COUNT(*) AS count FROM tests WHERE is_false_positive = 1",
    ),
    (
        "Actual Errors by Path (top 10, excluding OAuth false positives)",
        """
        SELECT
            p.path,
            COUNT(*) AS error_count,
            GROUP_CONCAT(DISTINCT f.name) AS fuzzers
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        JOIN paths p ON t.path_id = p.id
        JOIN fuzzers f ON t.fuzzer_id = f.id
        WHERE rt.name = 'error' AND t.is_false_positive = 0
        GROUP BY p.path
        ORDER BY error_count DESC
        LIMIT 10
        """,
    ),
    (
        "Warnings by Path (top 10, excluding OAuth false positives)",
        """
        SELECT
            p.path,
            COUNT(*) AS warn_count
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        JOIN paths p ON t.path_id = p.id
        WHERE rt.name = 'warn' AND t.is_false_positive = 0
        GROUP BY p.path
        ORDER BY warn_count DESC
        LIMIT 10
        """,
    ),
]


def format_table(cursor: sqlite3.Cursor) -> str:
    """Format cursor results as a simple column-aligned table."""
    if cursor.description is None:
        return ""
    headers = [d[0] for d in cursor.description]
    rows = cursor.fetchall()
    if not rows:
        return "(no results)\n"

    # Calculate column widths
    widths = [len(h) for h in headers]
    str_rows = []
    for row in rows:
        str_row = [str(v) if v is not None else "" for v in row]
        str_rows.append(str_row)
        for i, val in enumerate(str_row):
            widths[i] = max(widths[i], len(val))

    # Format
    lines = []
    header_line = "  ".join(h.ljust(w) for h, w in zip(headers, widths))
    lines.append(header_line)
    lines.append("  ".join("-" * w for w in widths))
    for row in str_rows:
        lines.append("  ".join(v.ljust(w) for v, w in zip(row, widths)))
    return "\n".join(lines) + "\n"


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Query CATS fuzzing results from the SQLite database.",
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "--db",
        default=None,
        help=f"Path to SQLite database (default: {DEFAULT_DB})",
    )
    args = parser.parse_args()
    apply_verbosity(args)

    db_path = Path(args.db) if args.db else get_project_root() / DEFAULT_DB
    if not db_path.exists():
        log_error(f"Database file not found: {db_path}")
        print("")
        log_info("First, run CATS fuzzing to generate and parse results:")
        print("  make cats-fuzz")
        sys.exit(1)

    print(f"CATS Results Database: {db_path}")
    print("=" * 40)

    conn = sqlite3.connect(str(db_path))
    try:
        for title, sql in QUERIES:
            print(f"\n{title}:")
            cursor = conn.execute(sql)
            print(format_table(cursor))
    finally:
        conn.close()

    print("Query examples:")
    print(f"  # All actual errors (excluding OAuth false positives):")
    print(f'  sqlite3 {db_path} "SELECT * FROM test_results_filtered_view WHERE result = \'error\';"')
    print("")
    print(f"  # OAuth false positives:")
    print(f'  sqlite3 {db_path} "SELECT * FROM test_results_view WHERE is_false_positive = 1;"')
    print("")
    print(f"  # Errors by fuzzer:")
    print(f'  sqlite3 {db_path} "SELECT fuzzer, COUNT(*) FROM test_results_filtered_view WHERE result = \'error\' GROUP BY fuzzer;"')


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Delete old shell script and update Makefile**

```bash
git rm scripts/query-cats-results.sh
```

The Makefile `query-cats-results` target was already updated in Task 3.

- [ ] **Step 3: Test**

Run: `uv run scripts/query-cats-results.py --help`
Expected: Help text

- [ ] **Step 4: Commit**

```bash
git add scripts/query-cats-results.py
git commit -m "refactor: replace query-cats-results.sh with Python script"
```

---

## Task 5: Create `scripts/run-api-tests.py`

**Files:**
- Create: `scripts/run-api-tests.py`
- Modify: `Makefile` (test-api, test-api-collection, test-api-list targets)

- [ ] **Step 1: Create the script**

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Run TMI API tests using Postman/Newman.

Modes:
  (default)           Run full API test suite
  --collection NAME   Run a specific Postman collection
  --list              List available collections
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

NEWMAN_INSTALL = "pnpm install -g newman"


def list_collections(postman_dir: Path) -> list[str]:
    """Return sorted list of collection names (without .json extension)."""
    return sorted(
        p.stem for p in postman_dir.glob("*.json") if p.is_file()
    )


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Run TMI API tests using Postman/Newman.",
    )
    add_verbosity_args(parser)
    parser.add_argument("--collection", default=None, help="Run a specific collection")
    parser.add_argument("--list", action="store_true", default=False, help="List available collections")
    parser.add_argument("--start-server", action="store_true", default=False, help="Auto-start server if needed")
    parser.add_argument(
        "--response-time-multiplier",
        type=int,
        default=1,
        help="Scale response time thresholds (default: 1, use higher for remote DBs)",
    )
    args = parser.parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    postman_dir = project_root / "test" / "postman"

    # List mode
    if args.list:
        log_info("Available Postman collections:")
        for name in list_collections(postman_dir):
            print(f"  {name}")
        return

    check_tool("newman", install_instructions=NEWMAN_INSTALL)

    env = {"RESPONSE_TIME_MULTIPLIER": str(args.response_time_multiplier)}

    if args.collection:
        # Run specific collection
        collection_file = postman_dir / f"{args.collection}.json"
        if not collection_file.exists():
            log_error(f"Collection not found: {collection_file}")
            log_info("Available collections:")
            for name in list_collections(postman_dir):
                print(f"  {name}")
            sys.exit(1)

        run_script = postman_dir / "run-postman-collection.sh"
        if not run_script.exists():
            log_error(f"Runner script not found: {run_script}")
            sys.exit(1)

        log_info(f"Running Postman collection: {args.collection}...")
        run_cmd(
            ["bash", str(run_script), args.collection],
            cwd=project_root,
            env=env,
        )
    else:
        # Run full suite
        run_script = postman_dir / "run-tests.sh"
        if not run_script.exists():
            log_error(f"API test script not found: {run_script}")
            sys.exit(1)

        log_info("Running comprehensive API test suite...")
        cmd = ["bash", str(run_script)]
        if args.start_server:
            cmd.append("--start-server")
        run_cmd(cmd, cwd=project_root, env=env)

    log_success("API tests completed")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets**

Replace the test-api section with:

```makefile
RESPONSE_TIME_MULTIPLIER ?= 1

test-api:
	@uv run scripts/run-api-tests.py --response-time-multiplier $(RESPONSE_TIME_MULTIPLIER) $(if $(filter true,$(START_SERVER)),--start-server,)

test-api-collection:
	@uv run scripts/run-api-tests.py --collection $(COLLECTION) --response-time-multiplier $(RESPONSE_TIME_MULTIPLIER)

test-api-list:
	@uv run scripts/run-api-tests.py --list
```

- [ ] **Step 3: Test**

Run: `uv run scripts/run-api-tests.py --help`
Expected: Help text

Run: `uv run scripts/run-api-tests.py --list`
Expected: List of collection names

- [ ] **Step 4: Commit**

```bash
git add scripts/run-api-tests.py Makefile
git commit -m "refactor: extract API testing to run-api-tests.py"
```

---

## Task 6: Add `--full` mode to `scripts/run-coverage.py`

**Files:**
- Modify: `scripts/run-coverage.py`
- Modify: `Makefile` (test-coverage target)

- [ ] **Step 1: Add `--full` flag to argparse**

In `parse_args()`, add to the `mode_group`:

```python
    mode_group.add_argument(
        "--full",
        action="store_true",
        default=False,
        help="Full pipeline: clean, start infra, run coverage, cleanup on exit",
    )
```

- [ ] **Step 2: Add full pipeline function**

Add before `main()`:

```python
def run_full_pipeline(project_root: Path, verbose: bool = False) -> int:
    """Orchestrate full coverage: clean, start infra, coverage, cleanup."""
    scripts_dir = project_root / "scripts"

    def run_script(name: str, *args: str) -> None:
        run_cmd(["uv", "run", str(scripts_dir / name), *args], cwd=project_root)

    try:
        run_script("clean.py", "all")
        run_script("manage-database.py", "start")
        run_script("manage-redis.py", "start")
        run_script("manage-database.py", "wait")

        ensure_dirs(project_root)
        run_unit_coverage(project_root, verbose=verbose)
        run_integration_coverage(project_root, verbose=verbose)
        merge_coverage(project_root, verbose=verbose)
        generate_reports(project_root, verbose=verbose)
        return 0
    except subprocess.CalledProcessError as exc:
        log_error(f"Command failed with exit code {exc.returncode}: {exc.cmd}")
        return exc.returncode
    finally:
        # Always clean up test infrastructure
        log_info("Cleaning up test infrastructure...")
        run_cmd(
            ["uv", "run", str(scripts_dir / "manage-database.py"), "--test", "clean"],
            check=False,
        )
        run_cmd(
            ["uv", "run", str(scripts_dir / "manage-redis.py"), "--test", "clean"],
            check=False,
        )
```

- [ ] **Step 3: Add `--full` handling in `main()`**

In the `main()` function, add before the existing `try` block:

```python
    if args.full:
        return run_full_pipeline(project_root, verbose=verbose)
```

- [ ] **Step 4: Update Makefile**

Replace the `test-coverage` target:

```makefile
test-coverage:
	@uv run scripts/run-coverage.py --full
```

- [ ] **Step 5: Test**

Run: `uv run scripts/run-coverage.py --help`
Expected: Help text showing `--full` option

- [ ] **Step 6: Commit**

```bash
git add scripts/run-coverage.py Makefile
git commit -m "refactor: add --full mode to run-coverage.py, simplify Makefile target"
```

---

## Task 7: Update `scripts/run-wstest.py`

**Files:**
- Modify: `scripts/run-wstest.py`
- Modify: `scripts/clean.py`
- Modify: `Makefile` (build-wstest, wstest, monitor-wstest, clean-wstest targets)

- [ ] **Step 1: Add argparse and `--monitor` flag to `run-wstest.py`**

Replace the current `main()` with argparse-based version. The script already builds on demand (lines 66-69). Add `--monitor` mode and proper argparse:

```python
def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run WebSocket test harness.",
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "--monitor",
        action="store_true",
        default=False,
        help="Run in monitor mode (foreground, single user)",
    )
    return parser.parse_args()
```

Add a `build_wstest()` function extracted from the existing code:

```python
def build_wstest(wstest_dir: Path) -> None:
    """Build the wstest binary if needed."""
    log_info("Building WebSocket test harness...")
    run_cmd(["go", "mod", "tidy"], cwd=wstest_dir)
    run_cmd(["go", "build", "-o", "wstest"], cwd=wstest_dir)
    log_success("WebSocket test harness built successfully")
```

Add monitor mode function:

```python
def run_monitor(wstest_dir: Path) -> None:
    """Run wstest in monitor mode (foreground)."""
    log_info("Checking that TMI server is running...")
    if not check_server_running(get_project_root()):
        log_error("Server not running. Please run 'make start-dev' first.")
        sys.exit(1)
    build_wstest(wstest_dir)
    log_info("Starting WebSocket monitor...")
    run_cmd(["./wstest", "--user", "monitor"], cwd=wstest_dir)
```

Update `main()`:

```python
def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    root = get_project_root()
    wstest_dir = root / "wstest"

    if args.monitor:
        run_monitor(wstest_dir)
        return

    # Original multi-terminal test logic follows...
    log_info("Checking that TMI server is running...")
    if not check_server_running(root):
        log_error("Server not running. Please run 'make start-dev' first.")
        sys.exit(1)

    build_wstest(wstest_dir)
    # ... rest of existing spawn_terminal logic ...
```

Add the argparse import and `add_verbosity_args`/`apply_verbosity` imports at the top.

- [ ] **Step 2: Add wstest cleanup to `clean.py`**

In `clean_process()`, add after the OAuth stub stop:

```python
    # Stop wstest processes
    run_cmd(["pkill", "-f", "wstest"], check=False)
```

In `clean_files()`, before `log_success`, add:

```python
    # Clean wstest logs
    wstest_dir = project_root / "wstest"
    if wstest_dir.is_dir():
        for logfile in wstest_dir.glob("*.log"):
            logfile.unlink()
```

- [ ] **Step 3: Update Makefile**

Remove `build-wstest`, `monitor-wstest`, and `clean-wstest` targets. Update `wstest`:

```makefile
.PHONY: wstest monitor-wstest

wstest:
	@uv run scripts/run-wstest.py

monitor-wstest:
	@uv run scripts/run-wstest.py --monitor
```

Remove the `clean-wstest` target entirely.

- [ ] **Step 4: Update the final print in `run-wstest.py`**

Change the last line of the multi-terminal flow from:
```python
    print("Watch the terminals for WebSocket activity. Use 'make clean-wstest' to stop all instances.")
```
to:
```python
    print("Watch the terminals for WebSocket activity. Use 'make clean-process' to stop all instances.")
```

- [ ] **Step 5: Test**

Run: `uv run scripts/run-wstest.py --help`
Expected: Help text with `--monitor` flag

- [ ] **Step 6: Commit**

```bash
git add scripts/run-wstest.py scripts/clean.py Makefile
git commit -m "refactor: add --monitor to run-wstest.py, fold cleanup into clean.py"
```

---

## Task 8: Create `scripts/manage-oci-functions.py`

**Files:**
- Create: `scripts/manage-oci-functions.py`
- Modify: `Makefile` (fn-* targets)

- [ ] **Step 1: Create the script**

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Manage OCI Functions operations.

Subcommands: build, deploy, invoke, logs
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    check_tool,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

FN_INSTALL = "Homebrew: brew install fn"


def require_app(args: argparse.Namespace) -> str:
    """Return the --app value, exiting if not provided."""
    if not args.app:
        log_error("--app is required for this operation")
        log_info("Set FN_APP env var or pass --app APP_NAME")
        sys.exit(1)
    return args.app


def cmd_build(args: argparse.Namespace) -> None:
    fn_dir = get_project_root() / "functions" / args.function
    log_info(f"Building function: {args.function}...")
    run_cmd(["fn", "build"], cwd=fn_dir)
    log_success(f"Function {args.function} built successfully")


def cmd_deploy(args: argparse.Namespace) -> None:
    app = require_app(args)
    fn_dir = get_project_root() / "functions" / args.function
    log_info(f"Deploying function {args.function} to app {app}...")
    run_cmd(["fn", "deploy", "--app", app], cwd=fn_dir)
    log_success(f"Function {args.function} deployed")


def cmd_invoke(args: argparse.Namespace) -> None:
    app = require_app(args)
    log_info(f"Invoking function {args.function}...")
    run_cmd(["fn", "invoke", app, args.function])
    log_success("Function invoked")


def cmd_logs(args: argparse.Namespace) -> None:
    app = require_app(args)
    log_info(f"Fetching logs for {args.function}...")
    run_cmd(["fn", "logs", app, args.function])


SUBCOMMANDS = {
    "build": (cmd_build, "Build the function"),
    "deploy": (cmd_deploy, "Deploy to OCI (requires --app)"),
    "invoke": (cmd_invoke, "Invoke function (requires --app)"),
    "logs": (cmd_logs, "View function logs (requires --app)"),
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage OCI Functions.")
    add_verbosity_args(parser)
    parser.add_argument("subcommand", choices=list(SUBCOMMANDS.keys()))
    parser.add_argument("--app", default=None, help="OCI Function Application name")
    parser.add_argument("--function", default="certmgr", help="Function name (default: certmgr)")
    args = parser.parse_args()
    apply_verbosity(args)

    check_tool("fn", install_instructions=FN_INSTALL)

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile**

Replace the OCI Functions section:

```makefile
# ============================================================================
# OCI FUNCTIONS - Certificate Manager
# ============================================================================

.PHONY: fn-build-certmgr fn-deploy-certmgr fn-invoke-certmgr fn-logs-certmgr

fn-build-certmgr:  ## Build the certificate manager OCI function
	@uv run scripts/manage-oci-functions.py build

fn-deploy-certmgr:  ## Deploy certificate manager function to OCI
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) deploy

fn-invoke-certmgr:  ## Invoke certificate manager function manually
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) invoke

fn-logs-certmgr:  ## View certificate manager function logs
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) logs
```

Remove the `fn-check` target.

- [ ] **Step 3: Test**

Run: `uv run scripts/manage-oci-functions.py --help`
Expected: Help text

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-oci-functions.py Makefile
git commit -m "refactor: extract OCI Functions targets to manage-oci-functions.py"
```

---

## Task 9: Create `scripts/manage-arazzo.py`

**Files:**
- Create: `scripts/manage-arazzo.py`
- Modify: `Makefile` (arazzo-* targets)

- [ ] **Step 1: Create the script**

```python
# /// script
# requires-python = ">=3.11"
# ///
"""Manage Arazzo workflow specification generation.

Subcommands: install, scaffold, enhance, generate (all three in sequence)
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)


def cmd_install(project_root: Path) -> None:
    log_info("Installing Arazzo tooling...")
    run_cmd(["pnpm", "install"], cwd=project_root)
    log_success("Arazzo tools installed")


def cmd_scaffold(project_root: Path) -> None:
    log_info("Generating base scaffold with Redocly CLI...")
    run_cmd(
        ["bash", str(project_root / "scripts" / "generate-arazzo-scaffold.sh")],
        cwd=project_root,
    )
    log_success("Base scaffold generated")


def cmd_enhance(project_root: Path) -> None:
    log_info("Enhancing with TMI workflow data...")
    run_cmd(
        ["uv", "run", str(project_root / "scripts" / "enhance-arazzo-with-workflows.py")],
        cwd=project_root,
    )
    log_success("Enhanced Arazzo created at api-schema/tmi.arazzo.yaml and .json")


def cmd_validate(project_root: Path) -> None:
    run_cmd(
        [
            "uv", "run",
            str(project_root / "scripts" / "validate-arazzo.py"),
            str(project_root / "api-schema" / "tmi.arazzo.yaml"),
            str(project_root / "api-schema" / "tmi.arazzo.json"),
        ],
        cwd=project_root,
    )


def cmd_generate(project_root: Path) -> None:
    cmd_install(project_root)
    cmd_scaffold(project_root)
    cmd_enhance(project_root)
    cmd_validate(project_root)
    log_success("Arazzo specification generation complete")


SUBCOMMANDS = {
    "install": (cmd_install, "Install Arazzo tooling (pnpm)"),
    "scaffold": (cmd_scaffold, "Generate base scaffold"),
    "enhance": (cmd_enhance, "Enhance with TMI workflow data"),
    "generate": (cmd_generate, "Full pipeline: install + scaffold + enhance + validate"),
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage Arazzo workflow specification.")
    add_verbosity_args(parser)
    parser.add_argument("subcommand", choices=list(SUBCOMMANDS.keys()))
    args = parser.parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(project_root)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile**

Replace arazzo section:

```makefile
arazzo-install:
	@uv run scripts/manage-arazzo.py install

arazzo-scaffold: arazzo-install
	@uv run scripts/manage-arazzo.py scaffold

arazzo-enhance:
	@uv run scripts/manage-arazzo.py enhance

generate-arazzo:
	@uv run scripts/manage-arazzo.py generate

validate-arazzo:
	@uv run scripts/validate-arazzo.py api-schema/tmi.arazzo.yaml api-schema/tmi.arazzo.json
```

- [ ] **Step 3: Test**

Run: `uv run scripts/manage-arazzo.py --help`
Expected: Help text

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-arazzo.py Makefile
git commit -m "refactor: extract Arazzo targets to manage-arazzo.py"
```

---

## Task 10: Miscellaneous script updates

**Files:**
- Modify: `scripts/manage-database.py`
- Modify: `scripts/generate-sbom.py`
- Modify: `scripts/clean.py`
- Modify: `scripts/manage-server.py`
- Modify: `Makefile`

- [ ] **Step 1: Add `check` subcommand to `manage-database.py`**

Add to the SUBCOMMANDS dict in `manage-database.py`:

```python
def cmd_check(cfg: dict, args: argparse.Namespace) -> None:
    """Validate database schema via the migrate tool."""
    project_root = get_project_root()
    config_path = Path(args.config)
    log_info("Checking database schema...")
    run_cmd(
        ["go", "run", "main.go", "--config", str(config_path.resolve()), "--validate"],
        cwd=project_root / "cmd" / "migrate",
    )
    log_success("Database schema is valid")
```

Add `"check": (cmd_check, "Validate database schema")` to the SUBCOMMANDS dict.

- [ ] **Step 2: Add `kill-port` subcommand to `manage-server.py`**

Add to SUBCOMMANDS in `manage-server.py`:

```python
def cmd_kill_port(cfg: dict, args: argparse.Namespace) -> None:
    """Kill all processes on the configured port."""
    port = args.port or config_get(cfg, "server.port", 8080)
    log_info(f"Killing processes on port {port}")
    kill_port(port)
    log_success(f"Port {port} cleared")
```

Add `"kill-port": (cmd_kill_port, "Kill all processes on configured port")` to the SUBCOMMANDS dict.

- [ ] **Step 3: Add `build` and `containers` scopes to `clean.py`**

Add two new functions:

```python
def clean_build() -> None:
    """Remove build artifacts from bin/ directory."""
    project_root = get_project_root()
    log_info("Cleaning build artifacts...")
    bin_dir = project_root / "bin"
    if bin_dir.is_dir():
        for item in bin_dir.iterdir():
            item.unlink()
    migrate = project_root / "migrate"
    if migrate.exists():
        migrate.unlink()
    log_success("Build artifacts cleaned")


def clean_containers() -> None:
    """Stop and remove development containers."""
    scripts_dir = get_project_root() / "scripts"
    log_info("Cleaning up containers...")
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-database.py"), "clean"],
        check=False,
    )
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-redis.py"), "clean"],
        check=False,
    )
    log_success("Container cleanup completed")
```

Add to `SUBCOMMANDS`:
```python
SUBCOMMANDS = {
    "logs": clean_logs,
    "files": clean_files,
    "process": clean_process,
    "build": clean_build,
    "containers": clean_containers,
    "all": clean_all,
}
```

Update the argparse help and docstring to include the new scopes.

- [ ] **Step 4: Add grype check to `generate-sbom.py`**

Add after the existing `check_cyclonedx()` function:

```python
def check_grype() -> None:
    """Verify grype is installed; exit with instructions if not."""
    if shutil.which("grype") is None:
        log_error("Grype not found")
        print("")
        log_info("Install using:")
        print("  Homebrew: brew install grype")
        print("  Script:   curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin")
        sys.exit(1)
```

Note: only call `check_grype()` when actually scanning (if `--scan` or similar flag is used). If the existing script doesn't have a scan mode, just add the function for now — it will be called when container scanning integrates. Actually, checking the Makefile: `check-grype` and `check-cyclonedx` are standalone targets. Since `generate-sbom.py` already calls `check_cyclonedx()` internally, just remove the `check-cyclonedx` Make target. For `check-grype`, it's used by container build scripts — leave it as-is for now but remove the Makefile target.

- [ ] **Step 5: Update Makefile for all misc changes**

```makefile
check-database:
	@uv run scripts/manage-database.py check

stop-process:
	@uv run scripts/manage-server.py --port $(SERVER_PORT) kill-port

clean-build:
	@uv run scripts/clean.py build

clean-containers:
	@uv run scripts/clean.py containers
```

Remove the `check-cyclonedx` and `check-grype` Make targets.

- [ ] **Step 6: Test**

Run: `uv run scripts/clean.py --help`
Expected: Shows `build`, `containers` in choices

Run: `uv run scripts/manage-database.py --help`
Expected: Shows `check` in subcommands

- [ ] **Step 7: Commit**

```bash
git add scripts/manage-database.py scripts/manage-server.py scripts/clean.py scripts/generate-sbom.py Makefile
git commit -m "refactor: add check/kill-port/build/containers subcommands, remove Makefile tool checks"
```

---

## Task 11: Remove all remaining inline logging from Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Strip logging from remaining targets**

For each of these targets, remove `$(call log_info,...)`, `$(call log_success,...)`, `$(call log_warning,...)`, `$(call log_error,...)` lines, and any `echo -e` lines that use color codes. The scripts handle their own output.

Targets to clean:

```makefile
# test-db-cleanup — remove log_info
test-db-cleanup:
	@uv run scripts/delete-test-users.py $(ARGS)

# validate-asyncapi — remove log_info, log_success; single call
validate-asyncapi:
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC) --format json --output $(ASYNCAPI_VALIDATION_REPORT)
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC)

# parse-openapi-validation — remove log_info, log_success
parse-openapi-validation:
	@uv run scripts/parse-openapi-validation.py --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB) --summary

# setup-heroku — remove log_info
setup-heroku:
	@uv run scripts/setup-heroku-env.py

setup-heroku-dry-run:
	@uv run scripts/setup-heroku-env.py --dry-run

# reset-db-heroku — remove log_warning
reset-db-heroku:
	@./scripts/heroku-reset-database.sh $(ARGS) tmi-server

# drop-db-heroku — remove log_warning
drop-db-heroku:
	@./scripts/heroku-drop-database.sh $(ARGS) tmi-server

# build-wstest in wstest target was already removed in Task 7
# build-with-sbom — just dependencies, already clean
```

- [ ] **Step 2: Verify no logging remains**

Run: `grep -n 'log_info\|log_success\|log_warning\|log_error\|echo.*\\033\|echo -e' Makefile`
Expected: No matches (or only in comments)

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "refactor: remove all inline logging from Makefile targets"
```

---

## Task 12: Final Makefile cleanup

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Remove macros and defines**

Delete the following sections from the Makefile:

1. The `kill_port` macro (lines ~57-71)
2. Color defines: `BLUE`, `GREEN`, `YELLOW`, `RED`, `NC` (lines ~27-33)
3. Logging defines: `log_info`, `log_success`, `log_warning`, `log_error` (lines ~35-49)
4. Build variables: `VERSION`, `COMMIT`, `BUILD_DATE` (lines ~23-25) — scripts read `.version` directly
5. Stale backward compatibility aliases: `build-container-db`, `build-container-redis`, `build-container-tmi`, `build-containers`, `build-containers-all`, `build-container-oracle`, `build-containers-oracle-push`, `containers-dev`, `report-containers`

- [ ] **Step 2: Verify the Makefile is clean**

Run: `grep -n 'define \|endef\|\\033\|log_info\|log_success\|kill_port' Makefile`
Expected: No matches

Run: `make list-targets`
Expected: All targets listed

Run: `make help`
Expected: Help output works

- [ ] **Step 3: Count lines**

Run: `wc -l Makefile`
Expected: ~250-400 lines

- [ ] **Step 4: Run lint**

Run: `make lint`
Expected: Pass (no Go changes, but validates the Makefile targets work)

- [ ] **Step 5: Run build and unit tests**

Run: `make build-server && make test-unit`
Expected: Both pass

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "refactor: remove Makefile macros, color defines, and stale aliases

Completes #215 — all Make targets are now thin pass-throughs to Python
scripts with zero inline logic or human-facing output.

Closes #215"
```

---

## Task 13: Final verification

- [ ] **Step 1: Verify no inline logic remains**

Run these checks:
```bash
# No echo statements (except in comments)
grep -n '^[^#]*echo' Makefile | grep -v '@uv\|@\./\|@bash'

# No shell conditionals
grep -n '^[^#]*if \[' Makefile

# No $(call ...) macros
grep -n '$(call ' Makefile

# No color codes
grep -n '\\033' Makefile
```

Expected: All empty

- [ ] **Step 2: Verify all scripts have `--help`**

```bash
for script in manage-terraform run-cats-fuzz query-cats-results run-api-tests manage-oci-functions manage-arazzo; do
    echo "=== $script ==="
    uv run scripts/$script.py --help 2>&1 | head -3
done
```

Expected: Each prints usage information

- [ ] **Step 3: Verify deleted files are gone**

```bash
ls scripts/run-cats-fuzz.sh scripts/oauth-stub-lib.sh scripts/query-cats-results.sh 2>&1
```

Expected: All "No such file or directory"

- [ ] **Step 4: Push**

```bash
git pull --rebase
git push
git status
```

Expected: Up to date with origin
