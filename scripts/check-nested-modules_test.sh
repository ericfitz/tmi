#!/usr/bin/env bash
# Self-test for check-nested-modules.sh against throwaway git repos holding a
# root module and a nested module that replaces it, like test/integration.
# Runs offline (GOPROXY=off). Usage: bash scripts/check-nested-modules_test.sh
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/check-nested-modules.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GOPROXY=off GOFLAGS=-mod=mod GOWORK=off GOTOOLCHAIN=local
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

gover=$(go env GOVERSION | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/')

# fixture <dir>: git repo with root module example.com/root and a tidy nested
# module sub/ that imports it through a replace directive.
fixture() {
  local d=$1
  mkdir -p "$d/lib" "$d/sub"
  printf 'module example.com/root\n\ngo %s\n' "$gover" >"$d/go.mod"
  printf 'package lib\n\nfunc Answer() int { return 42 }\n' >"$d/lib/lib.go"
  printf 'module example.com/sub\n\ngo %s\n\nreplace example.com/root => ../\n' "$gover" >"$d/sub/go.mod"
  printf 'package main\n\nimport (\n\t"fmt"\n\n\t"example.com/root/lib"\n)\n\nfunc main() { fmt.Println(lib.Answer()) }\n' >"$d/sub/main.go"
  (cd "$d/sub" && go mod tidy >/dev/null 2>&1)
  git -C "$d" init -q
  git -C "$d" add -A
}

pass=0 fail=0
# check <name> <dir> <want-exit> <want-substring> [<forbidden-substring>]
check() {
  local name=$1 dir=$2 want_exit=$3 want=$4 forbid=${5:-}
  local out got_exit=0
  out=$(bash "$script" "$dir" 2>&1) || got_exit=$?
  if [ "$got_exit" = "$want_exit" ] && [[ $out == *"$want"* ]] \
    && { [ -z "$forbid" ] || [[ $out != *"$forbid"* ]]; }; then
    pass=$((pass + 1)); echo "ok   $name"
  else
    fail=$((fail + 1)); echo "FAIL $name: exit $got_exit (want $want_exit); output:"; while IFS= read -r l; do echo "       $l"; done <<<"$out"
  fi
}

fixture "$work/clean"
check "clean nested module passes" "$work/clean" 0 "ok   sub"

fixture "$work/stale"
# Drop the require tidy added: the shape of a root bump the nested go.mod missed.
printf 'module example.com/sub\n\ngo %s\n\nreplace example.com/root => ../\n' "$gover" >"$work/stale/sub/go.mod"
check "stale go.mod fails as not tidy" "$work/stale" 1 "FAIL sub: not tidy"

fixture "$work/broken"
printf 'package main\n\nfunc broken( {\n' >"$work/broken/sub/broken.go"
git -C "$work/broken" add -A
check "build error fails" "$work/broken" 1 "build fails"

fixture "$work/vet"
printf 'package main\n\nimport "fmt"\n\nfunc init() { fmt.Printf("%%d\\n", "not a number") }\n' >"$work/vet/sub/vet.go"
git -C "$work/vet" add -A
check "vet-only failure fails without build failure" "$work/vet" 1 "FAIL sub: vet fails" "build fails"

fixture "$work/none"
git -C "$work/none" rm -rq --cached sub
rm -rf "$work/none/sub"
check "no nested modules passes" "$work/none" 0 "no nested modules"

fixture "$work/untracked"
mkdir -p "$work/untracked/scratch"
printf 'module example.com/scratch\n\ngo %s\n\nrequire example.com/missing v1.0.0\n' "$gover" >"$work/untracked/scratch/go.mod"
check "untracked go.mod is ignored" "$work/untracked" 0 "ok   sub" "scratch"

fixture "$work/two"
cp -R "$work/two/sub" "$work/two/sub2"
printf 'module example.com/sub2\n\ngo %s\n\nreplace example.com/root => ../\n' "$gover" >"$work/two/sub2/go.mod"
git -C "$work/two" add -A
check "names only the failing module" "$work/two" 1 "need attention: sub2" "FAIL sub:"

mkdir -p "$work/nomod"
check "rejects a dir without go.mod" "$work/nomod" 2 "has no go.mod"

mkdir -p "$work/nogit"
printf 'module example.com/root\n\ngo %s\n' "$gover" >"$work/nogit/go.mod"
check "rejects a dir outside git" "$work/nogit" 2 "not a git work tree"

echo "check-nested-modules self-test: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
