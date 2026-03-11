# Outputs for AWS Kubernetes (EKS) Module

# EKS Cluster
output "cluster_id" {
  description = "ID of the EKS cluster"
  value       = aws_eks_cluster.tmi.id
}

output "cluster_name" {
  description = "Name of the EKS cluster"
  value       = aws_eks_cluster.tmi.name
}

output "cluster_endpoint" {
  description = "Kubernetes API endpoint of the EKS cluster"
  value       = aws_eks_cluster.tmi.endpoint
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate for the EKS cluster"
  value       = base64decode(aws_eks_cluster.tmi.certificate_authority[0].data)
  sensitive   = true
}

output "kubeconfig_command" {
  description = "AWS CLI command to update kubeconfig for kubectl access"
  value       = "aws eks update-kubeconfig --region ${var.aws_region} --name ${aws_eks_cluster.tmi.name}"
}

# Fargate Profile
output "fargate_profile_id" {
  description = "ID of the Fargate profile"
  value       = aws_eks_fargate_profile.tmi.id
}

# OIDC Provider
output "oidc_provider_arn" {
  description = "ARN of the OIDC provider for IRSA"
  value       = aws_iam_openid_connect_provider.eks.arn
}

output "oidc_provider_url" {
  description = "URL of the OIDC provider for IRSA"
  value       = aws_eks_cluster.tmi.identity[0].oidc[0].issuer
}

# IAM Roles
output "eks_cluster_role_arn" {
  description = "ARN of the IAM role for the EKS cluster"
  value       = aws_iam_role.eks_cluster.arn
}

output "fargate_role_arn" {
  description = "ARN of the IAM role for Fargate pod execution"
  value       = aws_iam_role.fargate.arn
}

# Load Balancer (provisioned by ALB Ingress Controller)
output "load_balancer_hostname" {
  description = "Hostname of the ALB (provisioned by Ingress)"
  value = (
    length(kubernetes_ingress_v1.tmi) > 0 &&
    length(kubernetes_ingress_v1.tmi[0].status[0].load_balancer[0].ingress) > 0
    ? kubernetes_ingress_v1.tmi[0].status[0].load_balancer[0].ingress[0].hostname
    : null
  )
}

# Application URLs
output "server_url" {
  description = "HTTPS URL for the TMI API server"
  value       = var.server_domain != null ? "https://${var.server_domain}" : null
}

output "ux_url" {
  description = "HTTPS URL for the TMI-UX frontend"
  value       = var.ux_domain != null && var.tmi_ux_enabled ? "https://${var.ux_domain}" : null
}

output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  value       = var.server_domain != null ? "https://${var.server_domain}" : null
}

# Standard interface outputs for compatibility with OCI module
output "load_balancer_dns" {
  description = "Load balancer DNS hostname (standard interface)"
  value = (
    length(kubernetes_ingress_v1.tmi) > 0 &&
    length(kubernetes_ingress_v1.tmi[0].status[0].load_balancer[0].ingress) > 0
    ? kubernetes_ingress_v1.tmi[0].status[0].load_balancer[0].ingress[0].hostname
    : null
  )
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}
