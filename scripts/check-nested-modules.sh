#!/usr/bin/env bash
# Fail when a nested Go module (any tracked go.mod other than the repo root's)
# is not tidy, does not build, or does not vet.
#
# test/integration and wstest replace github.com/ericfitz/tmi with the repo
# root, so a root dependency bump leaves their go.mod stale ("go: updates to
# go.mod needed") and nothing else in CI notices (#1212). Run from CI's Build
# Verification job and from `make verify`.
#
# Usage: bash scripts/check-nested-modules.sh [repo-root]
# Exit: 0 all nested modules clean (or none), 1 one or more failed, 2 usage or
# environment error.
set -euo pipefail

root=${1:-$(cd "$(dirname "$0")/.." && pwd)}
if [ ! -f "$root/go.mod" ]; then
  echo "check-nested-modules: $root has no go.mod (pass the repo root)" >&2
  exit 2
fi
command -v go >/dev/null || { echo "check-nested-modules: go not on PATH" >&2; exit 2; }
git -C "$root" rev-parse --git-dir >/dev/null 2>&1 \
  || { echo "check-nested-modules: $root is not a git work tree" >&2; exit 2; }

modules=()
while IFS= read -r f; do
  [ "$f" = "go.mod" ] && continue
  modules+=("$(dirname "$f")")
done < <(git -C "$root" ls-files -- 'go.mod' '*/go.mod')

if [ "${#modules[@]}" -eq 0 ]; then
  echo "check-nested-modules: no nested modules"
  exit 0
fi

failed=()
for m in "${modules[@]}"; do
  dir="$root/$m"
  problems=()
  # GOWORK=off: judge each module on its own go.mod, as CI and contributors do.
  if ! (cd "$dir" && GOWORK=off go mod tidy -diff >/dev/null 2>&1); then
    problems+=("not tidy")
  fi
  if ! (cd "$dir" && GOWORK=off go build ./... >/dev/null 2>&1); then
    problems+=("build fails")
  fi
  if ! (cd "$dir" && GOWORK=off go vet ./... >/dev/null 2>&1); then
    problems+=("vet fails")
  fi
  if [ "${#problems[@]}" -eq 0 ]; then
    echo "ok   $m"
  else
    joined=$(IFS=,; echo "${problems[*]}")
    echo "FAIL $m: ${joined//,/, }"
    failed+=("$m")
  fi
done

if [ "${#failed[@]}" -gt 0 ]; then
  echo
  echo "check-nested-modules: ${#failed[@]} nested module(s) need attention: ${failed[*]}"
  echo "Fix: run 'go mod tidy' in each, then rerun this check to see any remaining build/vet errors."
  exit 1
fi
echo "check-nested-modules: all ${#modules[@]} nested module(s) clean"
