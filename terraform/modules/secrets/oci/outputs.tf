# Outputs for OCI Secrets Module

output "vault_id" {
  description = "OCID of the Vault"
  value       = oci_kms_vault.tmi.id
}

output "vault_management_endpoint" {
  description = "Management endpoint for the Vault"
  value       = oci_kms_vault.tmi.management_endpoint
}

output "vault_crypto_endpoint" {
  description = "Crypto endpoint for the Vault"
  value       = oci_kms_vault.tmi.crypto_endpoint
}

output "master_key_id" {
  description = "OCID of the master encryption key"
  value       = oci_kms_key.tmi.id
}

output "db_password_secret_id" {
  description = "OCID of the database password secret"
  value       = oci_vault_secret.db_password.id
}

output "db_username_secret_id" {
  description = "OCID of the database username secret"
  value       = oci_vault_secret.db_username.id
}

output "redis_password_secret_id" {
  description = "OCID of the Redis password secret"
  value       = oci_vault_secret.redis_password.id
}

output "jwt_secret_id" {
  description = "OCID of the JWT secret"
  value       = oci_vault_secret.jwt_secret.id
}

output "oauth_client_secret_id" {
  description = "OCID of the OAuth client secret (if created)"
  value       = var.oauth_client_secret != null ? oci_vault_secret.oauth_client_secret[0].id : null
}

output "api_key_secret_id" {
  description = "OCID of the API key secret (if created)"
  value       = var.api_key != null ? oci_vault_secret.api_key[0].id : null
}

output "all_secrets_id" {
  description = "OCID of the combined secrets (single-secret mode)"
  value       = var.create_combined_secret ? oci_vault_secret.all_secrets[0].id : null
}

output "dynamic_group_id" {
  description = "OCID of the dynamic group for container instances"
  value       = var.create_dynamic_group ? oci_identity_dynamic_group.tmi_containers[0].id : null
}

output "dynamic_group_name" {
  description = "Name of the dynamic group for container instances"
  value       = var.create_dynamic_group ? oci_identity_dynamic_group.tmi_containers[0].name : null
}

output "policy_id" {
  description = "OCID of the vault access policy"
  value       = var.create_dynamic_group ? oci_identity_policy.vault_access[0].id : null
}

# Standard interface outputs for multi-cloud compatibility
output "secrets_provider_id" {
  description = "Secrets provider ID (standard interface)"
  value       = oci_kms_vault.tmi.id
}

output "secrets_endpoint" {
  description = "Secrets endpoint (standard interface)"
  value       = oci_kms_vault.tmi.management_endpoint
}

# Secret names for environment variable configuration
output "secret_names" {
  description = "Map of secret names for TMI configuration"
  value = {
    db_username         = oci_vault_secret.db_username.secret_name
    db_password         = oci_vault_secret.db_password.secret_name
    redis_password      = oci_vault_secret.redis_password.secret_name
    jwt_secret          = oci_vault_secret.jwt_secret.secret_name
    oauth_client_secret = var.oauth_client_secret != null ? oci_vault_secret.oauth_client_secret[0].secret_name : null
    api_key             = var.api_key != null ? oci_vault_secret.api_key[0].secret_name : null
    all_secrets         = var.create_combined_secret ? oci_vault_secret.all_secrets[0].secret_name : null
  }
}
