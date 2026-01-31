# OCI Secrets Module for TMI
# Creates OCI Vault with encryption keys and secrets

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Vault
resource "oci_kms_vault" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-vault"
  vault_type     = var.vault_type

  freeform_tags = var.tags
}

# Wait for vault to be active
resource "time_sleep" "wait_for_vault" {
  depends_on = [oci_kms_vault.tmi]

  create_duration = "30s"
}

# Master Encryption Key
resource "oci_kms_key" "tmi" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-master-key"

  key_shape {
    algorithm = "AES"
    length    = 32
  }

  management_endpoint = oci_kms_vault.tmi.management_endpoint

  protection_mode = var.key_protection_mode

  depends_on = [time_sleep.wait_for_vault]

  freeform_tags = var.tags
}

# Database Password Secret
resource "oci_vault_secret" "db_password" {
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-db-password"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.db_password)
  }

  description = "TMI database password"

  freeform_tags = var.tags
}

# Database Username Secret
resource "oci_vault_secret" "db_username" {
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-db-username"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.db_username)
  }

  description = "TMI database username"

  freeform_tags = var.tags
}

# Redis Password Secret
resource "oci_vault_secret" "redis_password" {
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-redis-password"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.redis_password)
  }

  description = "TMI Redis password"

  freeform_tags = var.tags
}

# JWT Secret
resource "oci_vault_secret" "jwt_secret" {
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-jwt-secret"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.jwt_secret)
  }

  description = "TMI JWT signing secret"

  freeform_tags = var.tags
}

# OAuth Client Secret (optional)
resource "oci_vault_secret" "oauth_client_secret" {
  count          = var.oauth_client_secret != null ? 1 : 0
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-oauth-client-secret"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.oauth_client_secret)
  }

  description = "TMI OAuth client secret"

  freeform_tags = var.tags
}

# API Key Secret (optional)
resource "oci_vault_secret" "api_key" {
  count          = var.api_key != null ? 1 : 0
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-api-key"

  secret_content {
    content_type = "BASE64"
    content      = base64encode(var.api_key)
  }

  description = "TMI API key"

  freeform_tags = var.tags
}

# Combined secrets JSON for single-secret mode
# This allows TMI to fetch all secrets with one API call
resource "oci_vault_secret" "all_secrets" {
  count          = var.create_combined_secret ? 1 : 0
  compartment_id = var.compartment_id
  vault_id       = oci_kms_vault.tmi.id
  key_id         = oci_kms_key.tmi.id
  secret_name    = "${var.name_prefix}-all-secrets"

  secret_content {
    content_type = "BASE64"
    content = base64encode(jsonencode({
      TMI_DB_USER         = var.db_username
      TMI_DB_PASSWORD     = var.db_password
      REDIS_PASSWORD      = var.redis_password
      TMI_JWT_SECRET      = var.jwt_secret
      OAUTH_CLIENT_SECRET = var.oauth_client_secret
    }))
  }

  description = "TMI combined secrets (single-secret mode)"

  freeform_tags = var.tags
}

# Dynamic Group for Container Instances (to allow access to Vault)
resource "oci_identity_dynamic_group" "tmi_containers" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-containers"
  description    = "Dynamic group for TMI container instances"

  matching_rule = "ALL {resource.type = 'containerinstance', resource.compartment.id = '${var.compartment_id}'}"

  freeform_tags = var.tags
}

# Policy to allow Container Instances to read secrets
resource "oci_identity_policy" "vault_access" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-vault-access"
  description    = "Allow TMI containers to read secrets from Vault"

  statements = [
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_containers[0].name} to read secret-family in compartment id ${var.compartment_id}",
    "Allow dynamic-group ${oci_identity_dynamic_group.tmi_containers[0].name} to use keys in compartment id ${var.compartment_id}"
  ]

  freeform_tags = var.tags
}
