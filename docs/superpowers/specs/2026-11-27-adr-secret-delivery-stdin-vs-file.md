# ADR: In-code secret delivery uses stdin manifests, not temp files

Date: 2026-11-27. Status: accepted. PR: #1247. Related: #1243.

## Human-made architectural decision (Eric, 2026-11-27)

The umask-077 file plus `kubectl --from-file` pattern exists to keep secret values out of
agent sessions. It is not a universal rule. When deterministic code already holds the value
and there is no chance of it reaching an LLM session transcript, that code builds the
Secret manifest (or merge patch) in memory and pipes it to kubectl on stdin, writing
nothing to disk. When a step is agentic, a different solution that keeps the secret out of
the agent session is designed case by case.

## Why

CodeQL flags writing a secret to a file as `py/clear-text-storage-sensitive-data` (alert
1409 on PR #1247). Building the manifest in memory removes the disk write itself, so the
alert goes away for a real reason instead of being dismissed, and future instances of the
same pattern won't raise new false positives.

## How the code meets it

- `scripts/lib/deploy.py` `_secret_manifest()` builds an Opaque Secret as JSON with
  base64 `data`. `_apply_secret_from_values()` (tmi-embedding, tmi-oracle-db) runs
  `kubectl apply -f -`, and `ensure_redis_password_secret()` runs `kubectl create -f -`.
- `seed_tmi_secret_keys()` runs `kubectl patch --type=merge --patch-file=/dev/stdin`.
- Values never reach argv, the environment, a log or stdout.
- Operator-run bash scripts (`set-oauth-secret.sh`, `rotate-openai-keys.sh`,
  `deploy-aws.sh`, the Makefile `test-e2e-workers` target) keep the umask-file pattern.
