#!/bin/bash

# Query CATS Results Database
# Provides quick SQL queries against the parsed CATS results database

set -euo pipefail

DB_FILE="${1:-test/outputs/cats/cats-results.db}"

if [[ ! -f "$DB_FILE" ]]; then
    echo "Error: Database file not found: $DB_FILE"
    echo "Usage: $0 [database-file]"
    echo ""
    echo "First, parse CATS reports with:"
    echo "  uv run scripts/parse-cats-results.py -i test/outputs/cats/report/ -o test/outputs/cats/cats-results.db --create-schema"
    exit 1
fi

echo "CATS Results Database: $DB_FILE"
echo "========================================"
echo ""

# Summary statistics (excluding OAuth false positives)
echo "📊 Summary (excluding OAuth false positives):"
sqlite3 "$DB_FILE" <<SQL
.mode column
.headers on
SELECT
    rt.name AS result,
    COUNT(*) AS count,
    ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) AS percentage
FROM tests t
JOIN result_types rt ON t.result_type_id = rt.id
WHERE t.is_oauth_false_positive = 0
GROUP BY rt.name
ORDER BY count DESC;
SQL

echo ""
echo "🔐 OAuth/Auth False Positives (expected 401/403 responses):"
sqlite3 "$DB_FILE" "SELECT COUNT(*) FROM tests WHERE is_oauth_false_positive = 1;"

echo ""
echo "❌ Actual Errors by Path (top 10, excluding OAuth false positives):"
sqlite3 "$DB_FILE" <<SQL
.mode column
.headers on
SELECT
    p.path,
    COUNT(*) AS error_count,
    GROUP_CONCAT(DISTINCT f.name) AS fuzzers
FROM tests t
JOIN result_types rt ON t.result_type_id = rt.id
JOIN paths p ON t.path_id = p.id
JOIN fuzzers f ON t.fuzzer_id = f.id
WHERE rt.name = 'error' AND t.is_oauth_false_positive = 0
GROUP BY p.path
ORDER BY error_count DESC
LIMIT 10;
SQL

echo ""
echo "⚠️  Warnings by Path (top 10, excluding OAuth false positives):"
sqlite3 "$DB_FILE" <<SQL
.mode column
.headers on
SELECT
    p.path,
    COUNT(*) AS warn_count
FROM tests t
JOIN result_types rt ON t.result_type_id = rt.id
JOIN paths p ON t.path_id = p.id
WHERE rt.name = 'warn' AND t.is_oauth_false_positive = 0
GROUP BY p.path
ORDER BY warn_count DESC
LIMIT 10;
SQL

echo ""
echo "🔍 Query examples:"
echo "  # All actual errors (excluding OAuth false positives):"
echo "  sqlite3 $DB_FILE \"SELECT * FROM test_results_filtered_view WHERE result = 'error';\""
echo ""
echo "  # OAuth false positives:"
echo "  sqlite3 $DB_FILE \"SELECT * FROM test_results_view WHERE is_oauth_false_positive = 1;\""
echo ""
echo "  # Errors by fuzzer:"
echo "  sqlite3 $DB_FILE \"SELECT fuzzer, COUNT(*) FROM test_results_filtered_view WHERE result = 'error' GROUP BY fuzzer;\""
