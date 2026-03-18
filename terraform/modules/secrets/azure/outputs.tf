# Outputs for Azure Secrets Module

output "key_vault_id" {
  description = "ID of the Key Vault"
  value       = azurerm_key_vault.tmi.id
}

output "key_vault_name" {
  description = "Name of the Key Vault"
  value       = azurerm_key_vault.tmi.name
}

output "key_vault_uri" {
  description = "URI of the Key Vault"
  value       = azurerm_key_vault.tmi.vault_uri
}

output "db_password_secret_id" {
  description = "ID of the database password secret"
  value       = azurerm_key_vault_secret.db_password.id
}

output "redis_password_secret_id" {
  description = "ID of the Redis password secret"
  value       = azurerm_key_vault_secret.redis_password.id
}

output "jwt_secret_secret_id" {
  description = "ID of the JWT secret"
  value       = azurerm_key_vault_secret.jwt_secret.id
}

# Resolved secret values for use in K8s resources
output "db_password" {
  description = "Database password value"
  value       = local.db_password
  sensitive   = true
}

output "redis_password" {
  description = "Redis password value"
  value       = local.redis_password
  sensitive   = true
}

output "jwt_secret" {
  description = "JWT secret value"
  value       = local.jwt_secret
  sensitive   = true
}

# Standard interface outputs for multi-cloud compatibility
output "secrets_provider_id" {
  description = "Secrets provider ID (standard interface)"
  value       = azurerm_key_vault.tmi.id
}

output "secrets_endpoint" {
  description = "Secrets endpoint (standard interface)"
  value       = azurerm_key_vault.tmi.vault_uri
}
