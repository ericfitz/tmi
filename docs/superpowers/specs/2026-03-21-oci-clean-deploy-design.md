# OCI Clean Deploy: TMI + TMI-UX + TMI-TF-WH

**Date**: 2026-03-21
**Status**: Approved

## Overview

Clean deployment of all three TMI components (tmi server, tmi-ux frontend, tmi-tf-wh webhook analyzer) to OCI public environment on a fresh OCI tenancy with no pre-existing infrastructure.

## Scope

### Deliverables

1. **`terraform.tfvars`** — Real values for `terraform/environments/oci-public/`
2. **`push-oci.sh`** — Standalone bash script in the tmi-tf-wh repo for building and pushing the container to OCIR
3. **Terraform changes** — Add tmi-ux OCIR repository resource; add `TMI_WEBHOOK_ALLOW_HTTP_TARGETS` via `extra_env_vars`
4. **Deployment runbook** — Ordered commands for the full clean deploy

### Out of Scope

- TLS for tmi-tf-wh (not needed; internal ClusterIP with HTTP allowed via env var)
- New load balancers for tmi-tf-wh
- CI/CD automation
- Changes to tmi-tf-wh Dockerfile (already uses Oracle Linux 9)

## Architecture

### Deployment Topology

```
OCI Internal Load Balancers (Flexible, 10 Mbps)
├── tmi-api LB (port 80 -> pod 8080)
└── tmi-ux LB  (port 80 -> pod 8080)

Internal (ClusterIP only):
├── tmi-redis (port 6379)
└── tmi-tf-wh (port 8080) ← called by tmi-api via webhook subscription
```

Note: Both load balancers are configured as **internal** (private IP within VCN).
They are not directly internet-accessible. External access requires either:
- Changing `oci-load-balancer-internal` to `false` in the K8s service annotations
- Adding a public API gateway or reverse proxy
- Using VPN/bastion host access

For this initial deployment, access will be via the internal VCN network.

TMI calls tmi-tf-wh via its webhook delivery system. Since both are in the same K8s cluster, tmi-tf-wh runs as a ClusterIP service at `http://tmi-tf-wh:8080`. TMI's webhook URL validator normally requires HTTPS, but the `TMI_WEBHOOK_ALLOW_HTTP_TARGETS` environment variable relaxes this for intra-cluster communication.

### OCIR Repositories

Created by Terraform (`oci_artifacts_container_repository` resources):
- `tmi/tmi` — server (always created)
- `tmi/tmi-redis` — redis (always created)
- `tmi/tmi-ux` — frontend (conditional on `tmi_ux_enabled`) **NEW: must be added to Terraform**
- `tmi/tmi-tf-wh` — webhook analyzer (conditional on `tmi_tf_wh_enabled`)

Registry URL pattern: `iad.ocir.io/idqeh6gjpmoe/tmi/<component>:latest`

### IAM Policies

When `tmi_tf_wh_enabled = true`, the Terraform automatically adds:
- `use queues` — for OCI Queue job dispatch (scoped to the specific queue OCID)
- `manage queue-messages` — for reading/writing queue messages (scoped to the specific queue OCID)
- `use generative-ai-family` — for OCI Generative AI (compartment-scoped)

These are granted to the OKE workload identity dynamic group within the tmi compartment.

## Deliverable Details

### 1. terraform.tfvars

Target: `terraform/environments/oci-public/terraform.tfvars`

```hcl
# OCI Identity
tenancy_ocid       = "ocid1.tenancy.oc1..aaaaaaaaxlnh6tajzzylq7mm7mwu2wtf62ac2gvbrswbqlftnjwpmkq4pyka"
compartment_id     = "ocid1.compartment.oc1..aaaaaaaagclwz3dtsb5z3kcqxbxyajrpq3zhk6usz5vwuhey6naxbm5aikqq"
region             = "us-ashburn-1"
oci_config_profile = "tmi"

# OKE Node Image (ARM64, Oracle Linux 8.10, K8s v1.34.2)
node_image_id = "ocid1.image.oc1.iad.aaaaaaaamiw5rdac6ixhhtucfgs4xv25wioklldmhe6tcd6hlot6uba7yyga"

# Container Images (OCIR)
tmi_image_url   = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi:latest"
redis_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-redis:latest"

# TMI-UX Frontend
tmi_ux_enabled   = true
tmi_ux_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-ux:latest"

# TMI-TF-WH Webhook Analyzer
tmi_tf_wh_enabled   = true
tmi_tf_wh_image_url = "iad.ocir.io/idqeh6gjpmoe/tmi/tmi-tf-wh:latest"

# Deployer Environment Variables
extra_env_vars = {
  "TMI_WEBHOOK_ALLOW_HTTP_TARGETS" = "true"
  "TMI_ADMIN_PROVIDER"             = "tmi"
  "TMI_ADMIN_EMAIL"                = "charlie@tmi.local"
}

# Secrets: omitted — Terraform auto-generates random values for:
#   db_password (20 chars), redis_password (24 chars), jwt_secret (64 chars)
# Generated values stored in Terraform state and OCI Vault.
```

The existing `configmap_defaults` in main.tf already provides:
- `TMI_AUTH_BUILD_MODE = "dev"`
- `TMI_AUTH_AUTO_PROMOTE_FIRST_USER = "true"`
- `TMI_AUTH_EVERYONE_IS_A_REVIEWER = "true"`
- `TMI_SERVER_INTERFACE = "0.0.0.0"`, `TMI_SERVER_PORT = "8080"`
- `TMI_LOGGING_ALSO_LOG_TO_CONSOLE = "true"`
- `TMI_LOGGING_LOG_API_REQUESTS = "true"`, `TMI_LOGGING_LOG_API_RESPONSES = "true"`
- `TMI_LOGGING_LOG_WEBSOCKET_MESSAGES = "true"`
- `TMI_LOGGING_REDACT_AUTH_TOKENS = "true"`
- `TMI_LOGGING_SUPPRESS_UNAUTHENTICATED_LOGS = "true"`

### 2. push-oci.sh for tmi-tf-wh

Target: `/Users/efitz/Projects/tmi-tf-wh/scripts/push-oci.sh`

Standalone bash script following the tmi-ux push-oci.sh pattern:

**Features:**
- Prerequisites check: docker, oci CLI, jq
- OCIR repository discovery: `CONTAINER_REPO_OCID` env var, then search by compartment (`OCI_COMPARTMENT_ID`), then tenancy-wide fallback
- Docker authentication: credential helper with manual login fallback
- Build: `docker buildx build --platform linux/arm64` using existing `Dockerfile`
- Tag: `:latest` plus version from `pyproject.toml`
- Push to OCIR

**Options:**
- `--region REGION` (default: us-ashburn-1)
- `--repo-ocid OCID` (override auto-discovery)
- `--tag TAG` (default: latest)
- `--platform PLATFORM` (default: linux/arm64)
- `--no-cache` (build without Docker cache)

**Invocation:**
```bash
cd /Users/efitz/Projects/tmi-tf-wh
./scripts/push-oci.sh
# or with overrides:
OCI_COMPARTMENT_ID=tmi ./scripts/push-oci.sh --tag v0.1.0
```

### 3. Terraform Changes

**3a. Add tmi-ux OCIR repository** to `terraform/environments/oci-public/main.tf`:

```hcl
resource "oci_artifacts_container_repository" "tmi_ux" {
  count          = var.tmi_ux_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-ux"
  is_public      = true
}
```

Also add to `terraform/environments/oci-private/main.tf` with `is_public = false`.

**3b. TMI_WEBHOOK_ALLOW_HTTP_TARGETS** — No module change needed. Passed via `extra_env_vars` in `terraform.tfvars`, which merges into the TMI ConfigMap through the existing `extra_environment_variables` mechanism.

### 4. Deployment Runbook

**Prerequisites:**
- OCI CLI configured with profile `tmi`
- Docker running with buildx support
- Terraform >= 1.5.0 installed
- `pnpm` installed (for tmi-ux)

**Sequence:**

```bash
# Step 1: Deploy all infrastructure (VCN, OKE, ADB, Vault, OCIR repos, K8s resources)
# K8s pods will go into ImagePullBackOff until images are pushed.
cd /Users/efitz/Projects/tmi
make deploy-oci

# Step 2: Configure kubectl
# (use the command from terraform output kubernetes_config_command)
oci ce cluster create-kubeconfig --cluster-id <from-output> --region us-ashburn-1 --token-version 2.0.0 --profile tmi

# Step 3: Build & push TMI server + Redis containers
cd /Users/efitz/Projects/tmi
make build-app-oci

# Step 4: Build & push TMI-UX container
cd /Users/efitz/Projects/tmi-ux
pnpm run build:oci && pnpm run deploy:oci

# Step 5: Build & push TMI-TF-WH container
cd /Users/efitz/Projects/tmi-tf-wh
./scripts/push-oci.sh

# Step 6: Verify pods recover (Kubernetes auto-retries image pulls)
kubectl get pods -n tmi -w

# Step 7: Post-deployment configuration
# - Log in as charlie (first login auto-promotes to admin)
# - Set quotas via API
# - Create webhook subscription pointing tmi-tf-wh at http://tmi-tf-wh:8080/webhook
```

**Deployment ordering rationale:** Single `terraform apply` creates everything including K8s deployments. Pods enter ImagePullBackOff because images don't exist yet. After pushing images, Kubernetes automatically retries and pods start. This is simpler than multi-phase Terraform with `-target` flags.

## tmi-tf-wh Environment Configuration

The Terraform ConfigMap (`tmi-tf-wh-config`) provides:
- `LLM_PROVIDER = "oci"` — uses OCI Generative AI (no API keys needed)
- `OCI_COMPARTMENT_ID` — for Generative AI access
- `TMI_SERVER_URL = "http://tmi-api:8080"` — internal cluster URL
- `TMI_OAUTH_IDP = "tmi"` — authenticates with TMI server
- `TMI_CLIENT_PATH = "/opt/tmi-client"` — path to TMI Python client
- `QUEUE_OCID` — OCI Queue for job dispatch
- `VAULT_OCID` — OCI Vault for secrets
- `MAX_CONCURRENT_JOBS = "3"`, `JOB_TIMEOUT = "3600"`, `SERVER_PORT = "8080"`

OKE Workload Identity provides keyless authentication to OCI services (Vault, Queue, Generative AI).

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| OCIR repo creation | Terraform-managed | Already implemented for tmi/redis/tmi-tf-wh; tmi-ux repo must be added |
| Load balancers | Internal (private VCN) | Matches existing oci-public config; external access deferred |
| Deployment ordering | Single apply, then push images | Simpler; K8s auto-retries image pulls |
| TLS for tmi-tf-wh | Not needed | Internal ClusterIP; `TMI_WEBHOOK_ALLOW_HTTP_TARGETS=true` |
| LLM provider | OCI Generative AI | Native integration via workload identity; no API keys |
| Push script style | Standalone bash in tmi-tf-wh | Matches tmi-ux pattern; keeps repos self-contained |
| Secrets | Terraform auto-generated | `random_password` resources; stored in state + OCI Vault |
| Admin user | charlie@tmi.local via env vars | `TMI_ADMIN_PROVIDER=tmi`, `TMI_ADMIN_EMAIL=charlie@tmi.local` |
