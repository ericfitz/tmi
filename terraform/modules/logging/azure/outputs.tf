# Outputs for Azure Logging Module

output "workspace_id" {
  description = "ID of the Log Analytics Workspace"
  value       = azurerm_log_analytics_workspace.tmi.id
}

output "workspace_name" {
  description = "Name of the Log Analytics Workspace"
  value       = azurerm_log_analytics_workspace.tmi.name
}

output "workspace_primary_shared_key" {
  description = "Primary shared key of the Log Analytics Workspace"
  value       = azurerm_log_analytics_workspace.tmi.primary_shared_key
  sensitive   = true
}

output "workspace_customer_id" {
  description = "Customer ID (Workspace ID) of the Log Analytics Workspace"
  value       = azurerm_log_analytics_workspace.tmi.workspace_id
}

# Standard interface outputs for multi-cloud compatibility
output "log_group" {
  description = "Log group identifier (standard interface)"
  value       = azurerm_log_analytics_workspace.tmi.id
}

output "log_stream" {
  description = "Log stream identifier (standard interface)"
  value       = azurerm_log_analytics_workspace.tmi.workspace_id
}
