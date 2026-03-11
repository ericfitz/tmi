# TMI AWS Production Deployment
# This configuration deploys TMI on EKS (Elastic Kubernetes Service) with Fargate

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = ">= 2.25.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
    tls = {
      source  = "hashicorp/tls"
      version = ">= 4.0.0"
    }
  }

  # Uncomment and configure for remote state
  # backend "s3" {
  #   bucket         = "tmi-terraform-state"
  #   key            = "aws-production/terraform.tfstate"
  #   region         = "us-east-1"
  #   encrypt        = true
  #   dynamodb_table = "tmi-terraform-locks"
  # }
}

# AWS Provider configuration
provider "aws" {
  region = var.region
}

# Kubernetes Provider - configured after EKS cluster creation
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = module.kubernetes.cluster_ca_certificate
  token                  = data.aws_eks_cluster_auth.tmi.token
}

# Helm Provider - for installing AWS Load Balancer Controller
provider "helm" {
  kubernetes {
    host                   = module.kubernetes.cluster_endpoint
    cluster_ca_certificate = module.kubernetes.cluster_ca_certificate
    token                  = data.aws_eks_cluster_auth.tmi.token
  }
}

# EKS cluster auth for kubernetes provider
data "aws_eks_cluster_auth" "tmi" {
  name = "${var.name_prefix}-eks"
}

# Generate random passwords if not provided
resource "random_password" "db_password" {
  count            = var.db_password == null ? 1 : 0
  length           = 20
  special          = true
  override_special = "#$%&*()-_=+[]{}|:,.?"
}

resource "random_password" "redis_password" {
  count   = var.redis_password == null ? 1 : 0
  length  = 24
  special = false
}

resource "random_password" "jwt_secret" {
  count   = var.jwt_secret == null ? 1 : 0
  length  = 64
  special = false
}

locals {
  db_password    = var.db_password != null ? var.db_password : random_password.db_password[0].result
  redis_password = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret     = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result

  tags = merge(var.tags, {
    project     = "tmi"
    environment = "production"
    managed_by  = "terraform"
  })
}

# Network Module
module "network" {
  source = "../../modules/network/aws"

  name_prefix           = var.name_prefix
  vpc_cidr              = var.vpc_cidr
  public_subnet_cidrs   = var.public_subnet_cidrs
  private_subnet_cidrs  = var.private_subnet_cidrs
  database_subnet_cidrs = var.database_subnet_cidrs
  enable_multi_az_nat   = var.enable_multi_az_nat

  eks_api_authorized_cidrs = var.eks_api_authorized_cidrs

  tags = local.tags
}

# Database Module
module "database" {
  source = "../../modules/database/aws"

  name_prefix            = var.name_prefix
  db_name                = var.db_name
  db_username            = var.db_username
  db_password            = local.db_password
  engine_version         = var.db_engine_version
  instance_class         = var.db_instance_class
  allocated_storage      = var.db_allocated_storage
  max_allocated_storage  = var.db_max_allocated_storage
  multi_az               = var.db_multi_az
  db_subnet_group_name   = module.network.db_subnet_group_name
  vpc_security_group_ids = [module.network.rds_security_group_id]
  deletion_protection    = var.db_deletion_protection
  skip_final_snapshot    = var.db_skip_final_snapshot

  tags = local.tags
}

# Secrets Module
module "secrets" {
  source = "../../modules/secrets/aws"

  name_prefix    = var.name_prefix
  db_username    = var.db_username
  db_password    = local.db_password
  redis_password = local.redis_password
  jwt_secret     = local.jwt_secret

  create_combined_secret = true
  create_kms_key         = true

  tags = local.tags
}

# Logging Module
module "logging" {
  source = "../../modules/logging/aws"

  name_prefix    = var.name_prefix
  retention_days = 30

  create_archive_bucket   = true
  archive_transition_days = 90
  archive_retention_days  = 365
  create_alert_topic      = var.alert_email != null
  alert_email             = var.alert_email
  create_alarms           = var.alert_email != null

  tags = local.tags

  depends_on = [module.secrets]
}

# Kubernetes (EKS) Module
module "kubernetes" {
  source = "../../modules/kubernetes/aws"

  name_prefix        = var.name_prefix
  kubernetes_version = var.kubernetes_version
  aws_region         = var.region

  # Network configuration
  vpc_id                 = module.network.vpc_id
  private_subnet_ids     = module.network.private_subnet_ids
  public_subnet_ids      = module.network.public_subnet_ids
  fargate_subnet_ids     = module.network.private_subnet_ids
  eks_security_group_ids = [module.network.eks_node_security_group_id]
  authorized_cidrs       = var.eks_api_authorized_cidrs

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url
  tmi_replicas    = var.tmi_replicas

  # Redis configuration
  redis_password = local.redis_password

  # TMI-UX Frontend configuration (optional)
  tmi_ux_enabled   = var.tmi_ux_enabled
  tmi_ux_image_url = var.tmi_ux_image_url

  # Database configuration (PostgreSQL)
  db_username = var.db_username
  db_password = local.db_password
  db_endpoint = module.database.db_instance_endpoint
  db_name     = var.db_name

  # Secrets configuration
  secrets_secret_name = module.secrets.combined_secret_name
  jwt_secret          = local.jwt_secret
  kms_key_arn         = module.secrets.kms_key_arn

  # SSL and domain configuration (optional)
  enable_ingress      = var.enable_certificate_automation && var.server_domain != null
  ssl_certificate_arn = var.enable_certificate_automation ? module.certificates[0].certificate_arn : null
  server_domain       = var.server_domain
  ux_domain           = var.ux_domain

  # Build mode
  tmi_build_mode = var.tmi_build_mode

  # Cloud logging - wire to CloudWatch
  cloudwatch_log_group = module.logging.app_log_group_name
  cloud_log_level      = "info"
  logging_policy_arn   = module.logging.logging_policy_arn

  tags = local.tags

  depends_on = [module.network, module.database, module.secrets, module.logging]
}

# Certificates Module (optional - enabled when domain_name is set)
module "certificates" {
  source = "../../modules/certificates/aws"
  count  = var.enable_certificate_automation ? 1 : 0

  name_prefix = var.name_prefix
  domain_name = var.domain_name

  subject_alternative_names = var.subject_alternative_names
  zone_id                   = var.dns_zone_id
  wait_for_validation       = true

  tags = local.tags
}

# DNS Module (Route 53 records pointing domains to ALB)
module "dns" {
  source = "../../modules/dns/aws"
  count  = var.server_domain != null && var.dns_zone_id != null ? 1 : 0

  zone_id       = var.dns_zone_id
  server_domain = var.server_domain
  ux_domain     = var.tmi_ux_enabled ? var.ux_domain : null
  alb_dns_name  = module.kubernetes.load_balancer_hostname

  tags = local.tags

  depends_on = [module.kubernetes]
}
