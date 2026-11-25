#!/usr/bin/env bash
# Rotate the OpenAI keys TMI uses, on k3s-rp and/or EKS, by prompting for them.
#
#   bash scripts/rotate-openai-keys.sh            # asks which clusters
#   bash scripts/rotate-openai-keys.sh --dry-run  # prints what it would do
#
# It asks for two keys (input is hidden, never echoed):
#   * Timmy key      -> Secret tmi-timmy, key llm-api-key            (tmi-server chat)
#   * Embedding key  -> Secret tmi-timmy, key text-embedding-api-key (tmi-server)
#                       Secret tmi-embedding, key api-key            (tmi-chunk-embed worker)
# Press Enter at the embedding prompt to reuse the Timmy key.
#
# Keys live only in shell variables (never exported) and in a mode-600 temp dir
# that kubectl reads with --from-file, so they never reach argv, the
# environment, or a log. The temp dir is removed on exit, success or not.
# Re-running is safe: every write is create-or-replace (kubectl apply).
#
# After rotating, revoke the OLD key in the OpenAI console. A stored database
# value for timmy.llm_api_key (settings row-wins, #1201) overrides the Secret.
set -euo pipefail

NS=tmi-platform
K3S_CONTEXT=k3s-rp
EKS_CONTEXT=arn:aws:eks:us-east-1:967218005408:cluster/tmi-eks
DRY_RUN=0

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "ERROR: unknown argument '$arg' (accepted: --dry-run, --help)" >&2; exit 2 ;;
  esac
done

command -v kubectl >/dev/null || { echo "ERROR: kubectl not found on PATH" >&2; exit 1; }

# --- read keys -------------------------------------------------------------
read_key() { # $1 prompt, $2 name of variable to set; empty input allowed
  local _k
  IFS= read -rs -p "$1: " _k
  printf '\n' >&2
  if [[ "$_k" =~ [[:space:]] ]]; then
    echo "ERROR: key contains whitespace; paste only the key" >&2; exit 1
  fi
  printf -v "$2" '%s' "$_k"
}

check_key() { # $1 label, $2 value
  [[ -n "$2" ]] || { echo "ERROR: $1 is empty" >&2; exit 1; }
  if [[ "$2" != sk-* ]]; then
    echo "WARNING: $1 does not start with 'sk-'; OpenAI keys normally do." >&2
  fi
}

TIMMY_KEY=""
EMBED_KEY=""
read_key "Paste the new Timmy key" TIMMY_KEY
check_key "Timmy key" "$TIMMY_KEY"
read_key "Paste the new Embedding key (Enter = same as Timmy key)" EMBED_KEY
[[ -n "$EMBED_KEY" ]] || EMBED_KEY="$TIMMY_KEY"
check_key "Embedding key" "$EMBED_KEY"

# --- choose targets --------------------------------------------------------
ask_yes() { # $1 prompt, $2 default (y|n)
  local a
  read -r -p "$1 " a
  a="${a:-$2}"
  case "$a" in
    y|Y|yes|YES) return 0 ;;
    n|N|no|NO) return 1 ;;
    *) echo "ERROR: answer y or n, got '$a'" >&2; exit 1 ;;
  esac
}

TARGETS=()
ask_yes "Apply to k3s-rp (dev)? [Y/n]" y && TARGETS+=("$K3S_CONTEXT")
ask_yes "Apply to EKS (PRODUCTION, api.tmi.dev)? [y/N]" n && TARGETS+=("$EKS_CONTEXT")
[[ ${#TARGETS[@]} -gt 0 ]] || { echo "Nothing selected; no changes made."; exit 0; }

# --- write key files -------------------------------------------------------
umask 077
TMPD="$(mktemp -d "${TMPDIR:-/tmp}/tmi-keys.XXXXXX")"
cleanup() { rm -P "$TMPD"/* 2>/dev/null || true; rm -rf "$TMPD"; }
trap cleanup EXIT INT TERM
printf '%s' "$TIMMY_KEY" > "$TMPD/timmy"
printf '%s' "$EMBED_KEY" > "$TMPD/embed"
TIMMY_KEY=""; EMBED_KEY=""

run() { # print in dry-run, execute otherwise; never pass key values here
  if [[ $DRY_RUN -eq 1 ]]; then printf 'DRY-RUN:'; printf ' %q' "$@"; printf '\n'; else "$@"; fi
}

apply_secret() { # $1 context, $2 secret name, then key=file pairs
  local ctx="$1" name="$2"; shift 2
  local from=()
  for kv in "$@"; do from+=(--from-file="$kv"); done
  if [[ $DRY_RUN -eq 1 ]]; then
    run kubectl --context "$ctx" -n "$NS" create secret generic "$name" "${from[@]}"
    return
  fi
  kubectl --context "$ctx" -n "$NS" create secret generic "$name" "${from[@]}" \
    --dry-run=client -o yaml | kubectl --context "$ctx" apply -f - >/dev/null
}

# --- apply -----------------------------------------------------------------
for ctx in "${TARGETS[@]}"; do
  if [[ "$ctx" == "$EKS_CONTEXT" ]] && ! kubectl config get-contexts -o name | grep -qxF "$EKS_CONTEXT"; then
    # Adds the EKS context without switching the ambient one.
    prev="$(kubectl config current-context 2>/dev/null || true)"
    run env AWS_PROFILE=tmi aws eks update-kubeconfig --name tmi-eks --region us-east-1
    [[ -z "$prev" || $DRY_RUN -eq 1 ]] || kubectl config use-context "$prev" >/dev/null
  fi
  echo "== $ctx"
  apply_secret "$ctx" tmi-timmy "llm-api-key=$TMPD/timmy" "text-embedding-api-key=$TMPD/embed"
  apply_secret "$ctx" tmi-embedding "api-key=$TMPD/embed"
  run kubectl --context "$ctx" -n "$NS" rollout restart deploy/tmi-server deploy/tmi-chunk-embed
  if [[ $DRY_RUN -eq 0 ]]; then
    kubectl --context "$ctx" -n "$NS" rollout status deploy/tmi-server --timeout=180s
    kubectl --context "$ctx" -n "$NS" rollout status deploy/tmi-chunk-embed --timeout=180s
  fi
done

echo "Done. Now revoke the OLD key in the OpenAI console."
