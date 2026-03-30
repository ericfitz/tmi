# Outputs for TMI OCI Public Deployment

# ---------------------------------------------------------------------------
# Standard Outputs (consistent across all templates per spec Section 7)
# ---------------------------------------------------------------------------
output "tmi_api_endpoint" {
  description = "URL to reach the TMI API"
  value       = module.kubernetes.service_endpoint
  sensitive   = true
}

output "tmi_external_url" {
  description = "Internet-accessible URL for the TMI API"
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
  description = "Database connection endpoint (internal)"
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
# Additional Outputs
# ---------------------------------------------------------------------------
output "load_balancer_ip" {
  description = "Public IP address of the TMI API load balancer"
  value       = module.kubernetes.load_balancer_ip
}

output "tmi_ux_load_balancer_ip" {
  description = "Public IP address of the TMI-UX load balancer"
  value       = module.kubernetes.tmi_ux_load_balancer_ip
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
    1. Configure kubectl:
       ${format("oci ce cluster create-kubeconfig --cluster-id %s --region %s --token-version 2.0.0 --profile %s", module.kubernetes.cluster_id, var.region, var.oci_config_profile)}

    2. Verify pods are running:
       kubectl get pods -n tmi

    3. Test connectivity:
       curl http://${module.kubernetes.load_balancer_ip != null ? module.kubernetes.load_balancer_ip : "<pending>"}/

    4. Register an OAuth provider:
       - Set the callback URL at your IdP to: http://<LB_IP>/oauth2/callback
       - Add client ID/secret via extra_env_vars and re-apply

    5. (Optional) Configure DNS and TLS for a custom domain
  EOT
}
