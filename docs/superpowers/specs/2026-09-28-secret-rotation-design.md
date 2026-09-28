# #965: scheduled rotation of high-value secrets (design)

Date: 2026-09-28. Status: approved in brainstorming, section by section (Eric).
Threats: T379 (duplicate T364). Depends on PR 6 (`2026-09-28-redis-nats-tls-design.md`) for
Stakater Reloader and Redis TLS.

## Human decisions (Eric, 2026-09-28)

1. **Scheduled automatic rotation**, not only on-demand tooling or a runbook.
2. Engine: an **in-cluster CronJob** (`tmi-rotator`), identical on AWS, k3s-rp, and docker-desktop.
3. Source of truth: the **Kubernetes Secret `tmi-secrets` only**. Terraform seeds and then ignores it;
   the Secrets Manager copies of the rotating secrets are deleted.
4. JWT moves to **ES256 with JWKS** and `kid`-based key rollover (from HS256 with a single secret).
5. DB: **alternating users plus a `tmi_owner` role on PostgreSQL**; **single user with a brief gap on
   Oracle**. Eric accepts the zero-500 policy consequences for Oracle (dev/test only).
6. The settings-encryption **key id travels in the ciphertext envelope** (the existing contextID field),
   not in a separate column.
7. Delivered as **three PRs**: (1) rotator + Redis + settings key, (2) JWT, (3) DB.

## Current state (2026-09-28)

- All four secrets reach pods as env vars from `tmi-secrets`, written by Terraform
  (`terraform/modules/kubernetes/aws/k8s_resources.tf`) from `random_password` / `random_id` values
  that are also copied to Secrets Manager (`terraform/modules/secrets/aws/main.tf`). Nothing reloads at runtime.
- RDS uses a Terraform `random_password` master; the app connects as the master user.
- JWT: HS256, one key, no `kid` dispatch (`auth/jwt_key_manager.go`). Refresh tokens are opaque Redis entries.
- Settings key: current plus previous key, trial decrypt (`internal/crypto/settings_encryptor.go`);
  `ReEncryptAll` (`api/settings_service.go`) in one transaction. `ENC:` values also live in Redis (`auth/db/redis.go`).
- Redis has no persistence; any Redis restart drops sessions, refresh tokens, and the token blacklist.

## 1. Rotator shape

- `cmd/rotator`, built into the existing server image. CronJob in `tmi-platform`, daily,
  `concurrencyPolicy: Forbid`.
- Per-secret annotations on `tmi-secrets`: `tmi.dev/rotated-at.<name>`, `tmi.dev/rotate-every.<name>`
  (default `90d`), `tmi.dev/rotation-phase.<name>`. A secret rotates when due, or when forced:
  `kubectl create job --from=cronjob/tmi-rotator` with `ROTATE=<name>`.
- ServiceAccount RBAC: `get`/`patch` on `tmi-secrets` and `tmi-rotator-admin` only; read Deployment
  rollout status in `tmi-platform`. Nothing cluster-wide, no AWS permissions.
- Server and worker Deployments roll via Reloader when `tmi-secrets` changes. Redis and NATS must not:
  they use the targeted `secret.reloader.stakater.com/reload` annotation for their TLS Secrets only.
- Every rotation is phased: add the new credential while the old works, patch the Secret, wait for
  the rollout (10-minute timeout), then retire the old credential. Each step is idempotent; a failed
  or stalled run exits non-zero and the next run resumes from the recorded phase.
- Logging via slogging, never values. On AWS, a CloudWatch metric filter plus a "no successful rotation
  in 100 days" alarm to `tmi-security-alerts` (ties into #968).
- Terraform seeds the initial values, then `lifecycle { ignore_changes = [data] }` on `tmi-secrets`;
  the Secrets Manager copies of the DB, Redis, JWT, and settings secrets are removed.
  `deploy-aws.sh` `import_config` reads what it needs from `tmi-secrets` with the file-based
  (`umask 077`) pattern.

## 2. PR 1: Redis password and settings encryption key

- **Redis**: `ACL SETUSER default >NEW` (old still valid), patch `TMI_REDIS_PASSWORD`, wait for the
  server rollout, `ACL SETUSER default <OLD`. No Redis restart; sessions survive. A later Redis restart
  boots with `--requirepass` from the Secret (already NEW).
- **Settings key: key ids in the envelope.** `ENC:v1:<kid>:<timestamp>:<b64>`: the existing contextID
  slot becomes the key id. The keyring holds current and optional previous keys, each with an id.
  Encrypt writes the current id; decrypt selects by id, and falls back to trial for unknown ids
  (legacy data). Existing data is id 1; the first rotated key is id 2. Secret keys:
  `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` and `..._KEY_ID`, plus the previous-key equivalents
  (confirm exact names during planning).
- **Keyring rollout**: (1) stage: previous = NEW, current = OLD, roll; (2) promote: current = NEW,
  previous = OLD, roll; (3) re-encrypt in-process (admin endpoints refuse service-account tokens);
  (4) at a later run, drop previous once zero DB values carry its id across every encrypted column
  and Redis `ENC:` entries under it have expired (wait out the longest TTL).
- **Resumable `ReEncryptAll`**: select values not prefixed `ENC:v1:<currentID>:`, then decrypt,
  re-encrypt, and `UpdateColumn` each row in its own short transaction, in batches. A crash leaves a
  readable mix; the next run finishes it. Planning inventories every `ENC:` column (webhook secrets
  included) so re-encrypt covers all of them. Oracle review required.

## 3. PR 2: JWT on ES256 with JWKS

- `TMI_JWT_KEYRING` (one Secret value, atomic):
  `{"signing": {"kid", "private_pem"}, "verify": [{"kid", "public_pem"}, ...]}`; ECDSA P-256;
  `kid` = RFC 7638 JWK thumbprint. It replaces `TMI_JWT_SECRET`, `TMI_JWT_SIGNING_METHOD`, and the
  single-key file settings.
- Tokens carry `kid` and `alg: ES256`. Verification selects by `kid` and rejects unknown kids and any
  other alg (alg pinned per key). `/.well-known/jwks.json` publishes the verify set. Covers user
  access tokens and client-credential tokens. Planning inventories other `TMI_JWT_SECRET` uses
  (OAuth state, WS tickets); non-JWT HMAC uses get their own dedicated secret.
- Rotation: stage (add the new public key to verify, roll), promote (sign with new, keep old in
  verify, roll), drop the old key after the maximum access-token lifetime. Refresh tokens are
  unaffected, so nobody is logged out.
- HS256 cutover, once at deploy: old access tokens get 401, and clients refresh to ES256. Verify that
  tmi-ux refreshes on 401; if it doesn't, accept a one-time re-login (no HS256 verify path). Ask
  tmi-mcp and addons on Agentbus before merge, and move any local verifier to JWKS.
- Consumer check (Agentbus, 2026-09-28): tmi-ux, tmi-mcp, tmi-tf-wh, and the wiki treat tokens
  as opaque and refresh on 401; none verify locally or use `TMI_JWT_SECRET`. Open item from tmi-ux:
  verify the WebSocket collab handshake with an old HS256 token also triggers a refresh (test at
  cutover; fix client-side via a tmi-ux bug if not). Keep `/oauth2/token` client_credentials
  unchanged (tmi-tf-wh). After merge, ping dm/tmi-wiki: 14 pages document HS256/`TMI_JWT_SECRET`.
- No code default key: the server refuses to start without a keyring. `devenv.py` generates one
  into `tmi-secrets` when absent; the integration framework generates one per run.

## 4. PR 3: database credentials

- **PostgreSQL bootstrap** (idempotent rotator step, admin credential): create `tmi_owner` (NOLOGIN)
  and `tmi_a` / `tmi_b` (LOGIN, members of `tmi_owner`, `ALTER ROLE ... SET role = 'tmi_owner'`);
  per-object `ALTER ... OWNER TO tmi_owner` over the TMI schema (not `REASSIGN OWNED`, since the
  RDS master owns objects we must not move); `ALTER DEFAULT PRIVILEGES` so AutoMigrate objects land
  under `tmi_owner`; point `TMI_DATABASE_URL` at `tmi_a`. The app stops using the master user.
- **Rotation**: idle = the user `TMI_DATABASE_URL` doesn't name; `ALTER ROLE idle WITH LOGIN
  PASSWORD`; patch `TMI_DATABASE_URL`; wait for the rollout; wait `ConnMaxLifetime` (4 min) plus
  margin; `ALTER ROLE old NOLOGIN`.
- **Admin credential** (RDS master or local `postgres`) in `tmi-rotator-admin`, used only by the
  rotator and rotated on schedule. `pending_password` is written before `ALTER ROLE`; on resume try
  pending then current. Terraform `ignore_changes = [password]` on `aws_db_instance`. Runbook covers
  `aws rds modify-db-instance --master-user-password` if both are lost.
- **Oracle** (dev/test): `ALTER USER u IDENTIFIED BY new REPLACE old` by the app user itself, patch,
  roll. Connections in the gap may fail (accepted exception, decision 5).
- Oracle review covers the whole PR; the ownership transfer (sequences, RDS default privileges,
  GORM hooks) is the riskiest part.

## 5. Testing and rollout

- Unit: every rotator phase against fakes (Secret, Redis, DB): idempotent re-runs, resume from each
  recorded phase, rollout timeout, not-due skip, forced `ROTATE`. Settings keyring by id plus legacy
  fallback; batched resumable `ReEncryptAll`; JWT keyring select/reject; JWKS output.
- Integration: run the rotator in-process per secret while a background loop calls the API; on
  PostgreSQL assert no 5xx and no 401 (except the dedicated HS256-cutover test); on Oracle tolerate
  gap failures and assert recovery.
- Cluster verification on k3s-rp before AWS: force each rotation, watch Reloader rolls, confirm
  sessions survive where expected and the old credential is refused; full integration suite.
- Runbook (wiki "Secret rotation"): forcing, reading phase annotations, recovering a stuck phase,
  RDS master recovery, PR 6 manual CA rotation.
- Order: PR 6, then #965 PRs 1, 2, 3. One AWS deploy after all of them plus #968 and #972. The first
  deploy hands `tmi-secrets` from Terraform to the rotator; the rotator bootstraps DB users and the
  JWT keyring on its first run (planning decides whether `deploy-aws.sh` triggers it).
