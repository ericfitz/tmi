#!/usr/bin/env bash
# Download Go modules, retrying transient module-proxy failures (for example an
# HTTP/2 "stream error: INTERNAL_ERROR" from proxy.golang.org on a cold cache).
# Usage: ci-go-mod-download.sh [attempts] [delay-seconds]   (defaults: 3 10)
# The delay grows linearly: delay, 2*delay, ...
set -euo pipefail
attempts=${1:-3}
delay=${2:-10}
for v in "$attempts" "$delay"; do
  [[ "$v" =~ ^[0-9]+$ ]] || { echo "ci-go-mod-download: '$v' is not a non-negative integer" >&2; exit 2; }
done
[ "$attempts" -ge 1 ] || { echo "ci-go-mod-download: attempts must be at least 1" >&2; exit 2; }

for ((i = 1; i <= attempts; i++)); do
  if go mod download; then
    exit 0
  fi
  if [ "$i" -lt "$attempts" ]; then
    echo "ci-go-mod-download: attempt $i/$attempts failed; retrying in $((delay * i))s" >&2
    sleep $((delay * i))
  fi
done
echo "ci-go-mod-download: go mod download failed after $attempts attempts" >&2
exit 1
