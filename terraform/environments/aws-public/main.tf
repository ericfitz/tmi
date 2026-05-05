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
      source  = "hashicorp/helm"
      version = ">= 2.12.0"
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
}

# ============================================================================
# ECR Repository (container registry)
# ============================================================================

resource "aws_ecr_repository" "tmi" {
  name                 = "${var.name_prefix}-server"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = local.common_tags
}

resource "aws_ecr_repository" "redis" {
  name                 = "${var.name_prefix}-redis"
  image_tag_mutability = "MUTABLE"

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
# Kubernetes (EKS)
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
  alb_subnet_ids             = module.network.public_subnet_ids
  alb_scheme                 = "internet-facing"

  # Secrets Manager ARNs for IRSA
  secret_arns = values(module.secrets.secret_arns)

  # TMI Server
  tmi_image_url  = "${aws_ecr_repository.tmi.repository_url}:${var.tmi_image_tag}"
  tmi_build_mode = "dev"
  tmi_replicas   = 1

  # Redis
  redis_image_url = "${aws_ecr_repository.redis.repository_url}:${var.redis_image_tag}"
  redis_password  = module.secrets.redis_password

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
