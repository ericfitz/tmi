#!/usr/bin/env bash
# Self-test for ci-go-mod-download.sh using a fake `go` that fails a set number
# of times before succeeding. Usage: bash scripts/ci-go-mod-download_test.sh
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/ci-go-mod-download.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Fake go: fails while the counter file holds a positive number, logs each call.
cat >"$work/go" <<'EOF'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_GO_DIR/calls"
n=$(cat "$FAKE_GO_DIR/fails")
if [ "$n" -gt 0 ]; then echo $((n - 1)) >"$FAKE_GO_DIR/fails"; exit 1; fi
EOF
chmod +x "$work/go"

pass=0 fail=0
# check <name> <fails-before-success> <expected-exit> <expected-calls> [args...]
check() {
  local name=$1 fails=$2 want_exit=$3 want_calls=$4; shift 4
  echo "$fails" >"$work/fails"; : >"$work/calls"
  local got_exit=0
  PATH="$work:$PATH" FAKE_GO_DIR="$work" bash "$script" "$@" >/dev/null 2>&1 || got_exit=$?
  local got_calls; got_calls=$(wc -l <"$work/calls" | tr -d ' ')
  if [ "$got_exit" = "$want_exit" ] && [ "$got_calls" = "$want_calls" ]; then
    pass=$((pass + 1)); echo "ok   $name"
  else
    fail=$((fail + 1)); echo "FAIL $name: exit $got_exit (want $want_exit), calls $got_calls (want $want_calls)"
  fi
}

check "succeeds first try"           0 0 1 3 0
check "retries then succeeds"        2 0 3 3 0
check "gives up after attempts"      5 1 3 3 0
check "single attempt, no retry"     1 1 1 1 0
check "calls go mod download"        0 0 1 1 0
check "rejects non-numeric attempts" 0 2 0 abc 0
check "rejects non-numeric delay"    0 2 0 3 -1
check "rejects zero attempts"        0 2 0 0 0

# Verify the subcommand passed to go.
echo 0 >"$work/fails"; : >"$work/calls"
PATH="$work:$PATH" FAKE_GO_DIR="$work" bash "$script" 1 0 >/dev/null 2>&1
if [ "$(cat "$work/calls")" = "mod download" ]; then pass=$((pass + 1)); echo "ok   runs 'go mod download'"
else fail=$((fail + 1)); echo "FAIL ran 'go $(cat "$work/calls")'"; fi

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
