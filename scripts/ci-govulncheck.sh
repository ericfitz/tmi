#!/usr/bin/env bash
# Run govulncheck and fail on any reachable vulnerability that is not listed,
# unexpired, in the allowlist. Usage: ci-govulncheck.sh <allowlist> [dir]
set -euo pipefail
allow_file=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
cd "${2:-.}"
today=$(date -u +%Y-%m-%d)

# Reachable findings are the ones whose trace names a function.
found=$(govulncheck -format json ./... |
  jq -r 'select(.finding != null and .finding.trace[0].function != null) | .finding.osv' | sort -u)
[ -z "$found" ] && { echo "govulncheck: no reachable vulnerabilities"; exit 0; }

fail=0
for id in $found; do
  review_by=$(awk -v id="$id" '$1 == id { print $2 }' "$allow_file")
  if [ -n "$review_by" ] && [[ "$today" < "$review_by" || "$today" == "$review_by" ]]; then
    echo "govulncheck: $id allowed until $review_by (see $(basename "$allow_file"))"
  else
    echo "govulncheck: $id is reachable and not allowed${review_by:+ (allowance expired $review_by)}"
    fail=1
  fi
done
[ "$fail" -eq 0 ] || { govulncheck ./... || true; exit 1; }
