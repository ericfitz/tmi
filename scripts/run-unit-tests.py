# /// script
# requires-python = ">=3.11"
# ///
"""Run TMI unit tests with formatted output.

Runs go test against all TMI packages in short mode, captures output to a
temp file, parses results, and prints a formatted summary.  Failed tests
get full verbose output; passing packages show only their summary line.
"""

import argparse
import os
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
)
from tmi_test_runner import extract_failed_test_output, parse_output, print_results


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run TMI unit tests with formatted output."
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "--name",
        metavar="NAME",
        default=None,
        help="Run specific test by name (-run flag)",
    )
    parser.add_argument(
        "--count1",
        action="store_true",
        default=False,
        help="Run with --count=1 (disable caching)",
    )
    return parser.parse_args()


def build_test_command(args: argparse.Namespace) -> list[str]:
    """Construct the go test command list."""
    cmd = [
        "go", "test", "-short",
        "./api/...", "./auth/...", "./cmd/...", "./internal/...",
        "-v",
    ]
    if args.name:
        cmd.extend(["-run", args.name])
    if args.count1:
        cmd.append("--count=1")
    return cmd


def run_tests(cmd: list[str], raw_output_path: str, project_root: Path) -> int:
    """Run the test command, capturing all output to raw_output_path.

    Returns the process exit code.
    """
    env = {**os.environ, "LOGGING_IS_TEST": "true"}
    with open(raw_output_path, "w") as fh:
        result = subprocess.run(
            cmd,
            stdout=fh,
            stderr=subprocess.STDOUT,
            check=False,
            cwd=str(project_root),
            env=env,
        )
    return result.returncode


def try_clean_logs(project_root: Path) -> None:
    """Optionally run scripts/clean.py logs; silently skip if not found."""
    clean_script = project_root / "scripts" / "clean.py"
    if not clean_script.exists():
        return
    try:
        subprocess.run(
            ["uv", "run", str(clean_script), "logs"],
            cwd=str(project_root),
            check=False,
            capture_output=True,
        )
    except OSError:
        pass  # Best-effort cleanup


def main() -> int:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    cmd = build_test_command(args)

    log_info("Running unit tests")

    # Create temp file for raw output (do not auto-delete — we report its path)
    fd, raw_output_path = tempfile.mkstemp(prefix="tmi-test-unit-", dir="/tmp")
    os.close(fd)

    exit_code = run_tests(cmd, raw_output_path, project_root)
    stats = parse_output(raw_output_path)

    failed_output: list[str] = []
    if stats["failed"] > 0:
        failed_output = extract_failed_test_output(raw_output_path)

    print_results(stats, failed_output, raw_output_path, label="Tests")

    if exit_code != 0:
        log_error(f"Unit tests failed (exit code {exit_code})")
    else:
        log_success("All unit tests passed")

    try_clean_logs(project_root)

    return exit_code


if __name__ == "__main__":
    sys.exit(main())
