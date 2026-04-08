#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# ///
"""Query CATS results database and display summary statistics."""

import argparse
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmi_common import (  # noqa: E402
    add_verbosity_args,
    apply_verbosity,
    get_project_root,
    log_error,
    log_info,
)


def print_table(headers: list[str], rows: list[tuple]) -> None:
    """Print rows as a column-aligned table with header and separator."""
    if not rows:
        print("  (no results)")
        return

    # Calculate column widths from headers and data
    widths = [len(h) for h in headers]
    str_rows = [[str(v) for v in row] for row in rows]
    for row in str_rows:
        for i, val in enumerate(row):
            if i < len(widths):
                widths[i] = max(widths[i], len(val))

    fmt = "  ".join(f"{{:<{w}}}" for w in widths)
    print(fmt.format(*headers))
    print(fmt.format(*["-" * w for w in widths]))
    for row in str_rows:
        print(fmt.format(*row))


def query_summary(cur: sqlite3.Cursor) -> None:
    """Summary statistics excluding OAuth false positives."""
    print("Summary (excluding OAuth false positives):")
    cur.execute(
        """
        SELECT
            rt.name AS result,
            COUNT(*) AS count,
            ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) AS percentage
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        WHERE t.is_false_positive = 0
        GROUP BY rt.name
        ORDER BY count DESC;
        """
    )
    rows = cur.fetchall()
    print_table(["result", "count", "percentage"], rows)


def query_false_positives(cur: sqlite3.Cursor) -> None:
    """Count of OAuth/Auth false positives."""
    print("OAuth/Auth False Positives (expected 401/403 responses):")
    cur.execute("SELECT COUNT(*) FROM tests WHERE is_false_positive = 1;")
    count = cur.fetchone()[0]
    print(f"  {count}")


def query_errors_by_path(cur: sqlite3.Cursor) -> None:
    """Top 10 actual errors by path."""
    print("Actual Errors by Path (top 10, excluding OAuth false positives):")
    cur.execute(
        """
        SELECT
            p.path,
            COUNT(*) AS error_count,
            GROUP_CONCAT(DISTINCT f.name) AS fuzzers
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        JOIN paths p ON t.path_id = p.id
        JOIN fuzzers f ON t.fuzzer_id = f.id
        WHERE rt.name = 'error' AND t.is_false_positive = 0
        GROUP BY p.path
        ORDER BY error_count DESC
        LIMIT 10;
        """
    )
    rows = cur.fetchall()
    print_table(["path", "error_count", "fuzzers"], rows)


def query_warnings_by_path(cur: sqlite3.Cursor) -> None:
    """Top 10 warnings by path."""
    print("Warnings by Path (top 10, excluding OAuth false positives):")
    cur.execute(
        """
        SELECT
            p.path,
            COUNT(*) AS warn_count
        FROM tests t
        JOIN result_types rt ON t.result_type_id = rt.id
        JOIN paths p ON t.path_id = p.id
        WHERE rt.name = 'warn' AND t.is_false_positive = 0
        GROUP BY p.path
        ORDER BY warn_count DESC
        LIMIT 10;
        """
    )
    rows = cur.fetchall()
    print_table(["path", "warn_count"], rows)


def main() -> None:
    default_db = str(get_project_root() / "test" / "outputs" / "cats" / "cats-results.db")

    parser = argparse.ArgumentParser(
        description="Query CATS results database and display summary statistics.",
    )
    parser.add_argument(
        "--db",
        metavar="FILE",
        default=default_db,
        help=f"Path to CATS results SQLite database (default: {default_db})",
    )
    add_verbosity_args(parser)
    args = parser.parse_args()
    apply_verbosity(args)

    db_path = Path(args.db)
    if not db_path.exists():
        log_error(f"Database file not found: {db_path}")
        print("", file=sys.stderr)
        print("First, parse CATS reports with:", file=sys.stderr)
        print(
            "  uv run scripts/parse_cats_results.py -i test/outputs/cats/report/"
            " -o test/outputs/cats/cats-results.db --create-schema",
            file=sys.stderr,
        )
        sys.exit(1)

    log_info(f"CATS Results Database: {db_path}")
    print("=" * 40)
    print()

    conn = sqlite3.connect(str(db_path))
    cur = conn.cursor()

    try:
        query_summary(cur)
        print()
        query_false_positives(cur)
        print()
        query_errors_by_path(cur)
        print()
        query_warnings_by_path(cur)
        print()
    finally:
        conn.close()

    db_str = str(db_path)
    print("Query examples:")
    print("  # All actual errors (excluding OAuth false positives):")
    print(f'  sqlite3 {db_str} "SELECT * FROM test_results_filtered_view WHERE result = \'error\';"')
    print()
    print("  # OAuth false positives:")
    print(f'  sqlite3 {db_str} "SELECT * FROM test_results_view WHERE is_false_positive = 1;"')
    print()
    print("  # Errors by fuzzer:")
    print(
        f'  sqlite3 {db_str} "SELECT fuzzer, COUNT(*) FROM test_results_filtered_view'
        f" WHERE result = 'error' GROUP BY fuzzer;\""
    )


if __name__ == "__main__":
    main()
