# TMI OCI Free Development Deployment
# Zero-cost architecture: E5.Flex VM with PostgreSQL + Redis + TMI + TMI-UX in Podman containers
# No Oracle ADB, No Load Balancer, No OCI Vault, No OCI Bastion

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
    time = {
      source  = "hashicorp/time"
      version = ">= 0.9.0"
    }
  }

  # Uncomment and configure for remote state
  # backend "s3" {
  #   bucket   = "tmi-terraform-state"
  #   key      = "oci-free-tier/terraform.tfstate"
  #   region   = "us-east-1"
  #   encrypt  = true
  # }
}

# OCI Provider configuration
# Uses IMDS or ~/.oci/config for authentication
provider "oci" {
  region = var.region
}

# Generate random passwords if not provided
resource "random_password" "postgres_password" {
  count            = var.postgres_password == null ? 1 : 0
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

resource "random_password" "oauth_client_secret" {
  count   = var.oauth_client_secret == null ? 1 : 0
  length  = 48
  special = false
}

locals {
  postgres_password   = var.postgres_password != null ? var.postgres_password : random_password.postgres_password[0].result
  redis_password      = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret          = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result
  oauth_client_secret = var.oauth_client_secret != null ? var.oauth_client_secret : random_password.oauth_client_secret[0].result

  tags = merge(var.tags, {
    project     = "tmi"
    environment = "free-dev"
    managed_by  = "terraform"
  })
}

# Network Module
module "network" {
  source = "../../modules/network/oci-vm"

  compartment_id       = var.compartment_id
  name_prefix          = var.name_prefix
  dns_label            = var.dns_label
  vcn_cidr             = var.vcn_cidr
  public_subnet_cidr   = var.public_subnet_cidr
  private_subnet_cidr  = var.private_subnet_cidr
  database_subnet_cidr = var.database_subnet_cidr
  tags                 = local.tags
}

# Compute Module (A1.Flex ARM64 VM in public subnet with direct public IP)
module "compute" {
  source = "../../modules/compute/oci-vm"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # Network configuration — VM in public subnet with direct public IP
  public_subnet_id = module.network.public_subnet_id
  tmi_nsg_ids      = [module.network.tmi_server_nsg_id]
  redis_nsg_ids    = [module.network.redis_nsg_id]

  # Container images (linux/arm64, built natively on Apple Silicon)
  tmi_image_url      = var.tmi_image_url
  postgres_image_url = var.postgres_image_url
  redis_docker_image = var.redis_image_url
  tmi_ux_image_url   = var.tmi_ux_image_url
  tmi_ux_api_url     = var.tmi_ux_api_url

  # OCIR credentials for cloud-init to pull private images
  ocir_username   = var.ocir_username
  ocir_auth_token = var.ocir_auth_token

  # SSH key for direct VM access (replaces OCI Bastion)
  ssh_authorized_keys = var.ssh_authorized_keys

  # VM sizing (A1.Flex — 2 OCPU, 8 GB, Always Free)
  vm_ocpus            = 2
  vm_memory_gb        = 8
  boot_volume_size_gb = 50

  # Database configuration (PostgreSQL in container)
  postgres_password = local.postgres_password

  # Redis password (Redis runs as Podman container on the same VM)
  redis_password = local.redis_password

  # Auth secrets
  jwt_secret          = local.jwt_secret
  oauth_client_secret = local.oauth_client_secret

  tags = local.tags

  depends_on = [module.network]
}
