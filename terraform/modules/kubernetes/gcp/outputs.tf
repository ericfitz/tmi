# Outputs for GCP Kubernetes (GKE Autopilot) Module

# GKE Cluster
output "cluster_id" {
  description = "ID of the GKE cluster"
  value       = google_container_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the GKE cluster"
  value       = google_container_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the GKE cluster"
  value       = "https://${google_container_cluster.tmi.endpoint}"
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the GKE cluster"
  value       = google_container_cluster.tmi.master_auth[0].cluster_ca_certificate
  sensitive   = true
}

# Workload Identity
output "workload_identity_sa_email" {
  description = "Email of the GCP service account for Workload Identity"
  value       = google_service_account.tmi_workload.email
}

output "workload_identity_sa_name" {
  description = "Name of the GCP service account for Workload Identity"
  value       = google_service_account.tmi_workload.name
}

# Load Balancer (provisioned by Kubernetes Service)
output "load_balancer_ip" {
  description = "IP address of the load balancer (provisioned by K8s Service)"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip : null
}

# Application URLs
output "http_url" {
  description = "HTTP URL for the application"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null
}

output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  value       = length(kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress) > 0 ? "http://${kubernetes_service_v1.tmi_api.status[0].load_balancer[0].ingress[0].ip}" : null
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

# Client config for kubernetes provider
output "access_token" {
  description = "Access token for kubernetes provider authentication"
  value       = data.google_client_config.current.access_token
  sensitive   = true
}
