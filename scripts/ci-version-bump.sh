#!/bin/bash
# ci-version-bump.sh - Version computation and application for the automatic
# semantic versioning workflow (see .github/workflows/version-bump.yml, issue
# #627, and docs/superpowers/specs/2026-10-02-adr-post-merge-version-bump.md).
#
# CURRENT DESIGN (human decision, Eric 2026-10-02): PRs never touch version
# files. After each merge, a workflow on `main` runs `plan-pending` to fold
# every merged-but-unbumped commit into one bump and pushes a single
# chore(version) commit. The history below explains the earlier designs.
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
#       Writes the given version into .version and api/version.go (the
#       SERVER version only -- see decoupling note below). Does not touch
#       the OpenAPI spec or run `make build-server`.
#
#   ./ci-version-bump.sh is-docs-only [file ...]
#       Reads a list of changed file paths (one per line on stdin, or as
#       positional args) and prints "true" if every path matches docs/**,
#       PROGRESS.md, or *.md anywhere, "false" otherwise. An empty list
#       prints "true" (nothing non-doc changed). Pure: prints only.
#
#   ./ci-version-bump.sh compute-schema-version "<PR title>" <base-spec> <head-spec>
#       Prints the OpenAPI schema's own MAJOR.MINOR.PATCH. Compares
#       <base-spec> and <head-spec> with `.info.version` removed from both:
#       if they're otherwise identical, the schema didn't change and the
#       BASE spec's info.version is printed unchanged. If they differ, the
#       PR title's conventional-commit type bumps the base version: a
#       breaking marker (`^[a-z]+(\(.+\))?!:`) -> MAJOR + 1 (MINOR/PATCH
#       reset), `feat:` -> MINOR + 1 (PATCH reset), anything else -> PATCH + 1.
#
#   ./ci-version-bump.sh apply-schema-version <MAJOR.MINOR.PATCH>
#       Writes the given version into api-schema/tmi-openapi.json's
#       info.version only. Does not run `make generate-api` -- the caller
#       does that afterward so it can also install oapi-codegen.
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
#   ./ci-version-bump.sh plan-pending [ref]
#       Run inside the repo. Finds the last first-parent commit on [ref]
#       (default HEAD) that touched .version, then folds every later
#       first-parent commit into the bump, oldest first: docs-only commits
#       are skipped; each other commit bumps the server version per its
#       subject (the squash-merge subject is the PR title); the schema
#       version bumps per the same subject only when that commit changed the
#       spec (diff vs its first parent, info.version excluded). Prints
#       key=value lines: pending=<n>, server=<X.Y.Z>, schema=<X.Y.Z>,
#       schema_changed=<true|false>, base=<sha>. pending=0 means nothing to
#       do, which is what a run triggered by its own bump commit sees (that
#       commit touched .version), so the bump can never cascade.
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
#
# Decoupling (human decision, Eric 2026-09-28; see
# docs/superpowers/specs/2026-09-28-adr-versioning-docs-skip-and-schema-decoupling.md):
# the SERVER version (.version, api/version.go) bumps on every non-docs-only
# PR per the rule above. The OpenAPI SCHEMA version (info.version) is a
# separate value that only moves when the PR actually changes the schema
# (diff excluding info.version itself); it then uses its own bump rule,
# which additionally recognizes a `!` breaking marker as a MAJOR bump. A
# docs-only PR (every changed file under docs/**, PROGRESS.md, or *.md)
# bumps neither.

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

# Apply a PR title's schema-bump rule to MAJOR.MINOR.PATCH: a breaking marker
# -> MAJOR, feat -> MINOR, anything else -> PATCH. Pure: prints only.
bump_schema_version() {
    local title="$1" version="$2" major minor patch
    IFS='.' read -r major minor patch <<<"$version"
    if echo "$title" | grep -qE '^[a-z]+(\(.+\))?!:'; then
        major=$((major + 1))
        minor=0
        patch=0
    elif echo "$title" | grep -qE '^feat(\(.+\))?(!)?:'; then
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
}

# Decide whether a list of changed file paths is docs-only (see usage doc
# above). Reads stdin one path per line when no positional args are given.
# Pure: prints "true"/"false", touches nothing.
cmd_is_docs_only() {
    local -a files=()
    if [ "$#" -gt 0 ]; then
        files=("$@")
    else
        local line
        while IFS= read -r line; do
            [ -n "$line" ] && files+=("$line")
        done
    fi

    local f
    for f in "${files[@]}"; do
        case "$f" in
        docs/* | *.md) ;;
        *)
            echo "false"
            return 0
            ;;
        esac
    done
    echo "true"
}

cmd_compute_schema_version() {
    local title="${1:-}" base_spec="${2:-}" head_spec="${3:-}"
    if [ -z "$title" ] || [ -z "$base_spec" ] || [ -z "$head_spec" ]; then
        log_error "compute-schema-version requires <PR title> <base-spec> <head-spec>"
        exit 1
    fi
    if [ ! -f "$base_spec" ] || [ ! -f "$head_spec" ]; then
        log_error "compute-schema-version: spec file(s) not found"
        exit 1
    fi

    local base_version
    base_version=$(jq -r '.info.version' "$base_spec")

    local base_norm head_norm
    base_norm=$(jq -S 'del(.info.version)' "$base_spec")
    head_norm=$(jq -S 'del(.info.version)' "$head_spec")

    if [ "$base_norm" = "$head_norm" ]; then
        # Schema unchanged: the version continues from base, untouched.
        echo "$base_version"
        return 0
    fi

    bump_schema_version "$title" "$base_version"
}

# Fold every merged-but-unbumped first-parent commit into one bump (see usage).
# Reads git in the current directory; writes nothing.
cmd_plan_pending() {
    local ref="${1:-HEAD}"
    local base
    base=$(git log -1 --first-parent --format=%H "$ref" -- "$VERSION_FILE_DEFAULT")
    if [ -z "$base" ]; then
        log_error "plan-pending: no commit on $ref ever touched $VERSION_FILE_DEFAULT"
        exit 1
    fi

    # Commits after base don't touch .version, and Version Check forbids PRs
    # from changing info.version, so both values at ref are the last bumped ones.
    local server schema major minor patch
    server=$(git show "$ref:$VERSION_FILE_DEFAULT" | jq -r '"\(.major).\(.minor).\(.patch)"')
    schema=$(git show "$ref:$OPENAPI_FILE" | jq -r '.info.version')

    local pending=0 schema_changed=false c subject docs_only
    for c in $(git rev-list --first-parent --reverse "$base..$ref"); do
        subject=$(git log -1 --format=%s "$c")
        docs_only=$(git diff --name-only "$c^1" "$c" | cmd_is_docs_only)
        if [ "$docs_only" = "true" ]; then
            log_info "skip (docs-only) $c $subject"
            continue
        fi
        pending=$((pending + 1))
        IFS='.' read -r major minor patch <<<"$server"
        server=$(bump_version "$subject" "$major" "$minor" "$patch")
        if ! diff -q <(git show "$c^1:$OPENAPI_FILE" 2>/dev/null | jq -S 'del(.info.version)') \
            <(git show "$c:$OPENAPI_FILE" 2>/dev/null | jq -S 'del(.info.version)') >/dev/null; then
            schema=$(bump_schema_version "$subject" "$schema")
            schema_changed=true
        fi
        log_info "bump $c $subject -> server $server, schema $schema"
    done

    echo "pending=$pending"
    echo "server=$server"
    echo "schema=$schema"
    echo "schema_changed=$schema_changed"
    echo "base=$base"
}

# Fail when a PR edits version state that only the post-merge bump may write.
# Args: <merge-base> <head>. Reads git in the current directory; prints the
# offending paths and exits 1, or prints nothing and exits 0.
cmd_check_pr_untouched() {
    local mb="${1:-}" head="${2:-HEAD}" bad=()
    if [ -z "$mb" ]; then
        log_error "check-pr-untouched requires <merge-base> [head]"
        exit 1
    fi
    git diff --quiet "$mb" "$head" -- "$VERSION_FILE_DEFAULT" || bad+=("$VERSION_FILE_DEFAULT")
    git diff --quiet "$mb" "$head" -- "$VERSION_GO_FILE" || bad+=("$VERSION_GO_FILE")
    if [ "$(git show "$mb:$OPENAPI_FILE" | jq -r '.info.version')" != "$(git show "$head:$OPENAPI_FILE" | jq -r '.info.version')" ]; then
        bad+=("$OPENAPI_FILE info.version")
    fi
    if [ "${#bad[@]}" -gt 0 ]; then
        printf '%s\n' "${bad[@]}"
        return 1
    fi
}

cmd_apply_schema_version() {
    local new_version="${1:-}"
    if [ -z "$new_version" ]; then
        log_error "apply-schema-version requires a MAJOR.MINOR.PATCH argument"
        exit 1
    fi
    if ! [[ "$new_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log_error "apply-schema-version argument must be MAJOR.MINOR.PATCH, got: $new_version"
        exit 1
    fi

    cd "$REPO_ROOT"

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

    assert_docs_only() {
        local expected="$1"
        shift
        local got
        got=$(cmd_is_docs_only "$@")
        if [ "$got" = "$expected" ]; then
            echo "PASS: is-docs-only($*) -> $got"
        else
            echo "FAIL: is-docs-only($*) -> $got (expected $expected)"
            failures=$((failures + 1))
        fi
    }

    assert_docs_only "true" "docs/foo.md" "PROGRESS.md" "README.md"
    assert_docs_only "true" "docs/superpowers/specs/2026-01-01-x.md"
    assert_docs_only "false" "docs/foo.md" "api/version.go"
    assert_docs_only "false" "api-schema/tmi-openapi.json"
    assert_docs_only "true"

    local spec_base="$tmpdir/base-spec.json"
    local spec_head_same="$tmpdir/head-spec-same.json"
    local spec_head_changed="$tmpdir/head-spec-changed.json"
    cat >"$spec_base" <<'EOF'
{"info": {"version": "2.3.1", "title": "x"}, "paths": {"/a": {}}}
EOF
    cat >"$spec_head_same" <<'EOF'
{"info": {"version": "9.9.9", "title": "x"}, "paths": {"/a": {}}}
EOF
    cat >"$spec_head_changed" <<'EOF'
{"info": {"version": "9.9.9", "title": "x"}, "paths": {"/a": {}, "/b": {}}}
EOF

    assert_schema() {
        local title="$1" base="$2" head="$3" expected="$4"
        local got
        got=$(cmd_compute_schema_version "$title" "$base" "$head")
        if [ "$got" = "$expected" ]; then
            echo "PASS: schema '$title' -> $got"
        else
            echo "FAIL: schema '$title' -> $got (expected $expected)"
            failures=$((failures + 1))
        fi
    }

    assert_schema "fix: typo" "$spec_base" "$spec_head_same" "2.3.1"
    assert_schema "feat: add endpoint" "$spec_base" "$spec_head_changed" "2.4.0"
    assert_schema "feat(api)!: breaking rewrite" "$spec_base" "$spec_head_changed" "3.0.0"
    assert_schema "fix: patch bump" "$spec_base" "$spec_head_changed" "2.3.2"
    assert_schema "chore!: breaking chore" "$spec_base" "$spec_head_changed" "3.0.0"

    # plan-pending / check-pr-untouched against a scratch git repo.
    local repo="$tmpdir/repo"
    mkdir -p "$repo/api-schema" "$repo/docs" "$repo/api"
    (
        cd "$repo"
        git init -q -b main
        git config user.email t@example.invalid
        git config user.name t
        printf '{"major": 1, "minor": 8, "patch": 16, "prerelease": ""}\n' >.version
        printf '{"info": {"version": "2.3.1"}, "paths": {"/a": {}}}\n' >api-schema/tmi-openapi.json
        echo code >main.go
        git add -A && git commit -qm "chore(version): bump to 1.8.16"
    )
    assert_plan() {
        local label="$1" expected="$2" got
        got=$(cd "$repo" && cmd_plan_pending 2>/dev/null | tr '\n' ' ' | sed 's/ base=.*//')
        if [ "$got" = "$expected" ]; then
            echo "PASS: plan-pending $label -> $got"
        else
            echo "FAIL: plan-pending $label -> '$got' (expected '$expected')"
            failures=$((failures + 1))
        fi
    }
    assert_plan "right after a bump" "pending=0 server=1.8.16 schema=2.3.1 schema_changed=false"
    (cd "$repo" && echo x >>docs/a.md && git add -A && git commit -qm "docs: notes (#1)")
    assert_plan "docs-only only" "pending=0 server=1.8.16 schema=2.3.1 schema_changed=false"
    (cd "$repo" && echo y >>main.go && git add -A && git commit -qm "fix: one (#2)")
    (cd "$repo" && printf '{"info": {"version": "2.3.1"}, "paths": {"/a": {}, "/b": {}}}\n' >api-schema/tmi-openapi.json \
        && git add -A && git commit -qm "feat(api): add b (#3)")
    (cd "$repo" && echo z >>main.go && git add -A && git commit -qm "fix: two (#4)")
    assert_plan "fold of fix+feat(schema)+fix" "pending=3 server=1.9.1 schema=2.4.0 schema_changed=true"
    (
        cd "$repo"
        printf '{"major": 1, "minor": 9, "patch": 1, "prerelease": ""}\n' >.version
        jq '.info.version = "2.4.0"' api-schema/tmi-openapi.json >s.tmp && mv s.tmp api-schema/tmi-openapi.json
        git add -A && git commit -qm "chore(version): bump server to 1.9.1, schema to 2.4.0"
    )
    assert_plan "after the bump commit (no cascade)" "pending=0 server=1.9.1 schema=2.4.0 schema_changed=false"

    assert_untouched() {
        local label="$1" expected="$2" got
        got=$(cd "$repo" && cmd_check_pr_untouched HEAD~1 HEAD >/dev/null 2>&1 && echo ok || echo bad)
        if [ "$got" = "$expected" ]; then
            echo "PASS: check-pr-untouched $label -> $got"
        else
            echo "FAIL: check-pr-untouched $label -> $got (expected $expected)"
            failures=$((failures + 1))
        fi
    }
    (cd "$repo" && echo w >>main.go && git add -A && git commit -qm "fix: code only")
    assert_untouched "code-only PR" ok
    (cd "$repo" && printf '{"major": 9, "minor": 0, "patch": 0, "prerelease": ""}\n' >.version && git add -A && git commit -qm "fix: hand bump")
    assert_untouched "hand-edited .version" bad
    (cd "$repo" && jq '.info.version = "9.9.9"' api-schema/tmi-openapi.json >s.tmp && mv s.tmp api-schema/tmi-openapi.json && git add -A && git commit -qm "fix: hand schema bump")
    assert_untouched "hand-edited info.version" bad

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
    is-docs-only)
        cmd_is_docs_only "$@"
        ;;
    compute-schema-version)
        cmd_compute_schema_version "$@"
        ;;
    apply-schema-version)
        cmd_apply_schema_version "$@"
        ;;
    embedded-spec-version)
        cmd_embedded_spec_version "$@"
        ;;
    plan-pending)
        cmd_plan_pending "$@"
        ;;
    check-pr-untouched)
        cmd_check_pr_untouched "$@"
        ;;
    self-test)
        cmd_self_test "$@"
        ;;
    *)
        log_error "Unknown or missing command: '$cmd'"
        echo "Usage:"
        echo "  $0 compute-version \"<PR title>\" [version-file]"
        echo "  $0 apply-version <MAJOR.MINOR.PATCH>"
        echo "  $0 is-docs-only [file ...]   # or pipe paths on stdin"
        echo "  $0 compute-schema-version \"<PR title>\" <base-spec> <head-spec>"
        echo "  $0 apply-schema-version <MAJOR.MINOR.PATCH>"
        echo "  $0 embedded-spec-version [api.go path]"
        echo "  $0 plan-pending [ref]"
        echo "  $0 check-pr-untouched <merge-base> [head]"
        echo "  $0 self-test"
        exit 1
        ;;
    esac
}

main "$@"
