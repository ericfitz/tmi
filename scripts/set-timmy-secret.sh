#!/usr/bin/env bash
# Create/update the tmi-timmy Kubernetes Secret WITHOUT exposing the API key in
# argv, environment, or logs.
#
# Timmy needs an LLM key and a text-embedding key. Today both are the same
# OpenAI credential, but they are stored under separate keys so either can be
# rotated to a different provider without touching the other.
#
# Provide the key by placing it in a file (create this in YOUR OWN terminal, not
# through an assistant, so the value never enters a chat transcript):
#     (umask 077; printf '%s' 'sk-YOURKEY' > "$HOME/.tmi-timmy-key")
# then run:
#     scripts/set-timmy-secret.sh                     # AWS (tmi-eks), default
#     scripts/set-timmy-secret.sh --context k3s-rp    # local dev cluster
#     scripts/set-timmy-secret.sh /path/key           # explicit key file
#
# The key is read straight from the file by kubectl (--from-file), so it is
# never placed on a command line or in an environment variable. The key file is
# securely removed on success.
#
# The NON-secret half of Timmy's configuration (provider, model, base URL,
# embedding dimension) is not here — on AWS it lives in the Terraform-owned
# tmi-server-config ConfigMap, and for local dev in deployments/k8s/dev/.
# Two of those values are load-bearing and easy to get wrong:
#   * TMI_TIMMY_TEXT_EMBEDDING_MODEL must equal TMI_EMBEDDING_MODEL on the
#     tmi-chunk-embed worker, or documents are embedded at one dimension and
#     queried at another and retrieval silently returns nothing.
#   * TMI_TIMMY_EMBEDDING_DIMENSION must match what the model really returns
#     (text-embedding-3-large = 3072) and be > 0, or EmbeddingProfile.Validate
#     rejects every job envelope.
set -euo pipefail

NS=tmi-platform
CONTEXT=""
KEYFILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context) CONTEXT="$2"; shift 2 ;;
    -h|--help) sed -n '2,31p' "$0"; exit 0 ;;
    *)         KEYFILE="$1"; shift ;;
  esac
done
KEYFILE="${KEYFILE:-$HOME/.tmi-timmy-key}"

if [[ ! -s "$KEYFILE" ]]; then
  echo "ERROR: key file '$KEYFILE' not found or empty."
  echo
  echo "Create it privately in your own terminal (keeps the key out of any transcript):"
  echo "    (umask 077; printf '%s' 'sk-YOURKEY' > \"$KEYFILE\")"
  echo "then re-run:  $0 [--context CTX] [keyfile]"
  exit 1
fi

KCTL=(kubectl)
if [[ -n "$CONTEXT" ]]; then
  KCTL=(kubectl --context "$CONTEXT")
else
  # Default target is EKS; point kubeconfig at it (no-op if already set).
  export AWS_PROFILE="${AWS_PROFILE:-tmi}" AWS_REGION="${AWS_REGION:-us-east-1}"
  aws eks update-kubeconfig --name tmi-eks --region us-east-1 >/dev/null 2>&1 || true
fi

# Create-or-replace. --from-file reads the value directly from disk, so it never
# appears in argv or the environment. Both entries intentionally read the same
# file; see the header for why they are separate keys.
"${KCTL[@]}" -n "$NS" create secret generic tmi-timmy \
  --from-file=llm-api-key="$KEYFILE" \
  --from-file=text-embedding-api-key="$KEYFILE" \
  --dry-run=client -o yaml | "${KCTL[@]}" apply -f - >/dev/null

# Verify by printing key NAMES only (never values). Non-fatal so a verify
# hiccup never skips the pod refresh or the key-file shred below.
printf 'Secret tmi-timmy present with keys: '
# shellcheck disable=SC2016  # $k/$v are go-template vars, not shell vars
"${KCTL[@]}" -n "$NS" get secret tmi-timmy -o go-template='{{range $k, $v := .data}}{{$k}} {{end}}' 2>/dev/null || true
printf '\n'

# Restart the server so it picks up the rotated key.
"${KCTL[@]}" -n "$NS" rollout restart deploy/tmi-server >/dev/null 2>&1 || true

# Securely remove the key file.
if command -v shred >/dev/null 2>&1; then
  shred -u "$KEYFILE"
else
  rm -P "$KEYFILE" 2>/dev/null || rm -f "$KEYFILE"
fi
echo "Done. Key file removed."
