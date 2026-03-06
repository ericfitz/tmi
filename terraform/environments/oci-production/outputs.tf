# Outputs for TMI OCI Production Deployment (OKE)

# Application Access
output "application_url" {
  description = "URL to access the TMI application (API)"
  value       = module.kubernetes.service_endpoint
  sensitive   = true
}

output "load_balancer_ip" {
  description = "Public IP address of the load balancer"
  value       = module.kubernetes.load_balancer_ip
}

# Hostname-based URLs (when TMI-UX is enabled)
output "api_url" {
  description = "URL to access the TMI API (when hostname routing is configured)"
  value       = var.api_hostname != null ? (var.ssl_certificate_pem != null ? "https://${var.api_hostname}" : "http://${var.api_hostname}") : null
  sensitive   = true
}

output "ui_url" {
  description = "URL to access the TMI-UX frontend (when enabled)"
  value       = var.tmi_ux_enabled && var.ui_hostname != null ? (var.ssl_certificate_pem != null ? "https://${var.ui_hostname}" : "http://${var.ui_hostname}") : null
  sensitive   = true
}

output "tmi_ux_url" {
  description = "Direct URL to access the TMI-UX frontend via its own load balancer IP"
  value       = var.tmi_ux_enabled ? "http://${module.kubernetes.tmi_ux_load_balancer_ip}" : null
}

# Network Information
output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.network.vcn_id
}

output "private_subnet_id" {
  description = "OCID of the private subnet"
  value       = module.network.private_subnet_id
}

# Database Information
output "database_id" {
  description = "OCID of the Autonomous Database"
  value       = module.database.autonomous_database_id
}

output "database_connection_string" {
  description = "Database connection string"
  value       = module.database.connection_string_high
  sensitive   = true
}

output "wallet_par_url" {
  description = "Pre-authenticated request URL for wallet download"
  value       = module.database.wallet_par_url
  sensitive   = true
}

# OKE Cluster
output "oke_cluster_id" {
  description = "OCID of the OKE cluster"
  value       = module.kubernetes.cluster_id
}

output "oke_cluster_endpoint" {
  description = "Kubernetes API endpoint"
  value       = module.kubernetes.cluster_endpoint
}

output "kubernetes_namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = module.kubernetes.namespace
}

# Secrets
output "vault_id" {
  description = "OCID of the Vault"
  value       = module.secrets.vault_id
}

output "secret_names" {
  description = "Map of secret names"
  value       = module.secrets.secret_names
  sensitive   = true
}

# Logging
output "log_group_id" {
  description = "OCID of the Log Group"
  value       = module.logging.log_group_id
}

output "archive_bucket_name" {
  description = "Name of the log archive bucket"
  value       = module.logging.archive_bucket_name
}

# Generated Passwords (for initial setup - store securely!)
output "generated_passwords" {
  description = "Generated passwords (only shown if not provided)"
  sensitive   = true
  value = {
    db_password    = var.db_password == null ? "Generated - check terraform.tfstate" : "User provided"
    redis_password = var.redis_password == null ? "Generated - check terraform.tfstate" : "User provided"
    jwt_secret     = var.jwt_secret == null ? "Generated - check terraform.tfstate" : "User provided"
  }
}

# Certificate Automation (when enabled)
output "certificate_function_id" {
  description = "OCID of the certificate manager function"
  value       = var.enable_certificate_automation ? module.certificates[0].function_id : null
}

output "certificate_invoke_command" {
  description = "OCI CLI command to invoke the certificate manager function"
  value       = var.enable_certificate_automation ? module.certificates[0].invoke_command : null
}

output "certificate_config" {
  description = "Certificate automation configuration"
  value       = var.enable_certificate_automation ? module.certificates[0].certificate_config : null
}

# Useful Commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    kubeconfig_setup   = "oci ce cluster create-kubeconfig --cluster-id ${module.kubernetes.cluster_id} --region ${var.region} --token-version 2.0.0"
    kubectl_get_pods   = "kubectl get pods -n tmi"
    kubectl_logs_api   = "kubectl logs -n tmi -l app=tmi-api --tail=100"
    kubectl_logs_redis = "kubectl logs -n tmi -l app=tmi-redis --tail=100"
    logs_tail          = "oci logging search --search-query 'search \"${module.logging.log_group_id}\"' --time-start $(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ) --time-end $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    invoke_cert_manager = var.enable_certificate_automation ? "fn invoke ${var.name_prefix}-certmgr certmgr" : "Certificate automation not enabled"
  }
}
