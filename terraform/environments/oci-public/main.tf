# TMI OCI Public Deployment
# Low-cost "kick the tires" deployment using OCI Always Free tier resources.
# Single node, single TMI replica, no deletion protection.
# Estimated monthly cost: ~$0 (Always Free eligible)

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

  # Uncomment and configure for remote state using OCI Object Storage
  # backend "http" {
  #   address        = "https://objectstorage.<region>.oraclecloud.com/p/<par-token>/n/<namespace>/b/<bucket>/o/oci-public/terraform.tfstate"
  #   update_method  = "PUT"
  # }
}

# OCI Provider configuration
# Uses ~/.oci/config for authentication by default
provider "oci" {
  region              = var.region
  config_file_profile = var.oci_config_profile
  # auth = "InstancePrincipal"  # Uncomment for IMDS authentication
}

# Kubernetes Provider - uses kubeconfig for authentication.
#
# Fresh deployments require two applies (Phase 1: infra, Phase 2: K8s resources).
# The deploy-oci.sh script handles this automatically by providing an empty
# kubeconfig for Phase 1, then generating a real one after the OKE cluster is
# created and active.
provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kubeconfig_context
}

# ---------------------------------------------------------------------------
# Random passwords / secrets
# ---------------------------------------------------------------------------
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

resource "random_password" "oauth_client_secret" {
  length  = 32
  special = false
}

locals {
  db_password    = var.db_password != null ? var.db_password : random_password.db_password[0].result
  redis_password = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret     = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result

  tags = merge(var.tags, {
    project     = "tmi"
    environment = "public"
    managed_by  = "terraform"
  })

  # ConfigMap defaults for public (dev) deployment
  configmap_defaults = {
    TMI_AUTH_BUILD_MODE                       = "dev"
    TMI_AUTH_AUTO_PROMOTE_FIRST_USER          = "true"
    TMI_AUTH_EVERYONE_IS_A_REVIEWER           = "true"
    TMI_LOGGING_ALSO_LOG_TO_CONSOLE           = "true"
    TMI_LOGGING_LOG_API_REQUESTS              = "true"
    TMI_LOGGING_LOG_API_RESPONSES             = "true"
    TMI_LOGGING_LOG_WEBSOCKET_MESSAGES        = "true"
    TMI_LOGGING_REDACT_AUTH_TOKENS            = "true"
    TMI_LOGGING_SUPPRESS_UNAUTHENTICATED_LOGS = "true"
    TMI_SERVER_INTERFACE                      = "0.0.0.0"
    TMI_SERVER_PORT                           = "8080"
    OAUTH_PROVIDERS_TMI_CLIENT_SECRET         = random_password.oauth_client_secret.result
  }
}

# Get Object Storage namespace
data "oci_objectstorage_namespace" "ns" {
  compartment_id = var.compartment_id
}

# ---------------------------------------------------------------------------
# OCIR Container Registry
# ---------------------------------------------------------------------------
resource "oci_artifacts_container_repository" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi"
  is_public      = true

  # Always Free OCIR is public; no cost
}

resource "oci_artifacts_container_repository" "redis" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-redis"
  is_public      = true
}

resource "oci_artifacts_container_repository" "tmi_ux" {
  count          = var.tmi_ux_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-ux"
  is_public      = true
}

resource "oci_artifacts_container_repository" "tmi_tf_wh" {
  count          = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-tf-wh"
  is_public      = true
}

# ---------------------------------------------------------------------------
# Network Module
# ---------------------------------------------------------------------------
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
  oke_public_endpoint      = true
  lb_public                = true

  tags = local.tags
}

# ---------------------------------------------------------------------------
# Database Module (ADB Always Free, 23ai)
# ---------------------------------------------------------------------------
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

  # Always Free tier configuration
  is_free_tier                        = true
  db_version                          = "23ai"
  compute_count                       = 2
  is_auto_scaling_enabled             = false
  is_auto_scaling_for_storage_enabled = false
  deletion_protection                 = false

  tags = local.tags
}

# ---------------------------------------------------------------------------
# Secrets Module
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# Logging Module
# ---------------------------------------------------------------------------
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
  create_archive_bucket  = true
  create_alert_topic     = var.alert_email != null
  alert_email            = var.alert_email
  create_alarms          = var.alert_email != null

  tags = local.tags

  depends_on = [module.secrets, module.kubernetes]
}

# ---------------------------------------------------------------------------
# tmi-tf-wh Queue (optional — enabled when tmi_tf_wh_enabled is true)
# ---------------------------------------------------------------------------
resource "oci_queue_queue" "tmi_tf_wh" {
  count                            = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id                   = var.compartment_id
  display_name                     = "${var.name_prefix}-tf-wh-queue"
  visibility_in_seconds            = 3600
  retention_in_seconds             = 86400
  dead_letter_queue_delivery_count = 3
}

# ---------------------------------------------------------------------------
# Kubernetes (OKE) Module
# ---------------------------------------------------------------------------
module "kubernetes" {
  source = "../../modules/kubernetes/oci"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # OKE cluster configuration — single Always Free node
  kubernetes_version = var.kubernetes_version
  node_count         = 1
  node_shape         = "VM.Standard.A1.Flex"
  node_ocpus         = var.node_ocpus
  node_memory_gbs    = var.node_memory_gbs
  node_image_id      = var.node_image_id

  # Network configuration
  vcn_id               = module.network.vcn_id
  oke_api_subnet_id    = module.network.oke_api_subnet_id
  oke_public_endpoint  = true
  lb_public            = true
  oke_worker_subnet_id = module.network.private_subnet_id
  oke_pod_subnet_id    = module.network.oke_pod_subnet_id
  public_subnet_ids    = [module.network.public_subnet_id]
  oke_api_nsg_ids      = [module.network.oke_api_nsg_id]
  oke_pod_nsg_ids      = [module.network.oke_pod_nsg_id]
  lb_nsg_ids           = [module.network.lb_nsg_id]

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url

  # Always Free resource constraints (2 OCPU / 12 GB total)
  tmi_cpu_request          = "200m"
  tmi_memory_request       = "512Mi"
  tmi_cpu_limit            = "500m"
  tmi_memory_limit         = "1Gi"
  redis_cpu_request        = "100m"
  redis_memory_request     = "256Mi"
  redis_cpu_limit          = "250m"
  redis_memory_limit       = "512Mi"
  tmi_tf_wh_cpu_request    = "200m"
  tmi_tf_wh_memory_request = "512Mi"
  tmi_tf_wh_cpu_limit      = "500m"
  tmi_tf_wh_memory_limit   = "1Gi"

  # Redis configuration
  redis_password = local.redis_password

  # TMI-UX Frontend configuration (optional)
  tmi_ux_enabled   = var.tmi_ux_enabled
  tmi_ux_image_url = var.tmi_ux_image_url

  # tmi-tf-wh Webhook Analyzer configuration (optional)
  tmi_tf_wh_enabled        = var.tmi_tf_wh_enabled
  tmi_tf_wh_image_url      = var.tmi_tf_wh_image_url
  tmi_tf_wh_queue_ocid     = var.tmi_tf_wh_enabled ? oci_queue_queue.tmi_tf_wh[0].id : ""
  tmi_tf_wh_extra_env_vars = var.tmi_tf_wh_extra_env_vars

  # Database configuration
  db_username           = var.db_username
  db_password           = local.db_password
  oracle_connect_string = "${var.db_name}_high"
  wallet_base64         = module.database.wallet_content_base64

  # Secrets configuration
  vault_ocid          = module.secrets.vault_id
  jwt_secret          = local.jwt_secret
  oauth_client_secret = random_password.oauth_client_secret.result

  # Load Balancer configuration — OCI flexible LB (10 Mbps Always Free)
  lb_min_bandwidth_mbps = 10
  lb_max_bandwidth_mbps = 10

  # Build mode (dev for public template)
  tmi_build_mode = "dev"

  # Deployer-provided extra environment variables merged into ConfigMap
  extra_environment_variables = merge(local.configmap_defaults, var.extra_env_vars)

  tags = local.tags

  depends_on = [module.network, module.database, module.secrets]
}

# ---------------------------------------------------------------------------
# Certificates Module (optional — enabled when domain_name is set)
# ---------------------------------------------------------------------------
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
  load_balancer_id = null # OCI LB is provisioned by the Kubernetes Service

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

# ---------------------------------------------------------------------------
# Dynamic Group and IAM Policies
# ---------------------------------------------------------------------------
resource "oci_identity_dynamic_group" "tmi_oke" {
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-oke-workloads"
  description    = "Dynamic group for TMI OKE cluster workloads"

  matching_rule = "ALL {resource.type = 'workload', resource.cluster.id = '${module.kubernetes.cluster_id}'}"

  freeform_tags = local.tags

  depends_on = [module.kubernetes]
}

resource "oci_identity_policy" "vault_access" {
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-vault-access"
  description    = "Allow TMI OKE workloads to read secrets from Vault"

  statements = concat(
    [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to read secret-family in compartment id ${var.compartment_id}",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use keys in compartment id ${var.compartment_id}",
    ],
    var.tmi_tf_wh_enabled ? [
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to manage queues in compartment id ${var.compartment_id} where target.queue.id = '${oci_queue_queue.tmi_tf_wh[0].id}'",
      "Allow dynamic-group ${oci_identity_dynamic_group.tmi_oke.name} to use generative-ai-family in compartment id ${var.compartment_id}",
    ] : []
  )

  freeform_tags = local.tags
}
