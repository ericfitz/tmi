"""Shared utilities for TMI scripts.

This module is not run directly — it is imported by other scripts via sys.path
or the scripts/lib package.
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

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[1;33m"
BLUE = "\033[0;34m"
NC = "\033[0m"

_quiet: bool = False


def set_quiet(value: bool) -> None:
    """Set the global quiet flag. When True, log_info output is suppressed."""
    global _quiet
    _quiet = value


def log_info(msg: str) -> None:
    """Print an informational message (blue). Suppressed when quiet."""
    if not _quiet:
        print(f"{BLUE}[INFO]{NC} {msg}", flush=True)


def log_success(msg: str) -> None:
    """Print a success message (green). Never suppressed."""
    print(f"{GREEN}[SUCCESS]{NC} {msg}", flush=True)


def log_warning(msg: str) -> None:
    """Print a warning message (yellow). Never suppressed."""
    print(f"{YELLOW}[WARNING]{NC} {msg}", flush=True)


def log_error(msg: str) -> None:
    """Print an error message (red) to stderr. Never suppressed."""
    print(f"{RED}[ERROR]{NC} {msg}", file=sys.stderr, flush=True)


# ---------------------------------------------------------------------------
# Project paths
# ---------------------------------------------------------------------------


def get_project_root() -> Path:
    """Return the project root directory (grandparent of scripts/lib/)."""
    return Path(__file__).resolve().parent.parent.parent


# ---------------------------------------------------------------------------
# Config loading
# ---------------------------------------------------------------------------


def load_config(path: str | Path | None = None) -> dict:
    """Load a YAML config file.

    Defaults to config-development.yml in the project root if path is None.
    Requires PyYAML; exits with an error message if not installed.
    """
    try:
        import yaml
    except ImportError:
        log_error(
            "PyYAML is required but not installed. "
            "Install it with: uv pip install pyyaml"
        )
        sys.exit(1)

    if path is None:
        path = get_project_root() / "config-development.yml"

    path = Path(path)
    try:
        with path.open() as fh:
            return yaml.safe_load(fh) or {}
    except FileNotFoundError:
        log_error(f"Config file not found: {path}")
        sys.exit(1)
    except yaml.YAMLError as exc:
        log_error(f"Failed to parse config file {path}: {exc}")
        sys.exit(1)


def config_get(cfg: dict, dotpath: str, default=None):
    """Get a nested config value by dot-separated path.

    Example:
        config_get(cfg, "database.redis.port", 6379)
    """
    keys = dotpath.split(".")
    node = cfg
    for key in keys:
        if not isinstance(node, dict):
            return default
        node = node.get(key)
        if node is None:
            return default
    return node


# ---------------------------------------------------------------------------
# Version
# ---------------------------------------------------------------------------


def read_version() -> dict:
    """Read version from .version JSON file in the project root.

    Returns a dict with keys: major, minor, patch, prerelease.
    """
    version_file = get_project_root() / ".version"
    try:
        data = json.loads(version_file.read_text())
        for key in ("major", "minor", "patch", "prerelease"):
            if key not in data:
                log_error(
                    f".version file missing '{key}' key. "
                    "Expected JSON with major, minor, patch, prerelease keys."
                )
                sys.exit(1)
        return data
    except FileNotFoundError:
        log_error(
            f"Cannot read {version_file}. "
            "Ensure .version file exists with valid JSON."
        )
        sys.exit(1)
    except json.JSONDecodeError as exc:
        log_error(f".version file contains invalid JSON: {exc}")
        sys.exit(1)


def format_version(v: dict) -> str:
    """Format a version dict as a string.

    Examples:
        {"major": 1, "minor": 3, "patch": 0, "prerelease": ""} -> "1.3.0"
        {"major": 1, "minor": 3, "patch": 0, "prerelease": "rc.0"} -> "1.3.0-rc.0"
    """
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
    env: dict | None = None,
    verbose: bool = False,
) -> subprocess.CompletedProcess:
    """Run a subprocess command.

    Args:
        cmd: Command and arguments as a list.
        check: If True, raise CalledProcessError on non-zero exit.
        capture: If True, capture stdout and stderr.
        cwd: Working directory for the command.
        env: Additional environment variables (merged with os.environ).
        verbose: If True, log the command before running.

    Returns:
        CompletedProcess instance.
    """
    merged_env = None
    if env is not None:
        merged_env = {**os.environ, **env}

    if verbose:
        log_info(f"Running: {' '.join(str(c) for c in cmd)}")

    return subprocess.run(
        cmd,
        check=check,
        capture_output=capture,
        text=True,
        cwd=cwd,
        env=merged_env,
    )


# ---------------------------------------------------------------------------
# Docker helpers
# ---------------------------------------------------------------------------


def container_exists(name: str) -> bool:
    """Return True if a Docker container with the given name exists (any state)."""
    result = run_cmd(
        ["docker", "ps", "-a", "--filter", f"name=^{name}$", "--format", "{{.Names}}"],
        capture=True,
        check=False,
    )
    return name in result.stdout.splitlines()


def container_is_running(name: str) -> bool:
    """Return True if a Docker container with the given name is running."""
    result = run_cmd(
        ["docker", "ps", "--filter", f"name=^{name}$", "--format", "{{.Names}}"],
        capture=True,
        check=False,
    )
    return name in result.stdout.splitlines()


def ensure_volume(name: str) -> None:
    """Create a Docker volume if it does not already exist."""
    result = run_cmd(
        ["docker", "volume", "ls", "--filter", f"name=^{name}$", "--format", "{{.Name}}"],
        capture=True,
        check=False,
    )
    if name not in result.stdout.splitlines():
        log_info(f"Creating Docker volume: {name}")
        run_cmd(["docker", "volume", "create", name])


def ensure_container(
    name: str,
    host_port: int,
    container_port: int,
    image: str,
    env_vars: dict | None = None,
    volumes: dict | None = None,
) -> None:
    """Ensure a Docker container is running.

    - Creates the container if it does not exist.
    - Starts the container if it exists but is stopped.
    - No-op if the container is already running.

    Args:
        name: Container name.
        host_port: Host port to bind (on 127.0.0.1).
        container_port: Container port to expose.
        image: Docker image to use when creating.
        env_vars: Environment variables to pass to the container.
        volumes: Volume mounts as {host_path_or_volume_name: container_path}.
    """
    if container_is_running(name):
        log_info(f"Container already running: {name}")
        return

    if container_exists(name):
        log_info(f"Starting existing container: {name}")
        run_cmd(["docker", "start", name])
        log_success(f"Container started: {name} \u2713")
        return

    log_info(f"Creating container: {name}")
    cmd = [
        "docker", "run", "-d",
        "--name", name,
        "-p", f"127.0.0.1:{host_port}:{container_port}",
    ]

    if env_vars:
        for key, value in env_vars.items():
            cmd.extend(["-e", f"{key}={value}"])

    if volumes:
        for src, dst in volumes.items():
            cmd.extend(["-v", f"{src}:{dst}"])

    cmd.append(image)
    run_cmd(cmd)
    log_success(f"Container created and started: {name} \u2713")


def stop_container(name: str) -> None:
    """Stop a running Docker container. Does not fail if already stopped."""
    run_cmd(["docker", "stop", name], check=False)


def remove_container(name: str, volumes: list[str] | None = None) -> None:
    """Force-remove a Docker container.

    Args:
        name: Container name to remove.
        volumes: Optional list of named Docker volumes to also remove.
    """
    run_cmd(["docker", "rm", "-f", name], check=False)
    if volumes:
        for vol in volumes:
            run_cmd(["docker", "volume", "rm", vol], check=False)


def docker_exec(container: str, cmd: list[str], *, check: bool = True) -> subprocess.CompletedProcess:
    """Run a command inside a Docker container.

    Args:
        container: Container name or ID.
        cmd: Command to run inside the container.
        check: If True, raise CalledProcessError on non-zero exit.

    Returns:
        CompletedProcess instance.
    """
    return run_cmd(["docker", "exec", container] + cmd, check=check, capture=True)


def wait_for_container_ready(
    health_cmd: list[str],
    timeout: int = 300,
    label: str = "Service",
    interval: int = 2,
) -> None:
    """Poll until a health check command succeeds or timeout is reached.

    Args:
        health_cmd: Command to run as a health check (success = returncode 0).
        timeout: Maximum seconds to wait.
        label: Human-readable service name for log messages.
        interval: Seconds between poll attempts.
    """
    deadline = time.time() + timeout
    log_info(f"Waiting for {label} to be ready (timeout: {timeout}s)...")
    while time.time() < deadline:
        result = run_cmd(health_cmd, check=False, capture=True)
        if result.returncode == 0:
            log_success(f"{label} is ready")
            return
        time.sleep(interval)
    log_error(f"{label} did not become ready within {timeout}s")
    sys.exit(1)


# ---------------------------------------------------------------------------
# Process management
# ---------------------------------------------------------------------------


def graceful_kill(pid: int, timeout: float = 1.0) -> None:
    """Send SIGTERM to a process, then SIGKILL if it does not exit in time.

    Args:
        pid: Process ID to terminate.
        timeout: Seconds to wait after SIGTERM before sending SIGKILL.
    """
    try:
        os.kill(pid, signal.SIGTERM)
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                os.kill(pid, 0)  # Check if process exists
            except ProcessLookupError:
                return  # Process exited
            time.sleep(0.05)
        # Still alive — use SIGKILL
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    except ProcessLookupError:
        pass  # Already gone


def is_port_in_use(port: int) -> bool:
    """Return True if a local TCP port is accepting connections on 127.0.0.1."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(0.5)
        return sock.connect_ex(("127.0.0.1", port)) == 0


def get_pids_on_port(port: int) -> list[int]:
    """Return a list of PIDs listening on the given local port (via lsof)."""
    result = run_cmd(
        ["lsof", "-ti", f":{port}"],
        check=False,
        capture=True,
    )
    if result.returncode != 0 or not result.stdout.strip():
        return []
    pids = []
    for line in result.stdout.splitlines():
        line = line.strip()
        if line.isdigit():
            pids.append(int(line))
    return pids


def kill_port(port: int) -> None:
    """SIGTERM all processes on the given port, then SIGKILL survivors after 1s."""
    pids = get_pids_on_port(port)
    if not pids:
        return
    log_info(f"Stopping {len(pids)} process(es) on port {port}")
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    time.sleep(1.0)
    for pid in pids:
        try:
            os.kill(pid, 0)
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def wait_for_port(port: int, timeout: int = 300, label: str = "Service") -> None:
    """Poll until a port accepts TCP connections or timeout is reached.

    Args:
        port: TCP port to wait for on 127.0.0.1.
        timeout: Maximum seconds to wait.
        label: Human-readable service name for log messages.
    """
    deadline = time.time() + timeout
    log_info(f"Waiting for {label} on port {port} (timeout: {timeout}s)...")
    while time.time() < deadline:
        if is_port_in_use(port):
            log_success(f"{label} is accepting connections on port {port}")
            return
        time.sleep(1)
    log_error(f"{label} did not come up on port {port} within {timeout}s")
    sys.exit(1)


def read_pid_file(path: str | Path) -> int | None:
    """Read a PID file, returning the PID if the process is alive.

    Returns None if the file is missing, unreadable, or the process is dead.
    Removes stale PID files automatically.
    """
    path = Path(path)
    try:
        text = path.read_text().strip()
        if not text.isdigit():
            path.unlink(missing_ok=True)
            return None
        pid = int(text)
        # Check if the process is still alive
        os.kill(pid, 0)
        return pid
    except FileNotFoundError:
        return None
    except (ValueError, PermissionError):
        return None
    except ProcessLookupError:
        # Process is dead — clean up stale file
        path.unlink(missing_ok=True)
        return None


def write_pid_file(path: str | Path, pid: int) -> None:
    """Write a PID to a file, creating parent directories as needed.

    Args:
        path: File path to write the PID to.
        pid: Process ID to write.
    """
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(str(pid))


# ---------------------------------------------------------------------------
# CLI helpers
# ---------------------------------------------------------------------------


def add_config_arg(parser: argparse.ArgumentParser) -> None:
    """Add a --config argument to an ArgumentParser.

    Defaults to config-development.yml in the project root.
    """
    parser.add_argument(
        "--config",
        metavar="FILE",
        default=str(get_project_root() / "config-development.yml"),
        help="Path to YAML config file (default: config-development.yml)",
    )


def add_verbosity_args(parser: argparse.ArgumentParser) -> None:
    """Add --verbose/-v and --quiet/-q as a mutually exclusive group."""
    group = parser.add_mutually_exclusive_group()
    group.add_argument(
        "--verbose", "-v",
        action="store_true",
        default=False,
        help="Enable verbose output",
    )
    group.add_argument(
        "--quiet", "-q",
        action="store_true",
        default=False,
        help="Suppress informational output",
    )


def apply_verbosity(args: argparse.Namespace) -> None:
    """Apply parsed verbosity args to the global quiet flag.

    Calls set_quiet(True) if args.quiet is True.
    """
    if getattr(args, "quiet", False):
        set_quiet(True)
