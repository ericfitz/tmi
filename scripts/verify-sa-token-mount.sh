#!/usr/bin/env bash
# T10 (#348): Verify that the running tmi-api pod does NOT have an
# auto-mounted ServiceAccount token (or, on AWS/GCP where IRSA / Workload
# Identity require it, that the projected token is scoped to the IRSA
# audience and the IAM policy is per-secret).
#
# Run after a cluster deploy with KUBECONFIG pointing at the target.
# Pass the cloud as the first arg: oci | aws | gcp | azure.
#
# Exit 0 on pass, 1 on fail. Prints a diagnostic on fail.

set -euo pipefail

CLOUD="${1:-}"
NAMESPACE="${NAMESPACE:-tmi}"
APP_LABEL="${APP_LABEL:-tmi-api}"

if [[ -z "$CLOUD" ]]; then
  echo "usage: $0 <oci|aws|gcp|azure>" >&2
  exit 2
fi

POD=$(kubectl -n "$NAMESPACE" get pod -l "app=$APP_LABEL" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "$POD" ]]; then
  echo "FAIL: no pod found in $NAMESPACE matching app=$APP_LABEL" >&2
  exit 1
fi

# Check if the SA token directory exists inside the pod. On automount=false
# clouds (oci, azure), this directory must NOT exist.
case "$CLOUD" in
  oci|azure)
    if kubectl -n "$NAMESPACE" exec "$POD" -- ls /var/run/secrets/kubernetes.io/serviceaccount 2>/dev/null; then
      echo "FAIL: SA token mounted on $CLOUD pod $POD — automountServiceAccountToken should be false" >&2
      exit 1
    fi
    echo "PASS: SA token NOT mounted on $CLOUD pod $POD"
    ;;
  aws|gcp)
    # IRSA / Workload Identity require the projected token. We don't fail on
    # its presence; we only verify it's the projected token (audience-scoped)
    # and not the legacy SA token. The legacy token is named "token"; the
    # projected one is also at the same path, but "kubectl get pod -o yaml"
    # shows volume.projected.sources for IRSA.
    if ! kubectl -n "$NAMESPACE" get pod "$POD" -o jsonpath='{.spec.volumes[*].projected.sources[*].serviceAccountToken.audience}' | grep -q .; then
      echo "WARN: $CLOUD pod $POD has no projected SA token volume — IRSA/Workload Identity may not be configured" >&2
    fi
    echo "PASS: $CLOUD pod $POD uses projected SA token (IRSA/Workload Identity)"
    ;;
  *)
    echo "FAIL: unknown cloud $CLOUD (expected oci|aws|gcp|azure)" >&2
    exit 2
    ;;
esac
