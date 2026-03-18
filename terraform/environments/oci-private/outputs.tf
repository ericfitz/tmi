# Outputs for TMI OCI Private Deployment

# ---------------------------------------------------------------------------
# Standard Outputs (consistent across all templates per spec Section 7)
# ---------------------------------------------------------------------------
output "tmi_api_endpoint" {
  description = "URL to reach the TMI API (internal)"
  value       = module.kubernetes.service_endpoint
  sensitive   = true
}

output "tmi_internal_url" {
  description = "Internal URL for the TMI API (private — not internet-accessible)"
  value       = module.kubernetes.http_url
}

output "kubernetes_cluster_name" {
  description = "OKE cluster name for kubectl configuration"
  value       = module.kubernetes.cluster_name
}

output "kubernetes_config_command" {
  description = "Command to configure kubectl for this cluster"
  value       = "oci ce cluster create-kubeconfig --cluster-id ${module.kubernetes.cluster_id} --region ${var.region} --token-version 2.0.0 --profile ${var.oci_config_profile}"
}

output "database_host" {
  description = "Database connection endpoint (private)"
  value       = module.database.connection_string_high
  sensitive   = true
}

output "container_registry_url" {
  description = "OCIR registry URL for pushing TMI images"
  value       = "${var.region}.ocir.io/${data.oci_objectstorage_namespace.ns.namespace}/${var.name_prefix}"
}

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = "redis.tmi.svc.cluster.local:6379"
}

# ---------------------------------------------------------------------------
# Private Template Additional Outputs
# ---------------------------------------------------------------------------
output "note" {
  description = "Reminder for deployers"
  value       = "This is a private deployment. You must establish your own connectivity (VPN, bastion, private link, etc.) to reach the internal load balancer and K8s API."
}

# ---------------------------------------------------------------------------
# Additional Outputs
# ---------------------------------------------------------------------------
output "load_balancer_ip" {
  description = "Internal IP address of the load balancer"
  value       = module.kubernetes.load_balancer_ip
}

output "vcn_id" {
  description = "OCID of the VCN"
  value       = module.network.vcn_id
}

output "oke_cluster_id" {
  description = "OCID of the OKE cluster"
  value       = module.kubernetes.cluster_id
}

output "kubernetes_namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = module.kubernetes.namespace
}

output "database_id" {
  description = "OCID of the Autonomous Database"
  value       = module.database.autonomous_database_id
}

output "database_private_endpoint_ip" {
  description = "Private endpoint IP of the Autonomous Database"
  value       = module.database.private_endpoint_ip
}

output "vault_id" {
  description = "OCID of the Vault"
  value       = module.secrets.vault_id
}

output "log_group_id" {
  description = "OCID of the Log Group"
  value       = module.logging.log_group_id
}

# ---------------------------------------------------------------------------
# Post-Deployment Instructions
# ---------------------------------------------------------------------------
output "post_deploy_instructions" {
  description = "Steps to complete after terraform apply"
  value       = <<-EOT
    1. Establish connectivity to the private network:
       - Set up a VPN, bastion host, or private link to reach the VCN

    2. Configure kubectl (from within the private network):
       ${format("oci ce cluster create-kubeconfig --cluster-id %s --region %s --token-version 2.0.0 --profile %s", module.kubernetes.cluster_id, var.region, var.oci_config_profile)}

    3. Verify pods are running:
       kubectl get pods -n tmi

    4. Configure internal DNS:
       - Point your TMI hostname at the internal LB IP: ${module.kubernetes.load_balancer_ip != null ? module.kubernetes.load_balancer_ip : "<pending>"}

    5. Register an OAuth provider:
       - Set the callback URL at your IdP to: https://<your-internal-hostname>/oauth2/callback
       - Add client ID/secret via extra_env_vars and re-apply

    6. Configure WebSocket origins (if client hostname differs from API hostname):
       - Set WEBSOCKET_ALLOWED_ORIGINS in extra_env_vars

    7. (Recommended) Configure TLS:
       - Deploy a certificate (internal CA or provider-managed)
       - Configure the internal LB for HTTPS
  EOT
}
