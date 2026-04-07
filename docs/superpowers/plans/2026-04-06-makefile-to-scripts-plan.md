# Move Makefile Logic to Scripts — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract all inline shell logic from the Makefile into standalone Python scripts with a shared library, making the build system framework-agnostic.

**Architecture:** A shared Python library (`scripts/lib/tmi_common.py`) provides logging, config loading, Docker management, and process lifecycle utilities. Each Makefile target becomes a one-liner calling a Python script that imports from this library. Scripts read defaults from YAML config files and accept CLI flag overrides.

**Tech Stack:** Python 3.11+, PyYAML, uv (inline script dependencies), subprocess for command execution.

**Spec:** `docs/superpowers/specs/2026-04-06-makefile-to-scripts-design.md`
**Issue:** [#215](https://github.com/ericfitz/tmi/issues/215)

---

## File Structure

### New files

- `scripts/lib/__init__.py` — empty, enables Python imports
- `scripts/lib/tmi_common.py` — shared library (logging, config, Docker, process management, CLI helpers)
- `scripts/manage-database.py` — database container lifecycle + migrations
- `scripts/manage-redis.py` — Redis container lifecycle
- `scripts/manage-server.py` — server process lifecycle
- `scripts/start-dev.py` — dev environment orchestration
- `scripts/run-unit-tests.py` — unit test execution with output formatting
- `scripts/run-coverage.py` — coverage orchestration (unit, integration, merge, generate)
- `scripts/manage-oauth-stub.py` — OAuth stub lifecycle
- `scripts/run-cats-seed.py` — CATS database seeding
- `scripts/validate-openapi-spec.py` — OpenAPI validation (replaces inline Makefile logic)
- `scripts/check-unsafe-union-methods.py` — discriminator method safety check
- `scripts/status.py` — service status dashboard
- `scripts/clean.py` — cleanup orchestration (files, logs, containers, processes)
- `scripts/build-server.py` — Go binary builds (server, migrate, cats-seed)
- `scripts/generate-api.py` — oapi-codegen wrapper
- `scripts/generate-sbom.py` — SBOM generation
- `scripts/deploy-heroku.py` — Heroku deployment
- `scripts/lint.py` — lint orchestration
- `scripts/run-wstest.py` — WebSocket test terminal spawning
- `scripts/help.py` — help text generation

### Modified files

- `Makefile` — each target body replaced with script call

---

## Task 1: Shared Library — Core Utilities

**Files:**
- Create: `scripts/lib/__init__.py`
- Create: `scripts/lib/tmi_common.py`

- [ ] **Step 1: Create `scripts/lib/__init__.py`**

```python
# empty — enables Python package imports
```

- [ ] **Step 2: Create `scripts/lib/tmi_common.py` with logging utilities**

```python
#!/usr/bin/env python3
"""Shared utilities for TMI build and management scripts.

Provides logging, config loading, Docker container management,
process lifecycle helpers, and CLI argument patterns.
"""

import argparse
import json
import os
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Colors & Logging
# ---------------------------------------------------------------------------

RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[1;33m"
BLUE = "\033[0;34m"
NC = "\033[0m"

_quiet = False


def set_quiet(quiet: bool) -> None:
    """Suppress info-level logging when True."""
    global _quiet
    _quiet = quiet


def log_info(msg: str) -> None:
    if not _quiet:
        print(f"{BLUE}[INFO]{NC} {msg}", flush=True)


def log_success(msg: str) -> None:
    print(f"{GREEN}[SUCCESS]{NC} {msg}", flush=True)


def log_warning(msg: str) -> None:
    print(f"{YELLOW}[WARNING]{NC} {msg}", flush=True)


def log_error(msg: str) -> None:
    print(f"{RED}[ERROR]{NC} {msg}", file=sys.stderr, flush=True)


# ---------------------------------------------------------------------------
# Project paths
# ---------------------------------------------------------------------------

def get_project_root() -> Path:
    """Return the project root (grandparent of scripts/lib/)."""
    return Path(__file__).resolve().parent.parent.parent


# ---------------------------------------------------------------------------
# Config loading
# ---------------------------------------------------------------------------

def load_config(path: str | Path | None = None) -> dict:
    """Load a YAML config file.

    Args:
        path: Path to config file. Defaults to config-development.yml
              in the project root.

    Returns:
        Parsed YAML as a dict.
    """
    try:
        import yaml
    except ImportError:
        log_error("PyYAML is required. Install with: pip install pyyaml")
        sys.exit(1)

    if path is None:
        path = get_project_root() / "config-development.yml"
    path = Path(path)

    if not path.exists():
        log_error(f"Config file not found: {path}")
        sys.exit(1)

    with open(path) as f:
        return yaml.safe_load(f) or {}


def config_get(cfg: dict, dotpath: str, default: Any = None) -> Any:
    """Get a nested config value by dot-separated path.

    Example: config_get(cfg, "database.redis.port", "6379")
    """
    keys = dotpath.split(".")
    val = cfg
    for key in keys:
        if isinstance(val, dict):
            val = val.get(key)
        else:
            return default
        if val is None:
            return default
    return val


# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------

def read_version() -> dict:
    """Read version from .version JSON file.

    Returns dict with keys: major, minor, patch, prerelease.
    """
    version_file = get_project_root() / ".version"
    try:
        data = json.loads(version_file.read_text())
        for key in ("major", "minor", "patch"):
            if key not in data:
                log_error(f".version file missing '{key}' key.")
                sys.exit(1)
        return data
    except FileNotFoundError:
        log_error(f"Cannot read {version_file}.")
        sys.exit(1)
    except json.JSONDecodeError as e:
        log_error(f".version file contains invalid JSON: {e}")
        sys.exit(1)


def format_version(v: dict) -> str:
    """Format version dict as string (e.g., '1.3.0' or '1.3.0-rc.0')."""
    base = f"{v['major']}.{v['minor']}.{v['patch']}"
    if v.get("prerelease"):
        return f"{base}-{v['prerelease']}"
    return base


# ---------------------------------------------------------------------------
# Command execution
# ---------------------------------------------------------------------------

def run_cmd(
    cmd: list[str],
    *,
    check: bool = True,
    capture: bool = False,
    cwd: str | Path | None = None,
    env: dict[str, str] | None = None,
    verbose: bool = False,
) -> subprocess.CompletedProcess:
    """Run a command with optional logging.

    Args:
        cmd: Command and arguments.
        check: Raise CalledProcessError on non-zero exit.
        capture: Capture stdout/stderr.
        cwd: Working directory.
        env: Additional environment variables (merged with os.environ).
        verbose: Log the command before running.

    Returns:
        CompletedProcess instance.
    """
    if verbose:
        log_info(f"Running: {' '.join(cmd)}")
    run_env = None
    if env:
        run_env = {**os.environ, **env}
    return subprocess.run(
        cmd,
        check=check,
        capture_output=capture,
        text=True,
        cwd=cwd,
        env=run_env,
    )


# ---------------------------------------------------------------------------
# Docker helpers
# ---------------------------------------------------------------------------

def container_exists(name: str) -> bool:
    """Check if a Docker container exists (running or stopped)."""
    result = run_cmd(
        ["docker", "ps", "-a", "--format", "{{.Names}}"],
        capture=True, check=False,
    )
    return name in result.stdout.strip().split("\n")


def container_is_running(name: str) -> bool:
    """Check if a Docker container is currently running."""
    result = run_cmd(
        ["docker", "ps", "--format", "{{.Names}}"],
        capture=True, check=False,
    )
    return name in result.stdout.strip().split("\n")


def ensure_container(
    name: str,
    host_port: int | str,
    container_port: int | str,
    image: str,
    env_vars: dict[str, str] | None = None,
    volumes: list[str] | None = None,
) -> None:
    """Create a Docker container if missing, start it if stopped, no-op if running.

    Args:
        name: Container name.
        host_port: Port on the host (bound to 127.0.0.1).
        container_port: Port inside the container.
        image: Docker image to use.
        env_vars: Environment variables to pass (-e flags).
        volumes: Volume mounts (-v flags).
    """
    if container_is_running(name):
        log_info(f"{name} already running on port {host_port}")
        return

    if container_exists(name):
        log_info(f"Starting existing container {name}...")
        run_cmd(["docker", "start", name])
    else:
        log_info(f"Creating container {name}...")
        cmd = [
            "docker", "run", "-d",
            "--name", name,
            "-p", f"127.0.0.1:{host_port}:{container_port}",
        ]
        for k, v in (env_vars or {}).items():
            cmd.extend(["-e", f"{k}={v}"])
        for vol in (volumes or []):
            cmd.extend(["-v", vol])
        cmd.append(image)
        run_cmd(cmd)

    print(f"\u2705 {name} running on port {host_port}")


def stop_container(name: str) -> None:
    """Stop a Docker container if running."""
    run_cmd(["docker", "stop", name], check=False)


def remove_container(name: str, volumes: list[str] | None = None) -> None:
    """Remove a Docker container and optionally named volumes."""
    run_cmd(["docker", "rm", "-f", name], check=False)
    for vol in (volumes or []):
        run_cmd(["docker", "volume", "rm", vol], check=False)


def docker_exec(container: str, cmd: list[str], check: bool = True) -> subprocess.CompletedProcess:
    """Run a command inside a Docker container."""
    return run_cmd(["docker", "exec", container] + cmd, check=check, capture=True)


def ensure_volume(name: str) -> None:
    """Create a Docker volume if it doesn't exist."""
    result = run_cmd(
        ["docker", "volume", "ls", "--format", "{{.Name}}"],
        capture=True, check=False,
    )
    if name not in result.stdout.strip().split("\n"):
        run_cmd(["docker", "volume", "create", name])


def wait_for_container_ready(
    health_cmd: list[str],
    timeout: int = 300,
    label: str = "Service",
    interval: int = 2,
) -> None:
    """Poll until a health check command succeeds.

    Args:
        health_cmd: Command to run for health checking.
        timeout: Maximum wait time in seconds.
        label: Service name for log messages.
        interval: Seconds between checks.
    """
    remaining = timeout
    while remaining > 0:
        result = run_cmd(health_cmd, check=False, capture=True)
        if result.returncode == 0:
            log_success(f"{label} is ready!")
            return
        log_info(f"Waiting for {label}... ({remaining}s remaining)")
        time.sleep(interval)
        remaining -= interval
    log_error(f"{label} failed to start within {timeout} seconds")
    sys.exit(1)


# ---------------------------------------------------------------------------
# Process management
# ---------------------------------------------------------------------------

def graceful_kill(pid: int, timeout: int = 1) -> None:
    """Send SIGTERM, wait, then SIGKILL if still alive."""
    try:
        os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        return

    time.sleep(timeout)

    try:
        os.kill(pid, 0)  # Check if still alive
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def is_port_in_use(port: int) -> bool:
    """Check if a TCP port is in use on localhost."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        return s.connect_ex(("127.0.0.1", int(port))) == 0


def get_pids_on_port(port: int) -> list[int]:
    """Get PIDs of processes listening on a port using lsof."""
    result = run_cmd(
        ["lsof", "-ti", f":{port}"],
        check=False, capture=True,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return []
    return [int(p) for p in result.stdout.strip().split("\n") if p.strip()]


def kill_port(port: int) -> None:
    """Kill all processes on a port with SIGTERM, then SIGKILL survivors."""
    pids = get_pids_on_port(port)
    if not pids:
        return

    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass

    time.sleep(1)

    pids = get_pids_on_port(port)
    for pid in pids:
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def wait_for_port(port: int, timeout: int = 300, label: str = "Service") -> None:
    """Wait until a port is accepting connections."""
    remaining = timeout
    while remaining > 0:
        if is_port_in_use(port):
            log_success(f"{label} is ready on port {port}!")
            return
        time.sleep(2)
        remaining -= 2
    log_error(f"{label} failed to start on port {port} within {timeout} seconds")
    sys.exit(1)


def read_pid_file(path: str | Path) -> int | None:
    """Read a PID from a file, return None if missing or stale."""
    path = Path(path)
    if not path.exists():
        return None
    try:
        pid = int(path.read_text().strip())
        os.kill(pid, 0)  # Check if alive
        return pid
    except (ValueError, ProcessLookupError):
        path.unlink(missing_ok=True)
        return None


def write_pid_file(path: str | Path, pid: int) -> None:
    """Write a PID to a file."""
    Path(path).write_text(str(pid))


# ---------------------------------------------------------------------------
# CLI helpers
# ---------------------------------------------------------------------------

def add_config_arg(parser: argparse.ArgumentParser) -> None:
    """Add --config argument with default to config-development.yml."""
    parser.add_argument(
        "--config",
        default=str(get_project_root() / "config-development.yml"),
        help="Path to YAML config file (default: config-development.yml)",
    )


def add_verbosity_args(parser: argparse.ArgumentParser) -> None:
    """Add --verbose and --quiet flags."""
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--verbose", "-v", action="store_true", help="Verbose output")
    group.add_argument("--quiet", "-q", action="store_true", help="Suppress info messages")


def apply_verbosity(args: argparse.Namespace) -> None:
    """Apply verbosity settings from parsed args."""
    if hasattr(args, "quiet") and args.quiet:
        set_quiet(True)
```

- [ ] **Step 3: Verify the library imports correctly**

Run from the project root:
```bash
uv run python -c "import sys; sys.path.insert(0, 'scripts/lib'); from tmi_common import log_info, log_success; log_info('Library works'); log_success('All good')"
```

Expected: Colored `[INFO]` and `[SUCCESS]` messages printed.

- [ ] **Step 4: Commit**

```bash
git add scripts/lib/__init__.py scripts/lib/tmi_common.py
git commit -m "refactor: add shared Python library for script utilities

Foundation for extracting Makefile inline logic into standalone scripts.
Provides logging, config loading, Docker management, process lifecycle,
and CLI helpers.

Refs #215"
```

---

## Task 2: Infrastructure — Database Management

**Files:**
- Create: `scripts/manage-database.py`
- Modify: `Makefile` (lines 146-403)

- [ ] **Step 1: Create `scripts/manage-database.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///

"""Manage TMI PostgreSQL database containers and migrations.

Subcommands:
    start   - Create/start the database container
    stop    - Stop the database container
    clean   - Remove the container and volume
    wait    - Wait for the database to be ready
    migrate - Run database migrations
    reset   - Drop, recreate, and migrate the database

Use --test flag for ephemeral test containers (port 5433).

Examples:
    uv run scripts/manage-database.py start
    uv run scripts/manage-database.py start --test
    uv run scripts/manage-database.py migrate --config config-test-integration-pg.yml
"""

import argparse
import sys
from pathlib import Path
from urllib.parse import urlparse

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    config_get,
    docker_exec,
    ensure_container,
    ensure_volume,
    get_project_root,
    load_config,
    log_error,
    log_info,
    log_success,
    log_warning,
    remove_container,
    run_cmd,
    stop_container,
    wait_for_container_ready,
)

# Defaults for dev database
DEV_DEFAULTS = {
    "container": "tmi-postgresql",
    "port": 5432,
    "user": "tmi_dev",
    "password": "dev123",
    "database": "tmi_dev",
    "image": "tmi/tmi-postgresql:latest",
    "volume": "tmi-postgres-data",
}

# Defaults for test database
TEST_DEFAULTS = {
    "container": "tmi-postgresql-test",
    "port": 5433,
    "user": "tmi_dev",
    "password": "dev123",
    "database": "tmi_dev",
    "image": "tmi/tmi-postgresql:latest",
    "volume": None,  # ephemeral, no persistent volume
}


def get_db_settings(args) -> dict:
    """Resolve database settings from config + CLI overrides + defaults."""
    defaults = TEST_DEFAULTS if args.test else DEV_DEFAULTS

    cfg = load_config(args.config)

    # Parse connection info from database URL if available
    db_url = config_get(cfg, "database.url", "")
    parsed = {}
    if db_url:
        try:
            p = urlparse(db_url)
            parsed = {
                "user": p.username,
                "password": p.password,
                "port": p.port,
                "database": p.path.lstrip("/").split("?")[0],
            }
        except Exception:
            pass

    return {
        "container": getattr(args, "container", None) or defaults["container"],
        "port": getattr(args, "port", None) or parsed.get("port") or defaults["port"],
        "user": getattr(args, "user_name", None) or parsed.get("user") or defaults["user"],
        "password": parsed.get("password") or defaults["password"],
        "database": getattr(args, "database", None) or parsed.get("database") or defaults["database"],
        "image": getattr(args, "image", None) or defaults["image"],
        "volume": defaults["volume"],
    }


def cmd_start(args) -> None:
    settings = get_db_settings(args)
    log_info(f"Starting PostgreSQL container ({settings['container']})...")

    if settings["volume"]:
        ensure_volume(settings["volume"])

    volumes = []
    if settings["volume"]:
        volumes = [f"{settings['volume']}:/var/lib/postgresql/data"]

    ensure_container(
        name=settings["container"],
        host_port=settings["port"],
        container_port=5432,
        image=settings["image"],
        env_vars={
            "POSTGRES_USER": settings["user"],
            "POSTGRES_PASSWORD": settings["password"],
            "POSTGRES_DB": settings["database"],
        },
        volumes=volumes,
    )


def cmd_stop(args) -> None:
    settings = get_db_settings(args)
    log_info(f"Stopping PostgreSQL container ({settings['container']})...")
    stop_container(settings["container"])
    log_success("PostgreSQL container stopped")


def cmd_clean(args) -> None:
    settings = get_db_settings(args)
    log_warning(f"Removing PostgreSQL container and data ({settings['container']})...")
    volumes_to_remove = [settings["volume"]] if settings["volume"] else []
    remove_container(settings["container"], volumes=volumes_to_remove)
    log_success("PostgreSQL container and data removed")


def cmd_wait(args) -> None:
    settings = get_db_settings(args)
    log_info(f"Waiting for database to be ready ({settings['container']})...")
    timeout = getattr(args, "timeout", 300) or 300
    wait_for_container_ready(
        health_cmd=["docker", "exec", settings["container"],
                     "pg_isready", "-U", settings["user"]],
        timeout=timeout,
        label="Database",
    )


def cmd_migrate(args) -> None:
    settings = get_db_settings(args)
    config_file = args.config
    log_info(f"Running database migrations (config: {config_file})...")
    project_root = get_project_root()
    run_cmd(
        ["go", "run", "main.go", "--config", str(Path(config_file).resolve())],
        cwd=project_root / "cmd" / "migrate",
    )
    log_success("Database migrations completed")


def cmd_reset(args) -> None:
    settings = get_db_settings(args)
    log_warning(f"Dropping and recreating database (DESTRUCTIVE): {settings['database']}")

    docker_exec(
        settings["container"],
        ["psql", "-U", settings["user"], "-d", "postgres",
         "-c", f"DROP DATABASE IF EXISTS {settings['database']};"],
    )
    docker_exec(
        settings["container"],
        ["psql", "-U", settings["user"], "-d", "postgres",
         "-c", f"CREATE DATABASE {settings['database']};"],
    )
    log_success("Database dropped and recreated")

    cmd_migrate(args)


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage TMI PostgreSQL database")
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument("--test", action="store_true", help="Use ephemeral test container (port 5433)")
    parser.add_argument("--container", help="Override container name")
    parser.add_argument("--port", type=int, help="Override host port")
    parser.add_argument("--user-name", help="Override database user")
    parser.add_argument("--database", help="Override database name")
    parser.add_argument("--image", help="Override Docker image")
    parser.add_argument("--timeout", type=int, default=300, help="Wait timeout in seconds")

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("start", help="Start database container")
    subparsers.add_parser("stop", help="Stop database container")
    subparsers.add_parser("clean", help="Remove container and data")
    subparsers.add_parser("wait", help="Wait for database readiness")
    subparsers.add_parser("migrate", help="Run database migrations")
    subparsers.add_parser("reset", help="Drop, recreate, and migrate (DESTRUCTIVE)")

    args = parser.parse_args()
    apply_verbosity(args)

    commands = {
        "start": cmd_start,
        "stop": cmd_stop,
        "clean": cmd_clean,
        "wait": cmd_wait,
        "migrate": cmd_migrate,
        "reset": cmd_reset,
    }
    commands[args.command](args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Test the script manually**

```bash
# Verify it parses args and loads config correctly (--help)
uv run scripts/manage-database.py --help
uv run scripts/manage-database.py start --help
```

Expected: Help text with all subcommands and flags.

- [ ] **Step 3: Update Makefile targets for database management**

Replace the `start-database` target body (lines 146-179) with:

```makefile
start-database:
	@uv run scripts/manage-database.py start
```

Replace `stop-database` (lines 181-186):

```makefile
stop-database:
	@uv run scripts/manage-database.py stop
```

Replace `clean-database` (lines 188-194):

```makefile
clean-database:
	@uv run scripts/manage-database.py clean
```

Replace `start-test-database` (lines 233-235):

```makefile
start-test-database:
	@uv run scripts/manage-database.py --test start
```

Replace `stop-test-database` (lines 237-240):

```makefile
stop-test-database:
	@uv run scripts/manage-database.py --test stop
```

Replace `clean-test-database` (lines 242-245):

```makefile
clean-test-database:
	@uv run scripts/manage-database.py --test clean
```

Replace `wait-database` (lines 359-378):

```makefile
wait-database:
	@uv run scripts/manage-database.py wait
```

Replace `migrate-database` (lines 350-353):

```makefile
migrate-database:
	@uv run scripts/manage-database.py migrate
```

Replace `reset-database` (lines 380-392):

```makefile
reset-database:
	@uv run scripts/manage-database.py reset
```

Replace `wait-test-database` (lines 396-398):

```makefile
wait-test-database:
	@uv run scripts/manage-database.py --test --config config-test-integration-pg.yml wait
```

Replace `migrate-test-database` (lines 400-403):

```makefile
migrate-test-database:
	@uv run scripts/manage-database.py --config config-test-integration-pg.yml migrate
```

- [ ] **Step 4: Test that `make start-database` still works**

```bash
make start-database
make wait-database
make stop-database
```

Expected: Same behavior as before — container starts, readiness check passes, container stops.

- [ ] **Step 5: Test test-database targets**

```bash
make start-test-database
make wait-test-database
make stop-test-database
make clean-test-database
```

Expected: Ephemeral test container on port 5433 starts and stops.

- [ ] **Step 6: Commit**

```bash
git add scripts/manage-database.py Makefile
git commit -m "refactor: extract database management from Makefile to Python script

Moves start/stop/clean/wait/migrate/reset logic for both dev and test
PostgreSQL containers into scripts/manage-database.py. Makefile targets
now call the script.

Refs #215"
```

---

## Task 3: Infrastructure — Redis Management

**Files:**
- Create: `scripts/manage-redis.py`
- Modify: `Makefile` (lines 196-260)

- [ ] **Step 1: Create `scripts/manage-redis.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///

"""Manage TMI Redis containers.

Subcommands:
    start - Create/start the Redis container
    stop  - Stop the Redis container
    clean - Remove the Redis container

Use --test flag for ephemeral test containers (port 6380).
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    config_get,
    ensure_container,
    load_config,
    log_info,
    log_success,
    log_warning,
    remove_container,
    stop_container,
)

DEV_DEFAULTS = {
    "container": "tmi-redis",
    "port": 6379,
    "image": "tmi/tmi-redis:latest",
}

TEST_DEFAULTS = {
    "container": "tmi-redis-test",
    "port": 6380,
    "image": "tmi/tmi-redis:latest",
}


def get_redis_settings(args) -> dict:
    defaults = TEST_DEFAULTS if args.test else DEV_DEFAULTS
    cfg = load_config(args.config)
    return {
        "container": getattr(args, "container", None) or defaults["container"],
        "port": getattr(args, "port", None)
               or config_get(cfg, "database.redis.port")
               or defaults["port"],
        "image": getattr(args, "image", None) or defaults["image"],
    }


def cmd_start(args) -> None:
    settings = get_redis_settings(args)
    log_info(f"Starting Redis container ({settings['container']})...")
    ensure_container(
        name=settings["container"],
        host_port=settings["port"],
        container_port=6379,
        image=settings["image"],
    )


def cmd_stop(args) -> None:
    settings = get_redis_settings(args)
    log_info(f"Stopping Redis container ({settings['container']})...")
    stop_container(settings["container"])
    log_success("Redis container stopped")


def cmd_clean(args) -> None:
    settings = get_redis_settings(args)
    log_warning(f"Removing Redis container ({settings['container']})...")
    remove_container(settings["container"])
    log_success("Redis container removed")


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage TMI Redis containers")
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument("--test", action="store_true", help="Use ephemeral test container (port 6380)")
    parser.add_argument("--container", help="Override container name")
    parser.add_argument("--port", type=int, help="Override host port")
    parser.add_argument("--image", help="Override Docker image")

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("start", help="Start Redis container")
    subparsers.add_parser("stop", help="Stop Redis container")
    subparsers.add_parser("clean", help="Remove Redis container")

    args = parser.parse_args()
    apply_verbosity(args)

    commands = {"start": cmd_start, "stop": cmd_stop, "clean": cmd_clean}
    commands[args.command](args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets for Redis**

Replace `start-redis` (lines 196-214):
```makefile
start-redis:
	@uv run scripts/manage-redis.py start
```

Replace `stop-redis` (lines 216-221):
```makefile
stop-redis:
	@uv run scripts/manage-redis.py stop
```

Replace `clean-redis` (lines 223-228):
```makefile
clean-redis:
	@uv run scripts/manage-redis.py clean
```

Replace `start-test-redis` (lines 247-249):
```makefile
start-test-redis:
	@uv run scripts/manage-redis.py --test start
```

Replace `stop-test-redis` (lines 251-254):
```makefile
stop-test-redis:
	@uv run scripts/manage-redis.py --test stop
```

Replace `clean-test-redis` (lines 256-259):
```makefile
clean-test-redis:
	@uv run scripts/manage-redis.py --test clean
```

- [ ] **Step 3: Test Redis targets**

```bash
make start-redis
make stop-redis
make start-test-redis
make stop-test-redis
make clean-test-redis
```

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-redis.py Makefile
git commit -m "refactor: extract Redis management from Makefile to Python script

Refs #215"
```

---

## Task 4: Process Management — Server Lifecycle

**Files:**
- Create: `scripts/manage-server.py`
- Modify: `Makefile` (lines 409-488)

- [ ] **Step 1: Create `scripts/manage-server.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///

"""Manage the TMI server process.

Subcommands:
    start - Start the server (checks port, launches binary, writes PID file)
    stop  - Stop the server (PID file -> process name -> port kill)
    wait  - Wait until the server is responding on its port
"""

import argparse
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    config_get,
    get_pids_on_port,
    get_project_root,
    graceful_kill,
    is_port_in_use,
    kill_port,
    load_config,
    log_error,
    log_info,
    log_success,
    read_pid_file,
    run_cmd,
    wait_for_port,
    write_pid_file,
)

PROJECT_ROOT = get_project_root()


def cmd_start(args) -> None:
    cfg = load_config(args.config)
    port = args.port or int(config_get(cfg, "server.port", 8080))
    binary = args.binary or str(PROJECT_ROOT / "bin" / "tmiserver")
    config_file = args.config
    log_file = args.log_file or str(PROJECT_ROOT / "logs" / "server.log")
    pid_file = str(PROJECT_ROOT / ".server.pid")

    # Clean logs first
    logs_dir = Path(log_file).parent
    if logs_dir.exists():
        for f in logs_dir.iterdir():
            f.unlink(missing_ok=True)
    logs_dir.mkdir(parents=True, exist_ok=True)

    # Remove stale log files in project root
    for name in ("integration-test.log", "server.log"):
        (PROJECT_ROOT / name).unlink(missing_ok=True)
    stale_pid = PROJECT_ROOT / ".server.pid"
    if stale_pid.exists():
        # Check if PID is actually alive
        existing = read_pid_file(stale_pid)
        if existing is None:
            stale_pid.unlink(missing_ok=True)

    # Pre-flight: verify port is free
    if is_port_in_use(port):
        log_error(f"Port {port} is already in use.")
        log_info("Run 'make stop-server' first.")
        sys.exit(1)

    # Build with tags if requested
    if args.tags:
        log_info(f"Building server with tags: {args.tags}")
        run_cmd(
            ["go", "build", f"-tags={args.tags}", "-o", binary, "./cmd/server/"],
            cwd=PROJECT_ROOT,
        )

    log_info(f"Starting server binary: {binary}")
    with open(log_file, "w") as lf:
        proc = subprocess.Popen(
            [binary, f"--config={config_file}"],
            stdout=lf,
            stderr=lf,
            cwd=PROJECT_ROOT,
        )

    write_pid_file(pid_file, proc.pid)
    time.sleep(2)

    # Verify it's still running
    try:
        os.kill(proc.pid, 0)
    except ProcessLookupError:
        log_error(f"Server exited immediately. Check {log_file}")
        Path(pid_file).unlink(missing_ok=True)
        sys.exit(1)

    log_success(f"Server started with PID: {proc.pid}")


def cmd_stop(args) -> None:
    cfg = load_config(args.config)
    port = args.port or int(config_get(cfg, "server.port", 8080))
    pid_file = PROJECT_ROOT / ".server.pid"

    log_info("Stopping server...")

    # Layer 1: Kill via PID file
    pid = read_pid_file(pid_file)
    if pid:
        graceful_kill(pid)
        pid_file.unlink(missing_ok=True)

    # Layer 2: Kill tmiserver processes by name
    result = run_cmd(
        ["ps", "aux"], capture=True, check=False,
    )
    for line in result.stdout.split("\n"):
        if "bin/tmiserver" in line and "grep" not in line:
            parts = line.split()
            if len(parts) > 1:
                try:
                    graceful_kill(int(parts[1]))
                except ValueError:
                    pass

    # Layer 3: Kill anything on the port
    kill_port(port)

    # Verify port is free
    for _ in range(10):
        if not is_port_in_use(port):
            break
        time.sleep(0.5)
    else:
        log_error(f"Port {port} is still in use after stop attempts")
        sys.exit(1)

    pid_file.unlink(missing_ok=True)
    log_success("Server stopped")


def cmd_wait(args) -> None:
    cfg = load_config(args.config)
    port = args.port or int(config_get(cfg, "server.port", 8080))
    timeout = args.timeout or 300
    log_info(f"Waiting for server on port {port}...")
    wait_for_port(port, timeout=timeout, label="Server")


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage TMI server process")
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument("--port", type=int, help="Override server port")
    parser.add_argument("--binary", help="Override server binary path")
    parser.add_argument("--log-file", help="Override log file path")
    parser.add_argument("--tags", help="Go build tags")
    parser.add_argument("--timeout", type=int, default=300, help="Wait timeout in seconds")

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("start", help="Start the server")
    subparsers.add_parser("stop", help="Stop the server")
    subparsers.add_parser("wait", help="Wait for server readiness")

    args = parser.parse_args()
    apply_verbosity(args)

    commands = {"start": cmd_start, "stop": cmd_stop, "wait": cmd_wait}
    commands[args.command](args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets for server management**

Replace `start-server` (lines 415-446):
```makefile
start-server:
	@uv run scripts/manage-server.py start
```

Replace `stop-server` (lines 448-479):
```makefile
stop-server:
	@uv run scripts/manage-server.py stop
```

Replace `start-service` and `stop-service` (lines 481-483):
```makefile
start-service: start-server
stop-service: stop-server
```

Replace `wait-process` (lines 485-487):
```makefile
wait-process:
	@uv run scripts/manage-server.py wait
```

- [ ] **Step 3: Test server lifecycle**

```bash
make start-database
make start-redis
make wait-database
make build-server
make start-server
make wait-process
curl -s http://localhost:8080/ | jq .
make stop-server
```

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-server.py Makefile
git commit -m "refactor: extract server management from Makefile to Python script

Refs #215"
```

---

## Task 5: Process Management — Dev Environment Orchestration

**Files:**
- Create: `scripts/start-dev.py`
- Modify: `Makefile` (lines 702-731)

- [ ] **Step 1: Create `scripts/start-dev.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///

"""Start or restart the TMI development environment.

Orchestrates: database -> redis -> wait for DB -> start server.
With --restart: stop server -> rebuild -> clean logs -> start.
"""

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    log_info,
    log_success,
    run_cmd,
    get_project_root,
)


def main() -> None:
    parser = argparse.ArgumentParser(description="Start TMI development environment")
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument("--restart", action="store_true", help="Stop, rebuild, and restart")
    args = parser.parse_args()
    apply_verbosity(args)

    scripts_dir = Path(__file__).resolve().parent
    config_flag = ["--config", args.config]

    if args.restart:
        log_info("Restarting development environment")
        run_cmd(["uv", "run", str(scripts_dir / "manage-server.py")] + config_flag + ["stop"])
        run_cmd(["uv", "run", str(scripts_dir / "build-server.py")])
        run_cmd(["uv", "run", str(scripts_dir / "clean.py"), "logs"])

    log_info("Starting development environment")
    run_cmd(["uv", "run", str(scripts_dir / "manage-database.py")] + config_flag + ["start"])
    run_cmd(["uv", "run", str(scripts_dir / "manage-redis.py")] + config_flag + ["start"])
    run_cmd(["uv", "run", str(scripts_dir / "manage-database.py")] + config_flag + ["wait"])
    run_cmd(["uv", "run", str(scripts_dir / "manage-server.py")] + config_flag + ["start"])

    log_success("Development environment started on port 8080")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets**

Replace `start-dev` (lines 702-708):
```makefile
start-dev:
	@uv run scripts/start-dev.py
```

Replace `restart-dev` (lines 726-731):
```makefile
restart-dev:
	@uv run scripts/start-dev.py --restart
```

- [ ] **Step 3: Test**

```bash
make stop-server 2>/dev/null || true
make start-dev
curl -s http://localhost:8080/ | jq .
make stop-server
```

- [ ] **Step 4: Commit**

```bash
git add scripts/start-dev.py Makefile
git commit -m "refactor: extract dev environment orchestration from Makefile to Python script

Refs #215"
```

---

## Task 6: Testing — Unit Tests

**Files:**
- Create: `scripts/run-unit-tests.py`
- Modify: `Makefile` (lines 560-608)

- [ ] **Step 1: Create `scripts/run-unit-tests.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Run TMI unit tests with formatted output.

Executes Go unit tests, captures raw output, and presents a summary
showing only failures in detail and aggregate pass/fail/skip counts.

Options:
    --name NAME     Run specific test by name (-run flag)
    --count1        Run with --count=1 (disable caching)
    --passfail      Show only pass/fail status
"""

import argparse
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    log_error,
    log_info,
    log_success,
    run_cmd,
    get_project_root,
    BLUE, RED, GREEN, NC,
)

PACKAGES = ["./api/...", "./auth/...", "./cmd/...", "./internal/..."]


def main() -> None:
    parser = argparse.ArgumentParser(description="Run TMI unit tests")
    add_verbosity_args(parser)
    parser.add_argument("--name", help="Run specific test by name")
    parser.add_argument("--count1", action="store_true", help="Disable test caching")
    args = parser.parse_args()
    apply_verbosity(args)

    log_info("Running unit tests")

    cmd = ["go", "test", "-short"] + PACKAGES + ["-v"]
    if args.name:
        cmd.extend(["-run", args.name])
    if args.count1:
        cmd.extend(["--count=1"])

    env = {**os.environ, "LOGGING_IS_TEST": "true"}
    raw_output_file = tempfile.mktemp(prefix="tmi-test-unit-", dir="/tmp")

    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        env=env,
        cwd=get_project_root(),
    )

    output = result.stdout + result.stderr
    Path(raw_output_file).write_text(output)

    # Count results
    passed = len(re.findall(r"^--- PASS:", output, re.MULTILINE))
    failed = len(re.findall(r"^--- FAIL:", output, re.MULTILINE))
    skipped = len(re.findall(r"^--- SKIP:", output, re.MULTILINE))
    pkg_ok = len(re.findall(r"^ok ", output, re.MULTILINE))
    pkg_fail = len(re.findall(r"^FAIL\s", output, re.MULTILINE))

    print()

    # Show failed test details
    if failed > 0:
        print(f"{RED}=== FAILED TESTS (verbose output) ==={NC}")
        in_fail = False
        for line in output.split("\n"):
            if re.match(r"^--- FAIL:", line):
                in_fail = True
                print(line)
            elif re.match(r"^--- (PASS|SKIP):", line):
                in_fail = False
            elif re.match(r"^(=== RUN|ok |FAIL\t|PASS$)", line):
                in_fail = False
            elif in_fail:
                print(line)
        print(f"{RED}=== END FAILED TESTS ==={NC}")
        print()
        print(f"{RED}=== FAILED PACKAGES ==={NC}")
        for line in output.split("\n"):
            if re.match(r"^FAIL\s", line):
                print(line)
        print()

    # Package results
    print(f"{BLUE}=== PACKAGE RESULTS ==={NC}")
    for line in output.split("\n"):
        if re.match(r"^(ok |FAIL\s)", line):
            print(line)

    print()
    print(f"{BLUE}=== SUMMARY ==={NC}")
    print(f"  Tests:    {passed} passed, {failed} failed, {skipped} skipped")
    print(f"  Packages: {pkg_ok} passed, {pkg_fail} failed")
    print(f"  Raw log:  {raw_output_file}")
    print()

    if result.returncode != 0:
        log_error(f"Unit tests failed (exit code {result.returncode})")
        sys.exit(1)
    else:
        log_success("All unit tests passed")

    # Clean logs
    run_cmd(["uv", "run", str(Path(__file__).resolve().parent / "clean.py"), "logs"],
            check=False)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile target**

Replace `test-unit` (lines 560-608):
```makefile
test-unit:
	@uv run scripts/run-unit-tests.py $(if $(name),--name $(name),) $(if $(filter true,$(count1)),--count1,)
```

- [ ] **Step 3: Test**

```bash
make test-unit
make test-unit name=TestVersionEndpoint
```

- [ ] **Step 4: Commit**

```bash
git add scripts/run-unit-tests.py Makefile
git commit -m "refactor: extract unit test runner from Makefile to Python script

Refs #215"
```

---

## Task 7: Testing — Coverage

**Files:**
- Create: `scripts/run-coverage.py`
- Modify: `Makefile` (lines 734-808)

- [ ] **Step 1: Create `scripts/run-coverage.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Run TMI test coverage — unit, integration, merge, and report generation.

Subcommands:
    all              - Full coverage pipeline (unit + integration + merge + generate)
    --unit-only      - Unit test coverage only
    --integration-only - Integration test coverage only
    --merge-only     - Merge existing profiles
    --generate-only  - Generate HTML/text reports from existing profiles
"""

import argparse
import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

PROJECT_ROOT = get_project_root()
COV_DIR = PROJECT_ROOT / "coverage"
COV_HTML_DIR = PROJECT_ROOT / "coverage_html"
COV_MODE = "atomic"

UNIT_PROFILE = "unit_coverage.out"
UNIT_HTML = "unit_coverage.html"
UNIT_DETAILED = "unit_coverage_detailed.txt"
INTEGRATION_PROFILE = "integration_coverage.out"
INTEGRATION_HTML = "integration_coverage.html"
INTEGRATION_DETAILED = "integration_coverage_detailed.txt"
COMBINED_PROFILE = "combined_coverage.out"
COMBINED_HTML = "combined_coverage.html"
COMBINED_DETAILED = "combined_coverage_detailed.txt"
SUMMARY_FILE = "coverage_summary.txt"

UNIT_PACKAGES = ["./api/...", "./auth/...", "./cmd/...", "./internal/..."]
INTEGRATION_PACKAGES = ["./..."]


def run_unit_coverage() -> None:
    log_info("Running unit tests with coverage...")
    COV_DIR.mkdir(exist_ok=True)
    env = {**os.environ, "LOGGING_IS_TEST": "true"}
    run_cmd(
        ["go", "test", "-short",
         f"-coverprofile={COV_DIR / UNIT_PROFILE}",
         f"-covermode={COV_MODE}",
         "-coverpkg=./...",
         "-tags=!integration",
         "-timeout=5m", "-v"]
        + UNIT_PACKAGES,
        env=env, cwd=PROJECT_ROOT,
    )
    log_success("Unit test coverage completed")


def run_integration_coverage() -> None:
    log_info("Running integration tests with coverage...")
    COV_DIR.mkdir(exist_ok=True)
    env = {**os.environ, "LOGGING_IS_TEST": "true"}
    run_cmd(
        ["go", "test", "-short",
         f"-coverprofile={COV_DIR / INTEGRATION_PROFILE}",
         f"-covermode={COV_MODE}",
         "-coverpkg=./...",
         "-tags=integration",
         "-timeout=10m", "-v"]
        + INTEGRATION_PACKAGES,
        env=env, cwd=PROJECT_ROOT,
    )
    log_success("Integration test coverage completed")


def merge_coverage() -> None:
    log_info("Merging coverage profiles...")
    # Ensure gocovmerge is installed
    result = run_cmd(["which", "gocovmerge"], check=False, capture=True)
    if result.returncode != 0:
        log_info("Installing gocovmerge...")
        run_cmd(["go", "install", "github.com/wadey/gocovmerge@latest"])

    run_cmd([
        "gocovmerge",
        str(COV_DIR / UNIT_PROFILE),
        str(COV_DIR / INTEGRATION_PROFILE),
    ], cwd=PROJECT_ROOT)
    # gocovmerge outputs to stdout, redirect manually
    result = run_cmd(
        ["gocovmerge",
         str(COV_DIR / UNIT_PROFILE),
         str(COV_DIR / INTEGRATION_PROFILE)],
        capture=True, cwd=PROJECT_ROOT,
    )
    (COV_DIR / COMBINED_PROFILE).write_text(result.stdout)
    log_success("Coverage profiles merged")


def generate_reports() -> None:
    log_info("Generating coverage reports...")
    COV_HTML_DIR.mkdir(exist_ok=True)

    profiles = [
        (UNIT_PROFILE, UNIT_HTML, UNIT_DETAILED),
        (INTEGRATION_PROFILE, INTEGRATION_HTML, INTEGRATION_DETAILED),
        (COMBINED_PROFILE, COMBINED_HTML, COMBINED_DETAILED),
    ]

    for profile, html, detailed in profiles:
        profile_path = COV_DIR / profile
        if not profile_path.exists():
            continue
        run_cmd(["go", "tool", "cover", f"-html={profile_path}",
                 f"-o={COV_HTML_DIR / html}"], cwd=PROJECT_ROOT)
        result = run_cmd(
            ["go", "tool", "cover", f"-func={profile_path}"],
            capture=True, cwd=PROJECT_ROOT,
        )
        (COV_DIR / detailed).write_text(result.stdout)

    # Summary
    log_info("Generating coverage summary...")
    combined_path = COV_DIR / COMBINED_PROFILE
    if combined_path.exists():
        result = run_cmd(
            ["go", "tool", "cover", f"-func={combined_path}"],
            capture=True, cwd=PROJECT_ROOT,
        )
        last_line = result.stdout.strip().split("\n")[-1]
        summary = f"TMI Test Coverage Summary\nGenerated: {__import__('datetime').datetime.now()}\n{'=' * 38}\n\n{last_line}\n"
        (COV_DIR / SUMMARY_FILE).write_text(summary)
        print(summary)

    log_success(f"Coverage reports generated in {COV_DIR}/ and {COV_HTML_DIR}/")


def main() -> None:
    parser = argparse.ArgumentParser(description="Run TMI test coverage")
    add_verbosity_args(parser)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--unit-only", action="store_true", help="Unit coverage only")
    group.add_argument("--integration-only", action="store_true", help="Integration coverage only")
    group.add_argument("--merge-only", action="store_true", help="Merge profiles only")
    group.add_argument("--generate-only", action="store_true", help="Generate reports only")
    args = parser.parse_args()
    apply_verbosity(args)

    if args.unit_only:
        run_unit_coverage()
    elif args.integration_only:
        run_integration_coverage()
    elif args.merge_only:
        merge_coverage()
    elif args.generate_only:
        generate_reports()
    else:
        # Full pipeline
        log_info("Generating coverage reports")
        COV_DIR.mkdir(exist_ok=True)
        run_unit_coverage()
        run_integration_coverage()
        merge_coverage()
        generate_reports()


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets**

Replace `test-coverage` (lines 734-745):
```makefile
test-coverage:
	@trap '$(MAKE) -f $(MAKEFILE_LIST) clean-test-infrastructure' EXIT; \
	$(MAKE) -f $(MAKEFILE_LIST) clean-everything && \
	$(MAKE) -f $(MAKEFILE_LIST) start-database && \
	$(MAKE) -f $(MAKEFILE_LIST) start-redis && \
	$(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	uv run scripts/run-coverage.py
```

Replace `test-coverage-unit` (lines 754-764):
```makefile
test-coverage-unit:
	@uv run scripts/run-coverage.py --unit-only
```

Replace `test-coverage-integration` (lines 767-778):
```makefile
test-coverage-integration:
	@uv run scripts/run-coverage.py --integration-only
```

Replace `merge-coverage` (lines 780-789):
```makefile
merge-coverage:
	@uv run scripts/run-coverage.py --merge-only
```

Replace `generate-coverage` (lines 792-808):
```makefile
generate-coverage:
	@uv run scripts/run-coverage.py --generate-only
```

- [ ] **Step 3: Test**

```bash
make test-coverage-unit
```

- [ ] **Step 4: Commit**

```bash
git add scripts/run-coverage.py Makefile
git commit -m "refactor: extract coverage orchestration from Makefile to Python script

Refs #215"
```

---

## Task 8: OAuth Stub Management

**Files:**
- Create: `scripts/manage-oauth-stub.py`
- Modify: `Makefile` (lines 812-891)

- [ ] **Step 1: Create `scripts/manage-oauth-stub.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Manage the TMI OAuth callback stub.

Subcommands:
    start  - Start the OAuth stub daemon
    stop   - Graceful shutdown (magic URL -> SIGTERM -> SIGKILL)
    kill   - Force kill anything on port 8079
    status - Check if the stub is running
"""

import argparse
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_pids_on_port,
    get_project_root,
    graceful_kill,
    is_port_in_use,
    kill_port,
    log_error,
    log_info,
    log_success,
    log_warning,
    read_pid_file,
    run_cmd,
)

PROJECT_ROOT = get_project_root()
STUB_PORT = 8079
PID_FILE = PROJECT_ROOT / ".oauth-stub.pid"
STUB_SCRIPT = PROJECT_ROOT / "scripts" / "oauth-client-callback-stub.py"


def cmd_start(args) -> None:
    log_info(f"Starting OAuth callback stub on port {STUB_PORT}...")

    # Kill any existing stub
    kill_port(STUB_PORT)
    PID_FILE.unlink(missing_ok=True)

    run_cmd([
        "uv", "run", str(STUB_SCRIPT),
        "--port", str(STUB_PORT),
        "--daemon",
        "--pid-file", str(PID_FILE),
    ])

    # Wait for readiness
    for _ in range(10):
        if is_port_in_use(STUB_PORT):
            pid = PID_FILE.read_text().strip() if PID_FILE.exists() else "unknown"
            log_success(f"OAuth stub started on http://localhost:{STUB_PORT}/")
            log_info(f"Log file: /tmp/oauth-stub.log")
            log_info(f"PID: {pid}")
            return
        time.sleep(0.5)

    log_error(f"Failed to start OAuth stub (timeout after 5s)")
    PID_FILE.unlink(missing_ok=True)
    sys.exit(1)


def cmd_stop(args) -> None:
    log_info("Stopping OAuth callback stub...")

    # Step 1: Send magic exit URL
    log_info("Sending graceful shutdown request...")
    run_cmd(
        ["curl", "-s", f"http://localhost:{STUB_PORT}/?code=exit"],
        check=False, capture=True,
    )
    time.sleep(1)

    # Step 2: SIGTERM anything still on the port
    pids = get_pids_on_port(STUB_PORT)
    if pids:
        log_info(f"Found processes still on port {STUB_PORT}: {pids}")
        for pid in pids:
            graceful_kill(pid, timeout=2)

    # Step 3: SIGKILL survivors
    pids = get_pids_on_port(STUB_PORT)
    if pids:
        log_warning(f"Processes still running on port {STUB_PORT}: {pids}")
        for pid in pids:
            try:
                os.kill(pid, 9)
            except ProcessLookupError:
                pass
        time.sleep(1)

    PID_FILE.unlink(missing_ok=True)

    if not get_pids_on_port(STUB_PORT):
        log_success("OAuth stub stopped successfully")
    else:
        log_error(f"Failed to stop all processes on port {STUB_PORT}")


def cmd_kill(args) -> None:
    log_info(f"Force killing anything on port {STUB_PORT}...")
    kill_port(STUB_PORT)
    PID_FILE.unlink(missing_ok=True)
    log_success(f"Port {STUB_PORT} cleared")


def cmd_status(args) -> None:
    pid = read_pid_file(PID_FILE)
    if pid:
        log_success(f"OAuth stub is running (PID: {pid})")
        log_info(f"URL: http://localhost:{STUB_PORT}/")
        log_info(f"Latest endpoint: http://localhost:{STUB_PORT}/latest")
        return

    # Check for orphans
    pids = get_pids_on_port(STUB_PORT)
    if pids:
        log_warning(f"OAuth stub is running but no PID file found (PIDs: {pids})")
    else:
        log_info("OAuth stub is not running")


def main() -> None:
    parser = argparse.ArgumentParser(description="Manage TMI OAuth callback stub")
    add_verbosity_args(parser)

    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("start", help="Start the OAuth stub")
    subparsers.add_parser("stop", help="Stop the OAuth stub gracefully")
    subparsers.add_parser("kill", help="Force kill the OAuth stub")
    subparsers.add_parser("status", help="Check stub status")

    args = parser.parse_args()
    apply_verbosity(args)

    commands = {
        "start": cmd_start,
        "stop": cmd_stop,
        "kill": cmd_kill,
        "status": cmd_status,
    }
    commands[args.command](args)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets**

Replace `start-oauth-stub` (lines 813-828):
```makefile
start-oauth-stub:
	@uv run scripts/manage-oauth-stub.py start
```

Replace `stop-oauth-stub` (lines 830-864):
```makefile
stop-oauth-stub:
	@uv run scripts/manage-oauth-stub.py stop
```

Replace `kill-oauth-stub` (lines 866-870):
```makefile
kill-oauth-stub:
	@uv run scripts/manage-oauth-stub.py kill
```

Replace `check-oauth-stub` (lines 872-891):
```makefile
check-oauth-stub:
	@uv run scripts/manage-oauth-stub.py status
```

- [ ] **Step 3: Test**

```bash
make start-oauth-stub
make check-oauth-stub
make stop-oauth-stub
```

- [ ] **Step 4: Commit**

```bash
git add scripts/manage-oauth-stub.py Makefile
git commit -m "refactor: extract OAuth stub management from Makefile to Python script

Refs #215"
```

---

## Task 9: CATS Seed

**Files:**
- Create: `scripts/run-cats-seed.py`
- Modify: `Makefile` (lines 905-913)

- [ ] **Step 1: Create `scripts/run-cats-seed.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///

"""Build and run CATS database seeding tool.

Builds the cats-seed Go binary and runs it to create test objects.
Use --oci for Oracle ADB support (requires scripts/oci-env.sh).
"""

import argparse
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_config_arg,
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

PROJECT_ROOT = get_project_root()


def main() -> None:
    parser = argparse.ArgumentParser(description="Build and run CATS database seeding")
    add_config_arg(parser)
    add_verbosity_args(parser)
    parser.add_argument("--oci", action="store_true", help="Build with Oracle support")
    parser.add_argument("--user", default="charlie", help="CATS user (default: charlie)")
    parser.add_argument("--provider", default="tmi", help="Auth provider (default: tmi)")
    parser.add_argument("--server", default="http://localhost:8080", help="Server URL")
    args = parser.parse_args()
    apply_verbosity(args)

    binary = str(PROJECT_ROOT / "bin" / "cats-seed")

    if args.oci:
        oci_env = PROJECT_ROOT / "scripts" / "oci-env.sh"
        if not oci_env.exists():
            log_error("scripts/oci-env.sh not found. Copy from scripts/oci-env.sh.example and configure.")
            sys.exit(1)
        log_info("Building CATS seeding tool with Oracle support...")
        run_cmd(
            ["/bin/bash", "-c",
             f". scripts/oci-env.sh && go build -tags oracle -o {binary} github.com/ericfitz/tmi/cmd/cats-seed"],
            cwd=PROJECT_ROOT,
        )
        config_file = str(PROJECT_ROOT / "config-development-oci.yml")
    else:
        log_info("Building CATS seeding tool...")
        run_cmd(
            ["go", "build", "-o", binary, "github.com/ericfitz/tmi/cmd/cats-seed"],
            cwd=PROJECT_ROOT,
        )
        config_file = args.config

    log_info("Seeding CATS test data...")
    run_cmd([
        binary,
        f"--config={config_file}",
        f"--user={args.user}",
        f"--provider={args.provider}",
        f"--server={args.server}",
    ])
    log_success("CATS database seeding completed")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update Makefile targets**

Replace `cats-seed` (lines 905-908):
```makefile
cats-seed:
	@uv run scripts/run-cats-seed.py --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)
```

Replace `cats-seed-oci` (lines 910-913):
```makefile
cats-seed-oci:
	@uv run scripts/run-cats-seed.py --oci --user=$(CATS_USER) --provider=$(CATS_PROVIDER)
```

- [ ] **Step 3: Commit**

```bash
git add scripts/run-cats-seed.py Makefile
git commit -m "refactor: extract CATS seeding from Makefile to Python script

Refs #215"
```

---

## Task 10: Validation — OpenAPI and Unsafe Union Methods

**Files:**
- Create: `scripts/validate-openapi-spec.py`
- Create: `scripts/check-unsafe-union-methods.py`
- Modify: `Makefile` (lines 317-336, 1440-1469)

- [ ] **Step 1: Create `scripts/validate-openapi-spec.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Validate the TMI OpenAPI specification.

Runs JSON syntax validation (jq), then OpenAPI linting with Vacuum
(including OWASP rules). Reports errors and optionally loads results
into SQLite for analysis.
"""

import argparse
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
    run_cmd,
)

PROJECT_ROOT = get_project_root()
DEFAULT_SPEC = PROJECT_ROOT / "api-schema" / "tmi-openapi.json"
DEFAULT_REPORT = PROJECT_ROOT / "test" / "outputs" / "api-validation" / "openapi-validation-report.json"
DEFAULT_DB = PROJECT_ROOT / "test" / "outputs" / "api-validation" / "openapi-validation.db"


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate TMI OpenAPI specification")
    add_verbosity_args(parser)
    parser.add_argument("--spec", default=str(DEFAULT_SPEC), help="Path to OpenAPI spec")
    parser.add_argument("--report", default=str(DEFAULT_REPORT), help="Validation report output path")
    parser.add_argument("--db", default=str(DEFAULT_DB), help="SQLite database output path")
    args = parser.parse_args()
    apply_verbosity(args)

    spec = Path(args.spec)
    report = Path(args.report)
    db = Path(args.db)

    # Step 1: JSON syntax validation
    log_info("Validating JSON syntax...")
    result = run_cmd(["jq", "empty", str(spec)], check=False, capture=True)
    if result.returncode != 0:
        log_error(f"Invalid JSON syntax in {spec}")
        run_cmd(["jq", "empty", str(spec)])  # Show the error
        sys.exit(1)
    log_success("JSON syntax is valid")

    # Step 2: Vacuum linting
    if not shutil.which("vacuum"):
        log_error("Vacuum not found — required for OpenAPI validation")
        log_info("Install with: brew install vacuum")
        sys.exit(1)

    log_info("Running Vacuum OpenAPI analysis (with OWASP rules)...")
    report.parent.mkdir(parents=True, exist_ok=True)
    ruleset = PROJECT_ROOT / "vacuum-ruleset.yaml"
    run_cmd([
        "vacuum", "report", str(spec),
        "-r", str(ruleset),
        "--no-style", "-o",
    ], capture=True, check=False)

    # vacuum report outputs to file specified by -o, but we use stdout redirect
    result = run_cmd([
        "vacuum", "report", str(spec),
        "-r", str(ruleset),
        "--no-style", "-o",
    ], capture=True, check=False)
    report.write_text(result.stdout)

    # Parse results
    import json
    try:
        data = json.loads(result.stdout)
        errors = data.get("resultSet", {}).get("errorCount", 0)
        warnings = data.get("resultSet", {}).get("warningCount", 0)
        infos = data.get("resultSet", {}).get("infoCount", 0)
    except (json.JSONDecodeError, KeyError):
        errors, warnings, infos = 0, 0, 0

    log_info(f"Results: {errors} errors, {warnings} warnings, {infos} info")

    if errors > 0:
        log_error(f"Validation failed with {errors} errors")
        log_info("Loading results into SQLite database for analysis...")
        run_cmd([
            "uv", "run", str(PROJECT_ROOT / "scripts" / "parse-openapi-validation.py"),
            "--report", str(report),
            "--db", str(db),
            "--summary",
        ])
        log_info(f"Query database: sqlite3 {db} 'SELECT * FROM error_summary'")
        sys.exit(1)

    log_success(f"OpenAPI validation complete. Report: {report}")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Create `scripts/check-unsafe-union-methods.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Check that non-generated code doesn't use unsafe generated From*/Merge* methods.

These methods corrupt discriminator values (see api/cell_union_helpers.go).
"""

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_error,
    log_info,
    log_success,
)

UNSAFE_PATTERN = re.compile(r"\.(FromNode|MergeNode|FromMinimalNode|MergeMinimalNode)\b")
EXCLUDED_FILES = {"api/api.go", "api/cell_union_helpers_test.go"}


def main() -> None:
    log_info("Checking for unsafe generated union method calls...")

    project_root = get_project_root()
    api_dir = project_root / "api"
    violations = []

    for go_file in api_dir.glob("*.go"):
        rel = str(go_file.relative_to(project_root))
        if rel in EXCLUDED_FILES:
            continue

        for i, line in enumerate(go_file.read_text().split("\n"), 1):
            # Skip comments
            stripped = line.lstrip()
            if stripped.startswith("//"):
                continue
            if UNSAFE_PATTERN.search(line):
                violations.append(f"{rel}:{i}: {line.strip()}")

    if violations:
        log_error("Found unsafe generated union method calls:")
        for v in violations:
            print(v)
        print()
        print("Use SafeFromNode() or SafeFromEdge() instead (see api/cell_union_helpers.go).")
        print("The generated FromNode/MergeNode methods corrupt the shape discriminator field.")
        sys.exit(1)

    log_success("No unsafe generated union method calls found")


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Update Makefile targets**

Replace `check-unsafe-union-methods` (lines 319-336):
```makefile
check-unsafe-union-methods:
	@uv run scripts/check-unsafe-union-methods.py
```

Replace `validate-openapi` (lines 1440-1469):
```makefile
validate-openapi:
	@uv run scripts/validate-openapi-spec.py
```

- [ ] **Step 4: Test**

```bash
make check-unsafe-union-methods
make validate-openapi
```

- [ ] **Step 5: Commit**

```bash
git add scripts/validate-openapi-spec.py scripts/check-unsafe-union-methods.py Makefile
git commit -m "refactor: extract validation checks from Makefile to Python scripts

Refs #215"
```

---

## Task 11: Status & Cleanup

**Files:**
- Create: `scripts/status.py`
- Create: `scripts/clean.py`
- Modify: `Makefile` (lines 488-546, 1486-1568)

- [ ] **Step 1: Create `scripts/status.py`**

This is a large script — it checks server, database, Redis, application, and OAuth stub status. Due to its size, create it following the exact output format of the current `status` target (the tabular format with colored indicators).

The script should:
- Check server on port 8080 by PID file and port scan, then curl for health info
- Check database container by `docker ps --filter name=tmi-postgresql`
- Check Redis container by `docker ps --filter name=tmi-redis`
- Check application on port 4200 by `lsof`
- Check OAuth stub on port 8079 by PID file and port scan
- Print the same formatted table with status indicators

- [ ] **Step 2: Create `scripts/clean.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Clean up TMI files, logs, containers, and processes.

Subcommands:
    logs        - Remove log files
    files       - Remove logs, PID files, and CATS artifacts
    containers  - Stop and remove Docker containers
    process     - Stop server and OAuth stub
    all         - Everything (process + containers + redis + test infra + logs + files)
"""

import argparse
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    kill_port,
    log_info,
    log_success,
    run_cmd,
)

PROJECT_ROOT = get_project_root()


def clean_logs() -> None:
    log_info("Cleaning up log files...")
    for name in ("integration-test.log", "server.log", ".server.pid"):
        (PROJECT_ROOT / name).unlink(missing_ok=True)
    logs_dir = PROJECT_ROOT / "logs"
    if logs_dir.exists():
        for f in logs_dir.iterdir():
            f.unlink(missing_ok=True)
    log_success("Log files cleaned")


def clean_files() -> None:
    clean_logs()
    log_info("Cleaning CATS artifacts...")
    run_cmd(["pkill", "-f", "cats"], check=False, capture=True)
    import time
    time.sleep(1)
    cats_dir = PROJECT_ROOT / "test" / "outputs" / "cats"
    if cats_dir.exists():
        preserve = {"cats-results.db", "cats-results.db-shm", "cats-results.db-wal"}
        for item in cats_dir.iterdir():
            if item.name not in preserve:
                if item.is_dir():
                    shutil.rmtree(item)
                else:
                    item.unlink()
    cats_report = PROJECT_ROOT / "cats-report"
    if cats_report.exists():
        shutil.rmtree(cats_report)
    log_success("File cleanup completed")


def clean_process() -> None:
    scripts_dir = Path(__file__).resolve().parent
    run_cmd(["uv", "run", str(scripts_dir / "manage-server.py"), "stop"], check=False)
    run_cmd(["uv", "run", str(scripts_dir / "manage-oauth-stub.py"), "stop"], check=False)


def clean_all() -> None:
    scripts_dir = Path(__file__).resolve().parent
    clean_process()
    run_cmd(["uv", "run", str(scripts_dir / "manage-redis.py"), "clean"], check=False)
    run_cmd(["uv", "run", str(scripts_dir / "manage-database.py"), "--test", "clean"], check=False)
    run_cmd(["uv", "run", str(scripts_dir / "manage-redis.py"), "--test", "clean"], check=False)
    clean_files()


def main() -> None:
    parser = argparse.ArgumentParser(description="Clean TMI artifacts")
    add_verbosity_args(parser)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("logs", help="Clean log files")
    subparsers.add_parser("files", help="Clean logs, PID files, CATS artifacts")
    subparsers.add_parser("process", help="Stop server and OAuth stub")
    subparsers.add_parser("all", help="Full cleanup")

    args = parser.parse_args()
    apply_verbosity(args)

    commands = {
        "logs": clean_logs,
        "files": clean_files,
        "process": clean_process,
        "all": clean_all,
    }
    commands[args.command]()


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Update Makefile targets**

Replace `clean-logs` (lines 496-503):
```makefile
clean-logs:
	@uv run scripts/clean.py logs
```

Replace `clean-files` (lines 505-531):
```makefile
clean-files:
	@uv run scripts/clean.py files
```

Replace `clean-process` (line 544):
```makefile
clean-process:
	@uv run scripts/clean.py process
```

Replace `clean-everything` (line 546):
```makefile
clean-everything:
	@uv run scripts/clean.py all
```

Replace `status` (lines 1488-1568):
```makefile
status:
	@uv run scripts/status.py
```

- [ ] **Step 4: Test**

```bash
make status
make clean-logs
```

- [ ] **Step 5: Commit**

```bash
git add scripts/status.py scripts/clean.py Makefile
git commit -m "refactor: extract status and cleanup from Makefile to Python scripts

Refs #215"
```

---

## Task 12: Build, Generate, SBOM, Lint

**Files:**
- Create: `scripts/build-server.py`
- Create: `scripts/generate-api.py`
- Create: `scripts/generate-sbom.py`
- Create: `scripts/lint.py`
- Modify: `Makefile` (lines 270-316, 1196-1198, 1350-1393)

- [ ] **Step 1: Create `scripts/build-server.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Build TMI Go binaries.

Components: server (default), migrate, cats-seed.
"""

import argparse
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    format_version,
    get_project_root,
    log_info,
    log_success,
    read_version,
    run_cmd,
)

PROJECT_ROOT = get_project_root()

COMPONENTS = {
    "server": {
        "output": "bin/tmiserver",
        "package": "github.com/ericfitz/tmi/cmd/server",
        "tags": "dev",
        "ldflags": True,
    },
    "migrate": {
        "output": "bin/migrate",
        "package": "github.com/ericfitz/tmi/cmd/migrate",
        "tags": None,
        "ldflags": False,
    },
    "cats-seed": {
        "output": "bin/cats-seed",
        "package": "github.com/ericfitz/tmi/cmd/cats-seed",
        "tags": None,
        "ldflags": False,
    },
}


def main() -> None:
    parser = argparse.ArgumentParser(description="Build TMI Go binaries")
    add_verbosity_args(parser)
    parser.add_argument("--component", default="server",
                        choices=list(COMPONENTS.keys()),
                        help="Component to build (default: server)")
    parser.add_argument("--tags", help="Additional build tags")
    parser.add_argument("--oci", action="store_true",
                        help="Build with Oracle support (cats-seed only)")
    args = parser.parse_args()
    apply_verbosity(args)

    comp = COMPONENTS[args.component]
    log_info(f"Building {args.component} binary...")

    cmd = ["go", "build"]

    # Tags
    tags = args.tags or comp["tags"]
    if args.oci and args.component == "cats-seed":
        tags = "oracle"
    if tags:
        cmd.append(f"-tags={tags}")

    # Ldflags for server
    if comp["ldflags"]:
        v = read_version()
        commit = run_cmd(
            ["git", "rev-parse", "--short", "HEAD"],
            capture=True, check=False, cwd=PROJECT_ROOT,
        ).stdout.strip() or "development"
        build_date = run_cmd(
            ["date", "-u", "+%Y-%m-%dT%H:%M:%SZ"],
            capture=True,
        ).stdout.strip()
        ldflags = (
            f"-X github.com/ericfitz/tmi/api.VersionMajor={v['major']} "
            f"-X github.com/ericfitz/tmi/api.VersionMinor={v['minor']} "
            f"-X github.com/ericfitz/tmi/api.VersionPatch={v['patch']} "
            f"-X github.com/ericfitz/tmi/api.VersionPreRelease={v.get('prerelease', '')} "
            f"-X github.com/ericfitz/tmi/api.GitCommit={commit} "
            f"-X github.com/ericfitz/tmi/api.BuildDate={build_date}"
        )
        cmd.extend(["-ldflags", ldflags])

    cmd.extend(["-o", comp["output"], comp["package"]])

    if args.oci:
        oci_env = PROJECT_ROOT / "scripts" / "oci-env.sh"
        if not oci_env.exists():
            from tmi_common import log_error
            log_error("scripts/oci-env.sh not found.")
            sys.exit(1)
        run_cmd(
            ["/bin/bash", "-c", f". scripts/oci-env.sh && {' '.join(cmd)}"],
            cwd=PROJECT_ROOT,
        )
    else:
        run_cmd(cmd, cwd=PROJECT_ROOT)

    log_success(f"Binary built: {comp['output']}")


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Create `scripts/generate-api.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Generate API code from OpenAPI specification using oapi-codegen."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)


def main() -> None:
    project_root = get_project_root()
    log_info("Generating API code from OpenAPI specification...")
    run_cmd(
        ["oapi-codegen", "-config", "oapi-codegen-config.yml",
         "api-schema/tmi-openapi.json"],
        cwd=project_root,
    )
    log_success("API code generated: api/api.go")


if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Create `scripts/generate-sbom.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Generate Software Bill of Materials (SBOM) for TMI.

Uses cyclonedx-gomod to produce CycloneDX 1.6 JSON and XML SBOMs.
"""

import argparse
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    format_version,
    get_project_root,
    log_error,
    log_info,
    log_success,
    read_version,
    run_cmd,
)

PROJECT_ROOT = get_project_root()


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate TMI SBOM")
    add_verbosity_args(parser)
    parser.add_argument("--all", action="store_true", help="Also generate module SBOMs")
    args = parser.parse_args()
    apply_verbosity(args)

    if not shutil.which("cyclonedx-gomod"):
        log_error("cyclonedx-gomod not found")
        log_info("Install: brew install cyclonedx/cyclonedx/cyclonedx-gomod")
        sys.exit(1)

    version = format_version(read_version())
    sbom_dir = PROJECT_ROOT / "security-reports" / "sbom"
    sbom_dir.mkdir(parents=True, exist_ok=True)

    log_info("Generating SBOM for Go application...")
    for fmt, ext in [("-json", "json"), ("", "xml")]:
        out = sbom_dir / f"tmi-server-{version}-sbom.{ext}"
        cmd = ["cyclonedx-gomod", "app"]
        if fmt:
            cmd.append(fmt)
        cmd.extend(["-output", str(out), "-main", "cmd/server"])
        run_cmd(cmd, cwd=PROJECT_ROOT)
        log_success(f"SBOM generated: {out}")

    if args.all:
        log_info("Generating module SBOMs...")
        for fmt, ext in [("-json", "json"), ("", "xml")]:
            out = sbom_dir / f"tmi-module-{version}-sbom.{ext}"
            cmd = ["cyclonedx-gomod", "mod"]
            if fmt:
                cmd.append(fmt)
            cmd.extend(["-output", str(out)])
            run_cmd(cmd, cwd=PROJECT_ROOT)
        log_success("All Go SBOMs generated")


if __name__ == "__main__":
    main()
```

- [ ] **Step 4: Create `scripts/lint.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Run TMI linting: unsafe union method check + golangci-lint."""

import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)

PACKAGES = ["./api/...", "./auth/...", "./cmd/...", "./internal/..."]


def main() -> None:
    project_root = get_project_root()
    scripts_dir = Path(__file__).resolve().parent

    # Step 1: Check unsafe union methods
    run_cmd(["uv", "run", str(scripts_dir / "check-unsafe-union-methods.py")])

    # Step 2: golangci-lint
    log_info("Running golangci-lint...")
    golangci = os.path.expanduser("~/go/bin/golangci-lint")
    run_cmd([golangci, "run"] + PACKAGES, cwd=project_root)
    log_success("Linting passed")


if __name__ == "__main__":
    main()
```

- [ ] **Step 5: Update Makefile targets**

Replace `build-server` (lines 270-285):
```makefile
build-server:
	@uv run scripts/build-server.py
```

Replace `build-migrate` (lines 287-290):
```makefile
build-migrate:
	@uv run scripts/build-server.py --component migrate
```

Replace `build-cats-seed` (lines 292-295):
```makefile
build-cats-seed:
	@uv run scripts/build-server.py --component cats-seed
```

Replace `build-cats-seed-oci` (lines 297-304):
```makefile
build-cats-seed-oci:
	@uv run scripts/build-server.py --component cats-seed --oci
```

Replace `clean-build` (lines 306-310):
```makefile
clean-build:
	@rm -rf ./bin/*
	@rm -f migrate
```

Replace `generate-api` (lines 312-315):
```makefile
generate-api:
	@uv run scripts/generate-api.py
```

Replace `lint` (lines 1196-1197):
```makefile
lint:
	@uv run scripts/lint.py
```

Replace `generate-sbom` (lines 1378-1390):
```makefile
generate-sbom:
	@uv run scripts/generate-sbom.py $(if $(filter true,$(ALL)),--all,)
```

- [ ] **Step 6: Test**

```bash
make build-server
make lint
make generate-api
```

- [ ] **Step 7: Commit**

```bash
git add scripts/build-server.py scripts/generate-api.py scripts/generate-sbom.py scripts/lint.py Makefile
git commit -m "refactor: extract build, generate, SBOM, and lint from Makefile to Python scripts

Refs #215"
```

---

## Task 13: Remaining Targets — Deploy, WebSocket, Help

**Files:**
- Create: `scripts/deploy-heroku.py`
- Create: `scripts/run-wstest.py`
- Create: `scripts/help.py`
- Modify: `Makefile` (lines 1229-1248, 1271-1343, 1574-1681)

- [ ] **Step 1: Create `scripts/deploy-heroku.py`**

```python
#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Deploy TMI to Heroku: build, commit, push to GitHub and Heroku."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    get_project_root,
    log_info,
    log_success,
    run_cmd,
)


def main() -> None:
    project_root = get_project_root()

    log_info("Starting Heroku deployment...")

    # Build
    log_info("Building server binary...")
    run_cmd(["uv", "run", str(Path(__file__).resolve().parent / "build-server.py")],
            cwd=project_root)

    # Check for uncommitted changes
    log_info("Checking git status...")
    result = run_cmd(["git", "status", "--porcelain"], capture=True, cwd=project_root)
    if result.stdout.strip():
        log_info("Committing changes...")
        run_cmd(["git", "add", "-A"], cwd=project_root)
        run_cmd(
            ["git", "commit", "-m", "chore: Build and deploy to Heroku [skip ci]"],
            check=False, cwd=project_root,
        )
    else:
        log_info("No changes to commit")

    log_info("Pushing to GitHub main branch...")
    run_cmd(["git", "push", "origin", "main"], cwd=project_root)

    log_info("Pushing to Heroku...")
    run_cmd(["git", "push", "heroku", "main"], cwd=project_root)

    log_success("Deployment complete!")
    log_info("Checking deployment status...")
    run_cmd(["heroku", "releases", "--app", "tmi-server"], cwd=project_root)


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Create `scripts/run-wstest.py`**

This script replicates the terminal-spawning logic from the `wstest` target. It checks for macOS Terminal/iTerm, falls back to gnome-terminal/xterm, and finally background processes.

The script should:
- Check server is running on port 8080
- Build the wstest binary (`cd wstest && go mod tidy && go build -o wstest`)
- Spawn 3 terminals: alice (host), bob (participant), charlie (participant)
- Use `osascript` on macOS, `gnome-terminal`/`xterm` on Linux, background fallback

- [ ] **Step 3: Create `scripts/help.py`**

This script prints the same help text currently in the `help` target. It should read available Make targets or maintain its own list.

- [ ] **Step 4: Update Makefile targets**

Replace `deploy-heroku` (lines 1230-1248):
```makefile
deploy-heroku:
	@uv run scripts/deploy-heroku.py
```

Replace `wstest` (lines 1278-1321):
```makefile
wstest: build-wstest
	@uv run scripts/run-wstest.py
```

Replace `help` (lines 1574-1681):
```makefile
help:
	@uv run scripts/help.py
```

- [ ] **Step 5: Commit**

```bash
git add scripts/deploy-heroku.py scripts/run-wstest.py scripts/help.py Makefile
git commit -m "refactor: extract deploy, wstest, and help from Makefile to Python scripts

Refs #215"
```

---

## Task 14: Final Cleanup — Remove Macros and Dead Code

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Remove Makefile macros**

Delete the following from the Makefile:
- Color variable definitions (lines 28-32): `BLUE`, `GREEN`, `YELLOW`, `RED`, `NC`
- Logging function defines (lines 34-48): `log_info`, `log_success`, `log_warning`, `log_error`
- `graceful_kill` define (lines 57-66)
- `kill_port` define (lines 70-84)
- `ensure_container` define (lines 88-97)
- `wait_for_ready` define (lines 101-115)

Keep: `VERSION`, `COMMIT`, `BUILD_DATE`, `SHELL`, `.SHELLFLAGS`, `PATH` export, `SERVER_PORT`, `.DEFAULT_GOAL`, `.PHONY` declarations, coverage variable definitions (if still referenced by any remaining target), `CATS_*` defaults, `TF_*` variables, `OPENAPI_*`/`ASYNCAPI_*` variables.

- [ ] **Step 2: Remove coverage variable definitions that are no longer referenced**

The `COVERAGE_*` variables (lines 117-138) and `TOOLS_GOCOVMERGE` are now handled inside `scripts/run-coverage.py`. Remove them from the Makefile.

- [ ] **Step 3: Verify all targets still work**

```bash
make list-targets
make build-server
make test-unit name=TestVersionEndpoint
make lint
make status
make help
```

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "refactor: remove Makefile macros and dead variables after script extraction

Completes the migration of inline logic to standalone Python scripts.
The Makefile is now a thin pass-through layer.

Closes #215"
```

---

## Task 15: Update `container_build_helpers.py` to Use Shared Library (Optional)

**Files:**
- Modify: `scripts/container_build_helpers.py`

This is an optional follow-up: the existing `container_build_helpers.py` has its own logging, version reading, and subprocess helpers that duplicate `tmi_common.py`. If desired, refactor it to import from `tmi_common` instead.

- [ ] **Step 1: Replace duplicated functions**

Update `container_build_helpers.py` to import `log_info`, `log_success`, `log_warn`, `log_error`, `run`, `get_project_root`, `read_version`, `format_version` from `tmi_common` instead of defining them locally.

- [ ] **Step 2: Test container builds still work**

```bash
make build-app
```

- [ ] **Step 3: Commit**

```bash
git add scripts/container_build_helpers.py
git commit -m "refactor: deduplicate container_build_helpers by importing from tmi_common

Refs #215"
```
