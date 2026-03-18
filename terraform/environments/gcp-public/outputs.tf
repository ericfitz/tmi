# Outputs for TMI GCP Public Deployment

# Standard outputs (Section 7 of design spec)

output "tmi_api_endpoint" {
  description = "URL to reach the TMI API"
  value       = module.kubernetes.http_url
}

output "tmi_external_url" {
  description = "Internet-accessible URL for TMI"
  value       = module.kubernetes.http_url
}

output "kubernetes_cluster_name" {
  description = "GKE cluster name for kubectl config"
  value       = module.kubernetes.cluster_name
}

output "kubernetes_config_command" {
  description = "Command to configure kubectl"
  value       = "gcloud container clusters get-credentials ${module.kubernetes.cluster_name} --region ${var.region} --project ${var.project_id}"
}

output "database_host" {
  description = "PostgreSQL connection endpoint (internal)"
  value       = module.database.database_host
}

output "container_registry_url" {
  description = "Artifact Registry URL for pushing container images"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.tmi.repository_id}"
}

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = "tmi-redis.tmi.svc.cluster.local:6379"
}

# Additional useful outputs

output "load_balancer_ip" {
  description = "External IP address of the load balancer"
  value       = module.kubernetes.load_balancer_ip
}

output "database_connection_name" {
  description = "Cloud SQL connection name (for Cloud SQL Proxy)"
  value       = module.database.connection_name
}

output "gke_cluster_id" {
  description = "ID of the GKE cluster"
  value       = module.kubernetes.cluster_id
}

output "kubernetes_namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = module.kubernetes.namespace
}

# Useful commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    kubeconfig_setup   = "gcloud container clusters get-credentials ${module.kubernetes.cluster_name} --region ${var.region} --project ${var.project_id}"
    kubectl_get_pods   = "kubectl get pods -n tmi"
    kubectl_logs_api   = "kubectl logs -n tmi -l app=tmi-api --tail=100"
    kubectl_logs_redis = "kubectl logs -n tmi -l app=tmi-redis --tail=100"
    push_tmi_image     = "docker tag tmi:latest ${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.tmi.repository_id}/tmi:latest && docker push ${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.tmi.repository_id}/tmi:latest"
  }
}
