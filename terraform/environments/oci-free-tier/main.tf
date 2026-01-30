# TMI OCI Free Tier Deployment
# This configuration deploys TMI using OCI Always Free resources

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
  # auth   = "InstancePrincipal"  # Uncomment for IMDS authentication
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
    environment = "free-tier"
    managed_by  = "terraform"
  })
}

# Get Object Storage namespace
data "oci_objectstorage_namespace" "ns" {
  compartment_id = var.compartment_id
}

# Network Module
module "network" {
  source = "../../modules/network/oci"

  compartment_id       = var.compartment_id
  name_prefix          = var.name_prefix
  dns_label            = var.dns_label
  vcn_cidr             = var.vcn_cidr
  public_subnet_cidr   = var.public_subnet_cidr
  private_subnet_cidr  = var.private_subnet_cidr
  database_subnet_cidr = var.database_subnet_cidr
  tags                 = local.tags
}

# Database Module
module "database" {
  source = "../../modules/database/oci"

  compartment_id           = var.compartment_id
  region                   = var.region
  name_prefix              = var.name_prefix
  db_name                  = var.db_name
  admin_password           = local.db_password
  database_subnet_id       = module.network.database_subnet_id
  database_nsg_ids         = [module.network.database_nsg_id]
  object_storage_namespace = data.oci_objectstorage_namespace.ns.namespace

  # Free tier settings
  is_free_tier                        = true
  cpu_core_count                      = 1
  compute_count                       = 1  # Free tier only supports 1 ECPU
  data_storage_size_in_tbs            = 1
  is_auto_scaling_enabled             = false
  is_auto_scaling_for_storage_enabled = false
  prevent_destroy                     = var.prevent_database_destroy

  tags = local.tags
}

# Secrets Module
module "secrets" {
  source = "../../modules/secrets/oci"

  compartment_id = var.compartment_id
  tenancy_ocid   = var.tenancy_ocid
  name_prefix    = var.name_prefix

  db_username    = var.db_username
  db_password    = local.db_password
  redis_password = local.redis_password
  jwt_secret     = local.jwt_secret

  create_combined_secret = true
  create_dynamic_group   = true

  tags = local.tags
}

# Logging Module
module "logging" {
  source = "../../modules/logging/oci"

  compartment_id           = var.compartment_id
  tenancy_ocid             = var.tenancy_ocid
  name_prefix              = var.name_prefix
  object_storage_namespace = data.oci_objectstorage_namespace.ns.namespace

  retention_days         = 30
  archive_retention_days = 90
  create_archive_bucket  = true
  create_alert_topic     = var.alert_email != null
  alert_email            = var.alert_email
  create_alarms          = var.alert_email != null
  create_dynamic_group   = true

  tags = local.tags

  depends_on = [module.secrets]
}

# Compute Module
module "compute" {
  source = "../../modules/compute/oci"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # Network configuration
  private_subnet_id = module.network.private_subnet_id
  public_subnet_ids = [module.network.public_subnet_id]
  tmi_nsg_ids       = [module.network.tmi_server_nsg_id]
  redis_nsg_ids     = [module.network.redis_nsg_id]
  lb_nsg_ids        = [module.network.lb_nsg_id]

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url

  # TMI Server configuration (Free tier: 1 OCPU, 4 GB RAM)
  tmi_shape     = "CI.Standard.E4.Flex"
  tmi_ocpus     = 1
  tmi_memory_gb = 4

  # Redis configuration (Free tier: 1 OCPU, 2 GB RAM)
  redis_shape     = "CI.Standard.E4.Flex"
  redis_ocpus     = 1
  redis_memory_gb = 2
  redis_password  = local.redis_password

  # Database configuration
  db_username           = var.db_username
  db_password           = local.db_password
  oracle_connect_string = "${var.db_name}_high"
  wallet_base64         = module.database.wallet_content_base64

  # Secrets configuration
  vault_ocid = module.secrets.vault_id

  # Load Balancer configuration (Free tier: 10 Mbps)
  lb_min_bandwidth_mbps = 10
  lb_max_bandwidth_mbps = 10

  # SSL configuration (optional)
  ssl_certificate_pem    = var.ssl_certificate_pem
  ssl_private_key_pem    = var.ssl_private_key_pem
  ssl_ca_certificate_pem = var.ssl_ca_certificate_pem
  enable_http_redirect   = var.ssl_certificate_pem != null

  tags = local.tags

  depends_on = [module.network, module.database, module.secrets]
}

# Update logging module with container instance ID
# Note: Container instance logging requires the service name "oci-containerinstances"
# TODO: Re-enable when the correct logging configuration is verified
# resource "oci_logging_log" "container_logs" {
#   display_name = "${var.name_prefix}-container"
#   log_group_id = module.logging.log_group_id
#   log_type     = "SERVICE"
#
#   configuration {
#     compartment_id = var.compartment_id
#
#     source {
#       category    = "all"
#       resource    = module.compute.tmi_container_instance_id
#       service     = "oci-containerinstances"
#       source_type = "OCISERVICE"
#     }
#   }
#
#   is_enabled         = true
#   retention_duration = 30
#
#   freeform_tags = local.tags
# }
