# Outputs for OCI Kubernetes (OKE) Module

# OKE Cluster
output "cluster_id" {
  description = "OCID of the OKE cluster"
  value       = oci_containerengine_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the OKE cluster"
  value       = oci_containerengine_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the OKE cluster"
  value       = var.oke_public_endpoint ? "https://${oci_containerengine_cluster.tmi.endpoints[0].public_endpoint}" : "https://${oci_containerengine_cluster.tmi.endpoints[0].private_endpoint}"
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the OKE cluster"
  value       = base64decode(yamldecode(data.oci_containerengine_cluster_kube_config.tmi.content)["clusters"][0]["cluster"]["certificate-authority-data"])
  sensitive   = true
}

output "kubeconfig" {
  description = "Kubeconfig content for kubectl access"
  value       = data.oci_containerengine_cluster_kube_config.tmi.content
  sensitive   = true
}

# Node Pool
output "node_pool_id" {
  description = "OCID of the managed node pool"
  value       = oci_containerengine_node_pool.tmi.id
}

# Load Balancer IP — from ingress LB (discovered via setup script) or legacy per-service LB
output "load_balancer_ip" {
  description = "Public IP of the load balancer. When using ingress, this is null (LB IP discovered post-deploy via kubectl)."
  value       = var.api_hostname != null ? null : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null)
}

# Application URLs
output "http_url" {
  description = "HTTP URL for the application"
  value       = var.api_hostname != null ? "http://${var.api_hostname}" : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null)
}

output "https_url" {
  description = "HTTPS URL for the application"
  sensitive   = true
  value       = var.api_hostname != null ? "https://${var.api_hostname}" : (var.ssl_certificate_pem != null && length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null)
}

output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  sensitive   = true
  value       = var.api_hostname != null ? "https://${var.api_hostname}" : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? (var.ssl_certificate_pem != null ? "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}") : null)
}

output "load_balancer_dns" {
  description = "Load balancer DNS/IP (standard interface)"
  value       = var.api_hostname != null ? var.api_hostname : (length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null)
}

# TMI-UX
output "tmi_ux_load_balancer_ip" {
  description = "IP of the TMI-UX load balancer (null when using ingress)"
  value       = var.api_hostname != null ? null : (var.tmi_ux_enabled && length(kubernetes_service_v1.tmi_ux) > 0 && length(kubernetes_service_v1.tmi_ux[0].status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_ux[0].status[0].load_balancer[0].ingress[0].ip : null)
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}

# Ingress
output "ingress_enabled" {
  description = "Whether ingress-based routing is enabled"
  value       = var.api_hostname != null
}

output "api_hostname" {
  description = "API hostname (when ingress is enabled)"
  value       = var.api_hostname
}

output "ux_hostname" {
  description = "UX hostname (when ingress is enabled)"
  value       = var.ux_hostname
}
