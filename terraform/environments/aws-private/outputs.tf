# Outputs for TMI AWS Private Environment

output "tmi_api_endpoint" {
  description = "URL to reach the TMI API (internal)"
  value       = module.kubernetes.service_endpoint
}

output "tmi_internal_url" {
  description = "Private URL for the TMI API"
  value       = module.kubernetes.service_endpoint
}

output "kubernetes_cluster_name" {
  description = "EKS cluster name for kubectl configuration"
  value       = module.kubernetes.cluster_name
}

output "kubernetes_config_command" {
  description = "Command to configure kubectl for this cluster"
  value       = module.kubernetes.kubernetes_config_command
}

output "database_host" {
  description = "PostgreSQL connection endpoint (internal)"
  value       = module.database.host
}

output "container_registry_url" {
  description = "ECR repository URL for pushing TMI server images"
  value       = aws_ecr_repository.tmi.repository_url
}

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = module.kubernetes.redis_endpoint
}

output "cloudwatch_log_group" {
  description = "CloudWatch Log Group for TMI logs"
  value       = module.logging.log_group_name
}

output "note" {
  description = "Post-deployment reminder"
  value       = "This is a private deployment. You must establish your own connectivity (VPN, bastion, private link) to reach the internal ALB and EKS API. The public EKS API endpoint has been disabled."
}
