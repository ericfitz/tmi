#!/bin/bash
# Self-test for test/postman/lib/factory-globals.sh.
# Usage: bash test/postman/tests/factory-globals.test.sh
# FACTORY_GLOBALS_LIB may point at an alternative library file.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FACTORY_GLOBALS_LIB="${FACTORY_GLOBALS_LIB:-$SCRIPT_DIR/../lib/factory-globals.sh}"
FACTORY_JS="$SCRIPT_DIR/../test-data-factory.js"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

PASS=0
FAIL=0
check() { # check <description> <1|0>
    if [ "$2" = "1" ]; then
        PASS=$((PASS + 1))
        echo "PASS: $1"
    else
        FAIL=$((FAIL + 1))
        echo "FAIL: $1"
    fi
}
ok() { if "$@" >/dev/null 2>&1; then echo 1; else echo 0; fi; }

# shellcheck source=/dev/null
source "$FACTORY_GLOBALS_LIB"

# (a) generated file is valid globals JSON whose value equals the factory byte-for-byte
OUT="$WORK_DIR/globals.json"
check "a: generator succeeds" "$(ok write_factory_globals "$FACTORY_JS" "$OUT")"
check "a: valid JSON" "$(ok jq -e . "$OUT")"
check "a: one enabled TMITestDataFactory entry" \
    "$(ok jq -e '.values | length == 1 and .[0].key == "TMITestDataFactory" and .[0].enabled == true' "$OUT")"
jq -j '.values[0].value' "$OUT" >"$WORK_DIR/roundtrip.js"
check "a: value equals the factory file byte-for-byte" "$(ok cmp "$FACTORY_JS" "$WORK_DIR/roundtrip.js")"
check "a: file is not world-readable" "$([ "$(stat -f '%Lp' "$OUT" 2>/dev/null || stat -c '%a' "$OUT")" = "600" ] && echo 1 || echo 0)"

# (b) awkward content (quotes, backslashes, backticks, no trailing newline, unicode) round-trips
TRICKY="$WORK_DIR/tricky.js"
printf 'const a = "q\\"x" + `t${1}` + '"'"'\\n'"'"'; // \xe2\x9c\x85 \\u0041' >"$TRICKY"
OUT2="$WORK_DIR/globals2.json"
write_factory_globals "$TRICKY" "$OUT2"
jq -j '.values[0].value' "$OUT2" >"$WORK_DIR/tricky.rt"
check "b: tricky content round-trips" "$(ok cmp "$TRICKY" "$WORK_DIR/tricky.rt")"

# (c) missing factory file: non-zero, names the file, writes no output
OUT3="$WORK_DIR/globals3.json"
RC=0
ERR="$(write_factory_globals "$WORK_DIR/nope.js" "$OUT3" 2>&1)" || RC=$?
check "c: missing factory -> non-zero" "$([ "$RC" -ne 0 ] && echo 1 || echo 0)"
check "c: error names the missing file" "$(case "$ERR" in *nope.js*) echo 1 ;; *) echo 0 ;; esac)"
check "c: no output file left behind" "$([ ! -e "$OUT3" ] && echo 1 || echo 0)"

# (d) second run overwrites cleanly (idempotent)
write_factory_globals "$FACTORY_JS" "$OUT"
check "d: rerun still valid" "$(ok jq -e '.values | length == 1' "$OUT")"

# (e) make_factory_globals_tmp creates a file; the caller's trap-style cleanup removes it
TMP="$(make_factory_globals_tmp "$FACTORY_JS")"
check "e: temp globals file exists and is valid" "$([ -f "$TMP" ] && ok jq -e . "$TMP")"
rm -f "$TMP"
check "e: removed after cleanup" "$([ ! -e "$TMP" ] && echo 1 || echo 0)"

echo "Summary: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
