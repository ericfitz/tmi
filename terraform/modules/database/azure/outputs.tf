# Outputs for Azure Database Module

output "server_id" {
  description = "ID of the PostgreSQL Flexible Server"
  value       = azurerm_postgresql_flexible_server.tmi.id
}

output "server_name" {
  description = "Name of the PostgreSQL Flexible Server"
  value       = azurerm_postgresql_flexible_server.tmi.name
}

output "server_fqdn" {
  description = "FQDN of the PostgreSQL Flexible Server"
  value       = azurerm_postgresql_flexible_server.tmi.fqdn
}

output "database_name" {
  description = "Name of the database"
  value       = azurerm_postgresql_flexible_server_database.tmi.name
}

output "admin_username" {
  description = "Administrator username"
  value       = azurerm_postgresql_flexible_server.tmi.administrator_login
}

# Standard interface outputs for multi-cloud compatibility
output "database_id" {
  description = "Database ID (standard interface)"
  value       = azurerm_postgresql_flexible_server.tmi.id
}

output "database_endpoint" {
  description = "Database endpoint/hostname (standard interface)"
  value       = azurerm_postgresql_flexible_server.tmi.fqdn
}

output "database_port" {
  description = "Database port (standard interface)"
  value       = 5432
}

output "database_host" {
  description = "Database host for connection string"
  value       = azurerm_postgresql_flexible_server.tmi.fqdn
}
