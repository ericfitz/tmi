#!/bin/bash
# Self-test for test/postman/lib/auth.sh using a fake curl with recorded responses.
# Usage: bash test/postman/tests/auth.test.sh
# AUTH_LIB may point at an alternative library file (used to show the test failing
# against the pre-fix logic).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUTH_LIB="${AUTH_LIB:-$SCRIPT_DIR/../lib/auth.sh}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
CALLS="$WORK_DIR/calls.log"

# Scenario knobs (set per case)
CREDS_BODY=""     # body returned by GET /creds
ME_CODE=""        # HTTP code for GET /me, or "000" for unreachable
FLOW_STATUS=""    # "completed" or "failed"

# Fake sleep: never wait.
sleep() { :; }

# Fake curl: records "<METHOD> <URL>" and returns recorded responses.
curl() {
    local url="" method="GET" want_code=0 arg
    while [ $# -gt 0 ]; do
        arg="$1"
        case "$arg" in
            -X) method="$2"; shift ;;
            -w) want_code=1; shift ;;
            -H | -d | -o) shift ;;
            http*) url="$arg" ;;
            *) ;;
        esac
        shift
    done
    echo "$method $url" >>"$CALLS"
    case "$url" in
        */creds\?userid=*) printf '%s' "$CREDS_BODY" ;;
        */me)
            if [ "$ME_CODE" = "000" ]; then
                [ "$want_code" -eq 1 ] && printf '000'
                return 7
            fi
            [ "$want_code" -eq 1 ] && printf '%s' "$ME_CODE"
            ;;
        */flows/start) printf '{"flow_id":"f1"}' ;;
        */flows/f1)
            if [ "$FLOW_STATUS" = "failed" ]; then
                printf '{"status":"failed","tokens_ready":false,"error":"boom"}'
            else
                printf '{"status":"completed","tokens_ready":true,"tokens":{"access_token":"new.tok.en"}}'
            fi
            ;;
        *) echo "unexpected curl: $method $url" >&2; return 1 ;;
    esac
    return 0
}

# shellcheck source=/dev/null
source "$AUTH_LIB"

PASS=0
FAIL=0
check() {
    local desc="$1" ok="$2"
    if [ "$ok" = "1" ]; then
        echo "PASS: $desc"; PASS=$((PASS + 1))
    else
        echo "FAIL: $desc"; FAIL=$((FAIL + 1))
    fi
}
called() { grep -q "$1" "$CALLS" && echo 1 || echo 0; }

# run_case <creds> <me_code> <flow_status>; sets OUT, ERR, RC
run_case() {
    CREDS_BODY="$1"; ME_CODE="$2"; FLOW_STATUS="$3"
    : >"$CALLS"
    RC=0
    OUT="$(authenticate_user alice 2>"$WORK_DIR/err")" || RC=$?
    ERR="$(cat "$WORK_DIR/err")"
}
no_leak() { # neither token value may appear in stderr
    case "$ERR" in *cached.tok.en* | *new.tok.en*) echo 0 ;; *) echo 1 ;; esac
}

CACHED='{"access_token":"cached.tok.en"}'

# (a) cached token accepted by server -> reused, no new flow
run_case "$CACHED" 200 completed
check "a: returns cached token" "$([ "$OUT" = "cached.tok.en" ] && [ "$RC" -eq 0 ] && echo 1 || echo 0)"
check "a: no /flows/start call" "$([ "$(called 'POST .*/flows/start')" = 0 ] && echo 1 || echo 0)"
check "a: no token in log" "$(no_leak)"

# (b) cached token rejected (401) -> fresh flow
run_case "$CACHED" 401 completed
check "b: returns new token" "$([ "$OUT" = "new.tok.en" ] && [ "$RC" -eq 0 ] && echo 1 || echo 0)"
check "b: calls /flows/start" "$(called 'POST .*/flows/start')"
check "b: logs rejection with HTTP code" "$(case "$ERR" in *"rejected by server (HTTP 401)"*) echo 1 ;; *) echo 0 ;; esac)"
check "b: no token in log" "$(no_leak)"

# (c) no cached token -> fresh flow (empty object and null token)
run_case '{}' 200 completed
check "c1: {} -> fresh flow token" "$([ "$OUT" = "new.tok.en" ] && [ "$RC" -eq 0 ] && echo 1 || echo 0)"
check "c1: calls /flows/start" "$(called 'POST .*/flows/start')"
run_case '{"access_token":null}' 200 completed
check "c2: null token -> fresh flow token" "$([ "$OUT" = "new.tok.en" ] && [ "$RC" -eq 0 ] && echo 1 || echo 0)"
check "c2: server not asked to validate nothing" "$([ "$(called 'GET .*/me')" = 0 ] && echo 1 || echo 0)"

# (d) flow failed -> non-zero, nothing on stdout
run_case '{}' 200 failed
check "d: non-zero exit" "$([ "$RC" -ne 0 ] && echo 1 || echo 0)"
check "d: empty stdout" "$([ -z "$OUT" ] && echo 1 || echo 0)"

# (e) /me unreachable -> re-authenticate
run_case "$CACHED" 000 completed
check "e: returns new token" "$([ "$OUT" = "new.tok.en" ] && [ "$RC" -eq 0 ] && echo 1 || echo 0)"
check "e: calls /flows/start" "$(called 'POST .*/flows/start')"
check "e: logs HTTP 000" "$(case "$ERR" in *"(HTTP 000)"*) echo 1 ;; *) echo 0 ;; esac)"

# 5xx also invalid
run_case "$CACHED" 503 completed
check "f: 503 -> re-authenticates" "$([ "$OUT" = "new.tok.en" ] && echo 1 || echo 0)"

echo "Summary: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
