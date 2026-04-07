# /// script
# requires-python = ">=3.11"
# ///
"""Manage the TMI OAuth callback stub process.

Supports start, stop, kill, and status subcommands.
"""

import os
import subprocess
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
    log_warn,
    read_pid_file,
    run_cmd,
)

import argparse

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

STUB_PORT = 8079
PID_FILE = ".oauth-stub.pid"
STUB_SCRIPT = "scripts/oauth-client-callback-stub.py"
LOG_FILE = "/tmp/oauth-stub.log"
STUB_URL = f"http://localhost:{STUB_PORT}/"
LATEST_ENDPOINT = f"http://localhost:{STUB_PORT}/latest"


# ---------------------------------------------------------------------------
# Subcommand implementations
# ---------------------------------------------------------------------------


def cmd_start(args: argparse.Namespace) -> None:
    """Start the OAuth callback stub."""
    project_root = get_project_root()
    pid_file = project_root / PID_FILE

    # Step 1: Kill existing processes on port 8079
    log_info(f"Starting OAuth callback stub on port {STUB_PORT}...")
    kill_port(STUB_PORT)

    # Step 2: Remove stale PID file
    pid_file.unlink(missing_ok=True)

    # Step 3: Launch the stub in daemon mode
    stub_script = project_root / STUB_SCRIPT
    run_cmd(
        [
            "uv",
            "run",
            str(stub_script),
            "--port",
            str(STUB_PORT),
            "--daemon",
            "--pid-file",
            str(pid_file),
        ],
        cwd=project_root,
        verbose=getattr(args, "verbose", False),
    )

    # Step 4: Poll for readiness (up to 10 times with 0.5s sleep)
    for _ in range(10):
        if is_port_in_use(STUB_PORT):
            break
        time.sleep(0.5)
    else:
        log_error(f"Failed to start OAuth stub (timeout after 5s)")
        pid_file.unlink(missing_ok=True)
        sys.exit(1)

    # Step 5: Log success info
    pid = read_pid_file(pid_file)
    pid_str = str(pid) if pid is not None else "unknown"
    log_success(f"OAuth stub started on {STUB_URL}")
    log_info(f"PID: {pid_str}")
    log_info(f"Log file: {LOG_FILE}")


def cmd_stop(args: argparse.Namespace) -> None:
    """Gracefully stop the OAuth callback stub."""
    project_root = get_project_root()
    pid_file = project_root / PID_FILE

    log_info("Stopping OAuth callback stub...")

    # Step 1: Send magic exit URL
    log_info("Sending graceful shutdown request...")
    subprocess.run(
        ["curl", "-s", f"http://localhost:{STUB_PORT}/?code=exit"],
        check=False,
        capture_output=True,
    )
    time.sleep(1)

    # Step 2: Get PIDs on port 8079 and gracefully kill each
    pids = get_pids_on_port(STUB_PORT)
    for pid in pids:
        log_info(f"Sending SIGTERM to process {pid}...")
        graceful_kill(pid, timeout=2)

    # Step 3: Check again and SIGKILL any survivors
    pids = get_pids_on_port(STUB_PORT)
    if pids:
        log_warn(f"Processes still running on port {STUB_PORT}: {pids}")
        for pid in pids:
            log_info(f"Force killing process {pid} with SIGKILL...")
            try:
                os.kill(pid, 9)
            except ProcessLookupError:
                pass
    time.sleep(1)

    # Step 4: Remove PID file
    pid_file.unlink(missing_ok=True)

    # Step 5: Verify port is clear
    remaining = get_pids_on_port(STUB_PORT)
    if remaining:
        log_error(f"Failed to stop all processes on port {STUB_PORT}: {remaining}")
        sys.exit(1)
    else:
        log_success("OAuth stub stopped successfully")


def cmd_kill(args: argparse.Namespace) -> None:
    """Force kill anything on port 8079."""
    project_root = get_project_root()
    pid_file = project_root / PID_FILE

    log_info(f"Force killing anything on port {STUB_PORT}...")
    kill_port(STUB_PORT)
    pid_file.unlink(missing_ok=True)
    log_success(f"Port {STUB_PORT} cleared")


def cmd_status(args: argparse.Namespace) -> None:
    """Report the current status of the OAuth callback stub."""
    project_root = get_project_root()
    pid_file = project_root / PID_FILE

    # Step 1: Try PID file
    pid = read_pid_file(pid_file)
    if pid is not None:
        try:
            os.kill(pid, 0)
            log_success(f"OAuth stub is running (PID: {pid})")
            log_info(f"URL: {STUB_URL}")
            log_info(f"Latest endpoint: {LATEST_ENDPOINT}")
            return
        except (ProcessLookupError, PermissionError):
            log_warn(f"PID file exists but process {pid} is not running")
            pid_file.unlink(missing_ok=True)

    # Step 2: No valid PID file — check for orphans via port
    orphan_pids = get_pids_on_port(STUB_PORT)
    if orphan_pids:
        log_warn(f"OAuth stub is running but no PID file found")
        log_info(f"PIDs: {orphan_pids}")
        return

    # Step 3: Nothing found
    log_info("OAuth stub is not running")


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "start": (cmd_start, "Start the OAuth callback stub"),
    "stop": (cmd_stop, "Gracefully stop the OAuth callback stub"),
    "kill": (cmd_kill, "Force kill the OAuth callback stub"),
    "status": (cmd_status, "Report the status of the OAuth callback stub"),
}


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="manage-oauth-stub.py",
        description="Manage the TMI OAuth callback stub process.",
    )

    add_verbosity_args(parser)

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

    fn, _ = SUBCOMMANDS[args.subcommand]
    fn(args)


if __name__ == "__main__":
    main()
