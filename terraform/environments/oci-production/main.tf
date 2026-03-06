# TMI OCI Production Deployment
# This configuration deploys TMI on OKE (Oracle Kubernetes Engine) with private endpoints

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

# Kubernetes Provider - configured after OKE cluster creation
# Uses OCI CLI for token authentication
#
# NOTE: insecure = true is required because the OKE API server certificate
# (CN=kubernetes.default) does not pass Go's strict RFC 5280 compliance checks
# (introduced in Go 1.21+), even though OpenSSL/curl verifies it successfully.
# Authentication remains secure via OCI token (exec plugin). This is the standard
# workaround for OKE clusters where the API server cert uses legacy certificate fields.
provider "kubernetes" {
  host     = module.kubernetes.cluster_endpoint
  insecure = true

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "oci"
    args        = ["ce", "cluster", "generate-token", "--cluster-id", module.kubernetes.cluster_id, "--region", var.region]
  }
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

  # OKE-specific subnets
  oke_api_subnet_cidr      = var.oke_api_subnet_cidr
  oke_pod_subnet_cidr      = var.oke_pod_subnet_cidr
  oke_api_authorized_cidrs = var.oke_api_authorized_cidrs

  tags = local.tags
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

# Kubernetes (OKE) Module - replaces compute module
module "kubernetes" {
  source = "../../modules/kubernetes/oci"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # OKE cluster configuration
  kubernetes_version     = var.kubernetes_version
  virtual_node_count     = var.virtual_node_count
  virtual_node_pod_shape = var.virtual_node_pod_shape

  # Network configuration
  vcn_id            = module.network.vcn_id
  oke_api_subnet_id = module.network.oke_api_subnet_id
  oke_pod_subnet_id = module.network.oke_pod_subnet_id
  public_subnet_ids = [module.network.public_subnet_id]
  oke_api_nsg_ids   = [module.network.oke_api_nsg_id]
  oke_pod_nsg_ids   = [module.network.oke_pod_nsg_id]
  lb_nsg_ids        = [module.network.lb_nsg_id]

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url
  tmi_replicas    = var.tmi_replicas

  # Redis configuration
  redis_password = local.redis_password

  # TMI-UX Frontend configuration (optional)
  tmi_ux_enabled   = var.tmi_ux_enabled
  tmi_ux_image_url = var.tmi_ux_image_url

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

  # Build mode
  tmi_build_mode = var.tmi_build_mode

  # Cloud logging - disabled until Container Instance Instance Principal IAM is confirmed
  # TODO: re-enable once dynamic group matching rule includes Container Instances
  # (currently matches resource.type='cluster' which may not cover Container Instances)
  # The slogging.NewOCICloudWriter() call hangs when IMDS auth fails on Container Instances
  oci_log_id      = null
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
  # Note: When using OKE, the LB is provisioned by the Kubernetes Service.
  # The certificate module may need adaptation to discover the LB OCID.
  load_balancer_id = null # TODO: Retrieve OCI LB OCID from K8s-provisioned LB

  # Vault Configuration
  vault_id     = module.secrets.vault_id
  vault_key_id = module.secrets.master_key_id

  # Function Configuration
  certmgr_image_url = var.certmgr_image_url

  # IAM Configuration
  create_dynamic_group = true

  tags = local.tags

  depends_on = [module.network, module.secrets, module.kubernetes]
}

# Load Balancer Logging
# The OCI Load Balancer is auto-provisioned by the Kubernetes Service.
# LB access/error logs require the OCI LB OCID which is managed by the OKE CCM.
# OKE captures container stdout/stderr natively via the OCI Logging service.

# Dynamic Group and IAM Policies
# Created at environment level (not in modules) to match specific resource IDs.

# Dynamic group for OKE cluster workloads
resource "oci_identity_dynamic_group" "tmi_oke" {
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-oke-workloads"
  description    = "Dynamic group for TMI OKE cluster workloads"

  matching_rule = "ALL {resource.type = 'cluster', resource.compartment.id = '${var.compartment_id}'}"

  freeform_tags = local.tags
}

# Policy: OKE workload vault access
resource "oci_identity_policy" "vault_access" {
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-vault-access"
  description    = "Allow TMI OKE workloads to read secrets from Vault"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}"
  ]

  freeform_tags = local.tags
}

# Policy: OKE workload logging access
resource "oci_identity_policy" "logging_access" {
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-logging-policy"
  description    = "Allow TMI OKE workloads to write logs"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use log-content in compartment id ${var.compartment_id}"
  ]

  freeform_tags = local.tags
}
