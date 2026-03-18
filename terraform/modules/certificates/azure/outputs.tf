# Outputs for Azure Certificates Module

output "certificate_id" {
  description = "ID of the Key Vault certificate (if created)"
  value       = var.create_self_signed_cert ? azurerm_key_vault_certificate.tmi[0].id : null
}

output "certificate_secret_id" {
  description = "Secret ID of the certificate in Key Vault (if created)"
  value       = var.create_self_signed_cert ? azurerm_key_vault_certificate.tmi[0].secret_id : null
}

output "certificate_thumbprint" {
  description = "Thumbprint of the certificate (if created)"
  value       = var.create_self_signed_cert ? azurerm_key_vault_certificate.tmi[0].thumbprint : null
}

output "certificate_config" {
  description = "Summary of certificate configuration"
  value = {
    domain_name       = var.domain_name
    self_signed       = var.create_self_signed_cert
    key_vault_id      = var.key_vault_id
    alternative_names = var.subject_alternative_names
  }
}
