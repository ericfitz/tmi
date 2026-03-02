# TMI OCI Production Deployment
# This configuration deploys TMI using OCI paid-tier resources with private endpoints

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
  #   key      = "oci-production/terraform.tfstate"
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
    environment = "production"
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

  # Paid tier settings - enables private endpoint for ADB
  is_free_tier                        = false
  cpu_core_count                      = 1
  compute_count                       = 2
  data_storage_size_in_tbs            = 1
  is_auto_scaling_enabled             = true
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
  create_dynamic_group   = false

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
  create_dynamic_group   = false

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

  # TMI Server configuration
  tmi_shape     = "CI.Standard.E4.Flex"
  tmi_ocpus     = 1
  tmi_memory_gb = 4

  # Redis configuration
  redis_shape     = "CI.Standard.E4.Flex"
  redis_ocpus     = 1
  redis_memory_gb = 2
  redis_password  = local.redis_password

  # TMI-UX Frontend configuration (optional)
  tmi_ux_enabled   = var.tmi_ux_enabled
  tmi_ux_image_url = var.tmi_ux_image_url
  tmi_ux_nsg_ids   = var.tmi_ux_enabled ? [module.network.tmi_ux_nsg_id] : []
  tmi_ux_shape     = "CI.Standard.E4.Flex"
  tmi_ux_ocpus     = 1
  tmi_ux_memory_gb = 2

  # Hostname routing configuration (required when TMI-UX is enabled)
  api_hostname = var.api_hostname
  ui_hostname  = var.ui_hostname

  # Database configuration
  db_username           = var.db_username
  db_password           = local.db_password
  oracle_connect_string = "${var.db_name}_high"
  wallet_base64         = module.database.wallet_content_base64

  # Secrets configuration
  vault_ocid = module.secrets.vault_id
  jwt_secret = local.jwt_secret

  # Load Balancer configuration
  lb_min_bandwidth_mbps = 10
  lb_max_bandwidth_mbps = 10

  # SSL configuration (optional)
  ssl_certificate_pem    = var.ssl_certificate_pem
  ssl_private_key_pem    = var.ssl_private_key_pem
  ssl_ca_certificate_pem = var.ssl_ca_certificate_pem
  enable_http_redirect   = var.ssl_certificate_pem != null

  # Build mode
  tmi_build_mode = var.tmi_build_mode

  # Cloud logging - wire to OCI Logging service
  oci_log_id      = module.logging.app_log_id
  cloud_log_level = "info"

  tags = local.tags

  depends_on = [module.network, module.database, module.secrets, module.logging]
}

# Certificates Module (optional - enabled when domain_name is set)
module "certificates" {
  source = "../../modules/certificates/oci"
  count  = var.enable_certificate_automation ? 1 : 0

  compartment_id = var.compartment_id
  tenancy_ocid   = var.tenancy_ocid
  name_prefix    = var.name_prefix
  subnet_id      = module.network.private_subnet_id

  # DNS Configuration
  dns_zone_id = var.dns_zone_id
  domain_name = var.domain_name

  # ACME Configuration
  acme_contact_email       = var.acme_contact_email
  acme_directory           = var.acme_directory
  certificate_renewal_days = var.certificate_renewal_days

  # Load Balancer Configuration
  load_balancer_id = module.compute.load_balancer_id

  # Vault Configuration
  vault_id     = module.secrets.vault_id
  vault_key_id = module.secrets.master_key_id

  # Function Configuration
  certmgr_image_url = var.certmgr_image_url

  # IAM Configuration
  create_dynamic_group = true

  tags = local.tags

  depends_on = [module.network, module.secrets, module.compute]
}

# Container Instance and Load Balancer Logging
# These are standalone resources (not in modules) to avoid circular dependencies
# between the logging module (provides app_log_id) and compute module (provides instance IDs).

# TMI+Redis container instance stdout/stderr
resource "oci_logging_log" "container_logs" {
  display_name = "${var.name_prefix}-container"
  log_group_id = module.logging.log_group_id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "all"
      resource    = module.compute.tmi_container_instance_id
      service     = "oci-containerinstances"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = 30

  freeform_tags = local.tags
}

# TMI-UX container instance stdout/stderr (when enabled)
resource "oci_logging_log" "tmi_ux_container_logs" {
  count        = var.tmi_ux_enabled ? 1 : 0
  display_name = "${var.name_prefix}-container-ux"
  log_group_id = module.logging.log_group_id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "all"
      resource    = module.compute.tmi_ux_container_instance_id
      service     = "oci-containerinstances"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = 30

  freeform_tags = local.tags
}

# Load Balancer access logs
resource "oci_logging_log" "lb_access_logs" {
  display_name = "${var.name_prefix}-lb-access"
  log_group_id = module.logging.log_group_id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "access"
      resource    = module.compute.load_balancer_id
      service     = "loadbalancer"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = 30

  freeform_tags = local.tags
}

# Load Balancer error logs
resource "oci_logging_log" "lb_error_logs" {
  display_name = "${var.name_prefix}-lb-error"
  log_group_id = module.logging.log_group_id
  log_type     = "SERVICE"

  configuration {
    compartment_id = var.compartment_id

    source {
      category    = "error"
      resource    = module.compute.load_balancer_id
      service     = "loadbalancer"
      source_type = "OCISERVICE"
    }
  }

  is_enabled         = true
  retention_duration = 30

  freeform_tags = local.tags
}

# Dynamic Group and IAM Policies
# Created at environment level (not in modules) to match specific resource IDs.

# Dynamic group for TMI container instances (specific resource IDs)
resource "oci_identity_dynamic_group" "tmi_containers" {
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-containers"
  description    = "Dynamic group for TMI container instances"

  matching_rule = join("", [
    "ANY {",
    "resource.id = '${module.compute.tmi_container_instance_id}'",
    var.tmi_ux_enabled ? ", resource.id = '${module.compute.tmi_ux_container_instance_id}'" : "",
    "}"
  ])

  freeform_tags = local.tags
}

# Policy: container vault access
resource "oci_identity_policy" "vault_access" {
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-vault-access"
  description    = "Allow TMI containers to read secrets from Vault"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_containers.name} to read secret-family in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_containers.name} to use keys in compartment id ${var.compartment_id}"
  ]

  freeform_tags = local.tags
}

# Policy: container logging access
resource "oci_identity_policy" "logging_access" {
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-logging-policy"
  description    = "Allow TMI containers to write logs"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_containers.name} to use log-content in compartment id ${var.compartment_id}"
  ]

  freeform_tags = local.tags
}
