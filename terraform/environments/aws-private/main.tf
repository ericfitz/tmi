# TMI AWS Private Environment Template
# Internal deployment with no public ingress
#
# This template creates:
# - VPC with private subnets only (no public subnets/IGW on app subnets)
# - EKS cluster with single t3.medium managed node in private subnets
# - RDS PostgreSQL (db.t3.small, deletion protection ON)
# - ECR repository for container images (private by default)
# - Secrets Manager for credentials
# - CloudWatch logging with Fluent Bit
# - Internal ALB (private subnets only)
# - Temporary public EKS API endpoint for provisioning, removed after apply
#
# Estimated monthly cost: ~$150-180
#
# Post-deployment: Deployer must establish connectivity (VPN, bastion, private link)
# for users to reach the internal ALB.

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
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0.0"
    }
  }

  # Uncomment and configure for remote state storage:
  # backend "s3" {
  #   bucket         = "your-terraform-state-bucket"
  #   key            = "tmi/aws-private/terraform.tfstate"
  #   region         = "us-east-1"
  #   encrypt        = true
  #   dynamodb_table = "terraform-locks"
  # }
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
    Environment = "private"
    ManagedBy   = "terraform"
  })
}

# ============================================================================
# ECR Repository (container registry - private by default)
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
# Network (private subnets only, no public subnets)
# ============================================================================

module "network" {
  source = "../../modules/network/aws"

  name_prefix           = var.name_prefix
  vpc_cidr              = var.vpc_cidr
  enable_public_subnets = false
  alb_ingress_cidr      = var.vpc_cidr

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
# Database (db.t3.small with deletion protection)
# ============================================================================

module "database" {
  source = "../../modules/database/aws"

  name_prefix            = var.name_prefix
  instance_class         = "db.t3.small"
  db_name                = var.db_name
  db_username            = var.db_username
  db_password            = module.secrets.db_password
  db_subnet_group_name   = module.network.db_subnet_group_name
  vpc_security_group_ids = [module.network.rds_security_group_id]
  deletion_protection    = true
  skip_final_snapshot    = false

  tags = local.common_tags
}

# ============================================================================
# Kubernetes (EKS - temporary public API endpoint for provisioning)
# ============================================================================

module "kubernetes" {
  source = "../../modules/kubernetes/aws"

  name_prefix            = var.name_prefix
  node_instance_type     = "t3.medium"
  node_count             = 1
  endpoint_public_access = true
  public_access_cidrs    = ["0.0.0.0/0"]

  # Network - all private subnets
  vpc_id                     = module.network.vpc_id
  subnet_ids                 = module.network.private_subnet_ids
  node_subnet_ids            = module.network.private_subnet_ids
  cluster_security_group_ids = [module.network.eks_nodes_security_group_id]
  alb_subnet_ids             = module.network.private_subnet_ids
  alb_scheme                 = "internal"

  # Secrets Manager ARNs for IRSA
  secret_arns = values(module.secrets.secret_arns)

  # TMI Server
  tmi_image_url  = "${aws_ecr_repository.tmi.repository_url}:${var.tmi_image_tag}"
  tmi_build_mode = "production"
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
  retention_days    = 90
  oidc_provider_arn = module.kubernetes.oidc_provider_arn
  oidc_provider_url = replace(module.kubernetes.cluster_oidc_issuer_url, "https://", "")

  tags = local.common_tags
}

# ============================================================================
# Remove public EKS API access after provisioning
# ============================================================================

resource "null_resource" "disable_public_endpoint" {
  triggers = {
    cluster_name = module.kubernetes.cluster_name
    region       = var.aws_region
  }

  provisioner "local-exec" {
    command = <<-EOT
      echo "Disabling public endpoint access on EKS cluster..."
      aws eks update-cluster-config \
        --region ${var.aws_region} \
        --name ${module.kubernetes.cluster_name} \
        --resources-vpc-config endpointPublicAccess=false,endpointPrivateAccess=true
      echo "Public endpoint access disabled. Use VPN or bastion to reach the cluster."
    EOT
  }

  depends_on = [
    module.kubernetes,
    module.logging,
  ]
}
