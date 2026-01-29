# OCI Database Module for TMI
# Creates Oracle Autonomous Database (ADB) with private endpoint
# Supports Free Tier for Always Free deployment

terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = ">= 5.0.0"
    }
  }
}

# Random password for wallet if not provided
resource "random_password" "wallet" {
  count   = var.wallet_password == null ? 1 : 0
  length  = 20
  special = true
  # OCI wallet passwords require specific character types
  override_special = "#$%&*()-_=+[]{}|:,.?"
}

locals {
  wallet_password = var.wallet_password != null ? var.wallet_password : random_password.wallet[0].result
}

# Autonomous Database
resource "oci_database_autonomous_database" "tmi" {
  compartment_id = var.compartment_id
  db_name        = var.db_name
  display_name   = "${var.name_prefix}-adb"
  admin_password = var.admin_password

  # Compute configuration
  cpu_core_count           = var.cpu_core_count
  data_storage_size_in_tbs = var.data_storage_size_in_tbs
  compute_model            = "ECPU"
  compute_count            = var.compute_count

  # Database configuration
  db_version  = var.db_version
  db_workload = "OLTP"

  # Free Tier configuration
  is_free_tier = var.is_free_tier

  # Private endpoint configuration
  subnet_id = var.database_subnet_id
  nsg_ids   = var.database_nsg_ids

  # Access control
  is_access_control_enabled = true
  whitelisted_ips           = []

  # Scaling configuration
  is_auto_scaling_enabled             = var.is_auto_scaling_enabled
  is_auto_scaling_for_storage_enabled = var.is_auto_scaling_for_storage_enabled

  # Backup configuration
  is_local_data_guard_enabled = false

  # Lifecycle
  lifecycle {
    prevent_destroy = var.prevent_destroy
  }

  freeform_tags = var.tags
}

# Download wallet
resource "oci_database_autonomous_database_wallet" "tmi" {
  autonomous_database_id = oci_database_autonomous_database.tmi.id
  password               = local.wallet_password
  base64_encode_content  = true
  generate_type          = "SINGLE"
}

# Store wallet in Object Storage for container access (optional)
resource "oci_objectstorage_bucket" "wallet" {
  count          = var.create_wallet_bucket ? 1 : 0
  compartment_id = var.compartment_id
  namespace      = var.object_storage_namespace
  name           = "${var.name_prefix}-wallet"
  access_type    = "NoPublicAccess"

  freeform_tags = var.tags
}

resource "oci_objectstorage_object" "wallet" {
  count     = var.create_wallet_bucket ? 1 : 0
  bucket    = oci_objectstorage_bucket.wallet[0].name
  namespace = var.object_storage_namespace
  object    = "wallet.zip"
  content   = oci_database_autonomous_database_wallet.tmi.content
}

# Pre-authenticated request for wallet download (valid for 7 days)
resource "oci_objectstorage_preauthrequest" "wallet" {
  count        = var.create_wallet_bucket ? 1 : 0
  namespace    = var.object_storage_namespace
  bucket       = oci_objectstorage_bucket.wallet[0].name
  name         = "${var.name_prefix}-wallet-par"
  access_type  = "ObjectRead"
  object_name  = oci_objectstorage_object.wallet[0].object
  time_expires = timeadd(timestamp(), "168h") # 7 days

  lifecycle {
    create_before_destroy = true
  }
}
