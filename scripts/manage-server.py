# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml>=6.0"]
# ///
"""Manage the TMI server process.

Supports start, stop, and wait subcommands.
"""

import argparse
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
from _server_state import running_server_pid  # noqa: E402

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEFAULT_BINARY = "bin/tmiserver"
DEFAULT_LOG_FILE = "logs/server.log"
DEFAULT_CONFIG = "config-development.yml"
DEFAULT_PID_FILE = ".server.pid"
DEFAULT_PORT = 8080
DEFAULT_TIMEOUT = 300


# ---------------------------------------------------------------------------
# Config resolution
# ---------------------------------------------------------------------------


def resolve_config(args: argparse.Namespace) -> dict:
    """Build the effective configuration by layering defaults, config file, and CLI flags.

    Priority (highest wins): CLI flags > config file > defaults.
    """
    project_root = get_project_root()

    cfg = {
        "port": DEFAULT_PORT,
        "binary": str(project_root / DEFAULT_BINARY),
        "log_file": str(project_root / DEFAULT_LOG_FILE),
        "pid_file": str(project_root / DEFAULT_PID_FILE),
        "config": str(project_root / DEFAULT_CONFIG),
        "tags": None,
        "timeout": DEFAULT_TIMEOUT,
    }

    # Load config file and extract server port
    config_path = Path(args.config) if not Path(args.config).is_absolute() else Path(args.config)
    if not config_path.is_absolute():
        config_path = project_root / config_path
    raw = load_config(config_path)
    server_port = config_get(raw, "server.port")
    if server_port is not None:
        cfg["port"] = int(server_port)

    cfg["config"] = str(config_path)

    # CLI overrides
    if args.port is not None:
        cfg["port"] = args.port
    if args.binary is not None:
        binary = Path(args.binary)
        cfg["binary"] = str(binary) if binary.is_absolute() else str(project_root / binary)
    if args.log_file is not None:
        log_file = Path(args.log_file)
        cfg["log_file"] = str(log_file) if log_file.is_absolute() else str(project_root / log_file)
    if args.tags is not None:
        cfg["tags"] = args.tags
    if args.timeout is not None:
        cfg["timeout"] = args.timeout

    return cfg


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(cfg: dict, args: argparse.Namespace) -> None:
    """Start the TMI server process."""
    project_root = get_project_root()
    pid_file = cfg["pid_file"]
    log_file = cfg["log_file"]
    port = cfg["port"]
    binary = cfg["binary"]
    config_path = cfg["config"]

    # Step 1: Pre-flight — verify port is free before touching logs
    if is_port_in_use(port):
        log_error(f"Port {port} is already in use.")
        log_error("Run 'make stop-server' first.")
        sys.exit(1)

    # Step 2: Rotate logs — preserve the previous run for forensics, do not delete
    _rotate_logs(project_root, log_file)

    # Step 3: If --tags provided, build the binary first
    if cfg.get("tags"):
        tags = cfg["tags"]
        log_info(f"Building server with tags: {tags}")
        run_cmd(
            ["go", "build", f"-tags={tags}", "-o", binary, "./cmd/server/"],
            cwd=project_root,
            verbose=getattr(args, "verbose", False),
        )

    # Step 4: Launch binary in background
    log_info(f"Starting server binary: {binary}")
    with open(log_file, "w") as lf:
        proc = subprocess.Popen(
            [binary, f"--config={config_path}"],
            stdout=lf,
            stderr=lf,
            cwd=project_root,
        )

    # Step 5: Write PID to file
    write_pid_file(pid_file, proc.pid)

    # Step 6: Sleep 2 seconds, verify process is still alive
    time.sleep(2)
    if proc.poll() is not None:
        log_error(f"Server exited immediately after starting. Check {log_file}")
        Path(pid_file).unlink(missing_ok=True)
        sys.exit(1)

    log_success(f"Server started with PID: {proc.pid}")


def cmd_stop(cfg: dict, args: argparse.Namespace) -> None:
    """Stop the TMI server process."""
    port = cfg["port"]
    pid_file = cfg["pid_file"]

    log_info("Stopping server...")

    # Layer 1: Kill via PID file
    pid = read_pid_file(pid_file)
    if pid is not None:
        graceful_kill(pid)

    # Layer 2: Find processes matching "bin/tmiserver" via ps aux
    try:
        result = subprocess.run(
            ["ps", "aux"],
            capture_output=True,
            text=True,
        )
        for line in result.stdout.splitlines():
            if "bin/tmiserver" in line and not line.startswith("grep"):
                parts = line.split()
                if len(parts) >= 2:
                    try:
                        orphan_pid = int(parts[1])
                        graceful_kill(orphan_pid)
                    except ValueError:
                        pass
    except Exception:
        pass

    # Layer 3: Kill anything still holding the port
    kill_port(port)

    # Verify port is free (retry up to 10 times with 0.5s sleep)
    for _ in range(10):
        if not is_port_in_use(port):
            break
        time.sleep(0.5)

    if is_port_in_use(port):
        pids = get_pids_on_port(port)
        log_error(f"Port {port} is still in use after stop attempts (PIDs: {pids})")
        sys.exit(1)

    # Clean up PID file
    Path(pid_file).unlink(missing_ok=True)

    log_success("Server stopped")


def cmd_wait(cfg: dict, args: argparse.Namespace) -> None:
    """Wait for the server port to be accepting connections."""
    port = cfg["port"]
    timeout = cfg["timeout"]
    log_info(f"Waiting for server to be ready on port {port}")
    wait_for_port(port, timeout=timeout, label="Server")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _rotate_logs(project_root: Path, log_file: str) -> None:
    """Rotate the active server log to <name>.prev.

    Preserves the previous run for forensics. The rotation handles:
      - logs/tmi.log (or whatever --log-file says) → logs/tmi.log.prev
      - integration-test.log → integration-test.log.prev (project root)
      - server.log → server.log.prev (project root, legacy location)

    A stale .server.pid (server not running) is removed; a live .server.pid is
    preserved (cmd_start's pre-flight already refused to proceed if the port was
    in use, so a live PID file at this point is a stale-but-pointing-at-live-PID
    edge case that is handled by the caller's normal cleanup path).

    If a .prev already exists, it is overwritten — we only keep one previous run.
    """
    import os

    log_path = Path(log_file)
    if not log_path.is_absolute():
        log_path = project_root / log_path

    # Rotate the primary log
    if log_path.exists():
        prev = log_path.with_name(log_path.name + ".prev")
        if prev.exists():
            prev.unlink()
        log_path.rename(prev)
        log_info(f"Rotated previous log to {prev.name}")

    # Rotate legacy log files in project root (integration-test.log, server.log)
    for name in ("integration-test.log", "server.log"):
        path = project_root / name
        if path.exists():
            prev = path.with_name(path.name + ".prev")
            if prev.exists():
                prev.unlink()
            path.rename(prev)

    # Ensure the parent directory exists so the caller can open the new log
    log_path.parent.mkdir(parents=True, exist_ok=True)

    # Clean up stale .server.pid (process not running). Live PID files are left alone
    # because cmd_start's port pre-flight should have caught any running server already.
    pid_file = project_root / ".server.pid"
    if pid_file.exists():
        pid = read_pid_file(pid_file)
        if pid is not None:
            try:
                os.kill(pid, 0)
                # Process exists — leave the PID file
            except (ProcessLookupError, PermissionError):
                pid_file.unlink(missing_ok=True)
        else:
            pid_file.unlink(missing_ok=True)


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

def cmd_kill_port(cfg: dict, args: argparse.Namespace) -> None:
    """Kill all processes on the configured port."""
    port = cfg["port"]
    log_info(f"Killing processes on port {port}")
    kill_port(port)
    log_success(f"Port {port} cleared")


SUBCOMMANDS = {
    "start": (cmd_start, "Start the TMI server process"),
    "stop": (cmd_stop, "Stop the TMI server process"),
    "wait": (cmd_wait, "Wait until the server port is accepting connections"),
    "kill-port": (cmd_kill_port, "Kill all processes on configured port"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-server.py",
        description="Manage the TMI server process.",
    )

    # Global flags
    add_config_arg(parser)
    parser.add_argument(
        "--port",
        type=int,
        default=None,
        metavar="PORT",
        help="Override server port",
    )
    parser.add_argument(
        "--binary",
        metavar="PATH",
        default=None,
        help=f"Override server binary path (default: {DEFAULT_BINARY})",
    )
    parser.add_argument(
        "--log-file",
        metavar="PATH",
        default=None,
        dest="log_file",
        help=f"Override log file path (default: {DEFAULT_LOG_FILE})",
    )
    parser.add_argument(
        "--tags",
        metavar="TAGS",
        default=None,
        help="Go build tags (triggers build before start)",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=DEFAULT_TIMEOUT,
        metavar="SECS",
        help=f"Wait timeout in seconds (default: {DEFAULT_TIMEOUT})",
    )
    add_verbosity_args(parser)

    # Subcommands
    subparsers = parser.add_subparsers(dest="subcommand", metavar="SUBCOMMAND")
    subparsers.required = True
    for name, (_, help_text) in SUBCOMMANDS.items():
        subparsers.add_parser(name, help=help_text)

    return parser


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    apply_verbosity(args)

    cfg = resolve_config(args)

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(cfg, args)


if __name__ == "__main__":
    main()
