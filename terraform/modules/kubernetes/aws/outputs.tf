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
  value       = aws_eks_cluster.tmi.certificate_authority[0].data
  sensitive   = true
}

output "cluster_oidc_issuer_url" {
  description = "OIDC issuer URL for the EKS cluster"
  value       = aws_eks_cluster.tmi.identity[0].oidc[0].issuer
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC provider for IRSA"
  value       = aws_iam_openid_connect_provider.eks.arn
}

# Node Group
output "node_group_id" {
  description = "ID of the managed node group"
  value       = aws_eks_node_group.tmi.id
}

output "node_role_arn" {
  description = "ARN of the EKS node IAM role"
  value       = aws_iam_role.eks_nodes.arn
}

# TMI Pod Role
output "tmi_pod_role_arn" {
  description = "ARN of the TMI pod IAM role (IRSA)"
  value       = aws_iam_role.tmi_pod.arn
}

# Kubernetes config command
output "kubernetes_config_command" {
  description = "Command to configure kubectl for this cluster"
  value       = "aws eks update-kubeconfig --region ${data.aws_region.current.name} --name ${aws_eks_cluster.tmi.name}"
}

# Namespace
output "namespace" {
  description = "Kubernetes namespace for TMI resources"
  value       = kubernetes_namespace_v1.tmi.metadata[0].name
}

# Standard interface outputs for multi-cloud compatibility
output "service_endpoint" {
  description = "Service endpoint URL (standard interface)"
  value       = "http://${kubernetes_ingress_v1.tmi_api.status[0].load_balancer[0].ingress[0].hostname}"
}

output "load_balancer_dns" {
  description = "Load balancer DNS name (standard interface)"
  value       = kubernetes_ingress_v1.tmi_api.status[0].load_balancer[0].ingress[0].hostname
}

output "redis_endpoint" {
  description = "Internal Redis service address"
  value       = "tmi-redis.tmi.svc.cluster.local:6379"
}
