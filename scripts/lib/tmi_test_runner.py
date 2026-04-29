"""Shared helpers for parsing and rendering `go test -v` output.

Used by run-unit-tests.py and run-integration-tests.py to ensure consistent
behavior:
  - Raw test output is captured to a temp file (path reported in the summary).
  - stdout receives only a summary plus the verbose output of any failed
    tests.
  - The reported exit status is `go test`'s actual exit code, never 0 when
    test packages or subtests failed.
"""

from __future__ import annotations

import re

from tmi_common import BLUE, NC, RED


def parse_output(raw_output_path: str) -> dict:
    """Parse `go test -v` output and return aggregate counts.

    Returns a dict with keys: passed, failed, skipped, pkg_ok, pkg_fail,
    package_lines. package_lines preserves the order the lines appeared
    in the log so it can be replayed verbatim in summaries.
    """
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
    """Extract verbose lines belonging only to failed tests.

    State machine:
      - Accumulate RUN/PAUSE/CONT lines in a block buffer (the per-subtest
        preamble).
      - On `--- FAIL:`: flush the block + the FAIL line, then keep printing
        until the next PASS/SKIP or package boundary.
      - On `--- PASS:` / `--- SKIP:`: reset block and stop printing.
      - On package-level markers (`ok `, `FAIL\\t`, `PASS$`): reset and stop.
      - While printing: emit each line.
      - Otherwise: append to the block.
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


def print_results(
    stats: dict,
    failed_output: list[str],
    raw_output_path: str,
    label: str = "tests",
) -> None:
    """Print formatted test summary to stdout.

    label is a short descriptor used in the SUMMARY block (e.g. "Tests",
    "Integration tests"). Failed-test verbose output is printed first when
    any failures exist, then a per-package result list, then totals.
    """
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
    print(f"  {label}: {stats['passed']} passed, {stats['failed']} failed, {stats['skipped']} skipped")
    print(f"  Packages: {stats['pkg_ok']} passed, {stats['pkg_fail']} failed")
    print(f"  Raw log:  {raw_output_path}")
    print()
