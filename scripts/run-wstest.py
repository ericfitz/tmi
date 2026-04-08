# /// script
# requires-python = ">=3.11"
# ///
"""Launch WebSocket test harness in multiple terminals.

Usage:
    uv run scripts/run-wstest.py
    uv run scripts/run-wstest.py --monitor
"""

import argparse
import platform
import subprocess
import sys
import time
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


def check_server_running(root: Path) -> bool:
    """Return True if the TMI server is responding on localhost:8080."""
    result = run_cmd(
        ["curl", "-s", "http://localhost:8080"],
        check=False,
        capture=True,
        cwd=root,
    )
    return result.returncode == 0


def parse_args() -> argparse.Namespace:
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(description="Run WebSocket test harness.")
    add_verbosity_args(parser)
    parser.add_argument(
        "--monitor",
        action="store_true",
        default=False,
        help="Run in monitor mode (foreground, single user)",
    )
    return parser.parse_args()


def build_wstest(wstest_dir: Path) -> None:
    """Build the wstest binary."""
    log_info("Building WebSocket test harness...")
    run_cmd(["go", "mod", "tidy"], cwd=wstest_dir)
    run_cmd(["go", "build", "-o", "wstest"], cwd=wstest_dir)
    log_success("WebSocket test harness built successfully")


def run_monitor(wstest_dir: Path) -> None:
    """Run wstest in monitor mode (foreground)."""
    log_info("Checking that TMI server is running...")
    if not check_server_running(get_project_root()):
        log_error("Server not running. Please run 'make start-dev' first.")
        sys.exit(1)
    build_wstest(wstest_dir)
    log_info("Starting WebSocket monitor...")
    run_cmd(["./wstest", "--user", "monitor"], cwd=wstest_dir)


def spawn_terminal(cmd: str) -> None:
    """Spawn a new terminal window running cmd, using the best available method."""
    system = platform.system()
    if system == "Darwin":
        osascript_cmd = f'tell app "Terminal" to do script "{cmd}"'
        subprocess.run(["osascript", "-e", osascript_cmd], check=False)
    elif system == "Linux":
        if subprocess.run(["which", "gnome-terminal"], capture_output=True).returncode == 0:
            subprocess.Popen(["gnome-terminal", "--", "bash", "-c", f"{cmd}; exec bash"])
        elif subprocess.run(["which", "xterm"], capture_output=True).returncode == 0:
            subprocess.Popen(["xterm", "-e", cmd])
        else:
            # Background fallback handled by caller
            raise RuntimeError("no terminal emulator found")
    else:
        raise RuntimeError(f"unsupported platform: {system}")


def main() -> None:
    args = parse_args()
    apply_verbosity(args)

    root = get_project_root()
    wstest_dir = root / "wstest"

    if args.monitor:
        run_monitor(wstest_dir)
        return

    # Original multi-terminal flow
    log_info("Checking that TMI server is running...")
    if not check_server_running(root):
        log_error("Server not running. Please run 'make start-dev' first.")
        sys.exit(1)

    build_wstest(wstest_dir)

    # Terminal 1: Alice (host)
    log_info("Launching host terminal (alice)...")
    alice_cmd = (
        f"cd {wstest_dir} && "
        'timeout 30 ./wstest --user alice --host --participants "bob,charlie,hobobarbarian@gmail.com"'
    )
    try:
        spawn_terminal(alice_cmd)
    except RuntimeError:
        log_info("No terminal emulator found. Running alice in background...")
        with open(wstest_dir / "alice.log", "w") as fh:
            subprocess.Popen(
                ["./wstest", "--user", "alice", "--host",
                 "--participants", "bob,charlie,hobobarbarian@gmail.com"],
                cwd=wstest_dir,
                stdout=fh,
                stderr=fh,
            )
        log_info("Host (alice) running in background, see wstest/alice.log")

    # Wait for host to start
    log_info("Waiting for host to start...")
    time.sleep(3)

    # Terminal 2: Bob
    log_info("Launching participant terminal (bob)...")
    bob_cmd = f"cd {wstest_dir} && timeout 30 ./wstest --user bob"
    try:
        spawn_terminal(bob_cmd)
    except RuntimeError:
        log_info("No terminal emulator found. Running bob in background...")
        with open(wstest_dir / "bob.log", "w") as fh:
            subprocess.Popen(
                ["./wstest", "--user", "bob"],
                cwd=wstest_dir,
                stdout=fh,
                stderr=fh,
            )
        log_info("Participant (bob) running in background, see wstest/bob.log")

    # Terminal 3: Charlie
    log_info("Launching participant terminal (charlie)...")
    charlie_cmd = f"cd {wstest_dir} && timeout 30 ./wstest --user charlie"
    try:
        spawn_terminal(charlie_cmd)
    except RuntimeError:
        log_info("No terminal emulator found. Running charlie in background...")
        with open(wstest_dir / "charlie.log", "w") as fh:
            subprocess.Popen(
                ["./wstest", "--user", "charlie"],
                cwd=wstest_dir,
                stdout=fh,
                stderr=fh,
            )
        log_info("Participant (charlie) running in background, see wstest/charlie.log")

    log_success("WebSocket test started with 3 terminals")
    print("Watch the terminals for WebSocket activity. Use 'make clean-process' to stop all instances.")


if __name__ == "__main__":
    main()
