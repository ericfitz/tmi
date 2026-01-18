# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""
Parse vacuum OpenAPI validation JSON report and load results into SQLite database.

This script reads a vacuum JSON report file and loads the validation results into
a SQLite database for easier querying and analysis. It creates summary views for
analyzing issues by rule, severity, category, and path.

Usage:
    uv run scripts/parse-openapi-validation.py [--report PATH] [--db PATH]

Options:
    --report PATH   Path to vacuum JSON report (default: docs/reference/apis/openapi-validation-report.json)
    --db PATH       Path to SQLite database (default: docs/reference/apis/openapi-validation.db)
    --summary       Print summary statistics after loading
    --help          Show this help message

Examples:
    # Parse default report file
    uv run scripts/parse-openapi-validation.py

    # Parse with custom paths
    uv run scripts/parse-openapi-validation.py --report /tmp/report.json --db /tmp/validation.db

    # Parse and show summary
    uv run scripts/parse-openapi-validation.py --summary
"""

import argparse
import json
import sqlite3
import sys
from datetime import datetime
from pathlib import Path


def create_database_schema(conn: sqlite3.Connection) -> None:
    """Create database tables and views for validation results."""
    cursor = conn.cursor()

    # Drop existing tables to ensure clean state
    cursor.executescript("""
        DROP TABLE IF EXISTS validation_results;
        DROP TABLE IF EXISTS rules;
        DROP TABLE IF EXISTS category_statistics;
        DROP TABLE IF EXISTS report_metadata;
        DROP VIEW IF EXISTS results_by_rule;
        DROP VIEW IF EXISTS results_by_severity;
        DROP VIEW IF EXISTS results_by_category;
        DROP VIEW IF EXISTS results_by_path_prefix;
        DROP VIEW IF EXISTS error_summary;
    """)

    # Create tables
    cursor.executescript("""
        -- Report metadata
        CREATE TABLE report_metadata (
            id INTEGER PRIMARY KEY,
            generated_at TEXT NOT NULL,
            spec_type TEXT,
            spec_version TEXT,
            spec_format TEXT,
            file_size_kb INTEGER,
            num_paths INTEGER,
            num_operations INTEGER,
            num_schemas INTEGER,
            total_errors INTEGER,
            total_warnings INTEGER,
            total_info INTEGER,
            overall_score INTEGER,
            loaded_at TEXT NOT NULL
        );

        -- Rules definitions
        CREATE TABLE rules (
            rule_id TEXT PRIMARY KEY,
            description TEXT,
            severity TEXT,
            category_id TEXT,
            category_name TEXT,
            recommended INTEGER,
            rule_type TEXT,
            how_to_fix TEXT
        );

        -- Validation results
        CREATE TABLE validation_results (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            rule_id TEXT NOT NULL,
            rule_severity TEXT NOT NULL,
            message TEXT NOT NULL,
            path TEXT,
            line_start INTEGER,
            line_end INTEGER,
            character_start INTEGER,
            character_end INTEGER,
            FOREIGN KEY (rule_id) REFERENCES rules(rule_id)
        );

        -- Category statistics from report
        CREATE TABLE category_statistics (
            category_id TEXT PRIMARY KEY,
            category_name TEXT NOT NULL,
            num_issues INTEGER,
            score INTEGER,
            errors INTEGER,
            warnings INTEGER,
            info INTEGER,
            hints INTEGER
        );

        -- View: Results grouped by rule
        CREATE VIEW results_by_rule AS
        SELECT
            r.rule_id,
            r.description as rule_description,
            r.severity as rule_severity,
            r.category_name,
            COUNT(*) as count,
            SUM(CASE WHEN vr.rule_severity = 'error' THEN 1 ELSE 0 END) as error_count,
            SUM(CASE WHEN vr.rule_severity = 'warn' THEN 1 ELSE 0 END) as warning_count,
            SUM(CASE WHEN vr.rule_severity = 'info' THEN 1 ELSE 0 END) as info_count
        FROM validation_results vr
        LEFT JOIN rules r ON vr.rule_id = r.rule_id
        GROUP BY vr.rule_id
        ORDER BY count DESC;

        -- View: Results grouped by severity
        CREATE VIEW results_by_severity AS
        SELECT
            rule_severity,
            COUNT(*) as count
        FROM validation_results
        GROUP BY rule_severity
        ORDER BY
            CASE rule_severity
                WHEN 'error' THEN 1
                WHEN 'warn' THEN 2
                WHEN 'info' THEN 3
                ELSE 4
            END;

        -- View: Results grouped by category
        CREATE VIEW results_by_category AS
        SELECT
            COALESCE(r.category_name, 'Unknown') as category_name,
            COALESCE(r.category_id, 'unknown') as category_id,
            COUNT(*) as count,
            SUM(CASE WHEN vr.rule_severity = 'error' THEN 1 ELSE 0 END) as error_count,
            SUM(CASE WHEN vr.rule_severity = 'warn' THEN 1 ELSE 0 END) as warning_count,
            SUM(CASE WHEN vr.rule_severity = 'info' THEN 1 ELSE 0 END) as info_count
        FROM validation_results vr
        LEFT JOIN rules r ON vr.rule_id = r.rule_id
        GROUP BY r.category_id, r.category_name
        ORDER BY error_count DESC, count DESC;

        -- View: Results by path prefix (for finding problematic areas)
        CREATE VIEW results_by_path_prefix AS
        SELECT
            CASE
                WHEN path LIKE '$.paths%' THEN
                    substr(path, 1, instr(substr(path, 9), '/') + 7)
                WHEN path LIKE '$.components.schemas%' THEN
                    'components.schemas'
                WHEN path LIKE '$.components%' THEN
                    'components.other'
                ELSE
                    COALESCE(path, 'root')
            END as path_area,
            COUNT(*) as count,
            SUM(CASE WHEN rule_severity = 'error' THEN 1 ELSE 0 END) as error_count
        FROM validation_results
        GROUP BY path_area
        ORDER BY error_count DESC, count DESC
        LIMIT 50;

        -- View: Error summary for quick triage
        CREATE VIEW error_summary AS
        SELECT
            vr.rule_id,
            r.description as rule_description,
            r.category_name,
            r.how_to_fix,
            COUNT(*) as count
        FROM validation_results vr
        LEFT JOIN rules r ON vr.rule_id = r.rule_id
        WHERE vr.rule_severity = 'error'
        GROUP BY vr.rule_id
        ORDER BY count DESC;

        -- Create indexes for faster queries
        CREATE INDEX idx_results_rule_id ON validation_results(rule_id);
        CREATE INDEX idx_results_severity ON validation_results(rule_severity);
        CREATE INDEX idx_results_path ON validation_results(path);
    """)

    conn.commit()


def load_report(conn: sqlite3.Connection, report_data: dict) -> None:
    """Load vacuum report data into database."""
    cursor = conn.cursor()
    now = datetime.now().isoformat()

    # Load report metadata
    stats = report_data.get("statistics", {})
    cursor.execute("""
        INSERT INTO report_metadata (
            generated_at, spec_type, spec_version, spec_format,
            file_size_kb, num_paths, num_operations, num_schemas,
            total_errors, total_warnings, total_info, overall_score, loaded_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    """, (
        report_data.get("generated", ""),
        stats.get("specType", ""),
        stats.get("version", ""),
        stats.get("specFormat", ""),
        stats.get("filesizeKb", 0),
        stats.get("paths", 0),
        stats.get("operations", 0),
        stats.get("schemas", 0),
        stats.get("totalErrors", 0),
        stats.get("totalWarnings", 0),
        stats.get("totalInfo", 0),
        stats.get("overallScore", 0),
        now
    ))

    # Load rules
    rules = report_data.get("rules", {})
    for rule_id, rule_data in rules.items():
        category = rule_data.get("category", {})
        cursor.execute("""
            INSERT OR REPLACE INTO rules (
                rule_id, description, severity, category_id, category_name,
                recommended, rule_type, how_to_fix
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            rule_id,
            rule_data.get("description", ""),
            rule_data.get("severity", ""),
            category.get("id", ""),
            category.get("name", ""),
            1 if rule_data.get("recommended") else 0,
            rule_data.get("type", ""),
            rule_data.get("howToFix", "")
        ))

    # Load category statistics
    cat_stats = stats.get("categoryStatistics", [])
    for cat in cat_stats:
        cursor.execute("""
            INSERT OR REPLACE INTO category_statistics (
                category_id, category_name, num_issues, score,
                errors, warnings, info, hints
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            cat.get("categoryId", ""),
            cat.get("categoryName", ""),
            cat.get("numIssues", 0),
            cat.get("score", 0),
            cat.get("errors", 0),
            cat.get("warnings", 0),
            cat.get("info", 0),
            cat.get("hints", 0)
        ))

    # Load validation results
    results = report_data.get("resultSet", {}).get("results", [])
    for result in results:
        range_data = result.get("range", {})
        start = range_data.get("start", {})
        end = range_data.get("end", {})

        cursor.execute("""
            INSERT INTO validation_results (
                rule_id, rule_severity, message, path,
                line_start, line_end, character_start, character_end
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        """, (
            result.get("ruleId", ""),
            result.get("ruleSeverity", ""),
            result.get("message", ""),
            result.get("path", ""),
            start.get("line"),
            end.get("line"),
            start.get("character"),
            end.get("character")
        ))

    conn.commit()


def print_summary(conn: sqlite3.Connection) -> None:
    """Print summary statistics from the database."""
    cursor = conn.cursor()

    # Get metadata
    cursor.execute("SELECT * FROM report_metadata LIMIT 1")
    meta = cursor.fetchone()
    if meta:
        print("\n" + "=" * 70)
        print("OpenAPI Validation Report Summary")
        print("=" * 70)
        print(f"Generated: {meta[1]}")
        print(f"Spec: {meta[2]} {meta[3]} ({meta[4]})")
        print(f"Size: {meta[5]} KB | Paths: {meta[6]} | Operations: {meta[7]} | Schemas: {meta[8]}")
        print(f"Score: {meta[12]}/100")
        print(f"\nTotal Issues: {meta[9]} errors, {meta[10]} warnings, {meta[11]} info")

    # Results by severity
    print("\n" + "-" * 70)
    print("Issues by Severity")
    print("-" * 70)
    cursor.execute("SELECT * FROM results_by_severity")
    for row in cursor.fetchall():
        print(f"  {row[0]:10} {row[1]:>6}")

    # Results by category
    print("\n" + "-" * 70)
    print("Issues by Category")
    print("-" * 70)
    print(f"  {'Category':<25} {'Total':>8} {'Errors':>8} {'Warnings':>8} {'Info':>8}")
    print(f"  {'-'*25} {'-'*8} {'-'*8} {'-'*8} {'-'*8}")
    cursor.execute("SELECT * FROM results_by_category")
    for row in cursor.fetchall():
        print(f"  {row[0]:<25} {row[2]:>8} {row[3]:>8} {row[4]:>8} {row[5]:>8}")

    # Top errors by rule
    print("\n" + "-" * 70)
    print("Top 15 Rules by Error Count")
    print("-" * 70)
    print(f"  {'Rule ID':<35} {'Category':<20} {'Count':>8}")
    print(f"  {'-'*35} {'-'*20} {'-'*8}")
    cursor.execute("SELECT * FROM error_summary LIMIT 15")
    for row in cursor.fetchall():
        rule_id = row[0][:35] if row[0] else "unknown"
        category = (row[2] or "unknown")[:20]
        print(f"  {rule_id:<35} {category:<20} {row[4]:>8}")

    print("\n" + "=" * 70)
    print("Query the database for more details:")
    print("  sqlite3 <db-path> 'SELECT * FROM error_summary'")
    print("  sqlite3 <db-path> 'SELECT * FROM results_by_rule'")
    print("  sqlite3 <db-path> 'SELECT * FROM results_by_path_prefix'")
    print("=" * 70 + "\n")


def main() -> int:
    """Main entry point."""
    parser = argparse.ArgumentParser(
        description="Parse vacuum OpenAPI validation report into SQLite database"
    )
    parser.add_argument(
        "--report",
        default="docs/reference/apis/openapi-validation-report.json",
        help="Path to vacuum JSON report file"
    )
    parser.add_argument(
        "--db",
        default="docs/reference/apis/openapi-validation.db",
        help="Path to SQLite database file"
    )
    parser.add_argument(
        "--summary",
        action="store_true",
        help="Print summary statistics after loading"
    )

    args = parser.parse_args()

    report_path = Path(args.report)
    db_path = Path(args.db)

    # Check report file exists
    if not report_path.exists():
        print(f"Error: Report file not found: {report_path}", file=sys.stderr)
        return 1

    # Load report JSON
    print(f"Loading report from: {report_path}")
    try:
        with open(report_path) as f:
            report_data = json.load(f)
    except json.JSONDecodeError as e:
        print(f"Error: Invalid JSON in report file: {e}", file=sys.stderr)
        return 1

    # Delete existing database to ensure clean state (no duplicates)
    if db_path.exists():
        print(f"Removing existing database: {db_path}")
        db_path.unlink()

    # Create new database
    print(f"Creating database: {db_path}")
    db_path.parent.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.Connection(db_path)
    try:
        create_database_schema(conn)
        load_report(conn, report_data)

        # Get counts for confirmation
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM validation_results")
        result_count = cursor.fetchone()[0]
        cursor.execute("SELECT COUNT(*) FROM rules")
        rule_count = cursor.fetchone()[0]

        print(f"Loaded {result_count} validation results and {rule_count} rules")

        if args.summary:
            print_summary(conn)

    finally:
        conn.close()

    return 0


if __name__ == "__main__":
    sys.exit(main())
