# Outputs for OCI Certificates Module

output "function_application_id" {
  description = "OCID of the Function Application"
  value       = oci_functions_application.certmgr.id
}

output "function_id" {
  description = "OCID of the certificate manager function"
  value       = oci_functions_function.certmgr.id
}

output "function_invoke_endpoint" {
  description = "Invoke endpoint for the certificate manager function"
  value       = oci_functions_function.certmgr.invoke_endpoint
}

# Secret OCIDs
output "acme_account_key_secret_id" {
  description = "OCID of the ACME account key secret"
  value       = oci_vault_secret.acme_account_key.id
}

output "certificate_secret_id" {
  description = "OCID of the certificate secret"
  value       = oci_vault_secret.certificate.id
}

output "private_key_secret_id" {
  description = "OCID of the private key secret"
  value       = oci_vault_secret.private_key.id
}

# Dynamic group (if created)
output "dynamic_group_id" {
  description = "OCID of the dynamic group for functions (if created)"
  value       = var.create_dynamic_group ? oci_identity_dynamic_group.certmgr_functions[0].id : null
}

output "dynamic_group_name" {
  description = "Name of the dynamic group for functions"
  value       = local.dynamic_group_name
}

# Configuration summary
output "certificate_config" {
  description = "Summary of certificate configuration"
  value = {
    domain_name          = var.domain_name
    acme_directory       = var.acme_directory
    renewal_days         = var.certificate_renewal_days
    function_memory_mb   = var.function_memory_mb
    function_timeout_sec = var.function_timeout_seconds
  }
}

# Manual invocation command
output "invoke_command" {
  description = "OCI CLI command to manually invoke the certificate manager function"
  value       = "oci fn function invoke --function-id ${oci_functions_function.certmgr.id} --file - --body ''"
}
