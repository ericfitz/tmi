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
  value       = "https://${oci_containerengine_cluster.tmi.endpoints[0].private_endpoint}"
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

# Load Balancer (provisioned by Kubernetes Service)
output "load_balancer_ip" {
  description = "Public IP address of the load balancer (provisioned by K8s Service)"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null
}

# Application URLs
output "http_url" {
  description = "HTTP URL for the application"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null
}

output "https_url" {
  description = "HTTPS URL for the application (if SSL configured)"
  sensitive   = true
  value       = var.ssl_certificate_pem != null && length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null
}

output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  sensitive   = true
  value = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? (
    var.ssl_certificate_pem != null ?
    "https://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" :
    "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}"
  ) : null
}

# Standard interface outputs for compatibility
output "load_balancer_dns" {
  description = "Load balancer DNS/IP (standard interface)"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}
