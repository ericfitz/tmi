#!/bin/bash
# ci-version-bump.sh - Version computation and application for the PR-based
# automatic semantic versioning workflow (see .github/workflows/version-bump.yml
# and issue #627).
#
# The old post-commit-hook design amended the commit it just observed on
# `main`. That is unreachable under a PR-only branch-protection ruleset: every
# commit is authored on a `fix/*`/`dev/*` branch, and the commit that lands on
# `main` is GitHub's server-side squash-merge, where no local hook runs.
#
# The replacement computes the bump from the PR TITLE (the squash-merge commit
# subject) rather than from individual branch commits, and pushes the bump
# commit to the PR's own branch so squash-merge lands it as part of the same
# change. This script is the pure/deterministic core so both the CI workflow
# and a human can compute/apply the same way and self-test it offline.
#
# Usage:
#   ./ci-version-bump.sh compute-version "<PR title>" [version-file]
#       Prints "MAJOR.MINOR.PATCH" for the NEXT version, derived from the
#       conventional-commit type in <PR title> applied to the version in
#       [version-file] (default: .version). Does not modify anything.
#
#   ./ci-version-bump.sh apply-version <MAJOR.MINOR.PATCH>
#       Writes the given version into .version, api/version.go, and
#       api-schema/tmi-openapi.json (info.version). Does not run
#       `make generate-api` or `make build-server` -- the caller (the CI
#       workflow) does that afterward so it can also install oapi-codegen.
#
#   ./ci-version-bump.sh embedded-spec-version [api.go path]
#       Prints the `info.version` baked into the generated api.go's embedded
#       OpenAPI spec (the `swaggerSpec` base64+raw-deflate blob), by decoding
#       it directly -- no build or server start required. Default path:
#       api/api.go. This is what api.GetSwagger() actually serves at
#       runtime, so it is the ground truth that api-schema/tmi-openapi.json's
#       info.version reached the generated code (requires `make generate-api`
#       to have been re-run after editing the spec).
#
#   ./ci-version-bump.sh self-test
#       Runs the computation against a scratch .version with a handful of
#       synthetic PR titles and asserts the expected bump. Exits non-zero on
#       any mismatch. No files in the working tree are touched.
#
# Bump rule (same family as the regex historically used by --commit in
# update-version.sh:69, applied to the PR title instead of a commit message):
#   ^feat(\(.+\))?(!)?:   -> MINOR + 1, PATCH = 0
#   anything else         -> PATCH + 1
#
# update-version.sh is left in place and keeps working for direct/manual
# invocation (e.g. a maintainer bumping by hand); this script is CI's entry
# point and is title-driven rather than last-commit-driven.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION_FILE_DEFAULT=".version"
VERSION_GO_FILE="api/version.go"
OPENAPI_FILE="api-schema/tmi-openapi.json"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1" >&2; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

# Compute MAJOR.MINOR.PATCH for the next version given a PR title and a
# starting MAJOR/MINOR/PATCH triple. Pure function: prints, does not touch
# any file or global state.
bump_version() {
    local title="$1" major="$2" minor="$3" patch="$4"
    if echo "$title" | grep -qE '^feat(\(.+\))?(!)?:'; then
        minor=$((minor + 1))
        patch=0
    else
        patch=$((patch + 1))
    fi
    echo "${major}.${minor}.${patch}"
}

cmd_compute_version() {
    local title="${1:-}"
    local version_file="${2:-$VERSION_FILE_DEFAULT}"

    if [ -z "$title" ]; then
        log_error "compute-version requires a PR title argument"
        exit 1
    fi
    if [ ! -f "$version_file" ]; then
        log_error "Version file $version_file not found"
        exit 1
    fi

    local major minor patch
    major=$(jq -r '.major' "$version_file")
    minor=$(jq -r '.minor' "$version_file")
    patch=$(jq -r '.patch' "$version_file")

    bump_version "$title" "$major" "$minor" "$patch"
}

cmd_apply_version() {
    local new_version="${1:-}"
    if [ -z "$new_version" ]; then
        log_error "apply-version requires a MAJOR.MINOR.PATCH argument"
        exit 1
    fi
    if ! [[ "$new_version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        log_error "apply-version argument must be MAJOR.MINOR.PATCH, got: $new_version"
        exit 1
    fi
    local major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"

    cd "$REPO_ROOT"

    # .version (preserve prerelease, same shape as update-version.sh)
    local prerelease=""
    if [ -f "$VERSION_FILE_DEFAULT" ]; then
        prerelease=$(jq -r '.prerelease // ""' "$VERSION_FILE_DEFAULT")
    fi
    cat >"$VERSION_FILE_DEFAULT" <<EOF
{
  "major": $major,
  "minor": $minor,
  "patch": $patch,
  "prerelease": "$prerelease"
}
EOF
    log_success "Updated $VERSION_FILE_DEFAULT -> $new_version"

    # api/version.go (same sed block as update-version.sh)
    if [ -f "$VERSION_GO_FILE" ]; then
        sed -i.bak "s/VersionMajor = \"[0-9]*\"/VersionMajor = \"$major\"/" "$VERSION_GO_FILE"
        sed -i.bak "s/VersionMinor = \"[0-9]*\"/VersionMinor = \"$minor\"/" "$VERSION_GO_FILE"
        sed -i.bak "s/VersionPatch = \"[0-9]*\"/VersionPatch = \"$patch\"/" "$VERSION_GO_FILE"
        sed -i.bak "s/VersionPreRelease = \"[^\"]*\"/VersionPreRelease = \"$prerelease\"/" "$VERSION_GO_FILE"
        rm -f "${VERSION_GO_FILE}.bak"
        log_success "Updated $VERSION_GO_FILE -> $new_version"
    else
        log_error "$VERSION_GO_FILE not found"
        exit 1
    fi

    # api-schema/tmi-openapi.json info.version
    if [ -f "$OPENAPI_FILE" ]; then
        local tmp
        tmp=$(mktemp)
        jq --arg v "$new_version" '.info.version = $v' "$OPENAPI_FILE" >"$tmp"
        mv "$tmp" "$OPENAPI_FILE"
        log_success "Updated $OPENAPI_FILE info.version -> $new_version"
    else
        log_error "$OPENAPI_FILE not found"
        exit 1
    fi
}

cmd_embedded_spec_version() {
    local api_go="${1:-api/api.go}"
    if [ ! -f "$api_go" ]; then
        log_error "$api_go not found"
        exit 1
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        log_error "python3 is required to decode the embedded spec"
        exit 1
    fi

    local b64
    b64=$(awk '/^var swaggerSpec = \[\]string\{/{flag=1; next} /^\}/{if(flag) exit} flag' "$api_go" \
        | grep -oE '"[A-Za-z0-9+/=]*"' | tr -d '"\n')

    if [ -z "$b64" ]; then
        log_error "Could not find/extract the swaggerSpec literal in $api_go"
        exit 1
    fi

    # The blob goes in on stdin, never as an argv element: Linux caps a
    # single argument at MAX_ARG_STRLEN (128 KiB) and the embedded spec is
    # several times that, so `python3 -c '...' "$b64"` fails to exec at all
    # with "Argument list too long" (exit 126). macOS has no such per-argument
    # cap, which is why this only shows up in CI.
    printf '%s' "$b64" | python3 -c '
import base64, json, sys, zlib
raw = base64.b64decode(sys.stdin.read())
data = zlib.decompress(raw, -15)
spec = json.loads(data)
print(spec["info"]["version"])
'
}

cmd_self_test() {
    local failures=0
    local tmpdir
    tmpdir=$(mktemp -d)

    local scratch="$tmpdir/.version"
    cat >"$scratch" <<'EOF'
{
  "major": 1,
  "minor": 8,
  "patch": 16,
  "prerelease": ""
}
EOF

    assert_bump() {
        local title="$1" expected="$2"
        local got
        got=$(cmd_compute_version "$title" "$scratch")
        if [ "$got" = "$expected" ]; then
            echo "PASS: '$title' -> $got"
        else
            echo "FAIL: '$title' -> $got (expected $expected)"
            failures=$((failures + 1))
        fi
    }

    assert_bump "feat: add widget export" "1.9.0"
    assert_bump "feat(scope)!: breaking widget rewrite" "1.9.0"
    assert_bump "fix: correct widget off-by-one" "1.8.17"
    assert_bump "chore(deps): bump golang.org/x/net" "1.8.17"
    assert_bump "feat(api): add pagination cursor" "1.9.0"
    assert_bump "docs: update readme" "1.8.17"
    assert_bump "refactor(auth): simplify token check" "1.8.17"

    rm -rf "$tmpdir"

    if [ "$failures" -eq 0 ]; then
        log_success "self-test: all cases passed"
    else
        log_error "self-test: $failures case(s) failed"
        exit 1
    fi
}

main() {
    local cmd="${1:-}"
    shift || true
    case "$cmd" in
    compute-version)
        cmd_compute_version "$@"
        ;;
    apply-version)
        cmd_apply_version "$@"
        ;;
    embedded-spec-version)
        cmd_embedded_spec_version "$@"
        ;;
    self-test)
        cmd_self_test "$@"
        ;;
    *)
        log_error "Unknown or missing command: '$cmd'"
        echo "Usage:"
        echo "  $0 compute-version \"<PR title>\" [version-file]"
        echo "  $0 apply-version <MAJOR.MINOR.PATCH>"
        echo "  $0 embedded-spec-version [api.go path]"
        echo "  $0 self-test"
        exit 1
        ;;
    esac
}

main "$@"
