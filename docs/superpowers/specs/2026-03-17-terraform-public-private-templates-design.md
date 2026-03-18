# Terraform Public & Private Templates for All Cloud Providers

**Date:** 2026-03-17
**Issue:** [#176](https://github.com/ericfitz/tmi/issues/176)
**Branch:** main

## Overview

Create 8 self-contained Terraform templates — public and private variants for each of 4 cloud providers (AWS, OCI, GCP, Azure). Public templates are low-cost "kick the tires" deployments; private templates are for organizations deploying TMI internally without public internet exposure.

## Design Constraints

- **Single TMI server instance always** — TMI is stateful and not designed for HA configurations
- **Multi-arch container images** — amd64 + arm64 manifest lists to support OCI Always Free (Ampere arm64) alongside x86_64 on other providers
- **Provider-native registries** — ECR (AWS), OCIR (OCI), Artifact Registry (GCP), ACR (Azure)
- **Provider-native logging** — TMI logs to stdout; each provider's K8s log collection ships to their logging service
- **No provider-specific code in TMI** — all provider integration is at the infrastructure/K8s level
- **DNS is the deployer's responsibility** — templates do not create DNS records; deployers point their domain at the output load balancer/ingress IP
- **TLS termination at the load balancer** — each provider's managed certificate service (ACM, OCI Certs, Google-managed certs, Key Vault) handles TLS. Templates create certificates where the provider supports automated issuance; otherwise TLS configuration is documented as a post-deploy step.
- **Terraform state** — templates use local state by default. A commented-out remote backend block is included in each template with provider-appropriate configuration (S3, OCI Object Storage, GCS, Azure Blob) for deployers who want remote state.
- **Version constraints** — all templates include `required_version >= 1.5` and `required_providers` blocks pinning minimum provider versions to avoid breaking changes.

## Kubernetes Service Choices

| Provider | Public | Private | Virtual/Serverless Nodes |
|----------|--------|---------|--------------------------|
| AWS | EKS + single managed node | EKS + managed node, private subnets | Fargate works but costs more |
| OCI | OKE + managed node (Always Free) | OKE + managed node, private subnet | Virtual nodes — **blocked** (no init containers, max 1 emptyDir, requests must equal limits) |
| GCP | GKE Autopilot | GKE Autopilot | Autopilot is viable |
| Azure | AKS Free + B-series node | AKS Standard + private cluster | Virtual nodes — **blocked** (no init containers, no ConfigMap/Secret volumes) |

## Section 1: Directory Structure

```
terraform/environments/
├── aws-public/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── aws-private/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── oci-public/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── oci-private/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── gcp-public/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── gcp-private/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── azure-public/
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
└── azure-private/
    ├── main.tf
    ├── variables.tf
    ├── outputs.tf
    └── terraform.tfvars.example
```

Each template is a self-contained root module. The existing `aws-production/` and `oci-production/` directories are removed after new templates are in place. Each template ships `.tfvars.example` (not `.tfvars`) so users copy and fill in their values.

Approach rationale: Separate directories per variant chosen over a toggle-variable approach because clarity trumps DRY for infrastructure templates handed to users. Each template is a readable declaration of exactly what gets created.

## Section 2: Public Templates — Resource Choices

Public templates share these principles:
- **Minimal cost** (free tier where available, smallest paid shapes otherwise)
- **No delete protection** — easy teardown with `terraform destroy`
- **No HA** — single node, single replica, single AZ
- **Single TMI server instance** always
- **Redis self-hosted** in cluster as separate K8s Deployment (managed within the kubernetes module)
- **Multi-arch container images** from provider-native registries

| Resource | AWS | OCI | GCP | Azure |
|----------|-----|-----|-----|-------|
| **K8s Service** | EKS + 1 managed node | OKE + 1 managed node | GKE Autopilot | AKS Free + 1 node |
| **K8s Node Shape** | t3.medium (2 vCPU/4GB) | VM.Standard.A1.Flex (arm64, Always Free) | Auto-provisioned | B2s (2 vCPU/4GB) |
| **Database** | RDS PostgreSQL, db.t3.micro, single-AZ, `deletion_protection = false` | ADB Always Free (23ai), `is_free_tier = true` | Cloud SQL PostgreSQL, db-custom-1-3840 (1 vCPU/3.75GB), `deletion_protection = false` | Flexible Server B1ms, no delete lock |
| **Container Registry** | ECR (created in root template) | OCIR (created in root template) | Artifact Registry (created in root template) | ACR Basic (created in root template) |
| **Ingress/LB** | ALB (via AWS LB Controller) | OCI LB (flexible, 10Mbps free) | GKE-managed ingress | NGINX Ingress Controller |
| **Networking** | VPC, 1 public + 1 private subnet, 1 NAT GW, IGW | VCN, 1 public + 1 private subnet, NAT GW, Service GW | VPC, auto-managed by Autopilot | VNet, 1 subnet |
| **Secrets** | Secrets Manager | OCI Vault | Secret Manager | Key Vault |
| **Logging** | CloudWatch (Fluent Bit DaemonSet) | OCI Logging (Unified Monitoring Agent) | Cloud Logging (built-in) | Azure Monitor Container Insights |
| **Delete Protection** | Off | Off | Off | Off |
| **Est. Monthly Cost** | ~$140-150 ($74 EKS + $30 node + $32 NAT GW + $16 ALB) | ~$0 (Always Free eligible) | ~$80-100 (Autopilot + Cloud SQL) | ~$75-85 |

## Section 3: Private Templates — Resource Choices

Private templates share these principles:
- **No public ingress** — no internet-facing load balancer or public IP on the cluster
- **Outbound NAT** — pods can reach internet (container registries, OAuth IdPs) via NAT gateway
- **Temporary public access for K8s provisioning** — created during apply, removed at the end (see Section 6)
- **Delete protection ON** — via provider API flags (not Terraform `lifecycle.prevent_destroy`, which cannot be parameterized)
- **Single TMI server instance** always
- **Provider-native container registry** with private access
- **Deployer responsible for** establishing user connectivity and IdP integration

| Resource | AWS | OCI | GCP | Azure |
|----------|-----|-----|-----|-------|
| **K8s Service** | EKS + 1 managed node, private API endpoint | OKE + 1 managed node, private API endpoint | GKE Autopilot, private cluster | AKS Standard, private cluster |
| **K8s Node Shape** | t3.medium | VM.Standard.A1.Flex (arm64) | Auto-provisioned | B2ms (2 vCPU/8GB) |
| **Database** | RDS PostgreSQL, db.t3.small, single-AZ, `deletion_protection = true`, private subnet only | ADB (non-free tier for private endpoint support), protected via IAM policies | Cloud SQL PostgreSQL, private IP only, `deletion_protection = true` | Flexible Server B2s, private endpoint only, Azure resource lock |
| **Container Registry** | ECR (private by default, in root template) | OCIR (private, in root template) | Artifact Registry (private, in root template) | ACR Basic + private endpoint (in root template) |
| **Ingress/LB** | Internal ALB (private subnets only) | Internal OCI LB | Internal GKE ingress | Internal NGINX or internal LB |
| **Networking** | VPC, private subnets only + NAT GW, no IGW on app subnets | VCN, private subnets + NAT GW + Service GW | VPC, private cluster, NAT for egress | VNet, private subnets + NAT GW |
| **Secrets** | Secrets Manager + private endpoint | OCI Vault | Secret Manager | Key Vault + private endpoint |
| **Temp K8s Access** | Temporary public endpoint on EKS API, removed after provisioning | Temporary public LB to OKE API, removed after provisioning | Temporary authorized network entry for deployer IP, removed after provisioning | Temporary public API endpoint, disabled after provisioning |
| **Logging** | CloudWatch (Fluent Bit DaemonSet) | OCI Logging (Unified Monitoring Agent) | Cloud Logging (built-in) | Azure Monitor Container Insights |
| **Est. Monthly Cost** | ~$150-180 | ~$50-80 (non-free ADB) | ~$100-130 | ~$120-150 (AKS Standard $72 + B2ms $35 + DB + LB) |

## Section 4: Module Structure

All AWS, GCP, and Azure modules are **NEW**. Only OCI modules exist today. The existing `aws-production/` environment contained inline resources rather than reusable modules.

Container registry resources (ECR, OCIR, Artifact Registry, ACR) are created directly in each root template rather than as a separate module — they are 1-2 resources per provider and don't warrant a module.

```
terraform/modules/
├── certificates/
│   ├── aws/          # NEW — ACM
│   ├── oci/          # existing — Let's Encrypt via OCI Function
│   ├── gcp/          # NEW — Google-managed SSL certs
│   └── azure/        # NEW — Key Vault certs or App Gateway managed certs
├── compute/
│   └── oci/          # existing — Container Instances (alternative to K8s)
├── database/
│   ├── aws/          # NEW — RDS PostgreSQL (deletion_protection as variable)
│   ├── oci/          # existing — ADB (update default to 23ai, use provider-level deletion protection instead of lifecycle.prevent_destroy)
│   ├── gcp/          # NEW — Cloud SQL PostgreSQL
│   └── azure/        # NEW — Flexible Server PostgreSQL
├── kubernetes/
│   ├── aws/          # NEW — EKS with managed node group (includes K8s resources: TMI Deployment, Redis Deployment, Services, ConfigMaps, Secrets)
│   ├── oci/          # existing — OKE managed nodes (includes K8s resources)
│   ├── gcp/          # NEW — GKE Autopilot (includes K8s resources)
│   └── azure/        # NEW — AKS (includes K8s resources)
├── logging/
│   ├── aws/          # NEW — CloudWatch + Fluent Bit
│   ├── oci/          # existing — OCI Logging
│   ├── gcp/          # NEW — Cloud Logging config
│   └── azure/        # NEW — Azure Monitor Container Insights
├── network/
│   ├── aws/          # NEW — VPC
│   ├── oci/          # existing — VCN
│   ├── gcp/          # NEW — VPC
│   └── azure/        # NEW — VNet
└── secrets/
    ├── aws/          # NEW — Secrets Manager
    ├── oci/          # existing — OCI Vault
    ├── gcp/          # NEW — Secret Manager
    └── azure/        # NEW — Key Vault
```

Changes to existing OCI modules:
- **`database/oci`**: Update default `db_version` to 23ai. Replace `lifecycle { prevent_destroy = true }` with provider-level deletion protection flag, controlled by a variable. (Terraform does not allow variables in `lifecycle` blocks.)

## Section 5: Multi-Arch Container Image Build

TMI currently builds single-architecture container images. Multi-arch manifest lists are needed to support OCI Always Free (arm64 Ampere) alongside x86_64 nodes.

**Approach:** Docker buildx creates multi-platform images pushed as manifest lists to each provider's registry.

- **Build targets**: `linux/amd64` + `linux/arm64`
- **Base images**: Chainguard images already provide multi-arch variants
- **Go binary**: `CGO_ENABLED=0` cross-compiles cleanly via `GOARCH` environment variable
- **Oracle support excluded**: Oracle requires CGO, oracle-tagged builds remain x86_64 only

**New Make targets:**
- `make build-container-tmi-multiarch` — builds + pushes multi-arch TMI server image
- `make build-container-redis-multiarch` — builds + pushes multi-arch Redis image
- `make build-containers-multiarch` — builds all multi-arch images

Registry push is a deployment concern, not a Terraform concern — templates consume image URLs as variables via `.tfvars`.

## Section 6: Temporary K8s Provisioning Access (Private Templates)

Private templates need to reach the K8s API during `terraform apply` to create namespaces, deployments, services, etc. After provisioning, that access is removed.

**Per-provider mechanism:**

- **AWS EKS**: API endpoint created with `endpoint_public_access = true` and `public_access_cidrs` locked to deployer's IP. A `null_resource` with `depends_on` all K8s resources flips `endpoint_public_access = false` via `aws eks update-cluster-config` CLI call.
- **OCI OKE**: Temporary NSG rule allows inbound 6443 from deployer's IP on the API subnet. A `null_resource` removes the NSG rule after K8s resources are provisioned.
- **GCP GKE**: Deployer's IP added to `master_authorized_networks_config`. A `null_resource` calls `gcloud container clusters update` to clear the authorized network entry.
- **Azure AKS**: Deployer's IP added to `api-server-authorized-ip-ranges`. A `null_resource` calls `az aks update` to remove the authorized range.

**Common pattern:**
1. Terraform detects deployer IP (via `http` data source or variable)
2. Temporary access created as part of the cluster resource
3. Kubernetes provider uses the temporary access to deploy workloads
4. `null_resource` with `depends_on` [all K8s resources] revokes access via CLI
5. If apply fails mid-way, temporary access remains — cleaned up on next `apply` or `destroy`

**Known limitation — idempotency:** The cluster resource definition includes public access (needed for provisioning), but the `null_resource` revokes it after apply. A subsequent `terraform apply` may detect drift and attempt to re-enable public access, triggering another revoke cycle. This is a known Terraform anti-pattern with `null_resource` provisioners. Mitigation options to evaluate during implementation:
- Use `ignore_changes` on the public access attribute after initial provisioning
- Use a two-stage apply with `-target` (adds operational complexity but avoids drift)
- Accept the drift cycle as harmless (access is re-enabled briefly, then revoked again)

## Section 7: Template Outputs

All 8 templates produce consistent outputs.

**Standard outputs:**
- `tmi_api_endpoint` — URL to reach the TMI API
- `kubernetes_cluster_name` — cluster identifier for kubectl config
- `kubernetes_config_command` — copy-paste command to configure kubectl
- `database_host` — PostgreSQL connection endpoint (internal)
- `container_registry_url` — where to push updated images
- `redis_endpoint` — internal Redis service address

**Public template additional outputs:**
- `tmi_external_url` — the internet-accessible URL

**Private template additional outputs:**
- `tmi_internal_url` — the private URL/IP
- `note` — reminder that deployer must establish their own connectivity

No sensitive values in outputs — database passwords, JWT secrets, etc. are stored in the provider's secrets manager and referenced by K8s deployments.
