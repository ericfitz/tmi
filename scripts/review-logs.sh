#!/bin/sh
#
# review-test-logs.sh - Review TMI server logs from a test run for unexpected errors
#
# Scans all server logs (current + rotated gzip) and the integration test server
# log for ERROR and WARN entries, classifies them as expected (from CATS fuzzing,
# integration tests, etc.) or unexpected, and reports a summary.
#
# Usage: ./scripts/review-test-logs.sh [logs_dir]
#   logs_dir: path to logs directory (default: logs/)

set -eu

LOGS_DIR="${1:-logs}"

# Temp files
UNEXPECTED_ERROR_FILE=$(mktemp)
UNEXPECTED_WARN_FILE=$(mktemp)
trap 'rm -f "$UNEXPECTED_ERROR_FILE" "$UNEXPECTED_WARN_FILE"' EXIT

# -------------------------------------------------------------------
# Expected ERROR patterns - pipe-separated regex
# These are errors we expect during CATS fuzzing, integration tests,
# and normal API test operation.
# -------------------------------------------------------------------
read -r -d '' EXPECTED_ERROR_REGEX << 'PATTERNS' || true
OPENAPI_VALIDATION_FAILED|OPENAPI_ERROR_CONVERTED|PARAMETER_BINDING_FAILED|PARAMETER_ERROR_CONVERTED|not found|metadata key not found|client credential not found|asset not found|document not found|threat not found|note not found|repository not found|team not found|project not found|add-on not found|webhook subscription not found|survey not found|survey response not found|triage note not found|invocation not found|Failed to parse JWT token|token is malformed|token contains an invalid number|Authorization header is missing|invalid or expired token|violates foreign key constraint|violates unique constraint|violates check constraint|duplicate key value|invalid_input|invalid metadata|invalid survey|preferences already exist|Failed to create survey response|Failed to delete survey metadata|Failed to delete client credential|Failed to get team|Failed to get project|Failed to get add-on|Failed to get invocation|Failed to retrieve asset|Failed to retrieve document|Failed to retrieve threat|Failed to retrieve challenge token|cannot delete team|GORM query error|connect: connection refused|Failed to authenticate|rate limit|too many requests|SECURITY.*[Cc]hallenge|Invalid deletion challenge|failed to process message|failed to read from stream|Status message too long
PATTERNS

# -------------------------------------------------------------------
# Expected WARN patterns
# -------------------------------------------------------------------
read -r -d '' EXPECTED_WARN_REGEX << 'PATTERNS' || true
request validation failed.*security requirements failed|Request validation warning|Excessive parameter length|Empty path parameter|Empty string in likely required field|Request body too large|invalid Content-Type|[Uu]nsupported Content-Type|unsupported media type|Unsupported HTTP method|Rate limit|SAML|saml|Invalid JSON syntax|Request contains problematic Unicode|Null byte detected|Path traversal attempt|SQL injection attempt|Invalid UUID format|Foreign key constraint violation|Invalid description in client credential|Invalid name in client credential|Invalid request body|JSON contains duplicate keys|JSON contains trailing garbage|JWT_MIDDLEWARE.*Authentication failed|Access denied for user|Authentication failed|Admin check|No settings encryption key configured|PREFERENCES.*already exist|PREFERENCES.*Validation failed|Unknown field in revocation request|Client credentials not found
PATTERNS

echo "========================================================================"
echo "TMI Server Log Review"
echo "========================================================================"
echo ""
echo "Log directory: $LOGS_DIR"
echo "Scanning for ERROR and WARN entries..."
echo ""
echo "--- Per-file summary ---"
echo ""

TOTAL_FILES=0
TOTAL_ERRORS=0
TOTAL_WARNS=0
TOTAL_EXPECTED_ERRORS=0
TOTAL_EXPECTED_WARNS=0

# -------------------------------------------------------------------
# process_file <label> <command...>
# Runs <command> to produce log lines, filters ERROR and WARN,
# classifies as expected or unexpected.
# -------------------------------------------------------------------
process_file() {
  label="$1"
  shift

  TOTAL_FILES=$((TOTAL_FILES + 1))

  # Extract error/warn lines into temp files
  file_errors=$(mktemp)
  file_warns=$(mktemp)

  # Run the command twice (once for errors, once for warns)
  "$@" | grep 'level=ERROR' > "$file_errors" 2>/dev/null || true
  "$@" | grep 'level=WARN'  > "$file_warns"  2>/dev/null || true

  err_count=$(wc -l < "$file_errors" | tr -d ' ')
  warn_count=$(wc -l < "$file_warns" | tr -d ' ')

  # Classify errors
  expected_err=0
  unexpected_err=0
  if [ "$err_count" -gt 0 ]; then
    expected_err=$(grep -cE "$EXPECTED_ERROR_REGEX" "$file_errors" || true)
    expected_err=$(echo "$expected_err" | head -1 | tr -d '[:space:]')
    : "${expected_err:=0}"
    unexpected_err=$((err_count - expected_err))
    if [ "$unexpected_err" -gt 0 ]; then
      # Use | as sed delimiter to avoid conflicts with / in label
      grep -vE "$EXPECTED_ERROR_REGEX" "$file_errors" | \
        sed "s|^|[$label] |" >> "$UNEXPECTED_ERROR_FILE"
    fi
  fi

  # Classify warns
  expected_warn=0
  unexpected_warn=0
  if [ "$warn_count" -gt 0 ]; then
    expected_warn=$(grep -cE "$EXPECTED_WARN_REGEX" "$file_warns" || true)
    expected_warn=$(echo "$expected_warn" | head -1 | tr -d '[:space:]')
    : "${expected_warn:=0}"
    unexpected_warn=$((warn_count - expected_warn))
    if [ "$unexpected_warn" -gt 0 ]; then
      grep -vE "$EXPECTED_WARN_REGEX" "$file_warns" | \
        sed "s|^|[$label] |" >> "$UNEXPECTED_WARN_FILE"
    fi
  fi

  TOTAL_ERRORS=$((TOTAL_ERRORS + err_count))
  TOTAL_WARNS=$((TOTAL_WARNS + warn_count))
  TOTAL_EXPECTED_ERRORS=$((TOTAL_EXPECTED_ERRORS + expected_err))
  TOTAL_EXPECTED_WARNS=$((TOTAL_EXPECTED_WARNS + expected_warn))

  if [ "$err_count" -gt 0 ] || [ "$warn_count" -gt 0 ]; then
    printf "  %-55s E: %5d (%d unexpected)  W: %5d (%d unexpected)\n" \
      "$label" "$err_count" "$unexpected_err" "$warn_count" "$unexpected_warn"
  else
    printf "  %-55s (clean)\n" "$label"
  fi

  rm -f "$file_errors" "$file_warns"
}

# Process rotated gzip logs
for f in "$LOGS_DIR"/tmi-*.log.gz; do
  [ -f "$f" ] || continue
  process_file "$(basename "$f")" gunzip -c "$f"
done

# Process current tmi.log
if [ -f "$LOGS_DIR/tmi.log" ]; then
  process_file "tmi.log" cat "$LOGS_DIR/tmi.log"
fi

# Process integration test server log
if [ -f "$LOGS_DIR/integration-test-server.log" ]; then
  process_file "integration-test-server.log" cat "$LOGS_DIR/integration-test-server.log"
fi

# Process integration test subdirectory logs
for f in "$LOGS_DIR"/integration-test/*.log; do
  [ -f "$f" ] || continue
  process_file "integration-test/$(basename "$f")" cat "$f"
done

UNEXPECTED_ERRORS=$((TOTAL_ERRORS - TOTAL_EXPECTED_ERRORS))
UNEXPECTED_WARNS=$((TOTAL_WARNS - TOTAL_EXPECTED_WARNS))

echo ""
echo "--- Overall summary ---"
echo ""
printf "  Log files scanned:          %d\n" "$TOTAL_FILES"
printf "  Total ERROR entries:        %d  (%d expected, %d unexpected)\n" \
  "$TOTAL_ERRORS" "$TOTAL_EXPECTED_ERRORS" "$UNEXPECTED_ERRORS"
printf "  Total WARN entries:         %d  (%d expected, %d unexpected)\n" \
  "$TOTAL_WARNS" "$TOTAL_EXPECTED_WARNS" "$UNEXPECTED_WARNS"
echo ""

# Report unexpected entries (deduplicated)
if [ "$UNEXPECTED_ERRORS" -gt 0 ]; then
  echo "========================================================================"
  echo "UNEXPECTED ERRORS ($UNEXPECTED_ERRORS total, deduplicated below)"
  echo "========================================================================"
  echo ""
  sed 's/time=[^ ]* //' "$UNEXPECTED_ERROR_FILE" | \
    sed 's/request_id=[^ ]*//' | \
    sed 's/client_ip=[^ ]*//' | \
    sed 's/user_id=[^ ]*//' | \
    sed 's/source=[^ ]*//' | \
    sed 's/\[20[0-9]*-[0-9]*\.[0-9]*\]/[...]/g' | \
    sort -u | while IFS= read -r line; do
      printf "  %.250s\n" "$line"
    done
  echo ""
fi

if [ "$UNEXPECTED_WARNS" -gt 0 ]; then
  echo "========================================================================"
  echo "UNEXPECTED WARNINGS ($UNEXPECTED_WARNS total, deduplicated below)"
  echo "========================================================================"
  echo ""
  sed 's/time=[^ ]* //' "$UNEXPECTED_WARN_FILE" | \
    sed 's/request_id=[^ ]*//' | \
    sed 's/client_ip=[^ ]*//' | \
    sed 's/user_id=[^ ]*//' | \
    sed 's/source=[^ ]*//' | \
    sed 's/\[20[0-9]*-[0-9]*\.[0-9]*\]/[...]/g' | \
    sort -u | while IFS= read -r line; do
      printf "  %.250s\n" "$line"
    done
  echo ""
fi

# Exit code
if [ "$UNEXPECTED_ERRORS" -gt 0 ]; then
  echo "RESULT: FAIL - $UNEXPECTED_ERRORS unexpected error(s) found in server logs"
  exit 1
elif [ "$UNEXPECTED_WARNS" -gt 0 ]; then
  echo "RESULT: WARN - All errors expected, but $UNEXPECTED_WARNS unexpected warning(s) found"
  exit 0
else
  echo "RESULT: PASS - All errors and warnings are expected from test activity"
  exit 0
fi
