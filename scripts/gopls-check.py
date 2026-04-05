#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# ///

"""Run gopls check against all Go files in the repo and collect findings.

Usage:
    uv run scripts/gopls-check.py [OUTPUT_FILE] [--gopls PATH]

Output defaults to gopls-check-report.txt in the project root.
Findings are written to the output file as they are generated.
"""

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

BAR_CHAR = "#"


def find_go_files(root: Path) -> list[tuple[Path, int]]:
    """Find all .go files with sizes, excluding vendor and generated api/api.go."""
    excluded = {root / "vendor"}
    generated = {root / "api" / "api.go"}
    go_files = []
    for f in sorted(root.rglob("*.go")):
        if any(f.is_relative_to(ex) for ex in excluded):
            continue
        if f in generated:
            continue
        go_files.append((f, f.stat().st_size))
    return go_files


def run_gopls_check(gopls_bin: str, filepath: Path) -> str:
    """Run gopls check on a single file and return its stdout."""
    result = subprocess.run(
        [gopls_bin, "check", str(filepath)],
        capture_output=True,
        text=True,
        timeout=60,
    )
    return result.stdout.strip()


def print_progress(percent: int, rel_path: str, file_num: int, total_files: int) -> None:
    """Render a two-line progress display, overwriting the previous one."""
    term_width = shutil.get_terminal_size((80, 24)).columns
    bar_width = max(10, term_width - 6)  # leave room for " NNN%"
    filled = int(bar_width * percent / 100)
    bar = BAR_CHAR * filled + " " * (bar_width - filled)
    line1 = f"{bar} {percent:3d}%"
    line2 = f"Processing {rel_path} ({file_num}/{total_files})"
    # Truncate line2 if wider than terminal
    if len(line2) > term_width:
        line2 = line2[: term_width - 3] + "..."
    # Move up 2 lines, clear, and rewrite (first iteration has nothing to move over)
    sys.stdout.write(f"\033[2K{line1}\n\033[2K{line2}\n\033[2A")
    sys.stdout.flush()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "output",
        nargs="?",
        type=Path,
        default=PROJECT_ROOT / "gopls-check-report.txt",
        help="Output file path (default: gopls-check-report.txt)",
    )
    parser.add_argument(
        "--gopls",
        default="gopls",
        help="Path to gopls binary (default: gopls)",
    )
    args = parser.parse_args()

    # Verify gopls is available
    try:
        version_result = subprocess.run(
            [args.gopls, "version"],
            capture_output=True,
            text=True,
            timeout=10,
        )
        version_line = version_result.stdout.strip().split("\n")[0]
    except FileNotFoundError:
        print(f"Error: gopls not found at '{args.gopls}'", file=sys.stderr)
        return 1

    # Enumerate files and compute totals
    go_files = find_go_files(PROJECT_ROOT)
    total_files = len(go_files)
    total_bytes = sum(size for _, size in go_files)

    print(f"Using: {version_line}")
    print(f"Found {total_files} Go files ({total_bytes:,} bytes)")
    print()  # blank line before progress display
    print()  # second blank line — progress overwrites these two lines

    bytes_processed = 0
    files_with_findings = 0
    total_finding_lines = 0

    with open(args.output, "w") as out:
        # Write header
        out.write(f"gopls check report — {version_line}\n")
        out.write(f"Files checked: {total_files}\n")
        out.write(f"Total bytes: {total_bytes:,}\n")
        out.write("=" * 72 + "\n\n")
        out.flush()

        for i, (filepath, file_size) in enumerate(go_files, 1):
            rel = filepath.relative_to(PROJECT_ROOT)
            print_progress(
                int(bytes_processed * 100 / total_bytes) if total_bytes else 0,
                str(rel),
                i,
                total_files,
            )

            output = run_gopls_check(args.gopls, filepath)
            bytes_processed += file_size

            if output:
                files_with_findings += 1
                finding_lines = len(output.splitlines())
                total_finding_lines += finding_lines
                out.write(output + "\n\n")
                out.flush()

    # Final progress at 100%
    print_progress(100, "done", total_files, total_files)
    # Move past the progress display
    print("\n")

    # Append summary to output file
    with open(args.output, "a") as out:
        out.write("=" * 72 + "\n")
        out.write(f"Files with findings: {files_with_findings}\n")
        out.write(f"Total finding lines: {total_finding_lines}\n")

    print(f"Report written to {args.output}")
    print(f"  {files_with_findings} file(s) with findings, "
          f"{total_finding_lines} finding line(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
