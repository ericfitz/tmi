# TMI OCI Logging Test Deployment
# Deploys only: network, logging, kubernetes, IAM
# Purpose: Verify OKE pod -> OCI Logging pipeline works
# Database is NOT deployed; TMI app will crash after cloud logging init

terraform {
  required_version = ">= 1.5.0"

  required_providers {
    oci = {
      source  = "oracle/oci"
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
  }
}

# OCI Provider - uses ~/.oci/config with "tmi" profile
provider "oci" {
  region              = var.region
  config_file_profile = "tmi"
}

# Kubernetes Provider - configured after OKE cluster creation
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = module.kubernetes.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "oci"
    args        = ["ce", "cluster", "generate-token", "--cluster-id", module.kubernetes.cluster_id, "--region", var.region, "--profile", "tmi"]
  }
}

# Generate dummy secrets (not used for real DB, just to satisfy module inputs)
resource "random_password" "dummy_db_password" {
  length  = 20
  special = false
}

resource "random_password" "redis_password" {
  length  = 24
  special = false
}

resource "random_password" "jwt_secret" {
  length  = 64
  special = false
}

locals {
  tags = {
    project     = "tmi"
    environment = "logging-test"
    managed_by  = "terraform"
    purpose     = "verify-oci-logging"
  }
}

# Get Object Storage namespace
data "oci_objectstorage_namespace" "ns" {
  compartment_id = var.compartment_id
}

# Network Module (required by kubernetes)
module "network" {
  source = "../../modules/network/oci"

  compartment_id       = var.compartment_id
  name_prefix          = var.name_prefix
  dns_label            = var.dns_label
  vcn_cidr             = var.vcn_cidr
  public_subnet_cidr   = var.public_subnet_cidr
  private_subnet_cidr  = var.private_subnet_cidr
  database_subnet_cidr = var.database_subnet_cidr

  # OKE-specific subnets
  oke_api_subnet_cidr      = var.oke_api_subnet_cidr
  oke_pod_subnet_cidr      = var.oke_pod_subnet_cidr
  oke_api_authorized_cidrs = ["0.0.0.0/0"]

  tags = local.tags
}

# Logging Module
module "logging" {
  source = "../../modules/logging/oci"

  compartment_id           = var.compartment_id
  tenancy_ocid             = var.tenancy_ocid
  name_prefix              = var.name_prefix
  object_storage_namespace = data.oci_objectstorage_namespace.ns.namespace
  create_oke_log           = true
  create_container_log     = true
  oke_cluster_id           = module.kubernetes.cluster_id

  retention_days         = 30
  archive_retention_days = 90
  create_archive_bucket  = false # Skip for test
  create_alert_topic     = false
  create_alarms          = false

  tags = local.tags

  depends_on = [module.kubernetes]
}

# Kubernetes (OKE) Module
module "kubernetes" {
  source = "../../modules/kubernetes/oci"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # OKE cluster configuration
  kubernetes_version = var.kubernetes_version
  node_count         = 1
  node_shape         = var.node_shape
  node_ocpus         = var.node_ocpus
  node_memory_gbs    = var.node_memory_gbs
  node_image_id      = var.node_image_id

  # Network configuration
  vcn_id               = module.network.vcn_id
  oke_api_subnet_id    = module.network.oke_api_subnet_id
  oke_worker_subnet_id = module.network.private_subnet_id
  oke_pod_subnet_id    = module.network.oke_pod_subnet_id
  public_subnet_ids    = [module.network.public_subnet_id]
  oke_api_nsg_ids      = [module.network.oke_api_nsg_id]
  oke_pod_nsg_ids      = [module.network.oke_pod_nsg_id]
  lb_nsg_ids           = [module.network.lb_nsg_id]

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url
  tmi_replicas    = 1

  # Redis configuration
  redis_password = random_password.redis_password.result

  # Dummy database configuration (app will fail to connect, but logging via stdout works)
  db_username           = "ADMIN"
  db_password           = random_password.dummy_db_password.result
  oracle_connect_string = "dummy_high"
  wallet_base64         = base64encode("dummy-wallet-for-logging-test")

  # Secrets configuration (no vault in test)
  vault_ocid = ""
  jwt_secret = random_password.jwt_secret.result

  # Load Balancer configuration
  lb_min_bandwidth_mbps = 10
  lb_max_bandwidth_mbps = 10

  # Build mode
  tmi_build_mode = "dev"

  tags = local.tags

  depends_on = [module.network]
}

# Outputs
output "cluster_id" {
  value = module.kubernetes.cluster_id
}

output "cluster_endpoint" {
  value = module.kubernetes.cluster_endpoint
}

output "log_group_id" {
  value = module.logging.log_group_id
}

output "app_log_id" {
  value = module.logging.app_log_id
}

output "load_balancer_ip" {
  value = module.kubernetes.load_balancer_ip
}
