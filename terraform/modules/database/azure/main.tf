# Azure Database Module for TMI
# Creates Azure Database for PostgreSQL Flexible Server
# Supports deletion protection via Azure management lock

terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 3.80.0"
    }
  }
}

# PostgreSQL Flexible Server
resource "azurerm_postgresql_flexible_server" "tmi" {
  name                          = "${var.name_prefix}-pg"
  resource_group_name           = var.resource_group_name
  location                      = var.location
  version                       = var.postgresql_version
  administrator_login           = var.admin_username
  administrator_password        = var.admin_password
  sku_name                      = var.sku_name
  storage_mb                    = var.storage_mb
  backup_retention_days         = var.backup_retention_days
  geo_redundant_backup_enabled  = false
  public_network_access_enabled = !var.enable_private_access
  zone                          = var.availability_zone

  # VNet integration for private access
  delegated_subnet_id = var.enable_private_access ? var.database_subnet_id : null
  private_dns_zone_id = var.enable_private_access ? var.private_dns_zone_id : null

  tags = var.tags

  lifecycle {
    ignore_changes = [
      # High availability zone may change during failover
      high_availability[0].standby_availability_zone,
    ]
  }
}

# Database
resource "azurerm_postgresql_flexible_server_database" "tmi" {
  name      = var.database_name
  server_id = azurerm_postgresql_flexible_server.tmi.id
  charset   = "UTF8"
  collation = "en_US.utf8"
}

# Firewall rule to allow AKS subnet (only when not using private access)
resource "azurerm_postgresql_flexible_server_firewall_rule" "allow_azure" {
  count            = var.enable_private_access ? 0 : 1
  name             = "allow-azure-services"
  server_id        = azurerm_postgresql_flexible_server.tmi.id
  start_ip_address = "0.0.0.0"
  end_ip_address   = "0.0.0.0"
}

# Management lock for deletion protection
resource "azurerm_management_lock" "database" {
  count      = var.deletion_protection ? 1 : 0
  name       = "${var.name_prefix}-pg-lock"
  scope      = azurerm_postgresql_flexible_server.tmi.id
  lock_level = "CanNotDelete"
  notes      = "Deletion protection for TMI PostgreSQL database"
}
