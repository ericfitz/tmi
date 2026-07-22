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
  `ECR_REGISTRY_PLACEHOLDER` / `CERT_ARN_PLACEHOLDER` tokens described below
  (sed, in place, on a deploy-time working copy of this directory) before
  running `kubectl apply -k`. **No generated/gitignored kustomize component
  files are produced by the deploy script** — every rendered manifest comes
  straight from the files committed here, with only the placeholder tokens
  substituted.
- Postgres is **RDS**, not in-cluster — there is no Postgres base in this
  overlay's `resources:`.

## Placeholders

Two exact tokens are seeded by this overlay and rewritten by the deploy
script (sed-style substitution, in place, no generated files) — do not
rename them without updating the deploy script in lockstep:

| Placeholder | Where | Replaced with |
|---|---|---|
| `CERT_ARN_PLACEHOLDER` | `ingress.yml`, `alb.ingress.kubernetes.io/certificate-arn` | ACM certificate ARN |
| `ECR_REGISTRY_PLACEHOLDER` | `kustomization.yaml` (`images:` transformer, for `tmi-server`, `tmi-component-controller`, `tmi-redis`), `patches/extractor-image.yaml`, `patches/chunkembed-image.yaml` | Account's ECR registry URI |

All five workload images (`tmi-server`, `tmi-component-controller`,
`tmi-redis`, `tmi-extractor`, `tmi-chunk-embed`) are rewritten to
`ECR_REGISTRY_PLACEHOLDER/tmi-<component>:latest`. The server and controller
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
  Added two explicit `valueFrom.secretKeyRef` entries against the
  terraform-owned `tmi-secrets` Secret
  (`kubernetes_secret_v1.tmi` in
  `terraform/modules/kubernetes/aws/k8s_resources.tf`): `TMI_DATABASE_URL`
  and `TMI_JWT_SECRET`. Both are **required** —
  `internal/config/config.go` fails startup validation without
  `TMI_DATABASE_URL` (`"database url is required (TMI_DATABASE_URL)"`) and
  validates the JWT secret too. The `tmi-server-config` ConfigMap mounted at
  `/etc/tmi` supplies only a deliberately-empty `config.yml` (see that
  ConfigMap's `data["config.yml"]` comment in `k8s_resources.tf`) — it does
  **not** supply these values; an earlier version of this comment claimed
  otherwise and was wrong.
- **Deliberately explicit refs, not `envFrom: secretRef: tmi-secrets`**: the
  same Secret also carries `TMI_REDIS_PASSWORD`, and sweeping the whole
  Secret in via `envFrom` would silently inject it. See "Redis
  authentication" below for why that would break the server's Redis
  connection.
- **`envFrom: configMapRef: tmi-server-config`**: wires the terraform-owned
  ConfigMap's flat `TMI_*` keys in. See "ConfigMap flat keys" below for what
  this newly activates and why the explicit `env:` entries above aren't
  shadowed by it.
- **No `imagePullPolicy` override**: the dev base sets no explicit policy
  either, so Kubernetes' default applies — which is `Always` for a `:latest`
  tag (every image in this overlay resolves to
  `ECR_REGISTRY_PLACEHOLDER/tmi-<component>:latest`). A prior version of this
  patch set `imagePullPolicy: IfNotPresent` on the claim that "ECR tags are
  immutable per deploy" — false: `terraform/environments/aws-public/main.tf`
  sets `image_tag_mutability = "MUTABLE"` on every ECR repo, and
  `scripts/deploy-aws.sh` re-pushes `:latest` on every deploy. `IfNotPresent`
  would have left a pod that was never rescheduled running a stale image
  indefinitely. Note that `imagePullPolicy: Always` alone only helps a pod
  that gets (re)scheduled — a re-deploy onto an otherwise-unchanged
  Deployment spec does not trigger a new rollout by itself, so
  `scripts/deploy-aws.sh`'s `apply_overlay()` forces a
  `kubectl rollout restart` after applying, when it detects the Deployment
  already existed (i.e. this isn't the first install).
- **`serviceAccountName: tmi-api`**: attaches the IRSA-annotated
  ServiceAccount terraform creates, so the pod can assume the IAM role that
  reads secrets from Secrets Manager (see
  `internal/secrets/aws_provider.go`).

## Redis authentication — decision: unauthenticated in-cluster (matches local dev)

The terraform-owned `tmi-secrets` Secret carries a `TMI_REDIS_PASSWORD` key
(`var.redis_password`), but the in-cluster redis this overlay deploys
(`../redis.yml`, `cgr.dev/chainguard/redis`) is started with `--protected-mode
no` and no `requirepass` — exactly like every other dev target
(docker-desktop, k3s). It is **not** authenticated.

**Decision: `TMI_REDIS_PASSWORD` is intentionally omitted from the server's
env.** Verified this is safe, not just convenient:

- `internal/config/config.go`'s `RedisConfig.Password` (`env:"TMI_REDIS_PASSWORD"`)
  defaults to the empty string when unset — there is no "password required"
  validation for Redis anywhere in `config.go` (unlike `TMI_DATABASE_URL`
  and the JWT secret, which are validated).
- `auth/db/redis.go` passes `Password: cfg.Password` straight into
  `redis.NewClient(&redis.Options{...})`; the go-redis client only issues an
  `AUTH` command when `Password != ""`. An empty password is therefore a
  true no-op, not a client that tries to authenticate against an
  unauthenticated server (which would fail differently: `ERR Client sent
  AUTH, but no password is set`).

So option (a) — omit the var — was chosen over option (b) — wire
`requirepass` into the redis Deployment via the same Secret and inject the
password into the server. (a) matches local dev's security posture exactly,
needs no additional patch on `../redis.yml`, and avoids a chicken-and-egg
where a leaked or rotated `redis_password` Terraform variable could break
the in-cluster (already network-policy-isolated, non-internet-facing) redis
connection for no real security benefit — the redis Service has no
`Ingress`/external exposure and is only reachable from within the
`tmi-platform` namespace. If in-cluster redis auth is later required (e.g.
a compliance requirement for defense-in-depth even on internal traffic),
revisit as option (b): add a `requirepass` patch to `../redis.yml`'s args and
an explicit `TMI_REDIS_PASSWORD` `secretKeyRef` entry here, in the same
commit, so they can't drift apart.

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
name, and this patch's `env:` list already sets `TMI_SERVER_INTERFACE`,
`TMI_SERVER_PORT`, `TMI_REDIS_HOST`, `TMI_NATS_URL`, and
`TMI_AUTH_AUTO_PROMOTE_FIRST_USER` explicitly — so those five stay pinned to
the patch's values regardless of what the ConfigMap says, and `envFrom` only
newly activates `TMI_BUILD_MODE` and the `TMI_LOG_*` toggles above (plus
`TMI_AUTH_EVERYONE_IS_A_REVIEWER` in dev mode). The ConfigMap's `config.yml`
key is not a valid environment variable name and is silently skipped by
Kubernetes under `envFrom` (a benign warning Event, not a failure).

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
should read `ECR_REGISTRY_PLACEHOLDER/tmi-<component>:latest` for all five
workloads (`tmi-server`, `tmi-component-controller`, `tmi-redis`,
`tmi-extractor`, `tmi-chunk-embed`).

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
