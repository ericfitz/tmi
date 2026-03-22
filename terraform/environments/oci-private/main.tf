# TMI OCI Private Deployment
# Internal deployment with no public ingress. All resources in private subnets.
# NAT gateway provides outbound internet access (container registries, OAuth IdPs).
# Deployer is responsible for establishing user connectivity (VPN, bastion, etc.).
# Estimated monthly cost: ~$50-80 (non-free ADB for private endpoint support)

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
    null = {
      source  = "hashicorp/null"
      version = ">= 3.0.0"
    }
    http = {
      source  = "hashicorp/http"
      version = ">= 3.0.0"
    }
  }

  # Uncomment and configure for remote state using OCI Object Storage
  # backend "http" {
  #   address        = "https://objectstorage.<region>.oraclecloud.com/p/<par-token>/n/<namespace>/b/<bucket>/o/oci-private/terraform.tfstate"
  #   update_method  = "PUT"
  # }
}

# OCI Provider configuration
provider "oci" {
  region              = var.region
  config_file_profile = var.oci_config_profile
  # auth = "InstancePrincipal"  # Uncomment for IMDS authentication
}

# Kubernetes Provider - configured after OKE cluster creation
# Uses OCI CLI for token authentication
# Note: Run with GODEBUG=x509negativeserial=1 if Go 1.24+ rejects OKE certs
provider "kubernetes" {
  host                   = module.kubernetes.cluster_endpoint
  cluster_ca_certificate = module.kubernetes.cluster_ca_certificate

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "oci"
    args        = ["ce", "cluster", "generate-token", "--cluster-id", module.kubernetes.cluster_id, "--region", var.region, "--profile", var.oci_config_profile]
  }
}

# ---------------------------------------------------------------------------
# Detect deployer IP for temporary K8s API access during provisioning
# ---------------------------------------------------------------------------
data "http" "deployer_ip" {
  count = var.deployer_ip == null ? 1 : 0
  url   = "https://checkip.amazonaws.com"
}

locals {
  deployer_ip = var.deployer_ip != null ? var.deployer_ip : chomp(data.http.deployer_ip[0].response_body)
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

locals {
  db_password    = var.db_password != null ? var.db_password : random_password.db_password[0].result
  redis_password = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret     = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result

  tags = merge(var.tags, {
    project     = "tmi"
    environment = "private"
    managed_by  = "terraform"
  })

  # ConfigMap defaults for private (production) deployment
  configmap_defaults = {
    TMI_AUTH_BUILD_MODE                       = "production"
    TMI_AUTH_AUTO_PROMOTE_FIRST_USER          = "true"
    TMI_LOGGING_ALSO_LOG_TO_CONSOLE           = "true"
    TMI_LOGGING_REDACT_AUTH_TOKENS            = "true"
    TMI_LOGGING_SUPPRESS_UNAUTHENTICATED_LOGS = "true"
    TMI_SERVER_INTERFACE                      = "0.0.0.0"
    TMI_SERVER_PORT                           = "8080"
  }
}

# Get Object Storage namespace
data "oci_objectstorage_namespace" "ns" {
  compartment_id = var.compartment_id
}

# ---------------------------------------------------------------------------
# OCIR Container Registry (private)
# ---------------------------------------------------------------------------
resource "oci_artifacts_container_repository" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi"
  is_public      = false
}

resource "oci_artifacts_container_repository" "redis" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-redis"
  is_public      = false
}

resource "oci_artifacts_container_repository" "tmi_ux" {
  count          = var.tmi_ux_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-ux"
  is_public      = false
}

resource "oci_artifacts_container_repository" "tmi_tf_wh" {
  count          = var.tmi_tf_wh_enabled ? 1 : 0
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}/tmi-tf-wh"
  is_public      = false
}

# ---------------------------------------------------------------------------
# Network Module (private subnets, NAT gateway for outbound)
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
  oke_api_authorized_cidrs = ["${local.deployer_ip}/32"]

  tags = local.tags
}

# ---------------------------------------------------------------------------
# Temporary NSG rule: allow deployer IP to reach K8s API (port 6443)
# during provisioning. Removed by null_resource after K8s resources are created.
# ---------------------------------------------------------------------------
resource "oci_core_network_security_group_security_rule" "temp_k8s_api_access" {
  network_security_group_id = module.network.oke_api_nsg_id
  direction                 = "INGRESS"
  protocol                  = "6" # TCP

  source      = "${local.deployer_ip}/32"
  source_type = "CIDR_BLOCK"

  tcp_options {
    destination_port_range {
      min = 6443
      max = 6443
    }
  }

  description = "Temporary: deployer access for K8s provisioning"

  lifecycle {
    # The null_resource below will remove this rule after provisioning.
    # On subsequent applies, Terraform may recreate it (harmless — it will
    # be removed again by the null_resource).
    ignore_changes = all
  }
}

# ---------------------------------------------------------------------------
# Database Module (ADB non-free tier for private endpoint support)
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

  # Non-free tier for private endpoint support
  is_free_tier                        = false
  db_version                          = "23ai"
  compute_count                       = var.db_compute_count
  is_auto_scaling_enabled             = false
  is_auto_scaling_for_storage_enabled = false
  deletion_protection                 = true

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

  retention_days         = var.log_retention_days
  archive_retention_days = var.log_archive_retention_days
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
# Kubernetes (OKE) Module — private endpoint, internal LB
# ---------------------------------------------------------------------------
module "kubernetes" {
  source = "../../modules/kubernetes/oci"

  compartment_id = var.compartment_id
  name_prefix    = var.name_prefix

  # OKE cluster configuration — single node
  kubernetes_version = var.kubernetes_version
  node_count         = 1
  node_shape         = "VM.Standard.A1.Flex"
  node_ocpus         = var.node_ocpus
  node_memory_gbs    = var.node_memory_gbs
  node_image_id      = var.node_image_id

  # Network configuration — use private subnets for LB (internal only)
  vcn_id               = module.network.vcn_id
  oke_api_subnet_id    = module.network.oke_api_subnet_id
  oke_worker_subnet_id = module.network.private_subnet_id
  oke_pod_subnet_id    = module.network.oke_pod_subnet_id
  public_subnet_ids    = [module.network.private_subnet_id] # Internal LB in private subnet
  oke_api_nsg_ids      = [module.network.oke_api_nsg_id]
  oke_pod_nsg_ids      = [module.network.oke_pod_nsg_id]
  lb_nsg_ids           = [module.network.lb_nsg_id]

  # Container images
  tmi_image_url   = var.tmi_image_url
  redis_image_url = var.redis_image_url

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

  # Database configuration (private endpoint)
  db_username           = var.db_username
  db_password           = local.db_password
  oracle_connect_string = "${var.db_name}_high"
  wallet_base64         = module.database.wallet_content_base64

  # Secrets configuration
  vault_ocid = module.secrets.vault_id
  jwt_secret = local.jwt_secret

  # Load Balancer configuration — internal OCI flexible LB
  lb_min_bandwidth_mbps = var.lb_min_bandwidth_mbps
  lb_max_bandwidth_mbps = var.lb_max_bandwidth_mbps

  # Build mode (production for private template)
  tmi_build_mode = "production"

  # Deployer-provided extra environment variables merged into ConfigMap
  extra_environment_variables = merge(local.configmap_defaults, var.extra_env_vars)

  tags = local.tags

  depends_on = [
    module.network,
    module.database,
    module.secrets,
    oci_core_network_security_group_security_rule.temp_k8s_api_access,
  ]
}

# ---------------------------------------------------------------------------
# Remove temporary K8s API NSG rule after provisioning
# ---------------------------------------------------------------------------
resource "null_resource" "remove_temp_k8s_access" {
  triggers = {
    nsg_id      = module.network.oke_api_nsg_id
    rule_id     = oci_core_network_security_group_security_rule.temp_k8s_api_access.id
    region      = var.region
    oci_profile = var.oci_config_profile
  }

  provisioner "local-exec" {
    command = <<-EOT
      echo "Removing temporary K8s API access NSG rule..."
      oci network nsg rules remove \
        --nsg-id ${self.triggers.nsg_id} \
        --security-rule-ids '["${self.triggers.rule_id}"]' \
        --region ${self.triggers.region} \
        --profile ${self.triggers.oci_profile} \
        --force
      echo "Temporary K8s API access removed."
    EOT
  }

  depends_on = [module.kubernetes, module.logging]
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
  compartment_id = var.compartment_id
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

# Deletion protection policy — prevent ADB and Vault deletion via IAM
resource "oci_identity_policy" "deletion_protection" {
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-deletion-protection"
  description    = "Prevent accidental deletion of TMI database and vault resources"

  statements = [
    "Deny any-user to manage autonomous-databases in compartment id ${var.compartment_id} where request.permission = 'AUTONOMOUS_DATABASE_DELETE'",
    "Deny any-user to manage vaults in compartment id ${var.compartment_id} where request.permission = 'VAULT_DELETE'"
  ]

  freeform_tags = local.tags
}
