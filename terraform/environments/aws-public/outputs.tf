# Outputs for TMI AWS Public Environment

output "tmi_api_endpoint" {
  description = "URL to reach the TMI API. Always null until the deploy overlay's Ingress is applied and the ALB is provisioned — see service_endpoint on the kubernetes module."
  value       = module.kubernetes.service_endpoint
}

output "tmi_external_url" {
  description = "Internet-accessible URL for the TMI API. Always null — see tmi_api_endpoint."
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

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = module.kubernetes.redis_endpoint
}

output "cloudwatch_log_group" {
  description = "CloudWatch Log Group for TMI logs"
  value       = module.logging.log_group_name
}

# ============================================================================
# Outputs consumed by later tasks (deploy script / kustomize overlay)
# ============================================================================

output "ecr_repository_urls" {
  description = "ECR repository URLs keyed by component (server, redis, extractor, chunkembed, controller)"
  value       = { for k, r in aws_ecr_repository.tmi : k => r.repository_url }
}

output "certificate_arn" {
  description = "ARN of the DNS-validated ACM certificate for domain_name"
  value       = module.certificates.certificate_arn
}

output "rds_endpoint" {
  description = "RDS PostgreSQL connection endpoint"
  value       = module.database.host
}

output "cluster_name" {
  description = "EKS cluster name"
  value       = module.kubernetes.cluster_name
}

output "namespace" {
  description = "Kubernetes namespace containing the bootstrap objects (ConfigMap, Secret, ServiceAccount) that the deploy overlay's workloads must target"
  value       = module.kubernetes.namespace
}
