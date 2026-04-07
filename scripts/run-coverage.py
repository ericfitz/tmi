# /// script
# requires-python = ">=3.11"
# ///
"""Orchestrate TMI test coverage: unit, integration, merge, and report generation.

Runs Go coverage commands for unit and/or integration tests, merges profiles,
and generates HTML and text reports.
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

# ---------------------------------------------------------------------------
# Configuration constants
# ---------------------------------------------------------------------------

COVERAGE_DIR = "coverage"
COVERAGE_HTML_DIR = "coverage_html"
COVERAGE_MODE = "atomic"

UNIT_PACKAGES = ["./api/...", "./auth/...", "./cmd/...", "./internal/..."]
INTEGRATION_PACKAGES = ["./..."]

UNIT_TIMEOUT = "5m"
INTEGRATION_TIMEOUT = "10m"

# Profile filenames
UNIT_PROFILE = "unit_coverage.out"
UNIT_HTML = "unit_coverage.html"
UNIT_DETAILED = "unit_coverage_detailed.txt"

INTEGRATION_PROFILE = "integration_coverage.out"
INTEGRATION_HTML = "integration_coverage.html"
INTEGRATION_DETAILED = "integration_coverage_detailed.txt"

COMBINED_PROFILE = "combined_coverage.out"
COMBINED_HTML = "combined_coverage.html"
COMBINED_DETAILED = "combined_coverage_detailed.txt"

COVERAGE_SUMMARY = "coverage_summary.txt"

TOOLS_GOCOVMERGE = "github.com/wadey/gocovmerge@latest"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def coverage_path(project_root: Path, filename: str) -> Path:
    """Return the full path to a file in the coverage directory."""
    return project_root / COVERAGE_DIR / filename


def html_path(project_root: Path, filename: str) -> Path:
    """Return the full path to a file in the coverage HTML directory."""
    return project_root / COVERAGE_HTML_DIR / filename


def ensure_dirs(project_root: Path) -> None:
    """Create coverage output directories if they do not exist."""
    (project_root / COVERAGE_DIR).mkdir(parents=True, exist_ok=True)
    (project_root / COVERAGE_HTML_DIR).mkdir(parents=True, exist_ok=True)


def is_verbose(args: argparse.Namespace) -> bool:
    """Return True if verbose output is requested."""
    return getattr(args, "verbose", False)


# ---------------------------------------------------------------------------
# Pipeline stages
# ---------------------------------------------------------------------------


def run_unit_coverage(project_root: Path, verbose: bool = False) -> None:
    """Run unit tests with coverage profiling."""
    log_info("Running unit tests with coverage...")
    profile = coverage_path(project_root, UNIT_PROFILE)
    cmd = [
        "go", "test",
        "-short",
        f"-coverprofile={profile}",
        f"-covermode={COVERAGE_MODE}",
        "-coverpkg=./...",
        *UNIT_PACKAGES,
        '-tags=!integration',
        f"-timeout={UNIT_TIMEOUT}",
        "-v",
    ]
    run_cmd(
        cmd,
        cwd=project_root,
        env={"LOGGING_IS_TEST": "true"},
        verbose=verbose,
    )
    log_success("Unit test coverage completed")


def run_integration_coverage(project_root: Path, verbose: bool = False) -> None:
    """Run integration tests with coverage profiling."""
    log_info("Running integration tests with coverage...")
    profile = coverage_path(project_root, INTEGRATION_PROFILE)
    cmd = [
        "go", "test",
        "-short",
        f"-coverprofile={profile}",
        f"-covermode={COVERAGE_MODE}",
        "-coverpkg=./...",
        "-tags=integration",
        *INTEGRATION_PACKAGES,
        f"-timeout={INTEGRATION_TIMEOUT}",
        "-v",
    ]
    run_cmd(
        cmd,
        cwd=project_root,
        env={"LOGGING_IS_TEST": "true"},
        verbose=verbose,
    )
    log_success("Integration test coverage completed")


def merge_coverage(project_root: Path, verbose: bool = False) -> None:
    """Merge unit and integration coverage profiles into a combined profile."""
    log_info("Merging coverage profiles...")

    # Install gocovmerge if not available
    result = run_cmd(
        ["which", "gocovmerge"],
        check=False,
        capture=True,
    )
    if result.returncode != 0:
        log_info("Installing gocovmerge...")
        run_cmd(
            ["go", "install", TOOLS_GOCOVMERGE],
            cwd=project_root,
            verbose=verbose,
        )

    unit_profile = coverage_path(project_root, UNIT_PROFILE)
    integration_profile = coverage_path(project_root, INTEGRATION_PROFILE)
    combined_profile = coverage_path(project_root, COMBINED_PROFILE)

    result = run_cmd(
        ["gocovmerge", str(unit_profile), str(integration_profile)],
        cwd=project_root,
        capture=True,
        verbose=verbose,
    )
    combined_profile.write_text(result.stdout)
    log_success("Coverage profiles merged")


def generate_reports(project_root: Path, verbose: bool = False) -> None:
    """Generate HTML and text coverage reports from all profiles."""
    log_info("Generating coverage reports...")

    profiles = [
        (UNIT_PROFILE, UNIT_HTML, UNIT_DETAILED),
        (INTEGRATION_PROFILE, INTEGRATION_HTML, INTEGRATION_DETAILED),
        (COMBINED_PROFILE, COMBINED_HTML, COMBINED_DETAILED),
    ]

    for profile_name, html_name, detailed_name in profiles:
        profile = coverage_path(project_root, profile_name)
        html = html_path(project_root, html_name)
        detailed = coverage_path(project_root, detailed_name)

        # Generate HTML report
        run_cmd(
            ["go", "tool", "cover", f"-html={profile}", f"-o={html}"],
            cwd=project_root,
            verbose=verbose,
        )

        # Generate detailed text report
        result = run_cmd(
            ["go", "tool", "cover", f"-func={profile}"],
            cwd=project_root,
            capture=True,
            verbose=verbose,
        )
        detailed.write_text(result.stdout)

    # Generate summary from combined profile
    log_info("Generating coverage summary...")
    combined_profile = coverage_path(project_root, COMBINED_PROFILE)
    result = run_cmd(
        ["go", "tool", "cover", f"-func={combined_profile}"],
        cwd=project_root,
        capture=True,
        verbose=verbose,
    )

    lines = result.stdout.splitlines()
    summary_line = lines[-1] if lines else ""

    summary_path = coverage_path(project_root, COVERAGE_SUMMARY)
    summary_path.write_text(summary_line + "\n")
    print(summary_line)

    log_success(
        f"Coverage reports generated in {COVERAGE_DIR}/ and {COVERAGE_HTML_DIR}/"
    )


# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Orchestrate TMI test coverage collection and report generation."
    )
    add_verbosity_args(parser)

    mode_group = parser.add_mutually_exclusive_group()
    mode_group.add_argument(
        "--unit-only",
        action="store_true",
        default=False,
        help="Run unit coverage only",
    )
    mode_group.add_argument(
        "--integration-only",
        action="store_true",
        default=False,
        help="Run integration coverage only",
    )
    mode_group.add_argument(
        "--merge-only",
        action="store_true",
        default=False,
        help="Merge existing coverage profiles only",
    )
    mode_group.add_argument(
        "--generate-only",
        action="store_true",
        default=False,
        help="Generate reports from existing coverage profiles only",
    )

    return parser.parse_args()


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> int:
    args = parse_args()
    apply_verbosity(args)
    verbose = is_verbose(args)

    project_root = get_project_root()

    try:
        if args.unit_only:
            ensure_dirs(project_root)
            run_unit_coverage(project_root, verbose=verbose)

        elif args.integration_only:
            ensure_dirs(project_root)
            run_integration_coverage(project_root, verbose=verbose)

        elif args.merge_only:
            merge_coverage(project_root, verbose=verbose)

        elif args.generate_only:
            ensure_dirs(project_root)
            generate_reports(project_root, verbose=verbose)

        else:
            # Full pipeline
            ensure_dirs(project_root)
            run_unit_coverage(project_root, verbose=verbose)
            run_integration_coverage(project_root, verbose=verbose)
            merge_coverage(project_root, verbose=verbose)
            generate_reports(project_root, verbose=verbose)

    except subprocess.CalledProcessError as exc:
        log_error(f"Command failed with exit code {exc.returncode}: {exc.cmd}")
        return exc.returncode
    except Exception as exc:  # noqa: BLE001
        log_error(f"Unexpected error: {exc}")
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
