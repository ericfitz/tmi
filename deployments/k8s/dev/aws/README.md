# AWS (EKS) Kustomize Overlay

This directory contains the kustomize overlay that renders the full TMI
workload set on **Amazon EKS**, from the same bases local dev uses
(`../controller.yml`, `../redis.yml`, `../server.yml`,
`../../platform/components/tmi-extractor.yml`,
`../../platform/components/tmi-chunk-embed.yml`), plus an AWS-specific ALB
`Ingress`.

## Division of ownership

- **Terraform** provisions infrastructure (VPC, EKS cluster/node group, RDS
  Postgres, ECR, IAM/IRSA) and bootstrap objects in the `tmi-platform`
  namespace: the namespace itself, the `tmi-server-config` ConfigMap (mounted
  by the server at `/etc/tmi`), secrets, and the IRSA-annotated
  `tmi-api` ServiceAccount
  (`kubernetes_service_account_v1.tmi_api` in
  `terraform/modules/kubernetes/aws/k8s_resources.tf`).
- **This overlay** owns every workload: the TMI server, Redis, the
  TMIComponent controller, the extractor and chunk-embed TMIComponents, and
  the ALB Ingress.
- **`scripts/deploy-aws.sh`** applies NATS, KEDA, and the TMIComponent CRD
  before this overlay (mirroring `apply_platform_base` in
  `scripts/lib/deploy.py` for local dev), then rewrites the
  `ECR_REGISTRY_PLACEHOLDER` / `IMAGE_TAG_PLACEHOLDER` / `CERT_ARN_PLACEHOLDER`
  tokens described below
  (sed, in place, on a deploy-time working copy of this directory) before
  running `kubectl apply -k`. **No generated/gitignored kustomize component
  files are produced by the deploy script** — every rendered manifest comes
  straight from the files committed here, with only the placeholder tokens
  substituted.
- Postgres is **RDS**, not in-cluster — there is no Postgres base in this
  overlay's `resources:`.

## Placeholders

Three exact tokens are seeded by this overlay and rewritten by the deploy
script (sed-style substitution, in place, no generated files) — do not
rename them without updating the deploy script in lockstep:

| Placeholder | Where | Replaced with |
|---|---|---|
| `CERT_ARN_PLACEHOLDER` | `ingress.yml`, `alb.ingress.kubernetes.io/certificate-arn` | ACM certificate ARN |
| `ECR_REGISTRY_PLACEHOLDER` | `kustomization.yaml` (`images:` transformer, for `tmi-server`, `tmi-component-controller`, `tmi-redis`), `patches/extractor-image.yaml`, `patches/chunkembed-image.yaml` | Account's ECR registry URI |
| `IMAGE_TAG_PLACEHOLDER` | same places as `ECR_REGISTRY_PLACEHOLDER` | Short git SHA of the deployed commit |

All five workload images (`tmi-server`, `tmi-component-controller`,
`tmi-redis`, `tmi-extractor`, `tmi-chunk-embed`) are rewritten to
`ECR_REGISTRY_PLACEHOLDER/tmi-<component>:IMAGE_TAG_PLACEHOLDER`. The server and controller
go through the top-level `images:` transformer in `kustomization.yaml`
(kustomize's standard image-rewrite mechanism, matching the pattern
`../docker-desktop/kustomization.yaml` uses to strip the `localhost:5000/`
prefix); the two TMIComponent CRs go through their own JSON6902 patches
because kustomize's `images:` transformer does not know how to find an image
reference at a custom CRD path like `.spec.image`.

**Redis is rebuilt and pushed to ECR as `tmi-redis`** (see the `aws` case in
`scripts/container_build_helpers.py`) rather than pulled from
`cgr.dev/chainguard/redis` at deploy time. This removes the external
registry as a deploy-time dependency and puts Redis through the same
ECR-hosted, scanned image pipeline as every other TMI component. Local dev
(docker-desktop/k3s overlays) is unaffected — those still use
`cgr.dev/chainguard/redis:latest` directly, since they have no ECR to push
to.

## Resolved caveats

### 1. NATS storage class — no patch needed, and none is possible

The task brief for this overlay anticipated a `patches/nats-storageclass.yaml`
patch, conditioned on whether `deployments/k8s/platform/nats.yml`'s PVC
template pins an explicit `storageClassName`. Checking the actual base:

```console
$ rg -n 'storageClassName' deployments/k8s/platform/nats.yml
# (no matches)
```

The NATS `StatefulSet` in `nats.yml` has **no `volumeClaimTemplates` /
`PersistentVolumeClaim` at all** — its `/data/jetstream` mount is a plain
`emptyDir: {}`. There is no PVC and no `storageClassName` field anywhere in
that manifest, so there is nothing for a storage-class patch to target — not
"uses the cluster default", but "has no persistent volume to configure at
all". Consequently:

- `patches/nats-storageclass.yaml` was **not created**.
- The `nats-storageclass` patch entry was **removed** from
  `kustomization.yaml`.
- **`scripts/deploy-aws.sh` (Task 6) needs no storage-class override for
  NATS.** JetStream data on AWS is ephemeral, exactly as it is in local dev —
  a NATS pod restart loses in-flight stream state. If durable JetStream
  storage is later required on EKS, that's a change to the shared
  `platform/nats.yml` base (to add a real `volumeClaimTemplate`), not to this
  overlay.

### 2. Ingress subnets — no explicit annotation needed

The brief asked whether `terraform/modules/network/aws/main.tf` tags public
subnets `kubernetes.io/role/elb=1` (which lets the AWS Load Balancer
Controller auto-discover them) or not (which would require an explicit
`alb.ingress.kubernetes.io/subnets` annotation via the deploy script's
generated ingress patch). Checking the actual module:

```console
$ rg -n 'kubernetes.io/role/elb' terraform/modules/network/aws/main.tf
79:    "kubernetes.io/role/elb" = "1"
93:    "kubernetes.io/role/elb" = "1"
```

Both `aws_subnet.public` and `aws_subnet.public_secondary` carry the
`kubernetes.io/role/elb = "1"` tag. **The AWS Load Balancer Controller
auto-discovers these tagged subnets**, so `ingress.yml` deliberately omits
`alb.ingress.kubernetes.io/subnets`. **`scripts/deploy-aws.sh` (Task 6) does
not need to generate a subnets annotation patch.**

## `patches/server-config.yaml`

Strategic-merge patch on the `tmi-server` Deployment:

- **`env` (`$patch: replace`)**: swaps the entire dev env list for the AWS
  list. Dropped: `TMI_WEBHOOK_ALLOW_HTTP_TARGETS` and
  `TMI_SSRF_WEBHOOK_ALLOWLIST=host.docker.internal` — both exist only so the
  dev server can reach the host-run integration webhook receiver over
  plaintext HTTP; neither applies on AWS. Kept: `TMI_NATS_URL` — NATS runs
  in-cluster on AWS too, at the same `nats.tmi-platform.svc:4222` address.
  Added four explicit `valueFrom.secretKeyRef` entries against the
  terraform-owned `tmi-secrets` Secret
  (`kubernetes_secret_v1.tmi` in
  `terraform/modules/kubernetes/aws/k8s_resources.tf`): `TMI_DATABASE_URL`,
  `TMI_JWT_SECRET`, `TMI_REDIS_PASSWORD` (#551, see "Redis authentication"
  below) and `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` (#547, see
  "Settings-at-rest encryption" below). The first two are **required** —
  `internal/config/config.go` fails startup validation without
  `TMI_DATABASE_URL` (`"database url is required (TMI_DATABASE_URL)"`) and
  validates the JWT secret too. The `tmi-server-config` ConfigMap mounted at
  `/etc/tmi` supplies only a deliberately-empty `config.yml` (see that
  ConfigMap's `data["config.yml"]` comment in `k8s_resources.tf`) — it does
  **not** supply these values; an earlier version of this comment claimed
  otherwise and was wrong.
- **Deliberately explicit refs, not `envFrom: secretRef: tmi-secrets`**:
  sweeping the whole Secret in would also inject keys the server does not
  read, and listing them explicitly keeps the blast radius of a future key
  addition visible at the point of use.
- **`envFrom: configMapRef: tmi-server-config`**: wires the terraform-owned
  ConfigMap's flat `TMI_*` keys in. See "ConfigMap flat keys" below for what
  this newly activates and why the explicit `env:` entries above aren't
  shadowed by it.
- **No `imagePullPolicy` override**: the dev base sets no explicit policy
  either, so Kubernetes' default applies. Since #553 every image resolves to
  `ECR_REGISTRY_PLACEHOLDER/tmi-<component>:<short git SHA>` rather than
  `:latest`, and the default for any non-`:latest` tag is `IfNotPresent` —
  which is correct here, because a per-deploy tag names one immutable build
  and re-pulling it on every reschedule would be wasted work.

  This supersedes an earlier arrangement worth understanding, because it
  failed silently. With `:latest`, the rendered Deployment spec was
  byte-identical on every deploy, so `kubectl apply` was a no-op and no
  rollout happened; `apply_overlay()` compensated by issuing an explicit
  `kubectl rollout restart` against a hardcoded list of **two** Deployments
  (`tmi-server`, `tmi-component-controller`). Redis was not on that list, so a
  rebuilt `tmi-redis` image never reached the cluster at all. Per-deploy tags
  make the spec genuinely change, so Kubernetes rolls exactly the workloads
  whose image moved and the restart hack is gone.
- **`serviceAccountName: tmi-api`**: attaches the IRSA-annotated
  ServiceAccount terraform creates, so the pod can assume the IAM role that
  reads secrets from Secrets Manager (see
  `internal/secrets/aws_provider.go`).

## Redis authentication — authenticated + network-restricted (#551)

The in-cluster redis this overlay deploys is **authenticated**, and reachable
only from the API server pods. Two independent controls:

- `patches/redis-auth.yaml` starts redis with
  `--requirepass $(REDIS_PASSWORD)`, sourced from the terraform-owned
  `tmi-secrets` Secret's `TMI_REDIS_PASSWORD` key. The server side of the same
  Secret is injected by `patches/server-config.yaml`.
- `networkpolicy-redis.yml` restricts ingress to port 6379 to pods labelled
  `app=tmi-server`.

**These two patches must move together.** Injecting `TMI_REDIS_PASSWORD` into
the server without the redis patch (or vice versa) breaks every redis
connection — in opposite and equally confusing ways: a server with a password
against a passwordless redis gets `ERR Client sent AUTH, but no password is
set`, while a passwordless server against an authenticated redis gets
`NOAUTH Authentication required`.

Local dev (docker-desktop, k3s) is deliberately **unchanged** and still runs
redis unauthenticated. Only this overlay patches it, so the dev inner loop
keeps working without secrets plumbing.

### Why this reversed the previous decision

This overlay originally shipped redis unauthenticated, matching local dev, and
deliberately omitted `TMI_REDIS_PASSWORD` from the server env. That reasoning
was sound about the mechanics — `RedisConfig.Password` defaults to empty and
`auth/db/redis.go` only issues `AUTH` when it is non-empty, so omitting the
variable really was a no-op rather than a broken client — but it rested on a
premise that was not true: it described redis as "already
network-policy-isolated". No NetworkPolicy existed for redis, and even if one
had, EKS silently does not enforce NetworkPolicy objects unless the VPC CNI's
NetworkPolicy agent is enabled.

Both halves of that gap are now closed: terraform enables the agent
(`enableNetworkPolicy = "true"` on the `vpc-cni` addon), and the policy
actually exists. Redis holds session state and cached authorization decisions,
so on an internet-facing cluster "only reachable from inside the namespace" was
not a sufficient control by itself.

One residual, accepted: `--requirepass` puts the password in the redis
container's process table. The stored PodSpec is unaffected (`$(REDIS_PASSWORD)`
is expanded by the kubelet at start time, so `kubectl get pod -o yaml` does not
disclose it). Projecting a `redis.conf` from the Secret would avoid even that,
at the cost of a volume and an entrypoint override — revisit if redis ever
holds more than ephemeral state.

## Settings-at-rest encryption (#547)

`patches/server-config.yaml` injects `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` from
the terraform-owned `tmi-secrets` Secret. The name matters: this deployment
configures no secrets provider, so `internal/secrets/provider.go` falls back to
the `EnvProvider`, which maps the secret key `settings_encryption_key` to
`TMI_SECRET_<KEY>`. The value is a 32-byte AES-256-GCM key rendered as 64 hex
characters (`random_id.settings_encryption_key.hex` in
`terraform/modules/secrets/aws`); `internal/crypto/settings_encryptor.go`
rejects anything else.

Without it, `crypto.NewSettingsEncryptor` returns a **disabled but non-nil**
encryptor and every Secret-classified system setting — the OAuth client secrets
in the replicated DB config among them — is written to RDS in plaintext.
`cmd/server/startup_checks.go` reports this, but only as a single log line
(ERROR in production build mode, WARN otherwise); it never blocks startup, so
the failure is easy to miss.

Two things this does **not** do:

- **It does not retroactively encrypt.** Only values written after the key is
  in place are encrypted. Rows an earlier deploy already stored as plaintext
  stay that way until something rewrites them; converting them needs
  `POST /admin/settings/reencrypt`, which requires an interactive (PKCE) admin
  token because service-account tokens are refused on `/admin/*`.
- **It does not cover `dbtool`.** `cmd/dbtool/config.go` builds its own
  secrets provider from its own config file, so `--import-config` would write
  plaintext even with the server correctly configured. `scripts/deploy-aws.sh`
  therefore points dbtool's transient connect config at Secrets Manager
  (`secrets.provider: aws`), letting it read the key under the deployer's own
  AWS identity rather than passing the value through the environment.

## ConfigMap flat keys — naming bug fixed, and now wired via `envFrom`

The terraform-side naming bug this section originally flagged is **fixed**
in `terraform/modules/kubernetes/aws/k8s_resources.tf` (commit c581c2ff) —
every flat `TMI_*` key in the `tmi-server-config` ConfigMap now matches an
actual `env:` struct tag in `internal/config/config.go` (verified against
the tags, not guessed; each key has an inline comment citing the field and
line). That audit caught three more mismatches beyond the four originally
listed here (the dev-mode-only API/WebSocket logging toggles), all now
fixed too:

| ConfigMap key (terraform, before) | config.go expects (now used) |
|---|---|
| `TMI_AUTH_BUILD_MODE` | `TMI_BUILD_MODE` |
| `TMI_LOGGING_ALSO_LOG_TO_CONSOLE` | `TMI_LOG_ALSO_LOG_TO_CONSOLE` |
| `TMI_LOGGING_REDACT_AUTH_TOKENS` | `TMI_LOG_REDACT_AUTH_TOKENS` |
| `TMI_LOGGING_SUPPRESS_UNAUTHENTICATED_LOGS` | `TMI_LOG_SUPPRESS_UNAUTH_LOGS` |
| `TMI_LOGGING_LOG_API_REQUESTS` (dev-mode block) | `TMI_LOG_API_REQUESTS` |
| `TMI_LOGGING_LOG_API_RESPONSES` (dev-mode block) | `TMI_LOG_API_RESPONSES` |
| `TMI_LOGGING_LOG_WEBSOCKET_MESSAGES` (dev-mode block) | `TMI_LOG_WEBSOCKET_MESSAGES` |

`TMI_AUTH_AUTO_PROMOTE_FIRST_USER`, `TMI_AUTH_EVERYONE_IS_A_REVIEWER` (dev-mode
block), `TMI_SERVER_INTERFACE`, `TMI_SERVER_PORT`, `TMI_REDIS_HOST`, and
`TMI_NATS_URL` already matched and are unchanged.

With the naming fixed, `patches/server-config.yaml` now wires
`envFrom: - configMapRef: { name: tmi-server-config }` on the `tmi-server`
container, so the flat `TMI_*` keys actually reach the runtime. Kubernetes
resolves an explicit `env:` entry ahead of `envFrom` for the same variable
name, and this patch's `env:` list sets `TMI_SERVER_INTERFACE`,
`TMI_SERVER_PORT`, `TMI_REDIS_HOST`, `TMI_NATS_URL`, and
`OAUTH_PROVIDERS_TMI_ENABLED` explicitly — so those stay pinned to the patch's
values regardless of what the ConfigMap says, and `envFrom` supplies the rest:
`TMI_BUILD_MODE` (`"production"`), `TMI_AUTH_AUTO_PROMOTE_FIRST_USER`
(`"false"`), and `TMI_AUTH_EVERYONE_IS_A_REVIEWER` (`"true"`, set via the
terraform `everyone_is_a_reviewer` variable). The ConfigMap's `config.yml` key
is not a valid environment variable name and is silently skipped by Kubernetes
under `envFrom` (a benign warning Event, not a failure).

## Authentication posture (internet-facing)

This overlay serves a **public** ALB, so the authentication configuration is
deliberately hardened relative to local dev:

- **Production runtime build mode.** `TMI_BUILD_MODE=production` (terraform
  ConfigMap). The "tmi" OAuth provider stays **enabled**
  (`OAUTH_PROVIDERS_TMI_ENABLED=true`), but in production it is restricted to
  the **Client Credentials Grant**: `auth/test_provider.go`'s `ExchangeCode`
  refuses the Authorization Code / `login_hint` flow unless
  `isDevOrTestBuild()` (`TMI_BUILD_MODE in {dev,test}`), so no anonymous JWT
  can be minted from `login_hint`. Critically, this control is the **runtime
  build mode, not provider registration** — it holds even though the replicated
  DB config also enables the tmi provider. Interactive human sign-in comes from
  the real OAuth providers (e.g. Google) in the replicated config; the tmi
  provider serves only machine/client-credentials tokens (which require a real
  `client_id`/`client_secret`).
- **No first-user auto-promotion.** `TMI_AUTH_AUTO_PROMOTE_FIRST_USER="false"`
  (terraform ConfigMap). On a public endpoint auto-promotion would hand admin
  to the first random visitor. Admin is seeded explicitly via the
  `administrators` operational setting in the replicated DB config (the
  configured Google admin identity). **If no administrator is seeded, the
  deployment has no admin** — verify the imported config.
- **Everyone is a reviewer.** `TMI_AUTH_EVERYONE_IS_A_REVIEWER="true"` (set via
  the terraform `everyone_is_a_reviewer` variable, decoupled from build mode so
  it survives production mode) is a deliberate choice for this collaborative
  deployment: every user who authenticates via a real provider gets
  security-reviewer capability. Scoped to real-provider-authenticated users,
  not anonymous.

## Chunk-embed API key (`TMI_EMBEDDING_API_KEY`)

`deployments/k8s/platform/components/tmi-chunk-embed.yml` reads its
embedding-provider API key from `Secret/tmi-embedding`'s `api-key` key via
`secretKeyRef`. This overlay does not create that Secret — it's out of
scope for kustomize since the value is a deployer-supplied credential, not a
static manifest field. `scripts/deploy-aws.sh` creates/updates it from the
`TMI_EMBEDDING_API_KEY` environment variable before applying this overlay
(mirroring `create_embedding_secret()` in `scripts/lib/deploy.py`, used for
local dev). If `TMI_EMBEDDING_API_KEY` is unset, the script skips creating
the Secret and prints a warning instead of writing a placeholder: without
it, chunk-embed fails with `CreateContainerConfigError` the moment KEDA
scales it up from zero.

## Render test

```bash
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/aws
```

Renders successfully with placeholders in place — `kubectl kustomize` does
not resolve or validate placeholder values, only `kubectl apply` against a
real cluster would. To confirm no image reference was missed, verify zero
non-ECR image sources remain:

```bash
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/aws \
  | rg -c 'localhost:5000|cgr.dev'
```

`rg -c` should find no matches (exit status 1), and every `image:` line
should read `ECR_REGISTRY_PLACEHOLDER/tmi-<component>:IMAGE_TAG_PLACEHOLDER`
for all five workloads (`tmi-server`, `tmi-component-controller`,
`tmi-redis`, `tmi-extractor`, `tmi-chunk-embed`). A leftover literal
`:latest` on any of them means an image reference escaped the tag
substitution and would deploy a mutable tag.

## No generated files / `.gitignore`

Earlier drafts of this overlay assumed the deploy script would write a
gitignored `generated-images.yaml` kustomize component (and a matching
generated ingress patch) to inject account-specific values. That mechanism
was never implemented and is not how the placeholders are actually consumed:
**both `CERT_ARN_PLACEHOLDER` and `ECR_REGISTRY_PLACEHOLDER` are resolved by
the deploy script sed-rewriting the literal token in place**, not by
generating separate files. Consequently `.gitignore` carries no
`deployments/k8s/dev/aws/generated-*` entry — there is nothing for the
deploy script to produce in this directory that would need ignoring. If a
future deploy-script implementation switches to a generated-component
approach instead of in-place sed, re-add the `.gitignore` entry at that
time.
