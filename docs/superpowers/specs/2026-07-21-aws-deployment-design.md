# AWS Deployment Design: Hybrid Terraform Infra + Kustomize Workloads

**Date:** 2026-07-21
**Status:** Approved
**Target:** https://server.aws.tmi.dev — AWS account 967218005408 (CLI profile `tmi`), region us-east-1

## Goal

Deploy the complete current TMI stack (server, Redis, NATS, KEDA, component-controller,
extractor and chunk-embed workers) publicly on AWS EKS. Terraform owns AWS
infrastructure only; Kubernetes workloads come from the same manifest base that
`scripts/devenv.py` uses locally, via a new `aws` kustomize overlay.

## Motivation

The existing `terraform/environments/aws-public` template embeds its own Kubernetes
workload definitions (`terraform/modules/kubernetes/aws/k8s_resources.tf`: server +
Redis only). The dev deployment shape moved on (July 2026: NATS, KEDA,
component-controller, worker fleet in `deployments/k8s/`), so the terraform copy
drifted. The hybrid split makes the manifests the single source of truth for
workloads on every target, eliminating this class of drift permanently.

## Decisions made

| Decision | Choice |
|---|---|
| Deployment tier | Public "kick the tires" (internet-facing, no deletion protection) |
| Region | us-east-1 (also set as `tmi` profile default region) |
| Domain / TLS | `server.aws.tmi.dev`, ACM cert, HTTPS with 80→443 redirect |
| Cross-account DNS | Delegated subdomain zone `aws.tmi.dev` in the tmi account |
| Mechanism | Hybrid: Terraform = infra, kustomize = workloads |
| Workload scope | Full current shape (matches local dev, minus in-cluster Postgres) |
| OAuth | Dev build mode; built-in "tmi" test provider |
| DB config | Replicated from local dev DB via new dbtool `--export-config` + existing `--import-config`, with env-specific value adjustments |

## Split of responsibilities

### Terraform (`terraform/environments/aws-public`, refactored)

Provisions AWS infrastructure only:

- VPC (public + private subnets, single NAT gateway)
- EKS cluster, single managed node (t3.medium vs t3.large decided during planning
  from summed pod resource requests)
- ECR repositories for **5 images**: tmi-server, tmi-redis, tmi-extractor,
  tmi-chunk-embed, tmi-component-controller
- RDS PostgreSQL (db.t3.micro, no Multi-AZ, no deletion protection)
- Secrets Manager (DB/JWT credentials), IRSA roles
- CloudWatch logging via Fluent Bit
- AWS Load Balancer Controller (Helm)
- ACM certificate for `server.aws.tmi.dev` with DNS validation records and
  `aws_acm_certificate_validation`, plus CNAME to the ALB — all in hosted zone
  `Z05646533D2YL1I678JXS` (`aws.tmi.dev`, already delegated from the parent
  `tmi.dev` zone in the other account; delegation verified 2026-07-21)
- Bootstrap Kubernetes objects only: `tmi-platform` namespace, Secrets/ConfigMap
  materialized from Secrets Manager values, IRSA-annotated service accounts
- The workload resources in `k8s_resources.tf` (server/Redis Deployments,
  Services, Ingress) are **removed** from terraform

### Kustomize (`deployments/k8s/dev/aws/` — new overlay)

Sibling of `docker-desktop/` and `k3s/`, consuming the same bases (server, redis,
controller, NATS, KEDA, extractor + chunk-embed components) with AWS patches:

- Image names → ECR URIs
- No in-cluster Postgres; RDS endpoint injected via the terraform-created
  Secret/ConfigMap
- NATS PVC → gp3 storage class
- ALB Ingress manifest (cert ARN + `idle_timeout.timeout_seconds=3600`
  WebSocket annotation, values patched in from terraform outputs)
- Calico / kind-cluster bits excluded (EKS uses VPC CNI)

Terraform outputs (ECR URIs, cert ARN, RDS endpoint) bridge into the overlay via a
generated, gitignored patch/env file. Nothing tmi.dev-specific is committed.

### `scripts/deploy-aws.sh` (rewritten mid-section)

Preflight → build/push 5 images to ECR → `terraform init -backend-config` +
`apply` → `aws eks update-kubeconfig` → `kubectl apply -k deployments/k8s/dev/aws/`
→ verify. Fixes the stale `TF_DIR` (`aws-production` no longer exists).

## Build changes

- Add `component-controller` as a buildable/pushable component in
  `scripts/build-app-containers.py` (currently builds server, redis, extractor,
  chunkembed only)
- NATS runs public `nats:2.10-alpine` (no build); Redis stays Chainguard

## One-time account prep

1. Set `region = us-east-1` on the `tmi` profile (config only; credentials file
   never read)
2. S3 state bucket (versioned, encrypted) + DynamoDB lock table `tmi-tf-locks`;
   local gitignored `backend.hcl`
3. ~~Delegated zone~~ — already done: `aws.tmi.dev` zone exists in the tmi
   account and parent NS records match

## Deployment flow

1. `make deploy-aws-dry-run ARGS="--domain server.aws.tmi.dev --zone-id Z05646533D2YL1I678JXS"`
2. `make deploy-aws ARGS=...` (EKS ~15–20 min first run)
3. Verify, in order: DNS resolves → `curl https://server.aws.tmi.dev/` returns
   version banner (root endpoint; no /health) → OAuth flow via tmi provider →
   authenticated API call → extraction smoke test (async pipeline drains through
   NATS/workers) → optional `wstest` WebSocket check

## Failure handling

- Terraform idempotent; S3 state + DynamoDB locking survives interrupted runs
- Kustomize apply idempotent and re-runnable independently of terraform
- ACM validation stalls pre-empted by the (already verified) delegation
- Rollback: `kubectl delete -k` for workloads; `make destroy-aws` for everything
  (RDS data dropped — acceptable at this tier)
- Server tolerates NATS absence by design (`TMI_NATS_URL` unset → async
  extraction disabled); worker-plane failures do not take down the core API

## Configuration replication (dev DB → RDS)

The RDS database must start with the operational configuration currently stored
in the local dev instance's Postgres, not bare defaults.

- Add `--export-config` to `cmd/dbtool` (DB → YAML), symmetric with the existing
  `--import-config`
- Flow: export settings from the local dev DB → review/adjust environment-specific
  values (hostnames, URLs, anything referencing localhost or in-cluster names →
  AWS equivalents such as `server.aws.tmi.dev`) → import into RDS via
  `--import-config` after migrations run
- The adjusted export file is environment-specific and gitignored; the deploy
  script wires the import step in after `terraform apply` (RDS reachable) and
  before workload rollout
- Per project rules, dbtool changes ship with the schema/tooling change and the
  oracle-db-admin review gate applies to any DB-touching code added here

## Cost

Dominant fixed costs: EKS control plane ($73/mo), node, NAT gateway, ALB, RDS
micro ≈ **$150–170/mo**. t3.large instead of t3.medium adds ~$30/mo.

## Out of scope

- Production hardening (Multi-AZ RDS, HA NAT, alarms, deletion protection)
- Real OAuth providers (Google/GitHub) — dev build mode with test provider
- tmi-ux client hosting
- Oracle ADB (AWS deployment is PostgreSQL/RDS)
