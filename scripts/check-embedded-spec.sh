#!/bin/bash
# check-embedded-spec.sh - fail if the OpenAPI spec embedded in api/api.go
# disagrees with api-schema/tmi-openapi.json's info.version.
#
# version-bump-bot runs this as the schema stream's `verify` command in the PR
# guard (.github/version-bump.toml). It decodes the generated `swaggerSpec`
# literal (base64 of raw-deflate JSON) directly, so it needs only bash, awk,
# grep and python3: no Go toolchain, build, or server start. A mismatch means
# someone edited the spec without re-running `make generate-api`.
#
# Usage: scripts/check-embedded-spec.sh [api.go] [spec.json]

set -euo pipefail

api_go="${1:-api/api.go}"
spec="${2:-api-schema/tmi-openapi.json}"

for f in "$api_go" "$spec"; do
    if [ ! -f "$f" ]; then
        echo "check-embedded-spec: $f not found" >&2
        exit 1
    fi
done

b64=$(awk '/^var swaggerSpec = \[\]string\{/{flag=1; next} /^\}/{if(flag) exit} flag' "$api_go" \
    | grep -oE '"[A-Za-z0-9+/=]*"' | tr -d '"\n')
if [ -z "$b64" ]; then
    echo "check-embedded-spec: could not extract the swaggerSpec literal from $api_go" >&2
    exit 1
fi

# The blob goes in on stdin, never as an argv element: Linux caps a single
# argument at MAX_ARG_STRLEN (128 KiB) and the embedded spec is several times
# that, so passing it as an argument fails to exec in CI ("Argument list too
# long"). macOS has no such cap, so it would pass locally.
printf '%s' "$b64" | python3 -c '
import base64, json, sys, zlib
embedded = json.loads(zlib.decompress(base64.b64decode(sys.stdin.read()), -15))["info"]["version"]
with open(sys.argv[1]) as f:
    expected = json.load(f)["info"]["version"]
if embedded != expected:
    sys.exit(f"check-embedded-spec: {sys.argv[2]} embeds info.version {embedded}, but {sys.argv[1]} has {expected}; run make generate-api")
print(f"check-embedded-spec: embedded spec version {embedded} matches")
' "$spec" "$api_go"
