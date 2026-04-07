#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///
"""Check that non-generated Go code does not call unsafe From*/Merge* union methods.

The generated FromNode, MergeNode, FromMinimalNode, and MergeMinimalNode methods
in api/api.go hardcode the shape discriminator to an arbitrary fixed value,
corrupting cell shapes. Non-generated code must use SafeFromNode() or
SafeFromEdge() from api/cell_union_helpers.go instead.

Usage:
    uv run scripts/check-unsafe-union-methods.py
"""

import argparse
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

# Files to skip
SKIP_FILES = {"api.go", "cell_union_helpers_test.go"}

# Pattern matching unsafe method calls
UNSAFE_PATTERN = re.compile(r"\.(FromNode|MergeNode|FromMinimalNode|MergeMinimalNode)\b")


def main() -> int:
    parser = argparse.ArgumentParser(
        description=(
            "Check that non-generated Go code does not call unsafe "
            "FromNode/MergeNode/FromMinimalNode/MergeMinimalNode union methods."
        )
    )
    parser.parse_args()

    project_root = get_project_root()
    api_dir = project_root / "api"

    go_files = sorted(api_dir.glob("*.go"))
    if not go_files:
        log_error(f"No Go files found in {api_dir}")
        return 1

    log_info("Checking for unsafe generated union method calls...")

    violations: list[str] = []

    for go_file in go_files:
        if go_file.name in SKIP_FILES:
            continue

        lines = go_file.read_text(encoding="utf-8").splitlines()
        for lineno, line in enumerate(lines, start=1):
            stripped = line.strip()
            # Skip comment lines
            if stripped.startswith("//"):
                continue
            if UNSAFE_PATTERN.search(line):
                violations.append(f"{go_file.relative_to(project_root)}:{lineno}: {stripped}")

    if violations:
        log_error("Found unsafe generated union method calls:")
        for v in violations:
            print(f"  {v}", file=sys.stderr)
        print(file=sys.stderr)
        print(
            "Use SafeFromNode() or SafeFromEdge() instead (see api/cell_union_helpers.go).",
            file=sys.stderr,
        )
        print(
            "The generated FromNode/MergeNode methods corrupt the shape discriminator field.",
            file=sys.stderr,
        )
        return 1

    log_success("No unsafe generated union method calls found")
    return 0


if __name__ == "__main__":
    sys.exit(main())
