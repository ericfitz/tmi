#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Validate the TMI OpenAPI specification.

Performs two validation steps:
  1. JSON syntax validation using jq
  2. OpenAPI linting with Vacuum (OWASP rules via vacuum-ruleset.yaml)

Usage:
    uv run scripts/validate-openapi-spec.py [flags]

Options:
    --spec PATH    OpenAPI spec (default: api-schema/tmi-openapi.json)
    --report PATH  Report output (default: test/outputs/api-validation/openapi-validation-report.json)
    --db PATH      SQLite DB output (default: test/outputs/api-validation/openapi-validation.db)
    -v/--verbose   Enable verbose output
    -q/--quiet     Suppress informational output
    --help         Show this help message
"""

import argparse
import json
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


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate the TMI OpenAPI specification (JSON syntax + Vacuum linting)."
    )
    add_verbosity_args(parser)
    parser.add_argument(
        "--spec",
        default="api-schema/tmi-openapi.json",
        help="Path to OpenAPI spec (default: api-schema/tmi-openapi.json)",
    )
    parser.add_argument(
        "--report",
        default="test/outputs/api-validation/openapi-validation-report.json",
        help="Path for Vacuum JSON report output",
    )
    parser.add_argument(
        "--db",
        default="test/outputs/api-validation/openapi-validation.db",
        help="Path for SQLite DB output",
    )
    return parser.parse_args()


def validate_json_syntax(spec_path: Path) -> bool:
    """Validate JSON syntax using jq. Returns True on success."""
    log_info(f"Validating JSON syntax: {spec_path}")
    result = run_cmd(
        ["jq", "empty", str(spec_path)],
        check=False,
        capture=True,
    )
    if result.returncode != 0:
        log_error(f"Invalid JSON syntax in {spec_path}")
        if result.stderr:
            print(result.stderr, file=sys.stderr)
        return False
    log_success("JSON syntax is valid")
    return True


def check_vacuum_installed() -> bool:
    """Check if vacuum is available on PATH."""
    result = run_cmd(["which", "vacuum"], check=False, capture=True)
    return result.returncode == 0


def run_vacuum(spec_path: Path, report_path: Path) -> bool:
    """Run Vacuum and write JSON report. Returns True on success."""
    log_info("Running Vacuum OpenAPI analysis (with OWASP rules)...")
    project_root = get_project_root()
    ruleset = project_root / "vacuum-ruleset.yaml"

    result = run_cmd(
        ["vacuum", "report", str(spec_path), "-r", str(ruleset), "--no-style", "-o"],
        check=False,
        capture=True,
        cwd=project_root,
    )

    # vacuum writes the JSON report to stdout; stderr may contain progress info
    report_json = result.stdout

    if not report_json.strip():
        log_error("Vacuum produced no output")
        if result.stderr:
            print(result.stderr, file=sys.stderr)
        return False

    # Write report file
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(report_json, encoding="utf-8")
    log_info(f"Report written to: {report_path}")
    return True


def parse_counts(report_path: Path) -> tuple[int, int, int]:
    """Parse error/warning/info counts from the Vacuum JSON report."""
    try:
        data = json.loads(report_path.read_text(encoding="utf-8"))
        result_set = data.get("resultSet", {})
        errors = int(result_set.get("errorCount", 0) or 0)
        warnings = int(result_set.get("warningCount", 0) or 0)
        infos = int(result_set.get("infoCount", 0) or 0)
        return errors, warnings, infos
    except (json.JSONDecodeError, KeyError, TypeError) as exc:
        log_error(f"Failed to parse report JSON: {exc}")
        return 0, 0, 0


def main() -> int:
    args = parse_args()
    apply_verbosity(args)

    project_root = get_project_root()
    spec_path = project_root / args.spec
    report_path = project_root / args.report
    db_path = project_root / args.db

    # Step 1: JSON syntax validation
    if not validate_json_syntax(spec_path):
        return 1

    # Step 2: Check vacuum is installed
    if not check_vacuum_installed():
        log_error("vacuum not found — required for OpenAPI validation")
        log_error("Install with: brew install vacuum")
        return 1

    # Step 3: Run Vacuum
    if not run_vacuum(spec_path, report_path):
        return 1

    # Step 4: Parse counts
    errors, warnings, infos = parse_counts(report_path)
    log_info(f"Results: {errors} errors, {warnings} warnings, {infos} info")

    # Step 5: Fail on errors
    if errors > 0:
        log_error(f"Validation failed with {errors} errors")
        log_info("Loading results into SQLite database for analysis...")

        parse_script = project_root / "scripts" / "parse-openapi-validation.py"
        run_cmd(
            [
                "uv", "run", str(parse_script),
                "--report", str(report_path),
                "--db", str(db_path),
                "--summary",
            ],
            check=False,
            cwd=project_root,
        )
        log_info(f"Query database: sqlite3 {db_path} 'SELECT * FROM error_summary'")
        return 1

    log_success(f"OpenAPI validation complete. Report: {report_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
