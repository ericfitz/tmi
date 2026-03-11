# Outputs for TMI AWS Production Deployment

# Application Access
output "server_url" {
  description = "HTTPS URL for the TMI API server"
  value       = module.kubernetes.server_url
}

output "ux_url" {
  description = "HTTPS URL for the TMI-UX frontend"
  value       = module.kubernetes.ux_url
}

output "load_balancer_hostname" {
  description = "Hostname of the ALB"
  value       = module.kubernetes.load_balancer_hostname
}

# DNS Records
output "dns_server_fqdn" {
  description = "FQDN of the TMI API server DNS record"
  value       = length(module.dns) > 0 ? module.dns[0].server_fqdn : null
}

output "dns_ux_fqdn" {
  description = "FQDN of the TMI-UX frontend DNS record"
  value       = length(module.dns) > 0 ? module.dns[0].ux_fqdn : null
}

# Network Information
output "vpc_id" {
  description = "ID of the VPC"
  value       = module.network.vpc_id
}

output "private_subnet_ids" {
  description = "IDs of the private subnets"
  value       = module.network.private_subnet_ids
}

# Database Information
output "database_id" {
  description = "ID of the RDS instance"
  value       = module.database.db_instance_id
}

output "database_endpoint" {
  description = "RDS connection endpoint"
  value       = module.database.db_instance_endpoint
  sensitive   = true
}

# EKS Cluster
output "eks_cluster_id" {
  description = "ID of the EKS cluster"
  value       = module.kubernetes.cluster_id
}

output "eks_cluster_endpoint" {
  description = "Kubernetes API endpoint"
  value       = module.kubernetes.cluster_endpoint
}

output "kubernetes_namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = module.kubernetes.namespace
}

# Secrets
output "kms_key_arn" {
  description = "ARN of the KMS key for secrets encryption"
  value       = module.secrets.kms_key_arn
}

output "secret_names" {
  description = "Map of secret names"
  value       = module.secrets.secret_names
  sensitive   = true
}

# Logging
output "app_log_group" {
  description = "Name of the CloudWatch Log Group for app logs"
  value       = module.logging.app_log_group_name
}

output "archive_bucket_name" {
  description = "Name of the log archive S3 bucket"
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

# Certificate (when enabled)
output "certificate_arn" {
  description = "ARN of the ACM certificate"
  value       = var.enable_certificate_automation ? module.certificates[0].certificate_arn : null
}

output "certificate_config" {
  description = "ACM certificate configuration"
  value       = var.enable_certificate_automation ? module.certificates[0].certificate_config : null
}

# Useful Commands
output "useful_commands" {
  description = "Useful commands for managing the deployment"
  value = {
    kubeconfig_setup   = module.kubernetes.kubeconfig_command
    kubectl_get_pods   = "kubectl get pods -n tmi"
    kubectl_logs_api   = "kubectl logs -n tmi -l app=tmi-api --tail=100"
    kubectl_logs_redis = "kubectl logs -n tmi -l app=tmi-redis --tail=100"
    logs_tail          = "aws logs tail /tmi/${var.name_prefix}/app --follow --region ${var.region}"
  }
}
