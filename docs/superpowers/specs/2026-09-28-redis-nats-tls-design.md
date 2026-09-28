# PR 6: in-cluster TLS for Redis and NATS (design)

Date: 2026-09-28. Status: approved in brainstorming, section by section (Eric).
Threats: T388 (Redis), T356 (NATS), from the AWS Terraform threat model; the NetworkPolicy
half shipped in PR 4 (#971). Supersedes the plan's "Terraform CA" sketch
(`2026-09-27-aws-threat-remediation-plan.md`, PR 6).

## Human decisions (Eric, 2026-09-28)

1. TLS for Redis and NATS in **every** environment: AWS, k3s-rp, docker-desktop, and integration tests.
2. PKI via **cert-manager** in every cluster (not Terraform `tls_*`, not a generator script).
3. **mTLS for NATS**, server TLS plus password for Redis. Redis requires a password everywhere.
4. **Stakater Reloader** rolls pods when a mounted Secret changes (also reused by #965).
5. `TMI_WORKER_NATS_URL` is removed; `TMI_NATS_URL` is the single NATS endpoint setting.
   Separate settings are kept only with a good reason; ask Eric when unsure.

## Goal and success criteria

- No plaintext Redis (6379) or NATS client (4222) traffic in any shipped environment.
- NATS rejects clients without a certificate issued by the internal CA.
- Certificates renew automatically; pods pick them up without manual action.
- The Go code still runs without TLS when not configured (defaults off), so bare `make`
  targets and third-party deployments keep working.

## 1. Platform and PKI

- Vendor pinned manifests `deployments/k8s/platform/cert-manager.yml` and `reloader.yml`,
  applied like `keda.yml`: by `scripts/devenv.py` (docker-desktop, k3s) and by
  `scripts/deploy-aws.sh` `apply_platform_base` (AWS). kubectl, not Terraform.
- `deployments/k8s/platform/pki.yml`:
  - self-signed ClusterIssuer `tmi-bootstrap` issues Certificate `tmi-internal-ca`
    (isCA, ECDSA P-256, 5 years, Secret in the `cert-manager` namespace);
  - CA ClusterIssuer `tmi-internal` backed by that Secret signs all leaves.
  - The CA key exists only in that Secret: never in Terraform state or on disk.
- Leaf Certificates in `tmi-platform`, ECDSA P-256, `duration: 2160h` (90 d), `renewBefore: 720h` (30 d):
  - server: `redis-tls`, `nats-tls`; SANs `<svc>`, `<svc>.tmi-platform.svc`, `<svc>.tmi-platform.svc.cluster.local`;
  - NATS client (usage `client auth`): `nats-client-server`, `nats-client-controller`,
    `nats-client-extractor`, `nats-client-chunk-embed`, `nats-client-worker-probe`; CN = component name.
- Every leaf Secret carries `ca.crt`, `tls.crt`, `tls.key`, so one mount gives a pod its trust anchor.
- Server and controller carry `reloader.stakater.com/auto: "true"`; worker Deployments get it
  from the TMIComponent renderer. Redis and NATS use the targeted
  `secret.reloader.stakater.com/reload: "redis-tls"` / `"nats-tls"` instead: `auto` would roll Redis
  whenever `tmi-secrets` changes (e.g. #965 rotations) and wipe its in-memory sessions.
- CA rotation is manual and rare (5 years); the runbook goes into #965 (trust old+new CA, reissue, drop old).
- Out of scope: the PR 5 `tmi-server-tls` cert stays on Terraform.

## 2. Redis and NATS servers

- **Redis** (`deployments/k8s/dev/redis.yml`, base; Reloader annotation targets `redis-tls` only): `--port 0 --tls-port 6379
  --tls-cert-file /tls/tls.crt --tls-key-file /tls/tls.key --tls-ca-cert-file /tls/ca.crt
  --tls-auth-clients no`, Secret `redis-tls` mounted at `/tls`.
  - Password everywhere: the `requirepass` patch moves from the AWS overlay into the base.
    AWS keeps Terraform's `tmi-secrets`; `devenv.py` creates a random `TMI_REDIS_PASSWORD`
    in `tmi-secrets` for docker-desktop/k3s when absent (never printed, per secret-safety rules).
  - Verify during planning that the Chainguard Redis image is built with TLS; otherwise pick one that is.
- **NATS** (`deployments/k8s/platform/nats.yml` ConfigMap):
  `tls { cert_file: /tls/tls.crt, key_file: /tls/tls.key, ca_file: /tls/ca.crt, verify: true, timeout: 2 }`,
  Secret `nats-tls` at `/tls`. Monitoring stays `http: 8222` (KEDA scaler unchanged; PR 4
  NetworkPolicy restricts it). No `authorization {}` yet; per-component subject permissions
  via `verify_and_map` are a follow-up.
- Accepted: Redis has no persistence, so a Reloader roll (about every 60 days) drops sessions,
  refresh tokens, and the token blacklist, as any Redis restart already does. Persistence is a #965 question.

## 3. Go clients

- New package `internal/tlsconfig`: `Load(caFile, certFile, keyFile string) (*tls.Config, error)`.
  `MinVersion` TLS 1.2, `RootCAs` from `caFile` only (no system pool); when cert/key are given,
  `GetClientCertificate` re-reads them from disk on each handshake, so renewals apply on reconnect.
- **Redis** (`auth/db/redis.go`, `internal/config` registry, `cmd/server` `buildRedisConfig`,
  legacy `auth/config.go` RedisConfig): add `TMI_REDIS_TLS_ENABLED` (default false) and
  `TMI_REDIS_TLS_CA_FILE`; a `rediss://` URL also enables TLS. `redis.Options.TLSConfig` from
  `tlsconfig.Load(ca, "", "")` with `ServerName` = host.
- **NATS**: add `TMI_NATS_TLS_CA_FILE`, `TMI_NATS_TLS_CERT_FILE`, `TMI_NATS_TLS_KEY_FILE`; when the
  CA is set, add `nats.Secure(cfg)`. Applies in `internal/worker/nats.go` `Connect`,
  `internal/platform/controller/jetstream_provisioner.go`, and `cmd/worker-probe` (keeps its own
  connect code, uses the same helper). Shipped URLs become `tls://nats.tmi-platform.svc:4222`.
- Remove `TMI_WORKER_NATS_URL`: `bootstrap.LoadWorker` reads `TMI_NATS_URL`; update
  `process_env.go`, `config-reference.md`, and the test harness.
- **Workers**: the TMIComponent controller's Deployment renderer adds a volume from
  `nats-client-<component>` at `/etc/tmi-nats-tls`, the three NATS TLS env vars, and the
  Reloader annotation. Check whether controller RBAC needs anything for Secret volume references.
- **Server and controller** manifests (base and overlays) mount `nats-client-server` /
  `nats-client-controller` and `redis-tls` (CA only) and set the env vars.
- Code defaults stay off; every shipped manifest turns TLS on.

## 4. Testing and rollout

- Unit: `internal/tlsconfig` (in-memory CA via `crypto/x509`; bad CA, missing key, cert re-read
  after file replace); Redis options TLS on/off and `rediss://`; NATS options on/off; renderer
  golden test for volume, env, and annotation.
- Integration (`test/integration/framework`, `manage-redis.py`, `manage-nats.py`): a test-only Go
  helper writes a throwaway CA, Redis/NATS server certs, and a client cert to a temp dir; the
  containers mount it and start in TLS mode; the server under test gets the env vars. One new
  test asserts a plaintext Redis dial and a cert-less NATS connect both fail.
- Cluster verification, docker-desktop then k3s-rp, before AWS: clean `dev-up` goes Ready;
  `make test-integration` passes; worker-probe e2e including KEDA scale-from-zero; force a renewal
  (`cmctl renew` or delete the Secret) and confirm Reloader rolls pods.
- Rollout: one PR. `dev-up` / `deploy-aws.sh` apply cert-manager, Reloader, and PKI first, wait for
  the Certificates to be Ready, then Redis, NATS, and workloads. The AWS deploy waits for #968,
  #972, and #965, per Eric.
- No Oracle review needed: no database-touching change.

## Human decisions recorded during planning (Eric, 2026-09-28)

- A Reloader roll of NATS wipes the emptyDir JetStream store (streams, consumers, payload bucket), about every 60 days on cert renewal. Accepted, the same trade-off as Redis. Re-ensuring streams after a NATS reconnect is a follow-up.
- The integration-test Redis container also uses a password: a throwaway per-machine password in gitignored `.local/test-tls/`, delivered by redis.conf/--env-file and never argv.
