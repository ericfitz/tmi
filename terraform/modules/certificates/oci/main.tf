# OCI Certificates Module for TMI
# Creates OCI Function for Let's Encrypt certificate automation with DNS-01 challenges
#
# NOTE: OCI does not have a direct Terraform resource for scheduling function invocations.
# After deployment, set up scheduling via one of these methods:
# 1. OCI Console: Create a Scheduled Job in Resource Scheduler
# 2. OCI CLI: Use `oci fn function invoke` in a cron job
# 3. External scheduler (GitHub Actions, Jenkins, etc.)

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Function Application
resource "oci_functions_application" "certmgr" {
  compartment_id = var.compartment_id
  display_name   = "${var.name_prefix}-certmgr"
  subnet_ids     = [var.subnet_id]

  config = {
    CERTMGR_DOMAIN         = var.domain_name
    CERTMGR_DNS_ZONE_ID    = var.dns_zone_id
    CERTMGR_ACME_EMAIL     = var.acme_contact_email
    CERTMGR_RENEWAL_DAYS   = tostring(var.certificate_renewal_days)
    CERTMGR_LB_ID          = var.load_balancer_id
    CERTMGR_VAULT_ID       = var.vault_id
    CERTMGR_VAULT_KEY_ID   = var.vault_key_id
    CERTMGR_COMPARTMENT_ID = var.compartment_id
    CERTMGR_NAME_PREFIX    = var.name_prefix
    CERTMGR_ACME_DIRECTORY = var.acme_directory == "production" ? "https://acme-v02.api.letsencrypt.org/directory" : "https://acme-staging-v02.api.letsencrypt.org/directory"
  }

  freeform_tags = var.tags
}

# Certificate Manager Function
resource "oci_functions_function" "certmgr" {
  application_id     = oci_functions_application.certmgr.id
  display_name       = "certmgr"
  image              = var.certmgr_image_url
  memory_in_mbs      = var.function_memory_mb
  timeout_in_seconds = var.function_timeout_seconds

  freeform_tags = var.tags
}

# Secrets for certificate storage
resource "oci_vault_secret" "acme_account_key" {
  compartment_id = var.compartment_id
  vault_id       = var.vault_id
  key_id         = var.vault_key_id
  secret_name    = "${var.name_prefix}-acme-account-key"

  secret_content {
    content_type = "BASE64"
    # Initial empty content - will be populated by function on first run
    content = base64encode("")
  }

  description = "ACME account private key for Let's Encrypt"

  freeform_tags = var.tags

  lifecycle {
    ignore_changes = [secret_content]
  }
}

resource "oci_vault_secret" "certificate" {
  compartment_id = var.compartment_id
  vault_id       = var.vault_id
  key_id         = var.vault_key_id
  secret_name    = "${var.name_prefix}-certificate"

  secret_content {
    content_type = "BASE64"
    # Initial empty content - will be populated by function on first run
    content = base64encode("")
  }

  description = "TLS certificate for ${var.domain_name}"

  freeform_tags = var.tags

  lifecycle {
    ignore_changes = [secret_content]
  }
}

resource "oci_vault_secret" "private_key" {
  compartment_id = var.compartment_id
  vault_id       = var.vault_id
  key_id         = var.vault_key_id
  secret_name    = "${var.name_prefix}-private-key"

  secret_content {
    content_type = "BASE64"
    # Initial empty content - will be populated by function on first run
    content = base64encode("")
  }

  description = "TLS private key for ${var.domain_name}"

  freeform_tags = var.tags

  lifecycle {
    ignore_changes = [secret_content]
  }
}

# Dynamic Group for Functions (to allow access to OCI resources)
resource "oci_identity_dynamic_group" "certmgr_functions" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.tenancy_ocid
  name           = "${var.name_prefix}-certmgr-functions"
  description    = "Dynamic group for certificate manager function"

  matching_rule = "ALL {resource.type = 'fnfunc', resource.compartment.id = '${var.compartment_id}'}"

  freeform_tags = var.tags
}

# Local for dynamic group name (either created or existing)
locals {
  dynamic_group_name = var.create_dynamic_group ? oci_identity_dynamic_group.certmgr_functions[0].name : var.existing_dynamic_group_name
}

# Policy to allow Function to manage DNS records
resource "oci_identity_policy" "dns_management" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-certmgr-dns"
  description    = "Allow certificate manager function to manage DNS TXT records"

  statements = [
    "Allow dynamic-group ${local.dynamic_group_name} to manage dns-records in compartment id ${var.compartment_id} where target.dns-zone.id = '${var.dns_zone_id}'"
  ]

  freeform_tags = var.tags
}

# Policy to allow Function to read/write secrets in Vault
resource "oci_identity_policy" "vault_access" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-certmgr-vault"
  description    = "Allow certificate manager function to manage secrets in Vault"

  statements = [
    "Allow dynamic-group ${local.dynamic_group_name} to manage secret-family in compartment id ${var.compartment_id} where target.vault.id = '${var.vault_id}'",
    "Allow dynamic-group ${local.dynamic_group_name} to use keys in compartment id ${var.compartment_id}"
  ]

  freeform_tags = var.tags
}

# Policy to allow Function to update Load Balancer certificates
resource "oci_identity_policy" "lb_certificate" {
  count          = var.create_dynamic_group ? 1 : 0
  compartment_id = var.compartment_id
  name           = "${var.name_prefix}-certmgr-lb"
  description    = "Allow certificate manager function to update Load Balancer certificates"

  statements = [
    "Allow dynamic-group ${local.dynamic_group_name} to manage load-balancers in compartment id ${var.compartment_id} where target.loadbalancer.id = '${var.load_balancer_id}'"
  ]

  freeform_tags = var.tags
}
