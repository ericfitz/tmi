# Azure Secrets Module for TMI
# Creates Key Vault with secrets for database credentials, Redis password, and JWT secret

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.0.0"
    }
  }
}

# Current client configuration (for Key Vault access policy)
data "azurerm_client_config" "current" {}

# Key Vault
resource "azurerm_key_vault" "tmi" {
  name                       = "${var.name_prefix}-kv"
  resource_group_name        = var.resource_group_name
  location                   = var.location
  tenant_id                  = data.azurerm_client_config.current.tenant_id
  sku_name                   = "standard"
  soft_delete_retention_days = var.soft_delete_retention_days
  purge_protection_enabled   = var.purge_protection_enabled

  # Allow the deployer to manage secrets
  access_policy {
    tenant_id = data.azurerm_client_config.current.tenant_id
    object_id = data.azurerm_client_config.current.object_id

    secret_permissions = [
      "Get",
      "List",
      "Set",
      "Delete",
      "Purge",
      "Recover",
    ]
  }

  # Allow AKS managed identity to read secrets
  dynamic "access_policy" {
    for_each = var.aks_identity_object_id != null ? [1] : []
    content {
      tenant_id = data.azurerm_client_config.current.tenant_id
      object_id = var.aks_identity_object_id

      secret_permissions = [
        "Get",
        "List",
      ]
    }
  }

  tags = var.tags
}

# Random password for database
resource "random_password" "db_password" {
  count            = var.db_password == null ? 1 : 0
  length           = 24
  special          = true
  override_special = "!#$%&*()-_=+[]{}|:,.?"
}

# Random password for Redis
resource "random_password" "redis_password" {
  count   = var.redis_password == null ? 1 : 0
  length  = 24
  special = false
}

# Random JWT secret
resource "random_password" "jwt_secret" {
  count   = var.jwt_secret == null ? 1 : 0
  length  = 64
  special = false
}

locals {
  db_password    = var.db_password != null ? var.db_password : random_password.db_password[0].result
  redis_password = var.redis_password != null ? var.redis_password : random_password.redis_password[0].result
  jwt_secret     = var.jwt_secret != null ? var.jwt_secret : random_password.jwt_secret[0].result
}

# Database Password Secret
resource "azurerm_key_vault_secret" "db_password" {
  name         = "${var.name_prefix}-db-password"
  value        = local.db_password
  key_vault_id = azurerm_key_vault.tmi.id

  tags = var.tags
}

# Redis Password Secret
resource "azurerm_key_vault_secret" "redis_password" {
  name         = "${var.name_prefix}-redis-password"
  value        = local.redis_password
  key_vault_id = azurerm_key_vault.tmi.id

  tags = var.tags
}

# JWT Secret
resource "azurerm_key_vault_secret" "jwt_secret" {
  name         = "${var.name_prefix}-jwt-secret"
  value        = local.jwt_secret
  key_vault_id = azurerm_key_vault.tmi.id

  tags = var.tags
}
