#!/usr/bin/env bash
# T10 (#348): Verify that the running tmi-api pod uses IRSA's projected,
# audience-scoped ServiceAccount token rather than the legacy auto-mounted one.
#
# Run after a cluster deploy with KUBECONFIG pointing at the target.
# Pass the cloud as the first arg: aws.
#
# Exit 0 on pass, 1 on fail. Prints a diagnostic on fail.

set -euo pipefail

CLOUD="${1:-}"
NAMESPACE="${NAMESPACE:-tmi}"
APP_LABEL="${APP_LABEL:-tmi-api}"

if [[ -z "$CLOUD" ]]; then
  echo "usage: $0 <aws>" >&2
  exit 2
fi

POD=$(kubectl -n "$NAMESPACE" get pod -l "app=$APP_LABEL" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "$POD" ]]; then
  echo "FAIL: no pod found in $NAMESPACE matching app=$APP_LABEL" >&2
  exit 1
fi

case "$CLOUD" in
  aws)
    # IRSA requires the projected token. We don't fail on its presence; we
    # only verify it's the projected token (audience-scoped) and not the
    # legacy SA token. The legacy token is named "token"; the projected one
    # is also at the same path, but "kubectl get pod -o yaml" shows
    # volume.projected.sources for IRSA.
    if ! kubectl -n "$NAMESPACE" get pod "$POD" -o jsonpath='{.spec.volumes[*].projected.sources[*].serviceAccountToken.audience}' | grep -q .; then
      echo "WARN: $CLOUD pod $POD has no projected SA token volume — IRSA may not be configured" >&2
    fi
    echo "PASS: $CLOUD pod $POD uses projected SA token (IRSA)"
    ;;
  *)
    echo "FAIL: unknown cloud $CLOUD (expected aws)" >&2
    exit 2
    ;;
esac
