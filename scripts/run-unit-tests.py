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
import re
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    BLUE,
    GREEN,
    NC,
    RED,
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
    log_success,
)


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
            cwd=str(project_root),
            env=env,
        )
    return result.returncode


def parse_output(raw_output_path: str) -> dict:
    """Parse test output and return counts and line collections."""
    passed = 0
    failed = 0
    skipped = 0
    pkg_ok = 0
    pkg_fail = 0
    package_lines: list[str] = []

    with open(raw_output_path) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if line.startswith("--- PASS:"):
                passed += 1
            elif line.startswith("--- FAIL:"):
                failed += 1
            elif line.startswith("--- SKIP:"):
                skipped += 1

            if re.match(r"^ok\s+", line):
                pkg_ok += 1
                package_lines.append(line)
            elif re.match(r"^FAIL\s", line):
                pkg_fail += 1
                package_lines.append(line)

    return {
        "passed": passed,
        "failed": failed,
        "skipped": skipped,
        "pkg_ok": pkg_ok,
        "pkg_fail": pkg_fail,
        "package_lines": package_lines,
    }


def extract_failed_test_output(raw_output_path: str) -> list[str]:
    """Extract verbose output for failed tests only.

    Mirrors the awk logic in the original Makefile:
      - Accumulate RUN/PAUSE/CONT lines in a block buffer.
      - On FAIL: emit block + FAIL line, then stay in printing mode.
      - On PASS/SKIP: reset block, stop printing.
      - On package-level markers (ok, FAIL<tab>, PASS$): reset, stop printing.
      - While printing: emit lines.
      - Otherwise: accumulate into block.
    """
    lines: list[str] = []
    block: list[str] = []
    printing = False

    with open(raw_output_path) as fh:
        for raw_line in fh:
            line = raw_line.rstrip("\n")

            if re.match(r"^=== (RUN|PAUSE|CONT)", line):
                block.append(line)
                continue

            if line.startswith("--- FAIL:"):
                lines.extend(block)
                lines.append(line)
                block = []
                printing = True
                continue

            if re.match(r"^--- (PASS|SKIP):", line):
                block = []
                printing = False
                continue

            if re.match(r"^(=== RUN|ok |FAIL\t|PASS$)", line):
                block = []
                printing = False
                continue

            if printing:
                lines.append(line)
                continue

            block.append(line)

    return lines


def print_results(stats: dict, failed_output: list[str], raw_output_path: str) -> None:
    """Print formatted test results."""
    print()

    if stats["failed"] > 0:
        print(f"{RED}=== FAILED TESTS (verbose output) ==={NC}")
        for line in failed_output:
            print(line)
        print(f"{RED}=== END FAILED TESTS ==={NC}")
        print()

        print(f"{RED}=== FAILED PACKAGES ==={NC}")
        for line in stats["package_lines"]:
            if re.match(r"^FAIL\s", line):
                print(line)
        print()

    print(f"{BLUE}=== PACKAGE RESULTS ==={NC}")
    for line in stats["package_lines"]:
        print(line)
    print()

    print(f"{BLUE}=== SUMMARY ==={NC}")
    print(f"  Tests:    {stats['passed']} passed, {stats['failed']} failed, {stats['skipped']} skipped")
    print(f"  Packages: {stats['pkg_ok']} passed, {stats['pkg_fail']} failed")
    print(f"  Raw log:  {raw_output_path}")
    print()


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
    except Exception:
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

    print_results(stats, failed_output, raw_output_path)

    if exit_code != 0:
        log_error(f"Unit tests failed (exit code {exit_code})")
    else:
        log_success("All unit tests passed")

    try_clean_logs(project_root)

    return exit_code


if __name__ == "__main__":
    sys.exit(main())
