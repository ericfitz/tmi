#!/usr/bin/env bash
# Create/update the tmi-embedding Kubernetes secret WITHOUT exposing the API
# key in argv, environment, or logs.
#
# Provide the key by placing it in a file (create this in YOUR OWN terminal, not
# through the assistant, so the value never enters the chat):
#     (umask 077; printf '%s' 'sk-YOURKEY' > "$HOME/.tmi-embedding-key")
# then run:
#     scripts/set-embedding-secret.sh            # uses ~/.tmi-embedding-key
#     scripts/set-embedding-secret.sh /path/key  # or an explicit path
#
# The key is read straight from the file by kubectl (--from-file), so it is
# never placed on a command line or in an environment variable. The key file is
# securely removed on success.
set -euo pipefail

export AWS_PROFILE=tmi AWS_REGION=us-east-1
NS=tmi-platform
KEYFILE="${1:-$HOME/.tmi-embedding-key}"

if [[ ! -s "$KEYFILE" ]]; then
  echo "ERROR: key file '$KEYFILE' not found or empty."
  echo
  echo "Create it privately in your own terminal (keeps the key out of the chat):"
  echo "    (umask 077; printf '%s' 'sk-YOURKEY' > \"$KEYFILE\")"
  echo "then re-run:  $0 [keyfile]"
  exit 1
fi

# Point kubeconfig at the EKS cluster (no-op if already set).
aws eks update-kubeconfig --name tmi-eks --region us-east-1 >/dev/null 2>&1 || true

# Create-or-replace the secret. --from-file reads the value directly from disk,
# so it never appears in argv or the environment.
kubectl -n "$NS" create secret generic tmi-embedding \
  --from-file=api-key="$KEYFILE" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# Verify by printing key NAMES only (never values). Non-fatal (|| true) so a
# verify hiccup never skips the pod refresh or the key-file shred below.
printf 'Secret tmi-embedding present with keys: '
# shellcheck disable=SC2016  # $k/$v are go-template vars, not shell vars
kubectl -n "$NS" get secret tmi-embedding -o go-template='{{range $k, $v := .data}}{{$k}} {{end}}' 2>/dev/null || true
printf '\n'

# Recreate the chunk-embed pod so it picks up the key (it was in
# CreateContainerConfigError without the secret).
kubectl -n "$NS" delete pod -l app=tmi-chunk-embed --ignore-not-found >/dev/null 2>&1 || true

# Securely remove the key file.
if command -v shred >/dev/null 2>&1; then
  shred -u "$KEYFILE"
else
  rm -P "$KEYFILE" 2>/dev/null || rm -f "$KEYFILE"
fi
echo "Done. Key file removed."
