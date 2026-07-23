#!/usr/bin/env bash
# Set one key in the tmi-oauth-providers Kubernetes secret WITHOUT exposing the
# value in argv, environment, or logs.
#
# Provide the value in a file (create it in YOUR OWN terminal, not through the
# assistant, so the value never enters the chat):
#     (umask 077; printf '%s' 'THE-SECRET-VALUE' > "$HOME/.tmi-oauth-secret")
# then run:
#     scripts/set-oauth-secret.sh OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET
#     scripts/set-oauth-secret.sh <KEY> /path/to/valuefile
#
# The value is base64-encoded from the file into a umask-077 patch file and
# applied with `kubectl patch --patch-file`, so it never appears on a command
# line or in an environment variable. A single trailing newline is stripped
# defensively. The value file is securely removed on success.
#
# Does NOT restart the server; run a rollout restart / overlay re-apply after.
set -euo pipefail

export AWS_PROFILE=tmi AWS_REGION=us-east-1
NS=tmi-platform
SECRET=tmi-oauth-providers
KEY="${1:?usage: set-oauth-secret.sh <SECRET_KEY> [valuefile]}"
VALFILE="${2:-$HOME/.tmi-oauth-secret}"

if [[ ! -s "$VALFILE" ]]; then
  echo "ERROR: value file '$VALFILE' not found or empty."
  echo "Create it privately in your own terminal (keeps the value out of the chat):"
  echo "    (umask 077; printf '%s' 'THE-SECRET-VALUE' > \"$VALFILE\")"
  echo "then re-run:  $0 $KEY [valuefile]"
  exit 1
fi

aws eks update-kubeconfig --name tmi-eks --region us-east-1 >/dev/null 2>&1 || true

umask 077
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
# Strip a single trailing newline, then base64 (no wrapping). The value is only
# ever in a shell variable and the 0600 temp file, never in argv or logs.
b64="$(printf '%s' "$(cat "$VALFILE")" | base64 | tr -d '\n')"
printf '{"data":{"%s":"%s"}}' "$KEY" "$b64" > "$tmp"

kubectl -n "$NS" patch secret "$SECRET" --type=merge --patch-file="$tmp" >/dev/null
echo "Patched $SECRET key: $KEY (value not shown)."

if command -v shred >/dev/null 2>&1; then shred -u "$VALFILE"; else rm -P "$VALFILE" 2>/dev/null || rm -f "$VALFILE"; fi
echo "Value file removed. Run a rollout restart to apply."
