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
  `scripts/lib/deploy.py` for local dev), and writes two gitignored,
  generated files that this overlay does not itself produce:
  - `generated-images.yaml` — a kustomize `images:` transformer component
    pinning the ECR registry URI (account-specific).
  - a generated ingress patch supplying the real ACM certificate ARN in
    place of `CERT_ARN_PLACEHOLDER`.
- Postgres is **RDS**, not in-cluster — there is no Postgres base in this
  overlay's `resources:`.

## Placeholders

Two exact tokens are seeded by this overlay and rewritten by the deploy
script (sed-style substitution) — do not rename them without updating the
deploy script in lockstep:

| Placeholder | Where | Replaced with |
|---|---|---|
| `CERT_ARN_PLACEHOLDER` | `ingress.yml`, `alb.ingress.kubernetes.io/certificate-arn` | ACM certificate ARN |
| `ECR_REGISTRY_PLACEHOLDER` | `patches/extractor-image.yaml`, `patches/chunkembed-image.yaml` | Account's ECR registry URI |

The `tmi-server` and `tmi-component-controller` image URIs are **not**
placeholder-patched in this overlay at all; they are pinned by the deploy
script's generated `images:` transformer (`generated-images.yaml`), the same
mechanism the interface note above describes.

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
  Everything else the server needs (`TMI_DATABASE_URL`, `TMI_JWT_SECRET`,
  etc.) comes from the terraform-owned `tmi-server-config` ConfigMap mounted
  at `/etc/tmi`, unaffected by this patch.
- **`imagePullPolicy: IfNotPresent`**: overrides the dev base's `Always`.
  ECR image tags are immutable per deploy; only the local `:dev` tag needs
  `Always` to pick up `make restart-dev` churn.
- **`serviceAccountName: tmi-api`**: attaches the IRSA-annotated
  ServiceAccount terraform creates, so the pod can assume the IAM role that
  reads secrets from Secrets Manager (see
  `internal/secrets/aws_provider.go`).

## Render test

```bash
kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/aws
```

Renders successfully with placeholders in place — `kubectl kustomize` does
not resolve or validate placeholder values, only `kubectl apply` against a
real cluster would.
