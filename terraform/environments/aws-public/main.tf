# TMI AWS Public Environment Template
# Low-cost "kick the tires" deployment on AWS
#
# This template creates:
# - VPC with public and private subnets
# - EKS cluster with single t3.medium managed node
# - RDS PostgreSQL (db.t3.micro, no deletion protection)
# - ECR repository for container images
# - Secrets Manager for credentials
# - CloudWatch logging with Fluent Bit
# - ALB (internet-facing) with 3600s idle timeout for WebSocket
#
# Estimated monthly cost: ~$140-150

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
    helm = {
      source = "hashicorp/helm"
      # Pinned below 3.0: the helm provider v3 line replaced the
      # `kubernetes { ... }` nested block used by provider "helm" below with
      # a different schema, breaking `terraform validate`/`init` on an
      # unbounded ">= 2.12.0" constraint. Pre-existing gap, unrelated to
      # this change; fixed here because it blocked this task's validation gate.
      version = ">= 2.12.0, < 3.0.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = ">= 4.0.0"
    }
  }

  # T6/T11 (#344): remote, encrypted state is the default. Bucket / region /
  # lock table are provided via `terraform init -backend-config=<file>` so
  # the state location is per-deployer/environment but the encryption flag
  # and key path are pinned here. Local state is no longer a silent fallback.
  #
  # Example backend.hcl (NOT committed):
  #   bucket         = "tmi-deployer-tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "tmi-tf-locks"
  #
  # Then: terraform init -backend-config=backend.hcl
  backend "s3" {
    key     = "tmi/aws-public/terraform.tfstate"
    encrypt = true
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = ["eks", "get-token", "--cluster-name", module.kubernetes.cluster_name]
  }
}

provider "helm" {
  kubernetes {
    host                   = module.kubernetes.cluster_endpoint
    cluster_ca_certificate = base64decode(module.kubernetes.cluster_ca_certificate)
    exec {
      api_version = "client.authentication.k8s.io/v1beta1"
      command     = "aws"
      args        = ["eks", "get-token", "--cluster-name", module.kubernetes.cluster_name]
    }
  }
}

locals {
  common_tags = merge(var.tags, {
    Project     = "tmi"
    Environment = "public"
    ManagedBy   = "terraform"
  })

  # Components built/pushed by scripts/build-app-containers.py (aws target)
  # and scripts/container_build_helpers.py's `get_target_config("aws", ...)`.
  # Repo names must match what those scripts push:
  #   - server/redis/extractor default to "{name_prefix}-{component}" via
  #     container_build_helpers.py's image_name_prefix pattern.
  #   - controller is special-cased to "tmi-component-controller" (matching
  #     the manifests) rather than the default "{name_prefix}-controller" —
  #     see Task 3 brief.
  #   - chunkembed is special-cased to "tmi-chunk-embed" (hyphenated) to
  #     match the image name used throughout deployments/k8s (e.g.
  #     deployments/k8s/platform/components/tmi-chunk-embed.yml), NOT the
  #     unhyphenated "tmi-chunkembed" that container_build_helpers.py's
  #     default `{prefix}{component}` naming would otherwise produce for the
  #     "chunkembed" component key. container_build_helpers.py's aws target
  #     must add an explicit image_name_map entry so the pushed image name
  #     agrees with this repo name (tracked as a coordination item with
  #     Task 3 — see the Task 4 report).
  ecr_components = ["server", "redis", "extractor", "chunkembed", "controller"]

  ecr_repo_names = {
    server     = "${var.name_prefix}-server"
    redis      = "${var.name_prefix}-redis"
    extractor  = "${var.name_prefix}-extractor"
    chunkembed = "tmi-chunk-embed"
    controller = "tmi-component-controller"
  }
}

# ============================================================================
# ECR Repositories (container registry — one per app/worker component)
# ============================================================================

resource "aws_ecr_repository" "tmi" {
  for_each             = toset(local.ecr_components)
  name                 = local.ecr_repo_names[each.key]
  image_tag_mutability = "MUTABLE"

  # kick-the-tires environment: repos routinely still hold pushed images at
  # teardown time (every `scripts/deploy-aws.sh` deploy re-pushes :latest),
  # and ECR repositories with images in them block a plain `terraform
  # destroy`. force_delete lets destroy remove the repo (and its images)
  # in one step instead of failing partway through. Do not carry this into
  # an environment where image loss on destroy should require a separate,
  # explicit step.
  force_delete = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = local.common_tags
}

# ============================================================================
# Network
# ============================================================================

module "network" {
  source = "../../modules/network/aws"

  name_prefix           = var.name_prefix
  vpc_cidr              = var.vpc_cidr
  enable_public_subnets = true
  alb_ingress_cidr      = "0.0.0.0/0"

  tags = local.common_tags
}

# ============================================================================
# Secrets
# ============================================================================

module "secrets" {
  source = "../../modules/secrets/aws"

  name_prefix = var.name_prefix
  db_username = var.db_username

  tags = local.common_tags
}

# ============================================================================
# Database
# ============================================================================

module "database" {
  source = "../../modules/database/aws"

  name_prefix            = var.name_prefix
  instance_class         = "db.t3.micro"
  db_name                = var.db_name
  db_username            = var.db_username
  db_password            = module.secrets.db_password
  db_subnet_group_name   = module.network.db_subnet_group_name
  vpc_security_group_ids = [module.network.rds_security_group_id]
  deletion_protection    = false
  skip_final_snapshot    = true

  tags = local.common_tags
}

# ============================================================================
# Certificates (ACM, DNS-validated via Route 53)
# ============================================================================

module "certificates" {
  source = "../../modules/certificates/aws"

  name_prefix               = var.name_prefix
  domain_name               = var.domain_name
  hosted_zone_id            = var.hosted_zone_id
  subject_alternative_names = []
  tags                      = local.common_tags
}

# ============================================================================
# Kubernetes (EKS) — infra + bootstrap only (namespace, ConfigMap, Secret,
# ServiceAccount, ALB controller). Workloads are applied separately by the
# deployments/k8s/dev/aws kustomize overlay (deploy script).
# ============================================================================

module "kubernetes" {
  source = "../../modules/kubernetes/aws"

  name_prefix            = var.name_prefix
  node_instance_type     = "t3.medium"
  node_count             = 1
  endpoint_public_access = true

  # Network
  vpc_id                     = module.network.vpc_id
  subnet_ids                 = concat(module.network.public_subnet_ids, module.network.private_subnet_ids)
  node_subnet_ids            = module.network.private_subnet_ids
  cluster_security_group_ids = [module.network.eks_nodes_security_group_id]

  # Secrets Manager ARNs for IRSA
  secret_arns = values(module.secrets.secret_arns)

  # TMI Server (build mode only — image/replicas/ALB are overlay-owned now).
  # production: the TMI "tmi" OAuth provider stays enabled but is restricted to
  # the Client Credentials Grant — the Authorization Code / login_hint flow that
  # mints an ephemeral user with no credential check is disabled at runtime in
  # production (auth/test_provider.go isDevOrTestBuild gate). This is what keeps
  # the public endpoint from handing out anonymous JWTs; it does NOT depend on
  # whether the tmi provider is registered, so it is robust against the
  # replicated DB config re-enabling that provider.
  tmi_build_mode = "production"

  # Grant every authenticated user security-reviewer capability. Deliberately
  # decoupled from build_mode (a plain runtime flag) so it stays on under
  # production build mode.
  everyone_is_a_reviewer = true

  # Redis (password only — feeds the tmi-secrets Secret; image is overlay-owned)
  redis_password = module.secrets.redis_password

  # Database
  db_host     = module.database.host
  db_port     = module.database.port
  db_name     = var.db_name
  db_username = var.db_username
  db_password = module.secrets.db_password

  # JWT
  jwt_secret = module.secrets.jwt_secret

  # Extra env vars from deployer
  extra_environment_variables = var.extra_env_vars

  tags = local.common_tags
}

# ============================================================================
# Logging
# ============================================================================

module "logging" {
  source = "../../modules/logging/aws"

  name_prefix       = var.name_prefix
  retention_days    = 30
  oidc_provider_arn = module.kubernetes.oidc_provider_arn
  oidc_provider_url = replace(module.kubernetes.cluster_oidc_issuer_url, "https://", "")

  tags = local.common_tags
}
